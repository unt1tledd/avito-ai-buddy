"""Import-only context generation; content-addressed cache is independent of ACL."""

import asyncio
import hashlib
import json
import os
import sqlite3
from pathlib import Path

from ..clients.llm import LanguageModel, ModelError
from ..core.config import Settings
from ..core.diagnostics import UpstreamError
from ..core.text_preparation import CONTEXT_PROMPT, MAX_GENERATED_CONTEXT
from ..repositories.queries import (
    CONTEXT_CACHE_CREATE_SQL,
    CONTEXT_CACHE_GET_SQL,
    CONTEXT_CACHE_PUT_SQL,
)
from ..schemas.contracts import DocumentChunk
from .import_retry import retry_import_request


class ContextCache:
    def __init__(self, path: str):
        self.path = Path(path)
        self.db = None

    def __enter__(self):
        self.path.parent.mkdir(parents=True, exist_ok=True)
        # Cache contains derived knowledge: owner-only permissions, no raw prompts.
        descriptor = os.open(self.path, os.O_CREAT | os.O_RDWR, 0o600)
        os.fchmod(descriptor, 0o600)
        os.close(descriptor)
        self.db = sqlite3.connect(self.path, timeout=5)
        self.db.execute(CONTEXT_CACHE_CREATE_SQL)
        self.db.commit()
        return self

    def __exit__(self, *args):
        self.db.close()

    def get(self, key: str) -> str | None:
        row = self.db.execute(CONTEXT_CACHE_GET_SQL, (key,)).fetchone()
        if row is None:
            return None
        if not row[0].strip() or len(row[0]) > MAX_GENERATED_CONTEXT:
            raise ValueError("Повреждён кеш контекстуализации; удалите его и повторите импорт.")
        return row[0]

    def put(self, key: str, context: str) -> None:
        self.db.execute(CONTEXT_CACHE_PUT_SQL, (key, context))
        self.db.commit()


async def contextualize(chunks: list[DocumentChunk], model: LanguageModel, settings: Settings) -> dict:
    try:
        return await _contextualize(chunks, model, settings)
    except sqlite3.Error as error:
        raise ModelError("Context cache is unavailable; the index was not changed.", reason="cache_failed") from error


async def _contextualize(chunks: list[DocumentChunk], model: LanguageModel, settings: Settings) -> dict:
    stats = {"calls": 0, "cache_hits": 0, "input_tokens": 0, "output_tokens": 0, "usage_complete": True}
    semaphore = asyncio.Semaphore(settings.context_concurrency)
    start_lock = asyncio.Lock()
    next_start = 0.0
    grouped = {}
    # Chunk boundaries and IDs are unchanged. Position comes from the original
    # document group, not from a substring search (which is ambiguous on repeats).
    documents = {}
    for chunk in chunks:
        documents.setdefault((chunk.original_document_id, chunk.title, chunk.original_document), []).append(chunk)
    for siblings in documents.values():
        for part, chunk in enumerate(siblings, 1):
            payload = json.dumps({
                "title": chunk.title, "keywords": chunk.keywords, "document": chunk.original_document,
                "chunk": chunk.text, "part": part, "parts": len(siblings),
            }, ensure_ascii=False, sort_keys=True)
            key = hashlib.sha256(json.dumps([
                settings.llm_base_url.rstrip('/'), settings.llm_model, settings.context_prompt_version,
                CONTEXT_PROMPT, payload,
            ], ensure_ascii=False).encode()).hexdigest()
            grouped.setdefault(key, (payload, []))[1].append(chunk)

    with ContextCache(settings.context_cache_path) as cache:
        async def generate(key, payload, targets):
            cached = cache.get(key)
            if cached is not None:
                stats["cache_hits"] += len(targets)
                for chunk in targets:
                    chunk.generated_context = cached
                return
            async with semaphore:
                async def request_context():
                    nonlocal next_start
                    # Retries obey the same pacing and concurrency as first attempts.
                    async with start_lock:
                        loop = asyncio.get_running_loop()
                        await asyncio.sleep(max(0, next_start - loop.time()))
                        next_start = loop.time() + settings.context_min_interval_seconds
                    try:
                        async with asyncio.timeout(settings.context_timeout_seconds):
                            return await model.respond(
                                instructions=CONTEXT_PROMPT,
                                messages=[{"role": "user", "content": payload}], tools=[],
                            )
                    except TimeoutError as error:
                        raise ModelError(
                            "Contextualization timed out; the index was not changed.", code="context_timeout", reason="timeout",
                        ) from error

                response = await retry_import_request(request_context)
                context = response.text
                if response.calls or response.refused or response.status != "completed" or not context or len(context) > MAX_GENERATED_CONTEXT:
                    raise ModelError("Invalid chunk context; the index was not changed.", reason="invalid_context")
                stats["calls"] += 1
                for name in ("input_tokens", "output_tokens"):
                    value = (response.usage or {}).get(name)
                    if not isinstance(value, int) or value < 0:
                        stats["usage_complete"] = False
                    else:
                        stats[name] += value
                cache.put(key, context)
                for chunk in targets:
                    chunk.generated_context = context

        # TaskGroup cancels and awaits siblings on failure, before any index write.
        try:
            async with asyncio.TaskGroup() as tasks:
                for key, (payload, targets) in grouped.items():
                    tasks.create_task(generate(key, payload, targets))
        except ExceptionGroup as errors:
            # Keep corpus/prompts out of CLI diagnostics.
            first = errors.exceptions[0]
            reason = first.reason if isinstance(first, UpstreamError) else "context_failed"
            raise ModelError(
                "Contextualization failed; the index was not changed.", code="context_failed", reason=reason,
            ) from errors
    return stats
