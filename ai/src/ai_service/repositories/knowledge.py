import asyncio
import json
from collections.abc import Sequence

import psycopg
from psycopg_pool import AsyncConnectionPool, PoolTimeout, TooManyRequests
from pydantic import ValidationError

from ..core.config import Settings
from ..core.text_preparation import TEXT_PREPARATION_VERSION
from ..core.diagnostics import UpstreamError, operation
from ..schemas.contracts import Actor, Document, DocumentChunk, MAX_CHUNK_TEXT, SearchHit, SearchResult


SEARCH_DB_TIMEOUT_SECONDS = 3
MAX_SEARCH_HITS = 3
IMPORT_DB_CONNECT_TIMEOUT_SECONDS = 5
IMPORT_DB_TIMEOUT_SECONDS = 30

DOCUMENT_ACCESS_SQL = """
    EXISTS (
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
"""

ACCESSIBLE_SOURCES_SQL = f"""
    SELECT c.source_id AS id
    FROM document_chunks AS c
    JOIN documents AS d ON d.id = c.document_id
    WHERE c.embedding_profile = %(profile)s
      AND c.text_preparation_version = %(preparation)s
      AND c.source_id = ANY(%(source_ids)s::text[])
      AND {DOCUMENT_ACCESS_SQL}
"""

