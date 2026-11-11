import argparse
import asyncio
from pathlib import Path

import httpx
from pydantic import TypeAdapter, ValidationError

from ..clients.embeddings import Embeddings
from ..core.diagnostics import UpstreamError
from ..core.config import Settings
from ..repositories.knowledge import KnowledgeRepository
from ..schemas.contracts import Document


IMPORT_EMBEDDING_TIMEOUT_SECONDS = 25
IMPORT_EMBEDDING_BATCH_SIZE = 32
MAX_IMPORT_BYTES = 4 * 1024 * 1024
MAX_IMPORT_DOCUMENTS = 1000


async def import_documents(source: Path, settings: Settings) -> None:
    with source.open("rb") as stream:
        raw = stream.read(MAX_IMPORT_BYTES + 1)
    if len(raw) > MAX_IMPORT_BYTES:
        raise ValueError("Файл базы знаний превышает 4 МиБ.")
    try:
        documents = TypeAdapter(list[Document]).validate_json(raw)
    except ValidationError as error:
        # The CLI reports locations and error codes without dumping corpus contents.
        issues = [
            (item["loc"][:1], item["type"])
            for item in error.errors(include_input=False)[:5]
        ]
        raise ValueError(f"Некорректный файл базы знаний. Ошибки: {issues}") from None
    if len(documents) > MAX_IMPORT_DOCUMENTS:
        raise ValueError("База знаний превышает 1000 фрагментов.")
    if not documents:
        raise ValueError("Для импорта нужен хотя бы один фрагмент.")
    if len({document.id for document in documents}) != len(documents):
        raise ValueError("ID фрагментов должны быть уникальными.")
    if not settings.knowledge_db_dsn:
        raise ValueError("Укажи KNOWLEDGE_DB_DSN.")

    if not settings.knowledge_processing_approved:
        raise ValueError("Подтверди политику обработки данных через KNOWLEDGE_PROCESSING_APPROVED.")

    vectors: list[list[float]] = []
    async with httpx.AsyncClient(timeout=IMPORT_EMBEDDING_TIMEOUT_SECONDS) as http:
        embeddings = Embeddings(http, settings)
        for start in range(0, len(documents), IMPORT_EMBEDDING_BATCH_SIZE):
            batch = documents[start:start + IMPORT_EMBEDDING_BATCH_SIZE]
            vectors.extend(await embeddings.embed([document.embedding_text() for document in batch]))

    await KnowledgeRepository(settings.knowledge_db_dsn).replace(documents, vectors, embeddings.model, embeddings.profile)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Векторизация и импорт фрагментов базы знаний в PostgreSQL.")
    parser.add_argument("source", type=Path)
    args = parser.parse_args()
    try:
        asyncio.run(import_documents(args.source, Settings.from_env()))
    except (RuntimeError, ValueError, OSError, UpstreamError) as error:
        parser.exit(1, f"{error}\n")
