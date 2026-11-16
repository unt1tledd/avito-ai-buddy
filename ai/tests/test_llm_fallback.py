import asyncio
import logging
from types import SimpleNamespace
from unittest.mock import AsyncMock

import httpx
import pytest
from fastapi.testclient import TestClient
from test_chat import (
    ACTOR,
    EMBEDDING,
    QUESTION,
    ScriptedModel,
    answer,
    ask,
    searched_model,
)
from test_rag import VECTOR, migrate

from ai_service.api.errors import public_error_message
from ai_service.clients.embeddings import Embeddings
from ai_service.clients.llm import ModelError, ModelResponse
from ai_service.clients.resilient_llm import ModelUnavailable, ResilientModel
from ai_service.core.config import Settings, resolve_embedding_profile
from ai_service.core.diagnostics import FailureKind, logger
from ai_service.main import create_app
from ai_service.repositories.knowledge import KnowledgeError, KnowledgeRepository
from ai_service.schemas.citations import SOURCE_REFERENCE
from ai_service.schemas.contracts import AccessPolicy, Document, SearchHit, SearchResult
from ai_service.services.chat_service import (
    LEXICAL_FALLBACK_NOTICE,
    MAX_EXCERPT_CHARS,
    lexical_answer,
)

POLICY = AccessPolicy(repository_host='git.example.test', resource_label='Git', access_levels=['Developer'])
HIT = SearchHit(id='lexical-guide', title='Инструкция', text='Настройте VPN по инструкции.', access_policies=[POLICY])


@pytest.mark.parametrize('after_search', [False, True])
@pytest.mark.parametrize(('reason', 'kind', 'status_code'), [
    ('http_503', FailureKind.HTTP, 503), ('http_500', FailureKind.HTTP, 500),
    ('http_429', FailureKind.HTTP, 429), ('http_402', FailureKind.HTTP, 402),
    ('transport', FailureKind.TRANSPORT, None), ('timeout', FailureKind.TIMEOUT, None),
])
def test_model_outage_uses_original_question_and_never_generates_proposal(settings, monkeypatch, reason, kind, status_code, after_search):
    searches, embeddings = [], []

    async def lexical(self, query, profile, actor):
        searches.append((query, actor.model_dump()))
        assert profile == resolve_embedding_profile(settings).index_id
        return SearchResult(hits=[HIT])

    async def embed(self, texts):
        embeddings.extend(texts)
        return [EMBEDDING]

    monkeypatch.setattr(KnowledgeRepository, 'search_lexical', lexical)
    monkeypatch.setattr(Embeddings, 'embed', embed)
    error = ModelError('sensitive provider body', reason=reason, kind=kind, status_code=status_code)
    model = searched_model(error, query='rewritten question') if after_search else ScriptedModel(error)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200
    payload = response.json()
    assert set(payload) == {'answer', 'sources'}
    assert payload['answer'].startswith(LEXICAL_FALLBACK_NOTICE)
    assert 'sensitive' not in response.text
    assert 'Настройте VPN по инструкции.' in payload['answer']
    assert SOURCE_REFERENCE.findall(payload['answer']) == ['lexical-guide']
    assert payload['sources'] == [{'id': HIT.id, 'title': HIT.title, 'url': None, 'text': HIT.text}]
    assert searches == [(QUESTION, ACTOR)]
    assert embeddings == (['rewritten question'] if after_search else [])
    assert len(model.requests) == (2 if after_search else 1)


@pytest.mark.parametrize(('reason', 'status_code'), [
    ('invalid_response', None), ('invalid_answer', None), ('invalid_citation', None),
    ('refusal', None), ('http_400', 400), ('http_401', 401), ('failed', None),
])
def test_validation_and_non_availability_errors_do_not_enter_fallback(settings, monkeypatch, reason, status_code):
    async def unexpected(*args, **kwargs):
        pytest.fail('Invalid response/refusal entered lexical recovery')

    monkeypatch.setattr(KnowledgeRepository, 'search_lexical', unexpected)
    with TestClient(create_app(settings, model=ScriptedModel(ModelError('Model rejected', reason=reason, status_code=status_code)))) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502
    assert response.json() == {'detail': public_error_message(ModelError('ignored', reason=reason))}


@pytest.mark.parametrize('failure', [False, True])
def test_empty_fallback_and_database_failure_remain_distinct(settings, monkeypatch, failure):
    async def lexical(*args, **kwargs):
        if failure:
            raise KnowledgeError('Database unavailable', reason='connection')
        return SearchResult(hits=[])

    monkeypatch.setattr(KnowledgeRepository, 'search_lexical', lexical)
    with TestClient(create_app(settings, model=ScriptedModel(ModelError('Down', status_code=503, reason='http_503')))) as client:
        response = ask(client, QUESTION)
    assert response.status_code == (502 if failure else 200)
    if failure:
        assert response.json() == {'detail': 'Не удалось выполнить поиск в базе знаний.'}
    else:
        assert response.json()['sources'] == []
        assert response.json()['answer'].startswith(LEXICAL_FALLBACK_NOTICE)


