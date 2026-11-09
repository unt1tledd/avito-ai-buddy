import argparse
import asyncio
from pathlib import Path

import httpx
from pydantic import TypeAdapter

from ..clients.embeddings import Embeddings
from ..core.config import Settings
from ..repositories.knowledge import KnowledgeRepository
from ..schemas.contracts import Document


IMPORT_EMBEDDING_TIMEOUT_SECONDS = 25
IMPORT_EMBEDDING_BATCH_SIZE = 32


async def import_documents(source: Path, settings: Settings) -> None:
    documents = TypeAdapter(list[Document]).validate_json(source.read_bytes())
    if not documents:
        raise ValueError("Для импорта нужен хотя бы один фрагмент.")
    if len({document.id for document in documents}) != len(documents):
        raise ValueError("ID фрагментов должны быть уникальными.")
    if not settings.knowledge_db_dsn:
        raise ValueError("Укажи KNOWLEDGE_DB_DSN.")

    vectors: list[list[float]] = []
    async with httpx.AsyncClient(timeout=IMPORT_EMBEDDING_TIMEOUT_SECONDS) as http:
        embeddings = Embeddings(http, settings)
        for start in range(0, len(documents), IMPORT_EMBEDDING_BATCH_SIZE):
            batch = documents[start:start + IMPORT_EMBEDDING_BATCH_SIZE]
            vectors.extend(await embeddings.embed([document.embedding_text() for document in batch]))

    await KnowledgeRepository(settings.knowledge_db_dsn).replace(documents, vectors, embeddings.model)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Векторизация и импорт фрагментов базы знаний в PostgreSQL.")
    parser.add_argument("source", type=Path)
    args = parser.parse_args()
    asyncio.run(import_documents(args.source, Settings.from_env()))
