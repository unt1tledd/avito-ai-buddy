import argparse
import asyncio
import json
from pathlib import Path

import httpx
import psycopg
from psycopg.rows import dict_row
from pydantic import Field, TypeAdapter, ValidationError

from .config import Settings
from .contracts import Actor, Identifier, SearchHit, SearchResult
from .embeddings import EmbeddingError, Embeddings


class Document(SearchHit):
    roles: set[Identifier] = Field(min_length=1)
    teams: set[Identifier] = Field(default_factory=set)
    keywords: list[str] = Field(default_factory=list)

    def embedding_text(self) -> str:
        return "\n".join([self.title, self.text, *self.keywords])


class KnowledgeError(Exception):
    pass


class Knowledge:
    def __init__(self, settings: Settings, embeddings: Embeddings):
        if not -1 <= settings.knowledge_min_similarity <= 1:
            raise ValueError("KNOWLEDGE_MIN_SIMILARITY must be between -1 and 1")
        self.dsn = settings.knowledge_db_dsn
        self.min_similarity = settings.knowledge_min_similarity
        self.embeddings = embeddings

    async def search(self, query: str, actor: Actor) -> SearchResult:
        try:
            [embedding] = await self.embeddings.embed([query])
        except EmbeddingError as error:
            raise KnowledgeError(str(error)) from error

        try:
            async with asyncio.timeout(8), await psycopg.AsyncConnection.connect(self.dsn, row_factory=dict_row) as db:
                cursor = await db.execute(
                    """
                    WITH accessible AS MATERIALIZED (
                        SELECT id, title, url, body, embedding
                        FROM ai_knowledge_chunks
                        WHERE embedding_model = %s AND vector_dims(embedding) = %s
                          AND %s = ANY(roles)
                          AND (cardinality(teams) = 0 OR %s = ANY(teams))
                    ), nearest AS (
                        SELECT id, title, url, body AS text, embedding <=> %s::vector AS distance
                        FROM accessible
                        ORDER BY distance, id
                        LIMIT 3
                    )
                    SELECT id, title, url, text
                    FROM nearest
                    WHERE distance <= %s
                    ORDER BY distance, id
                    """,
                    (
                        self.embeddings.model,
                        len(embedding),
                        actor.role,
                        actor.team,
                        json.dumps(embedding),
                        1 - self.min_similarity,
                    ),
                )
                return SearchResult(hits=[SearchHit.model_validate(row) for row in await cursor.fetchall()])
        except (psycopg.Error, TimeoutError, ValidationError) as error:
            raise KnowledgeError("Не удалось выполнить поиск в базе знаний.") from error


async def import_documents(source: Path, settings: Settings) -> None:
    documents = TypeAdapter(list[Document]).validate_json(source.read_bytes())
    if len({document.id for document in documents}) != len(documents):
        raise ValueError("ID фрагментов должны быть уникальными.")
    if not settings.knowledge_db_dsn:
        raise ValueError("Укажи KNOWLEDGE_DB_DSN.")

    vectors: list[list[float]] = []
    async with httpx.AsyncClient(timeout=25) as http:
        embeddings = Embeddings(http, settings)
        for start in range(0, len(documents), 32):
            batch = documents[start:start + 32]
            vectors.extend(await embeddings.embed([document.embedding_text() for document in batch]))

    if len({len(vector) for vector in vectors}) > 1:
        raise EmbeddingError("Сервис эмбеддингов вернул векторы разной размерности.")

    rows = [
        (
            document.id,
            document.title,
            str(document.url) if document.url else None,
            document.text,
            sorted(document.roles),
            sorted(document.teams),
            embeddings.model,
            json.dumps(vector),
        )
        for document, vector in zip(documents, vectors, strict=True)
    ]
    async with await psycopg.AsyncConnection.connect(settings.knowledge_db_dsn, connect_timeout=5) as db:
        # Serialize imports while allowing searches to read the previous snapshot until commit.
        await db.execute("LOCK TABLE ai_knowledge_chunks IN SHARE ROW EXCLUSIVE MODE")
        await db.execute("DELETE FROM ai_knowledge_chunks")
        await db.cursor().executemany(
            """
            INSERT INTO ai_knowledge_chunks (id, title, url, body, roles, teams, embedding_model, embedding)
            VALUES (%s, %s, %s, %s, %s, %s, %s, %s::vector)
            """,
            rows,
        )


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Векторизация и импорт фрагментов базы знаний в PostgreSQL.")
    parser.add_argument("source", type=Path)
    args = parser.parse_args()
    asyncio.run(import_documents(args.source, Settings.from_env()))
