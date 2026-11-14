import asyncio
import logging
from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest

from ai_service.clients.embeddings import EmbeddingError
from ai_service.core.config import Settings
from ai_service.core.diagnostics import logger
from ai_service.rag.retriever import Retriever
from ai_service.repositories.knowledge import KnowledgeError
from ai_service.schemas.contracts import SearchHit, SearchResult
from test_rag import ACTOR, PROFILE, VECTOR


HITS = SearchResult(hits=[SearchHit(id="vpn", title="VPN", text="Инструкция VPN")])


def retriever(**overrides):
    settings = Settings(**overrides)
    repository = SimpleNamespace(
        settings=settings, search=AsyncMock(return_value=HITS), search_lexical=AsyncMock(return_value=HITS),
    )
    embeddings = SimpleNamespace(profile=PROFILE, embed=AsyncMock(return_value=[VECTOR]))
    return Retriever(repository, embeddings, .3), repository, embeddings


@pytest.mark.parametrize("mode", ["hybrid", "vector"])
@pytest.mark.parametrize("reason", ["http_503", "http_429", "transport", "timeout", "invalid_dimensions"])
def test_embedding_errors_use_same_actor_query_and_profile_for_lexical(mode, reason):
    knowledge, repository, embeddings = retriever(retrieval_mode=mode)
    embeddings.embed.side_effect = EmbeddingError("provider failure", reason=reason)
    assert asyncio.run(knowledge.search("AUTH-403 VPN", ACTOR)) == HITS
    repository.search.assert_not_awaited()
    repository.search_lexical.assert_awaited_once_with("AUTH-403 VPN", PROFILE, ACTOR)


def test_embedding_timeout_cancels_upstream_and_runs_lexical():
    knowledge, repository, embeddings = retriever(search_embedding_timeout_seconds=.01)
    cancelled = []

    async def slow(_texts):
        try:
            await asyncio.Event().wait()
        finally:
            cancelled.append(True)

    embeddings.embed.side_effect = slow
    assert asyncio.run(knowledge.search("VPN", ACTOR)) == HITS
    assert cancelled == [True]
    repository.search.assert_not_awaited()
    repository.search_lexical.assert_awaited_once()


def test_healthy_embeddings_keep_existing_search_path():
    knowledge, repository, embeddings = retriever()
    assert asyncio.run(knowledge.search("VPN", ACTOR)) == HITS
    repository.search.assert_awaited_once_with(VECTOR, PROFILE, ACTOR, .3, query="VPN")
    repository.search_lexical.assert_not_awaited()


@pytest.mark.parametrize("fallback", [False, True])
def test_database_failure_is_not_replaced_by_empty_results_or_retried(fallback):
    knowledge, repository, embeddings = retriever()
    error = KnowledgeError("database unavailable", reason="connection")
    if fallback:
        embeddings.embed.side_effect = EmbeddingError("unavailable")
        repository.search_lexical.side_effect = error
    else:
        repository.search.side_effect = error
    with pytest.raises(KnowledgeError) as caught:
        asyncio.run(knowledge.search("VPN", ACTOR))
    assert caught.value is error
    assert repository.search.await_count + repository.search_lexical.await_count == 1


def test_empty_lexical_result_is_a_success():
    knowledge, repository, embeddings = retriever()
    embeddings.embed.side_effect = EmbeddingError("unavailable")
    repository.search_lexical.return_value = SearchResult(hits=[])
    assert asyncio.run(knowledge.search("unknown", ACTOR)).hits == []


def test_open_circuit_skips_provider_and_allows_one_recovery_probe(monkeypatch):
    now = [0.]
    monkeypatch.setattr("ai_service.rag.retriever.monotonic", lambda: now[0])
    knowledge, repository, embeddings = retriever(embedding_circuit_failure_threshold=2)

    async def run():
        embeddings.embed.side_effect = EmbeddingError("unavailable")
        for _ in range(3):
            await knowledge.search("VPN", ACTOR)
        assert embeddings.embed.await_count == 2
        now[0] = 31
        entered, release = asyncio.Event(), asyncio.Event()

        async def recovering(_texts):
            entered.set()
            await release.wait()
            return [VECTOR]

        embeddings.embed.side_effect = recovering
        probe = asyncio.create_task(knowledge.search("probe", ACTOR))
        await entered.wait()
        await knowledge.search("concurrent", ACTOR)
        assert embeddings.embed.await_count == 3
        release.set()
        await probe
        await knowledge.search("recovered", ACTOR)
        assert embeddings.embed.await_count == 4
        assert repository.search.await_count == 2
        assert repository.search_lexical.await_count == 4

    asyncio.run(run())


