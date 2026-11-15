"""Regressions for deployment defaults, crisis routing and bounded import recovery."""

import asyncio
import json
import os
from datetime import UTC, datetime, timedelta
from email.utils import format_datetime

import httpx
import pytest
from fastapi.testclient import TestClient
from pydantic import ValidationError

from ai_service.clients.embeddings import Embeddings
from ai_service.clients.llm import ModelError
from ai_service.core.config import Settings, resolve_embedding_profile
from ai_service.core.diagnostics import retry_after_delay
from ai_service.main import create_app
from ai_service.rag.import_documents import import_documents
from ai_service.rag.import_retry import retry_import_request
from ai_service.repositories.knowledge import KnowledgeRepository
from ai_service.schemas.contracts import SearchHit, SearchResult
from ai_service.schemas.support import IMMEDIATE_SAFETY_ANSWER, needs_safety_fallback
from test_chat import EMBEDDING, QUESTION, ScriptedModel, ask, call, settings  # noqa: F401


HIT = SearchHit(id="vpn-guide", title="VPN", text="Настройте VPN по инструкции.")


@pytest.mark.parametrize("provider,model,dimensions", [
    ("https://api.openai.com/v1", "text-embedding-3-small", 1536),
    ("https://api.siliconflow.cn/v1", "Qwen/Qwen3-Embedding-8B", 1024),
])
def test_empty_optional_dimensions_preserve_provider_default(monkeypatch, provider, model, dimensions):
    monkeypatch.setattr(os, "environ", {
        "EMBEDDING_DIMENSIONS": "", "EMBEDDING_BASE_URL": provider,
        "EMBEDDING_MODEL": model, "EMBEDDING_API_KEY": "test-key",
    })
    settings = Settings.from_env()
    assert settings.embedding_dimensions is None
    assert resolve_embedding_profile(settings).dimensions == dimensions


def test_nonempty_invalid_dimensions_still_fail(monkeypatch):
    monkeypatch.setattr(os, "environ", {"EMBEDDING_DIMENSIONS": "not-a-number"})
    with pytest.raises(ValidationError, match="EMBEDDING_DIMENSIONS"):
        Settings.from_env()