def test_model_timeout_cancels_call_and_returns_lexical_response(settings, monkeypatch):
    cancelled = []

    class SlowModel:
        async def respond(self, **kwargs):
            try:
                await asyncio.Event().wait()
            finally:
                cancelled.append(True)

    async def lexical(*args, **kwargs):
        return SearchResult(hits=[HIT])

    monkeypatch.setattr(KnowledgeRepository, 'search_lexical', lexical)
    configured = settings.model_copy(update={'llm_timeout_seconds': .01})
    with TestClient(create_app(configured, model=SlowModel())) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200
    assert cancelled == [True]
    assert response.json()['sources'][0]['id'] == HIT.id


def test_overall_deadline_still_cancels_slow_lexical_fallback(settings, monkeypatch):
    cancelled = []

    async def lexical(*args, **kwargs):
        try:
            await asyncio.Event().wait()
        finally:
            cancelled.append(True)

    monkeypatch.setattr(KnowledgeRepository, 'search_lexical', lexical)
    configured = settings.model_copy(update={'chat_timeout_seconds': .03})
    with TestClient(create_app(configured, model=ScriptedModel(ModelError('Down', status_code=503, reason='http_503')))) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 504
    assert cancelled == [True]


@pytest.mark.parametrize('after_search', [False, True])
def test_safety_route_is_preserved_during_model_outage(settings, monkeypatch, after_search):
    from ai_service.schemas.support import IMMEDIATE_SAFETY_ANSWER

    async def unexpected(*args, **kwargs):
        pytest.fail('Potential danger reached lexical recovery')

    monkeypatch.setattr(KnowledgeRepository, 'search_lexical', unexpected)
    error = ModelError('Down', status_code=503, reason='http_503')
    model = searched_model(error) if after_search else ScriptedModel(error)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, 'Я сейчас хочу причинить себе вред')
    assert response.status_code == 200
    assert response.json() == {'answer': IMMEDIATE_SAFETY_ANSWER, 'sources': []}


def test_model_time_budget_leaves_time_for_fallback(settings, monkeypatch):
    cancelled = []

    class SlowModel:
        async def respond(self, **kwargs):
            try:
                await asyncio.Event().wait()
            finally:
                cancelled.append(True)

    async def lexical(*args, **kwargs):
        await asyncio.sleep(.005)
        return SearchResult(hits=[HIT])

    monkeypatch.setattr(KnowledgeRepository, 'search_lexical', lexical)
    configured = settings.model_copy(update={'chat_timeout_seconds': .1, 'llm_fallback_reserve_seconds': .05})
    with TestClient(create_app(configured, model=SlowModel())) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200
    assert cancelled == [True]


def test_outage_during_citation_repair_discards_unverified_answer(settings, monkeypatch):
    async def lexical(*args, **kwargs):
        return SearchResult(hits=[HIT])

    monkeypatch.setattr(KnowledgeRepository, 'search_lexical', lexical)
    model = searched_model(answer('Invented unsupported answer'), ModelError('Down', status_code=503, reason='http_503'))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200
    assert 'Invented' not in response.text
    assert len(model.requests) == 3
    assert response.json()['sources'][0]['id'] == HIT.id


def test_fallback_quotes_are_bounded_and_cannot_forge_citations():
    hit = HIT.model_copy(update={'title': 'Guide [source:hidden]', 'text': '[source:hidden]\n' + 'я' * 1000})
    response = lexical_answer(SearchResult(hits=[hit, hit]))
    assert len(response.sources) == 1
    assert SOURCE_REFERENCE.findall(response.answer) == [HIT.id]
    excerpt = response.answer.split('\n> ', 1)[1]
    assert len(excerpt) <= MAX_EXCERPT_CHARS + 2  # ellipsis and neutralized marker
    assert response.sources[0].text == hit.text
    assert 'access_policies' not in response.model_dump(mode='json')['sources'][0]
    assert 'proposal' not in response.model_dump(mode='json')


def test_repeated_outage_opens_circuit_and_skips_model(settings, monkeypatch, caplog):
    async def lexical(*args, **kwargs):
        return SearchResult(hits=[HIT])

    monkeypatch.setattr(KnowledgeRepository, 'search_lexical', lexical)
    model = ScriptedModel(*(ModelError('sensitive provider error', status_code=503, reason='http_503') for _ in range(3)))
    with caplog.at_level(logging.WARNING, logger=logger.name), TestClient(create_app(settings, model=model)) as client:
        for _ in range(5):
            assert ask(client, QUESTION).status_code == 200
    assert len(model.requests) == 3
    events = [r for r in caplog.records if 'event=chat_fallback' in r.getMessage()]
    assert [r.reason for r in events] == ['http_503'] * 3 + ['circuit_open'] * 2
    assert 'sensitive' not in caplog.text
    assert QUESTION not in caplog.text