def test_failed_probe_reopens_circuit_and_success_resets_failure_streak(monkeypatch):
    now = [0.]
    monkeypatch.setattr("ai_service.rag.retriever.monotonic", lambda: now[0])
    knowledge, repository, embeddings = retriever(embedding_circuit_failure_threshold=2)

    async def run():
        embeddings.embed.side_effect = [EmbeddingError("fail"), [VECTOR], EmbeddingError("fail"),
                                       EmbeddingError("fail"), EmbeddingError("probe fail"), [VECTOR]]
        for _ in range(4):
            await knowledge.search("VPN", ACTOR)
        await knowledge.search("open", ACTOR)
        assert embeddings.embed.await_count == 4
        now[0] = 31
        await knowledge.search("failed probe", ACTOR)
        now[0] = 60
        await knowledge.search("still open", ACTOR)
        assert embeddings.embed.await_count == 5
        now[0] = 62
        await knowledge.search("successful probe", ACTOR)
        assert embeddings.embed.await_count == 6
        assert repository.search.await_count == 2

    asyncio.run(run())


def test_stale_success_cannot_close_circuit_opened_by_concurrent_failure(monkeypatch):
    now = [0.]
    monkeypatch.setattr("ai_service.rag.retriever.monotonic", lambda: now[0])
    knowledge, repository, embeddings = retriever(embedding_circuit_failure_threshold=1)

    async def run():
        entered, release = asyncio.Event(), asyncio.Event()

        async def upstream(texts):
            if texts == ["slow"]:
                entered.set()
                await release.wait()
                return [VECTOR]
            raise EmbeddingError("fail")

        embeddings.embed.side_effect = upstream
        slow = asyncio.create_task(knowledge.search("slow", ACTOR))
        await entered.wait()
        await knowledge.search("failure", ACTOR)
        release.set()
        await slow
        await knowledge.search("open", ACTOR)
        assert embeddings.embed.await_count == 2

    asyncio.run(run())


@pytest.mark.parametrize("probe", [False, True])
@pytest.mark.parametrize("outer_timeout", [False, True])
def test_caller_cancellation_never_starts_fallback_or_blocks_next_probe(monkeypatch, probe, outer_timeout):
    now = [0.]
    monkeypatch.setattr("ai_service.rag.retriever.monotonic", lambda: now[0])
    knowledge, repository, embeddings = retriever(embedding_circuit_failure_threshold=1)

    async def run():
        if probe:
            embeddings.embed.side_effect = EmbeddingError("fail")
            await knowledge.search("open circuit", ACTOR)
            now[0] = 31
            repository.search_lexical.reset_mock()
        entered = asyncio.Event()

        async def blocked(_texts):
            entered.set()
            await asyncio.Event().wait()

        embeddings.embed.side_effect = blocked
        if outer_timeout:
            with pytest.raises(TimeoutError):
                async with asyncio.timeout(.01):
                    await knowledge.search("cancelled", ACTOR)
        else:
            task = asyncio.create_task(knowledge.search("cancelled", ACTOR))
            await entered.wait()
            task.cancel()
            with pytest.raises(asyncio.CancelledError):
                await task
        repository.search.assert_not_awaited()
        repository.search_lexical.assert_not_awaited()
        embeddings.embed.side_effect = None
        assert await knowledge.search("next request", ACTOR) == HITS
        repository.search.assert_awaited_once()

    asyncio.run(run())


def test_fallback_logs_mode_cause_and_count_without_question(caplog):
    knowledge, repository, embeddings = retriever()
    embeddings.embed.side_effect = EmbeddingError("sensitive upstream body", reason="http_503")
    with caplog.at_level(logging.INFO, logger=logger.name):
        asyncio.run(knowledge.search("sensitive user question", ACTOR))
    completed = [r for r in caplog.records if "event=retrieval_completed" in r.getMessage()]
    assert len(completed) == 1
    assert (completed[0].retrieval_mode, completed[0].reason, completed[0].hit_count) == ("lexical", "http_503", 1)
    assert "sensitive" not in caplog.text
    assert PROFILE not in caplog.text