SEARCH_SQL = f"""
    WITH index_state AS (
        SELECT count(*) > 0 AS present,
               coalesce(bool_and(text_preparation_version IS NOT DISTINCT FROM %(preparation)s
                                 AND search_text <> ''), false) AS prepared
        FROM document_chunks WHERE embedding_profile = %(profile)s
    ), eligible AS MATERIALIZED (
        SELECT c.id AS chunk_id, c.source_id AS id, d.title, d.url, c.body AS text,
               d.access_policies, c.embedding, c.token_count
        FROM document_chunks AS c JOIN documents AS d ON d.id = c.document_id
        WHERE c.embedding_profile = %(profile)s
          AND c.text_preparation_version = %(preparation)s
          AND {DOCUMENT_ACCESS_SQL}
    ), corpus AS (
        SELECT count(*)::float8 AS n, avg(token_count)::float8 AS avgdl FROM eligible
    ), query_terms AS (
        SELECT term FROM knowledge_terms(%(query)s)
    ), matches AS (
        SELECT e.chunk_id, e.id, e.token_count, p.tf, corpus.n, corpus.avgdl,
               count(*) OVER (PARTITION BY p.term)::float8 AS df
        FROM query_terms q JOIN chunk_terms p ON p.term = q.term
        JOIN eligible e ON e.chunk_id = p.chunk_id CROSS JOIN corpus
    ), bm25_scores AS (
        SELECT id, sum(
            ln(1 + (n - df + 0.5) / (df + 0.5)) *
            (tf * (%(bm25_k1)s + 1)) /
            (tf + %(bm25_k1)s * (1 - %(bm25_b)s + %(bm25_b)s * token_count / nullif(avgdl, 0)))
        ) AS score
        FROM matches GROUP BY id
    ), lexical AS (
        SELECT id, score FROM bm25_scores WHERE score > 0
        ORDER BY score DESC, id COLLATE "C" LIMIT %(candidates)s
    ), semantic AS (
        SELECT id, embedding <=> %(vector)s::vector AS distance
        FROM eligible
        WHERE embedding <=> %(vector)s::vector <= %(max_distance)s
        ORDER BY distance, id COLLATE "C" LIMIT %(candidates)s
    ), votes AS (
        SELECT id, %(bm25_weight)s::float8 / (%(rrf_k)s + row_number() OVER (ORDER BY score DESC, id COLLATE "C")) AS score
        FROM lexical
        UNION ALL
        SELECT id, %(vector_weight)s::float8 / (%(rrf_k)s + row_number() OVER (ORDER BY distance, id COLLATE "C")) AS score
        FROM semantic
    ), fused AS (
        SELECT id, sum(score) AS score FROM votes GROUP BY id
        ORDER BY score DESC, id COLLATE "C" LIMIT %(limit)s
    )
    SELECT index_state.present, index_state.prepared, e.id, e.title, e.url, e.text, e.access_policies
    FROM index_state LEFT JOIN (fused JOIN eligible e USING (id)) ON true
    ORDER BY fused.score DESC, e.id COLLATE "C"
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
                raise KnowledgeError("Не удалось проверить доступ к источникам истории.", reason=db_failure(error)) from error

    async def search(
        self, embedding: list[float], profile: str, actor: Actor, min_similarity: float, *, query: str = ""
    ) -> SearchResult:
        assert self.pool is not None
        with operation("db.search"):
            try:
                async with asyncio.timeout(SEARCH_DB_TIMEOUT_SECONDS), self.pool.connection() as db:
                    cursor = await db.execute(SEARCH_SQL, {
                        "profile": profile, "role": actor.role, "team": actor.team,
                        "vector": json.dumps(embedding), "limit": self.settings.search_top_k,
                        "preparation": TEXT_PREPARATION_VERSION,
                        "query": query if self.settings.retrieval_mode == "hybrid" else "",
                        "candidates": self.settings.search_candidates,
                        "rrf_k": self.settings.rrf_k,
                        "bm25_k1": self.settings.bm25_k1, "bm25_b": self.settings.bm25_b,
                        "bm25_weight": self.settings.rrf_bm25_weight,
                        "vector_weight": self.settings.rrf_vector_weight,
                        "max_distance": 1 - min_similarity,
                    })
                    rows = await cursor.fetchall()
                if not rows[0]["present"]:
                    raise missing_profile()
                if not rows[0]["prepared"]:
                    raise KnowledgeError("Требуется полный повторный импорт базы знаний.", reason="reindex_required")
                return SearchResult(hits=[
                    SearchHit.model_validate({key: row[key] for key in ("id", "title", "url", "text", "access_policies")})
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
                        SELECT vector_dims(c.embedding), c.source_id, c.body, d.title, d.url, d.access_policies, a.principal_id
                        FROM document_chunks AS c
                        JOIN documents AS d ON d.id = c.document_id
                        JOIN document_access AS a ON a.document_id = d.id
                        WHERE a.principal_type = 'role' LIMIT 0
                    """)
                    cursor = await db.execute("""
                        SELECT count(*) AS chunks,
                               coalesce(bool_or(char_length(body) > %s), false) AS oversized,
                               coalesce(bool_and(text_preparation_version IS NOT DISTINCT FROM %s
                                                 AND search_text <> ''), false) AS prepared
                        FROM document_chunks WHERE embedding_profile = %s
                    """, (MAX_CHUNK_TEXT, TEXT_PREPARATION_VERSION, profile))
                    state = await cursor.fetchone()
                    if not state["chunks"]:
                        raise missing_profile()
                    if state["oversized"] or not state["prepared"]:
                        raise KnowledgeError(
                            "Версия подготовки базы знаний устарела или содержит длинные фрагменты; выполните повторный импорт.",
                            reason="reindex_required",
                        )
            except (psycopg.Error, PoolTimeout, TimeoutError) as error:
                raise KnowledgeError("База знаний не готова.", reason=db_failure(error)) from error

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
                    INSERT INTO documents (title, url, access_policies, keywords, original_body) VALUES (%s, %s, %s::jsonb, %s, %s)
                    RETURNING id
                ), new_chunk AS (
                    INSERT INTO document_chunks (document_id, source_id, body, generated_context, search_text,
                                                 text_preparation_version, embedding_model, embedding_profile, embedding)
                    SELECT id, %s, %s, %s, %s, %s, %s, %s, %s::vector FROM new_document
                    RETURNING id, search_text
                ), postings AS (
                    INSERT INTO chunk_terms (chunk_id, term, tf)
                    SELECT id, terms.term, terms.tf FROM new_chunk
                    CROSS JOIN LATERAL knowledge_terms(search_text) AS terms
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

            await db.execute("""
                UPDATE document_chunks c SET token_count = stats.length
                FROM (SELECT chunk_id, sum(tf)::integer AS length FROM chunk_terms GROUP BY chunk_id) stats
                WHERE c.id = stats.chunk_id
            """)