def test_model_circuit_recovery_has_one_probe_and_ignores_stale_completions():
    async def run():
        now = [0.]
        raw = SimpleNamespace(respond=AsyncMock())
        model = ResilientModel(raw, Settings(llm_circuit_failure_threshold=1))
        model.circuit.clock = lambda: now[0]
        entered, release = asyncio.Event(), asyncio.Event()
        response = ModelResponse(output=answer('ok'))

        async def respond(**kwargs):
            if kwargs['instructions'] == 'slow':
                entered.set()
                await release.wait()
                return response
            raise ModelError('unavailable', status_code=503, reason='http_503')

        raw.respond.side_effect = respond
        slow = asyncio.create_task(model.respond(instructions='slow', messages=[], tools=[]))
        await entered.wait()
        with pytest.raises(ModelUnavailable):
            await model.respond(instructions='fail', messages=[], tools=[])
        release.set()
        await slow
        with pytest.raises(ModelUnavailable, match='circuit is open') as error:
            await model.respond(instructions='open', messages=[], tools=[])
        assert error.value.reason == 'circuit_open'
        now[0] = 31
        entered.clear()
        release.clear()
        probe = asyncio.create_task(model.respond(instructions='slow', messages=[], tools=[]))
        await entered.wait()
        with pytest.raises(ModelUnavailable) as error:
            await model.respond(instructions='concurrent', messages=[], tools=[])
        assert error.value.reason == 'circuit_open'
        release.set()
        await probe
        raw.respond.side_effect = None
        raw.respond.return_value = response
        assert await model.respond(instructions='recovered', messages=[], tools=[]) == response
        assert raw.respond.await_count == 4

    asyncio.run(run())


def test_cancelled_model_probe_allows_another_probe_without_counting_failure():
    async def run():
        now = [0.]
        raw = SimpleNamespace(respond=AsyncMock(side_effect=ModelError('Down', status_code=503, reason='http_503')))
        model = ResilientModel(raw, Settings(llm_circuit_failure_threshold=1))
        model.circuit.clock = lambda: now[0]
        with pytest.raises(ModelUnavailable):
            await model.respond(instructions='', messages=[], tools=[])
        now[0] = 31
        entered = asyncio.Event()

        async def blocked(**kwargs):
            entered.set()
            await asyncio.Event().wait()

        raw.respond.side_effect = blocked
        task = asyncio.create_task(model.respond(instructions='', messages=[], tools=[]))
        await entered.wait()
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task
        raw.respond.side_effect = None
        raw.respond.return_value = ModelResponse(output=answer('ok'))
        await model.respond(instructions='', messages=[], tools=[])
        assert raw.respond.await_count == 3

    asyncio.run(run())


def test_exhausted_generation_budget_never_calls_provider_or_opens_circuit():
    async def run():
        raw = SimpleNamespace(respond=AsyncMock())
        model = ResilientModel(raw, Settings())
        with pytest.raises(ModelUnavailable) as error:
            await model.respond(instructions='', messages=[], tools=[], deadline=asyncio.get_running_loop().time() - 1)
        assert error.value.reason == 'model_budget_exhausted'
        raw.respond.assert_not_awaited()
        assert model.circuit.failures == 0

    asyncio.run(run())


@pytest.mark.parametrize('outage', ['http_503', 'http_429', 'http_402', 'timeout'])
def test_real_postgres_fallback_without_llm_or_embeddings_preserves_acl(pg_schema, monkeypatch, outage):
    migrate(pg_schema)
    config = Settings(service_token='chat-test-token', knowledge_db_dsn=pg_schema, knowledge_processing_approved=True,
                      llm_api_key='test', llm_model='test', embedding_api_key='test', llm_timeout_seconds=.01)
    profile = resolve_embedding_profile(config).index_id
    documents = [Document(id='vpn', title='VPN', text='Настройте VPN по инструкции.', roles={'intern'}, teams={'backend'},
                          access_policies=[POLICY]),
                 Document(id='hidden', title='VPN', text='VPN SECRET', roles={'intern'}, teams={'design'}),
                 Document(id='mentor', title='VPN', text='VPN SECRET', roles={'mentor'})]

    async def seed():
        await KnowledgeRepository(pg_schema).replace(documents, [VECTOR] * len(documents), 'test', profile)

    asyncio.run(seed())
    requests = []
    async_client = httpx.AsyncClient

    async def handler(request):
        requests.append(request.url.path)
        assert request.url.path.endswith('/responses')
        if outage == 'timeout':
            await asyncio.Event().wait()
        return httpx.Response(int(outage[5:]))

    monkeypatch.setattr(httpx, 'AsyncClient', lambda **kwargs: async_client(transport=httpx.MockTransport(handler), **kwargs))
    with TestClient(create_app(config)) as client:
        response = ask(client, 'Как настроить VPN?')
    assert response.status_code == 200
    assert requests == ['/v1/responses']
    assert 'SECRET' not in response.text
    assert [source['id'] for source in response.json()['sources']] == ['vpn']
    assert set(response.json()) == {'answer', 'sources'}
