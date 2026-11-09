import asyncio
import json

import psycopg
from psycopg.rows import dict_row
from pydantic import ValidationError

from ..schemas.contracts import Actor, Document, SearchHit, SearchResult


SEARCH_DB_TIMEOUT_SECONDS = 8
MAX_SEARCH_HITS = 3
IMPORT_DB_CONNECT_TIMEOUT_SECONDS = 5


class KnowledgeError(Exception):
    pass


class KnowledgeRepository:
    def __init__(self, dsn: str):
        self.dsn = dsn

    async def search(
        self, embedding: list[float], embedding_model: str, actor: Actor, min_similarity: float
    ) -> SearchResult:
        try:
            async with (
                asyncio.timeout(SEARCH_DB_TIMEOUT_SECONDS),
                await psycopg.AsyncConnection.connect(self.dsn, row_factory=dict_row) as db,
            ):
                cursor = await db.execute(
                    """
                    WITH accessible AS MATERIALIZED (
                        SELECT c.source_id AS id, d.title, d.url, c.body, c.embedding
                        FROM document_chunks AS c
                        JOIN documents AS d ON d.id = c.document_id
                        WHERE c.embedding_model = %s
                          AND EXISTS (
                              SELECT 1 FROM document_access
                              WHERE document_id = d.id AND principal_type = 'role' AND principal_id = %s
                          )
                          AND (
                              NOT EXISTS (
                                  SELECT 1 FROM document_access
                                  WHERE document_id = d.id AND principal_type = 'team'
                              )
                              OR EXISTS (
                                  SELECT 1 FROM document_access
                                  WHERE document_id = d.id AND principal_type = 'team' AND principal_id = %s
                              )
                          )
                    ), nearest AS (
                        SELECT id, title, url, body AS text, embedding <=> %s::vector AS distance
                        FROM accessible
                        ORDER BY distance, id
                        LIMIT %s
                    )
                    SELECT id, title, url, text
                    FROM nearest
                    WHERE distance <= %s
                    ORDER BY distance, id
                    """,
                    (
                        embedding_model,
                        actor.role,
                        actor.team,
                        json.dumps(embedding),
                        MAX_SEARCH_HITS,
                        1 - min_similarity,
                    ),
                )
                return SearchResult(hits=[SearchHit.model_validate(row) for row in await cursor.fetchall()])
        except (psycopg.Error, TimeoutError, ValidationError) as error:
            raise KnowledgeError("Не удалось выполнить поиск в базе знаний.") from error


    async def replace(
        self, documents: list[Document], vectors: list[list[float]], embedding_model: str
    ) -> None:
        rows = [
            (
                document.title,
                str(document.url) if document.url else None,
                document.id,
                document.text,
                embedding_model,
                json.dumps(vector),
                sorted(document.roles),
                sorted(document.teams),
            )
            for document, vector in zip(documents, vectors, strict=True)
        ]
        async with await psycopg.AsyncConnection.connect(
            self.dsn, connect_timeout=IMPORT_DB_CONNECT_TIMEOUT_SECONDS
        ) as db:
            # Serialize imports while allowing searches to read the previous snapshot until commit.
            await db.execute("LOCK TABLE documents IN SHARE ROW EXCLUSIVE MODE")
            await db.execute("DELETE FROM documents")
            await db.cursor().executemany(
                """
                WITH new_document AS (
                    INSERT INTO documents (title, url) VALUES (%s, %s)
                    RETURNING id
                ), new_chunk AS (
                    INSERT INTO document_chunks (document_id, source_id, body, embedding_model, embedding)
                    SELECT id, %s, %s, %s, %s::vector FROM new_document
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
