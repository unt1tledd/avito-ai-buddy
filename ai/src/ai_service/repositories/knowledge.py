import asyncio
import json
from collections.abc import Sequence

import psycopg
from psycopg_pool import AsyncConnectionPool, PoolTimeout, TooManyRequests
from pydantic import ValidationError

from ..core.config import Settings
from ..core.diagnostics import UpstreamError, operation
from ..core.text_preparation import TEXT_PREPARATION_VERSION
from ..schemas.contracts import (
    MAX_CHUNK_TEXT,
    Actor,
    Document,
    DocumentChunk,
    SearchHit,
    SearchResult,
)
from .queries import (
    ACCESSIBLE_SOURCES_SQL,
    DELETE_DOCUMENTS_SQL,
    INSERT_DOCUMENT_SQL,
    LEXICAL_SEARCH_SQL,
    LOCK_DOCUMENTS_SQL,
    READINESS_SCHEMA_SQL,
    READINESS_STATE_SQL,
    SEARCH_SQL,
    UPDATE_TOKEN_COUNTS_SQL,
)

SEARCH_DB_TIMEOUT_SECONDS = 3
MAX_SEARCH_HITS = 3
IMPORT_DB_CONNECT_TIMEOUT_SECONDS = 5
IMPORT_DB_TIMEOUT_SECONDS = 30


class KnowledgeError(UpstreamError):
    domain = "knowledge"


def db_failure(error: Exception) -> str:
    if isinstance(error, TooManyRequests):
        return "pool_saturated"
    if isinstance(error, (PoolTimeout, TimeoutError, psycopg.errors.QueryCanceled)):
        return "timeout"
    if isinstance(error, (psycopg.errors.UndefinedTable, psycopg.errors.UndefinedColumn, psycopg.errors.UndefinedFunction)):
        return "schema_missing"
    if isinstance(error, ValidationError):
        return "invalid_document"
    if isinstance(error, psycopg.OperationalError):
        return "connection"
    return "sql"


def missing_profile() -> KnowledgeError:
    return KnowledgeError("Knowledge index is missing for the active profile.", reason="profile_missing")


