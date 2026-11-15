import argparse
import asyncio
import json
from time import perf_counter
from pathlib import Path

import httpx
from pydantic import TypeAdapter, ValidationError

from ..clients.embeddings import Embeddings
from ..clients.llm import ResponsesModel, ModelError
from ..core.diagnostics import UpstreamError
from ..core.config import Settings, resolve_embedding_profile
from ..repositories.knowledge import KnowledgeRepository
from ..schemas.contracts import Document
from .chunking import chunk_documents
from .contextualization import contextualize


IMPORT_EMBEDDING_TIMEOUT_SECONDS = 25
IMPORT_EMBEDDING_BATCH_SIZE = 32
MAX_IMPORT_BYTES = 4 * 1024 * 1024
MAX_IMPORT_DOCUMENTS = 1000


async def import_documents(source: Path, settings: Settings) -> dict:
    try:
        async with asyncio.timeout(settings.import_timeout_seconds):
            return await _import_documents(source, settings)
    except TimeoutError as error:
        raise ModelError("Import timed out; the index was not changed.", code="import_timeout", reason="timeout") from error


async def _import_documents(source: Path, settings: Settings) -> dict:
    started = perf_counter()
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
        raise ValueError("База знаний превышает 1000 документов.")
    if not documents:
        raise ValueError("Для импорта нужен хотя бы один документ.")
    if len({document.id for document in documents}) != len(documents):
        raise ValueError("ID документов должны быть уникальными.")
    chunks = chunk_documents(documents)
    if not settings.knowledge_db_dsn:
        raise ValueError("Укажи KNOWLEDGE_DB_DSN.")

    if not settings.knowledge_processing_approved:
        raise ValueError("Подтверди политику обработки данных через KNOWLEDGE_PROCESSING_APPROVED.")

    profile = resolve_embedding_profile(settings)
    context_stats = {"calls": 0, "cache_hits": 0, "input_tokens": 0, "output_tokens": 0, "usage_complete": True}
    vectors: list[list[float]] = []
    async with httpx.AsyncClient(timeout=IMPORT_EMBEDDING_TIMEOUT_SECONDS) as http:
        embeddings = Embeddings(http, profile)
        if settings.contextualization_enabled:
            if not settings.llm_model.strip():
                raise ValueError("Укажите LLM_MODEL для контекстуализации импорта.")
            model = ResponsesModel(http, settings.llm_base_url, settings.llm_api_key, settings.llm_model)
            context_stats = await contextualize(chunks, model, settings)
        for start in range(0, len(chunks), IMPORT_EMBEDDING_BATCH_SIZE):
            batch = chunks[start:start + IMPORT_EMBEDDING_BATCH_SIZE]
            vectors.extend(await embeddings.embed([chunk.embedding_text() for chunk in batch]))

    await KnowledgeRepository(settings.knowledge_db_dsn).replace(chunks, vectors, embeddings.model, embeddings.profile)
    return {
        "documents": len(documents), "chunks": len(chunks), "profile": embeddings.profile,
        "duration_seconds": round(perf_counter() - started, 3), "context": context_stats,
        "embedding": {"calls": embeddings.calls, "input_tokens": embeddings.input_tokens,
                      "usage_complete": embeddings.usage_complete},
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Контекстуализация, векторизация и атомарный импорт гибридного индекса.")
    parser.add_argument("source", type=Path)
    args = parser.parse_args()
    try:
        print(json.dumps(asyncio.run(import_documents(args.source, Settings.from_env())), ensure_ascii=False))
    except (RuntimeError, ValueError, OSError, UpstreamError) as error:
        parser.exit(1, f"{error}\n")