@pytest.mark.parametrize("message", [
    "Как безопасно настроить VPN?", "Как выполнить kill зависшего процесса?",
    "Вредно ли выключать VPN?", "Я не собираюсь причинять себе вред, как настроить VPN?",
])
@pytest.mark.parametrize("status", [400, 429, 503])
def test_technical_words_do_not_select_crisis_on_model_error(settings, monkeypatch, message, status):
    queries = []

    async def lexical(self, query, profile, actor):
        queries.append(query)
        return SearchResult(hits=[HIT])

    monkeypatch.setattr(KnowledgeRepository, "search_lexical", lexical)
    model = ScriptedModel(ModelError("Provider unavailable", status_code=status, reason=f"http_{status}"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, message)
    assert response.status_code == (502 if status == 400 else 200)
    assert response.json().get("answer") != IMMEDIATE_SAFETY_ANSWER
    assert queries == ([] if status == 400 else [message])


@pytest.mark.parametrize("message", [
    "Я сейчас хочу причинить себе вред", "Я не хочу жить", "Меня сейчас бьют",
    "Мне угрожают", "I want to kill myself",
    "Я не хочу помощи, хочу причинить себе вред",
    "I don't want help, I want to kill myself",
])
def test_explicit_danger_still_has_safe_failure_route(message):
    assert needs_safety_fallback(message)


@pytest.mark.parametrize("phase", ["model", "embedding", "search", "history"])
def test_entire_primary_path_leaves_time_for_recovery_with_legacy_timeout(settings, monkeypatch, phase):
    # Scale the real 25/30-second boundary down; a legacy 60-second setting
    # and a hung tool must not consume the time reserved for lexical recovery.
    monkeypatch.setattr("ai_service.core.config.MAX_CHAT_TIMEOUT_SECONDS", .2)
    cancelled = []

    async def slow(*args, **kwargs):
        try:
            await asyncio.Event().wait()
        finally:
            cancelled.append(phase)

    async def lexical(*args, **kwargs):
        await asyncio.sleep(.005)
        return SearchResult(hits=[HIT])

    model = ScriptedModel(call("find_instruction", query="VPN"))
    history = []
    if phase == "model":
        model.respond = slow
    elif phase == "embedding":
        monkeypatch.setattr(Embeddings, "embed", slow)
    elif phase == "search":
        monkeypatch.setattr(KnowledgeRepository, "search", slow)
    else:
        monkeypatch.setattr(KnowledgeRepository, "accessible_source_ids", slow)
        history = [{"role": "assistant", "content": "Инструкция [source:vpn-guide]", "source_ids": ["vpn-guide"]}]
    monkeypatch.setattr(KnowledgeRepository, "search_lexical", lexical)
    configured = settings.model_copy(update={"chat_timeout_seconds": 60, "llm_timeout_seconds": 60})
    with TestClient(create_app(configured, model=model)) as client:
        response = ask(client, QUESTION, history=history)
    assert response.status_code == 200, response.text
    assert response.json()["sources"][0]["id"] == HIT.id
    assert cancelled == [phase]


@pytest.mark.parametrize("value,expected", [(None, None), ("", None), ("bad", None),
    ("nan", None), ("inf", None), ("-1", 0), ("0", 0), ("3", 3)])
def test_retry_after_is_validated(value, expected):
    assert retry_after_delay(value) == expected


def test_retry_after_accepts_http_date():
    future = format_datetime(datetime.now(UTC) + timedelta(seconds=60), usegmt=True)
    assert 58 < retry_after_delay(future) <= 60
    assert retry_after_delay("Wed, 01 Jan 2020 00:00:00 GMT") == 0


@pytest.mark.parametrize("status,attempts", [(429, 3), (500, 3), (502, 3), (503, 3),
    (504, 3), (400, 1), (401, 1), (403, 1), (501, 1)])
def test_retry_count_is_bounded_and_permanent_errors_fail_immediately(monkeypatch, status, attempts):
    calls, delays = [], []

    async def request():
        calls.append(True)
        raise ModelError("Provider unavailable", status_code=status, reason=f"http_{status}", retry_after_seconds=3)

    async def wait(delay):
        delays.append(delay)

    monkeypatch.setattr("ai_service.rag.import_retry.sleep", wait)
    with pytest.raises(ModelError):
        asyncio.run(retry_import_request(request))
    assert len(calls) == attempts
    assert len(delays) == attempts - 1
    assert all(delay >= 3 for delay in delays)


def test_import_deadline_cancels_retry_after_wait():
    calls = []

    async def request():
        calls.append(True)
        raise ModelError("Limited", status_code=429, retry_after_seconds=60)

    async def run():
        async with asyncio.timeout(.01):
            await retry_import_request(request)

    with pytest.raises(TimeoutError):
        asyncio.run(run())
    assert calls == [True]


@pytest.mark.parametrize("exhausted", [False, True])
def test_import_retries_context_and_vectors_before_single_index_write(tmp_path, monkeypatch, exhausted):
    source = tmp_path / "documents.json"
    source.write_text(json.dumps([{"id": "vpn", "title": "VPN", "text": "VPN setup", "roles": ["intern"]}]))
    configured = Settings(
        knowledge_db_dsn="unused", knowledge_processing_approved=True,
        embedding_api_key="test", llm_api_key="test", llm_model="test",
        context_cache_path=str(tmp_path / "cache.sqlite"),
    )
    calls = {"context": 0, "embedding": 0, "replace": 0}
    delays = []

    def handler(request):
        if request.url.path.endswith("/responses"):
            calls["context"] += 1
            if calls["context"] == 1 or exhausted:
                return httpx.Response(429, headers={"Retry-After": "3"}, json={"error": "private provider data"})
            return httpx.Response(200, json={"output": [{"type": "message", "content": [
                {"type": "output_text", "text": "VPN instructions"},
            ]}]})
        assert request.url.path.endswith("/embeddings")
        calls["embedding"] += 1
        if calls["embedding"] == 1:
            return httpx.Response(503)
        return httpx.Response(200, json={"data": [{"index": 0, "embedding": EMBEDDING}]})

    async_client = httpx.AsyncClient
    monkeypatch.setattr(httpx, "AsyncClient", lambda **kwargs: async_client(transport=httpx.MockTransport(handler), **kwargs))

    async def wait(delay):
        delays.append(delay)

    async def replace(self, chunks, vectors, model, profile):
        assert calls == {"context": 2, "embedding": 2, "replace": 0}
        assert chunks[0].generated_context == "VPN instructions"
        assert vectors == [EMBEDDING]
        calls["replace"] += 1

    monkeypatch.setattr("ai_service.rag.import_retry.sleep", wait)
    monkeypatch.setattr(KnowledgeRepository, "replace", replace)
    if exhausted:
        with pytest.raises(ModelError, match="index was not changed"):
            asyncio.run(import_documents(source, configured))
        assert calls == {"context": 3, "embedding": 0, "replace": 0}
        assert len(delays) == 2
    else:
        result = asyncio.run(import_documents(source, configured))
        assert result["chunks"] == 1
        assert calls == {"context": 2, "embedding": 2, "replace": 1}
        assert delays[0] >= 3
        # Retrying the import reuses the successful context rather than paying
        # for it again; embedding preparation still precedes the index write.
        calls["embedding"] = 0
        calls["replace"] = 0
        again = asyncio.run(import_documents(source, configured))
        assert again["context"]["cache_hits"] == 1
        assert calls == {"context": 2, "embedding": 2, "replace": 1}
