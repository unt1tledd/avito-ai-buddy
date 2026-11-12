import asyncio
import json
from collections.abc import Sequence

import psycopg
from psycopg_pool import AsyncConnectionPool, PoolTimeout, TooManyRequests
from pydantic import ValidationError

from ..core.diagnostics import UpstreamError, operation
from ..schemas.contracts import Actor, Document, DocumentChunk, MAX_CHUNK_TEXT, SearchHit, SearchResult


SEARCH_DB_TIMEOUT_SECONDS = 3
MAX_SEARCH_HITS = 3
IMPORT_DB_CONNECT_TIMEOUT_SECONDS = 5
IMPORT_DB_TIMEOUT_SECONDS = 30

SEARCH_SQL = """
    SELECT profile.present, nearest.id, nearest.title, nearest.url, nearest.text
    FROM (SELECT EXISTS (
        SELECT 1 FROM document_chunks WHERE embedding_profile = %(profile)s
    ) AS present) AS profile
    LEFT JOIN LATERAL (
        SELECT c.source_id AS id, d.title, d.url, c.body AS text,
               c.embedding <=> %(vector)s::vector AS distance
        FROM document_chunks AS c
        JOIN documents AS d ON d.id = c.document_id
        WHERE c.embedding_profile = %(profile)s
          AND EXISTS (
              SELECT 1 FROM document_access
              WHERE document_id = d.id AND principal_type = 'role' AND principal_id = %(role)s
          )
          AND (
              NOT EXISTS (
                  SELECT 1 FROM document_access
                  WHERE document_id = d.id AND principal_type = 'team'
              )
              OR EXISTS (
                  SELECT 1 FROM document_access
                  WHERE document_id = d.id AND principal_type = 'team' AND principal_id = %(team)s
              )
          )
        ORDER BY distance, c.source_id COLLATE "C"
        LIMIT %(limit)s
    ) AS nearest ON nearest.distance <= %(max_distance)s
    ORDER BY nearest.distance, nearest.id COLLATE "C"
"""


class KnowledgeError(UpstreamError):
    pass


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
    return KnowledgeError("База знаний не проиндексирована для активного профиля.", reason="profile_missing")


class KnowledgeRepository:
    def __init__(self, dsn: str, pool: AsyncConnectionPool | None = None):
        self.dsn = dsn
        self.pool = pool

    async def search(
        self, embedding: list[float], profile: str, actor: Actor, min_similarity: float
    ) -> SearchResult:
        assert self.pool is not None
        with operation("db.search"):
            try:
                async with asyncio.timeout(SEARCH_DB_TIMEOUT_SECONDS), self.pool.connection() as db:
                    cursor = await db.execute(SEARCH_SQL, {
                        "profile": profile, "role": actor.role, "team": actor.team,
                        "vector": json.dumps(embedding), "limit": MAX_SEARCH_HITS,
                        "max_distance": 1 - min_similarity,
                    })
                    rows = await cursor.fetchall()
                if not rows[0]["present"]:
                    raise missing_profile()
                return SearchResult(hits=[
                    SearchHit.model_validate({key: row[key] for key in ("id", "title", "url", "text")})
                    for row in rows if row["id"] is not None
                ])
            except (psycopg.Error, PoolTimeout, TimeoutError, ValidationError) as error:
                raise KnowledgeError("Не удалось выполнить поиск в базе знаний.", reason=db_failure(error)) from error

    async def check_ready(self, profile: str) -> None:
        assert self.pool is not None
        with operation("db.readiness"):
            try:
                async with asyncio.timeout(SEARCH_DB_TIMEOUT_SECONDS), self.pool.connection() as db:
                    await db.execute("""
                        SELECT vector_dims(c.embedding), c.source_id, c.body, d.title, d.url, a.principal_id
                        FROM document_chunks AS c
                        JOIN documents AS d ON d.id = c.document_id
                        JOIN document_access AS a ON a.document_id = d.id
                        WHERE a.principal_type = 'role' LIMIT 0
                    """)
                    cursor = await db.execute("""
                        SELECT count(*) AS chunks,
                               coalesce(bool_or(char_length(body) > %s), false) AS oversized
                        FROM document_chunks WHERE embedding_profile = %s
                    """, (MAX_CHUNK_TEXT, profile))
                    state = await cursor.fetchone()
                    if not state["chunks"]:
                        raise missing_profile()
                    if state["oversized"]:
                        raise KnowledgeError(
                            "База знаний содержит длинные фрагменты; выполните повторный импорт.",
                            reason="reindex_required",
                        )
            except (psycopg.Error, PoolTimeout, TimeoutError) as error:
                raise KnowledgeError("База знаний не готова.", reason=db_failure(error)) from error

    async def replace(
        self, documents: Sequence[Document], vectors: list[list[float]], model: str, profile: str
    ) -> None:
        # Vectors must already correspond to bounded chunks. Never split after embedding.
        chunks = [DocumentChunk.model_validate(document.model_dump()) for document in documents]
        rows = [
            (
                document.title,
                str(document.url) if document.url else None,
                document.id,
                document.text,
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
                raise KnowledgeError("Не удалось заменить базу знаний.", reason=db_failure(error)) from error

    async def _replace(self, rows: list[tuple]) -> None:
        async with (
            asyncio.timeout(IMPORT_DB_TIMEOUT_SECONDS),
            await psycopg.AsyncConnection.connect(self.dsn, connect_timeout=IMPORT_DB_CONNECT_TIMEOUT_SECONDS) as db,
        ):
            # Searches see the previous snapshot until commit. Concurrent replacements are serialized.
            await db.execute("LOCK TABLE documents IN SHARE ROW EXCLUSIVE MODE")
            await db.execute("DELETE FROM documents")
            await db.cursor().executemany(
                """
                WITH new_document AS (
                    INSERT INTO documents (title, url) VALUES (%s, %s)
                    RETURNING id
                ), new_chunk AS (
                    INSERT INTO document_chunks (document_id, source_id, body, embedding_model, embedding_profile, embedding)
                    SELECT id, %s, %s, %s, %s, %s::vector FROM new_document
                )
                INSERT INTO document_access (document_id, principal_type, principal_id)
                SELECT d.id, p.principal_type, p.principal_id
                FROM new_document AS d
                CROSS JOIN (
                    SELECT 'role' AS principal_type, unnest(%s::text[]) AS principal_id
                    UNION ALL
                    SELECT 'team', unnest(%s::text[])
                ) AS p
                """,
                rows,
            )