class KnowledgeRepository:
    def __init__(self, dsn: str, pool: AsyncConnectionPool | None = None, settings: Settings | None = None):
        self.dsn = dsn
        self.pool = pool
        self.settings = settings or Settings()

    async def accessible_source_ids(self, source_ids: set[str], profile: str, actor: Actor) -> set[str]:
        if not source_ids:
            return set()
        assert self.pool is not None
        with operation("db.history_access"):
            try:
                async with asyncio.timeout(SEARCH_DB_TIMEOUT_SECONDS), self.pool.connection() as db:
                    cursor = await db.execute(ACCESSIBLE_SOURCES_SQL, {
                        "source_ids": sorted(source_ids), "profile": profile, "role": actor.role, "team": actor.team,
                        "preparation": TEXT_PREPARATION_VERSION,
                    })
                    return {row["id"] for row in await cursor.fetchall()}
            except (psycopg.Error, PoolTimeout, TimeoutError) as error:
                raise KnowledgeError(
                    "Failed to check history source access.", code="history_access_failed", reason=db_failure(error),
                ) from error

    async def search(
        self, embedding: list[float], profile: str, actor: Actor, min_similarity: float, *, query: str = ""
    ) -> SearchResult:
        return await self._search(SEARCH_SQL, profile, actor, {
            "vector": json.dumps(embedding),
            "query": query if self.settings.retrieval_mode == "hybrid" else "",
            "rrf_k": self.settings.rrf_k,
            "bm25_weight": self.settings.rrf_bm25_weight,
            "vector_weight": self.settings.rrf_vector_weight,
            "max_distance": 1 - min_similarity,
        })

    async def search_lexical(self, query: str, profile: str, actor: Actor) -> SearchResult:
        # No query vector or cosine operator; keep the active corpus and ACL checks.
        return await self._search(LEXICAL_SEARCH_SQL, profile, actor, {"query": query})

    async def _search(self, statement: str, profile: str, actor: Actor, parameters: dict) -> SearchResult:
        assert self.pool is not None
        with operation("db.search"):
            try:
                async with asyncio.timeout(SEARCH_DB_TIMEOUT_SECONDS), self.pool.connection() as db:
                    cursor = await db.execute(statement, {
                        "profile": profile, "role": actor.role, "team": actor.team,
                        "limit": self.settings.search_top_k,
                        "preparation": TEXT_PREPARATION_VERSION,
                        "candidates": self.settings.search_candidates,
                        "bm25_k1": self.settings.bm25_k1, "bm25_b": self.settings.bm25_b,
                        **parameters,
                    })
                    rows = await cursor.fetchall()
                if not rows[0]["present"]:
                    raise missing_profile()
                if not rows[0]["prepared"]:
                    raise KnowledgeError("Knowledge index requires a complete reimport.", reason="reindex_required")
                return SearchResult(hits=[
                    SearchHit.model_validate({key: row[key] for key in ("id", "title", "url", "text", "access_policies")})
                    for row in rows if row["id"] is not None
                ])
            except (psycopg.Error, PoolTimeout, TimeoutError, ValidationError) as error:
                raise KnowledgeError(
                    "Knowledge search failed.", code="search_failed", reason=db_failure(error),
                ) from error

    async def check_ready(self, profile: str) -> None:
        assert self.pool is not None
        with operation("db.readiness"):
            try:
                async with asyncio.timeout(SEARCH_DB_TIMEOUT_SECONDS), self.pool.connection() as db:
                    await db.execute(READINESS_SCHEMA_SQL)
                    cursor = await db.execute(READINESS_STATE_SQL, (MAX_CHUNK_TEXT, TEXT_PREPARATION_VERSION, profile))
                    state = await cursor.fetchone()
                    if not state["chunks"]:
                        raise missing_profile()
                    if state["oversized"] or not state["prepared"]:
                        raise KnowledgeError(
                            "Knowledge index is outdated or contains oversized chunks; reimport is required.",
                            code="outdated_index", reason="reindex_required",
                        )
            except (psycopg.Error, PoolTimeout, TimeoutError) as error:
                raise KnowledgeError("Knowledge index is not ready.", code="not_ready", reason=db_failure(error)) from error

    async def replace(
        self, documents: Sequence[Document], vectors: list[list[float]], model: str, profile: str
    ) -> None:
        # Vectors must already correspond to bounded chunks. Never split after embedding.
        chunks = [document if isinstance(document, DocumentChunk) else DocumentChunk.model_validate(document.model_dump())
                  for document in documents]
        rows = [
            (
                document.title,
                str(document.url) if document.url else None,
                json.dumps([policy.model_dump(mode="json") for policy in document.access_policies]),
                document.keywords,
                document.original_document or document.text,
                document.id,
                document.text,
                document.generated_context,
                document.embedding_text(),
                TEXT_PREPARATION_VERSION,
                model,
                profile,
                json.dumps(vector),
                sorted(document.roles),
                sorted(document.teams),
            )
            for document, vector in zip(chunks, vectors, strict=True)
        ]
        with operation("db.import"):
            try:
                await self._replace(rows)
            except (psycopg.Error, TimeoutError) as error:
                raise KnowledgeError(
                    "Failed to replace the knowledge index.", code="import_failed", reason=db_failure(error),
                ) from error

    async def _replace(self, rows: list[tuple]) -> None:
        async with (
            asyncio.timeout(IMPORT_DB_TIMEOUT_SECONDS),
            await psycopg.AsyncConnection.connect(self.dsn, connect_timeout=IMPORT_DB_CONNECT_TIMEOUT_SECONDS) as db,
        ):
            # Searches see the previous snapshot until commit. Concurrent replacements are serialized.
            await db.execute(LOCK_DOCUMENTS_SQL)
            await db.execute(DELETE_DOCUMENTS_SQL)
            await db.cursor().executemany(
                INSERT_DOCUMENT_SQL,
                rows,
            )

            await db.execute(UPDATE_TOKEN_COUNTS_SQL)
