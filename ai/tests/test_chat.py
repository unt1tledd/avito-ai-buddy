"""Scripted model tests verify orchestration and validation, not LLM semantic accuracy."""

import asyncio
import json
import logging

import httpx
import psycopg
import pytest
from fastapi.testclient import TestClient

from ai_service.clients.embeddings import EmbeddingError, Embeddings
from ai_service.clients.llm import ModelError, ModelResponse
from ai_service.core.config import EMBEDDING_DIMENSIONS, Settings
from ai_service.core.diagnostics import logger
from ai_service.main import create_app
from ai_service.rag.retriever import Retriever
from ai_service.repositories.knowledge import KnowledgeRepository
from conversation_examples import CLARIFICATION_QUESTIONS, DEFAULT_CLARIFICATION
from ai_service.schemas.citations import SOURCE_REFERENCE
from ai_service.schemas.contracts import SearchHit, SearchResult


AUTH = {"Authorization": "Bearer chat-test-token"}
ACTOR = {"user_id": "42", "team": "backend", "role": "intern"}
QUESTION = "Как настроить окружение?"
EMBEDDING = [1.0] + [0.0] * (EMBEDDING_DIMENSIONS - 1)


class ScriptedModel:
    def __init__(self, *outputs):
        self.outputs = iter(outputs)
        self.requests = []

    async def respond(self, *, instructions, messages, tools):
        self.requests.append((instructions, list(messages), tools))
        output = next(self.outputs)
        if isinstance(output, Exception):
            raise output
        return ModelResponse(output=output)


class ContextFillingModel(ScriptedModel):
    """Leave room for small tool results regardless of the system prompt size."""

    async def respond(self, *, instructions, messages, tools):
        from ai_service.agents.agent import MAX_CONTEXT_BYTES, json_size

        response = await super().respond(instructions=instructions, messages=messages, tools=tools)
        if len(self.requests) == 1:
            items = [item.model_dump(mode="json", exclude_unset=True) for item in response.output]
            from importlib.resources import files
            from ai_service.agents.agent import Agent
            final_prompt = Agent(None, None, files("ai_service.agents").joinpath("prompts", "system.txt").read_text()).prompt
            used = json_size([final_prompt, messages + items, tools])
            assert used + 2 * 1024 < MAX_CONTEXT_BYTES
            response.output[0].encrypted_content = "x" * (MAX_CONTEXT_BYTES - used - 2 * 1024)
        return response


def call(name, *, call_id=None, **arguments):
    return [{
        "type": "function_call",
        "call_id": call_id or f"call-{name}",
        "name": name,
        "arguments": json.dumps(arguments),
    }]


def answer(text, *, kind="grounded_answer", proposal=None):
    payload = {"kind": kind, "answer": text}
    if proposal is not None:
        references = SOURCE_REFERENCE.findall(text)
        source_id = references[0] if references else "git-guide"
        payload["access_request"] = {
            "policy_source_id": source_id, "policy_index": 0,
            "target_status": "selected", "level_status": "explicit",
            "requested_level": proposal["access_level"],
        }
        if proposal["resource_id"].startswith("http") or source_id == "git-guide":
            payload["access_request"]["repository_url"] = proposal["resource_id"]
    final = json.dumps(payload, ensure_ascii=False)
    return [{"type": "message", "content": [{"type": "output_text", "text": final}]}]




@pytest.fixture
def settings(monkeypatch):
    visible_sources = {
        ("intern", "backend"): ["demo-dev-environment"],
        ("intern", "51"): ["demo-dev-environment"],
        ("mentor", "backend"): ["demo-mentor-environment", "demo-dev-environment"],
    }

    async def embed(self, texts):
        assert len(texts) == 1
        return [EMBEDDING]

    async def search(self, vector, profile, actor, min_similarity, *, query):
        assert len(profile) == 64
        assert vector == EMBEDDING
        assert min_similarity == 0.3
        rows = visible_sources.get((actor.role, actor.team), [])
        return SearchResult(hits=[
            SearchHit(id=key, title=key, text="Фрагмент инструкции") for key in rows
        ])

    async def accessible_source_ids(self, source_ids, profile, actor):
        assert len(profile) == 64
        return source_ids.intersection(visible_sources.get((actor.role, actor.team), []))

    monkeypatch.setattr(Embeddings, "embed", embed)
    monkeypatch.setattr(KnowledgeRepository, "search", search)
    monkeypatch.setattr(KnowledgeRepository, "accessible_source_ids", accessible_source_ids)
    return Settings(
        service_token="chat-test-token",
        knowledge_db_dsn="postgresql://knowledge.test/knowledge",
        knowledge_processing_approved=True,
        embedding_api_key="test-embedding-token",
    )


@pytest.fixture
def use_mock_http(monkeypatch):
    async_client = httpx.AsyncClient

    def use(handler):
        def client(**kwargs):
            return async_client(transport=httpx.MockTransport(handler), **kwargs)

        monkeypatch.setattr(httpx, "AsyncClient", client)

    return use


@pytest.mark.parametrize(("overrides", "variable", "reason"), [
    ({"llm_base_url": "https://[private-host"}, "LLM_BASE_URL", r"an HTTP\(S\) base URL"),
    ({"llm_api_key": "некорректный-ключ"}, "LLM_API_KEY", "an ASCII Bearer token without whitespace"),
    ({"embedding_base_url": "https://example.test?secret"}, "EMBEDDING_BASE_URL", r"an HTTP\(S\) base URL"),
    ({"embedding_api_key": "invalid key"}, "EMBEDDING_API_KEY", "an ASCII Bearer token without whitespace"),
])
def test_chat_rejects_invalid_provider_settings_on_startup(settings, overrides, variable, reason):
    configured = settings.model_copy(update={"llm_api_key": "llm-key", "llm_model": "test-model", **overrides})
    with pytest.raises(RuntimeError, match=rf"^{variable} must be {reason}$"):
        with TestClient(create_app(configured)):
            pytest.fail("Chat started with invalid provider settings")


def ask(client, message, actor=ACTOR, **extra):
    return client.post("/api/v1/answer", headers=AUTH, json={"actor": actor, "message": message, **extra})


def searched_model(*outputs, query=QUESTION):
    """Script an explicit planning call followed by the supplied model outputs."""
    return ScriptedModel(call("find_instruction", call_id="initial-search", query=query), *outputs)


def tool_output(model, request_index=1):
    return json.loads(model.requests[request_index][1][-1]["output"])


def first_context(model, request_index=0):
    return json.loads(model.requests[request_index][1][-2]["content"].split("\n", 1)[1])


@pytest.mark.parametrize(("actor", "ids"), [
    (ACTOR, ["demo-dev-environment"]),
    ({**ACTOR, "role": "mentor"}, ["demo-mentor-environment", "demo-dev-environment"]),
    ({**ACTOR, "team": "design"}, []),
])
def test_chat_forwards_accessible_sources_from_search(settings, actor, ids):
    text = "Ответ. " + " ".join(f"[source:{id}]" for id in ids)
    model = ScriptedModel(call("find_instruction", query="окружение"), answer(text if ids else "Нет доступных сведений.", kind="grounded_answer" if ids else "no_knowledge"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, actor)
    assert response.status_code == 200, response.text
    assert response.json()["answer"] == (text if ids else "Нет доступных сведений.")
    assert sorted(source["id"] for source in response.json()["sources"]) == sorted(ids)
    assert first_context(model) == {}
    assert sorted(hit["id"] for hit in tool_output(model)["hits"]) == sorted(ids)
    assert set(response.json()) == {"answer", "sources"}
    assert [tool["name"] for tool in model.requests[0][2]] == ["find_instruction"]


def test_answer_uses_chat_profile_without_calling_user(settings, monkeypatch, use_mock_http):
    def unexpected_http(request):
        pytest.fail(f"Unexpected outgoing HTTP request: {request.method} {request.url}")

    async def check_ready(self, profile):
        assert len(profile) == 64

    use_mock_http(unexpected_http)
    monkeypatch.setattr(KnowledgeRepository, "check_ready", check_ready)
    model = searched_model(answer("Ответ с первого вызова. [source:demo-dev-environment]"))
    payload = {
        "conversation_id": "123",
        "actor": {"user_id": "42", "team": "51", "role": "intern", "actual_end_date": None},
        "message": QUESTION,
    }
    with TestClient(create_app(settings, model=model)) as client:
        assert client.get("/readyz").status_code == 200
        response = client.post("/api/v1/answer", headers=AUTH, json=payload)

    assert response.status_code == 200, response.text
    assert set(response.json()) == {"answer", "sources"}
    assert [source["id"] for source in response.json()["sources"]] == ["demo-dev-environment"]
    assert len(model.requests) == 2
    _, messages, _ = model.requests[0]
    assert messages[-1] == {"role": "user", "content": QUESTION}
    assert first_context(model) == {}
    assert response.json()["sources"] == tool_output(model)["hits"]
    assert "actual_end_date" not in json.dumps(messages)


@pytest.mark.parametrize("path", ["/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
def test_answer_endpoint_and_compatibility_routes(settings, path):
    model = searched_model(answer("Инструкция [source:demo-dev-environment]"))
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post(path, headers=AUTH, json={"actor": ACTOR, "message": QUESTION})
    assert response.status_code == 200, response.text
    assert response.json()["answer"] == "Инструкция [source:demo-dev-environment]"
    assert response.json()["sources"] == tool_output(model)["hits"]
    assert response.headers["x-request-id"]


@pytest.mark.parametrize("headers", [{}, {"Authorization": "Bearer wrong-token"}, {"Authorization": "Basic wrong-token"}])
def test_supplied_profile_does_not_bypass_chat_authentication(settings, monkeypatch, headers):
    def unexpected_work(*args, **kwargs):
        pytest.fail("Unauthenticated request reached dependencies")

    monkeypatch.setattr(Embeddings, "embed", unexpected_work)
    model = ScriptedModel()
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post("/api/v1/answer", headers=headers, json={"actor": ACTOR, "message": QUESTION})
    assert response.status_code == 401
    assert response.headers["www-authenticate"] == "Bearer"
    assert model.requests == []


@pytest.mark.parametrize("actor", [
    {"user_id": "42"},
    {"user_id": "42", "team": "backend"},
    {"user_id": "42", "role": "intern"},
    {**ACTOR, "team": ""},
    {**ACTOR, "role": ""},
    {**ACTOR, "user_id": ""},
    {**ACTOR, "actual_end_date": "2026-01-01"},
    {**ACTOR, "actual_end_date": "invalid-date"},
])
def test_invalid_chat_profile_is_rejected_before_dependencies(settings, monkeypatch, actor):
    def unexpected_work(*args, **kwargs):
        pytest.fail("Invalid profile reached dependencies")

    monkeypatch.setattr(Embeddings, "embed", unexpected_work)
    monkeypatch.setattr(KnowledgeRepository, "search", unexpected_work)
    model = ScriptedModel()
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, actor)
    assert response.status_code == 422, response.text
    assert response.json()["detail"][0]["loc"][:2] == ["body", "actor"]
    assert model.requests == []


@pytest.mark.parametrize("path", ["/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
@pytest.mark.parametrize("step_id", ["dev-environment", None])
def test_step_confirmation_is_rejected_before_any_work(
    settings, use_mock_http, monkeypatch, path, step_id
):
    def unexpected_work(*args, **kwargs):
        pytest.fail("Step confirmation must not invoke dependencies")

    use_mock_http(unexpected_work)
    monkeypatch.setattr(Embeddings, "embed", unexpected_work)
    monkeypatch.setattr(KnowledgeRepository, "search", unexpected_work)
    model = ScriptedModel()
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post(path, headers=AUTH, json={
            "conversation_id": "123", "actor": ACTOR, "message": "Подтверждаю шаг",
            "confirmed_step_id": step_id,
        })

    assert response.status_code == 422
    assert response.json()["detail"][0]["loc"] == ["body", "confirmed_step_id"]
    assert response.json()["detail"][0]["type"] == "extra_forbidden"
    assert model.requests == []


def test_chat_uses_request_profile_and_excludes_closed_reply_after_role_change(settings):
    model = ScriptedModel(
        call("find_instruction", query="окружение"),
        answer("Закрытая инструкция. [source:demo-mentor-environment] [source:demo-dev-environment]"),
        call("find_instruction", query="окружение"), answer("Ответ. [source:demo-dev-environment]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        first = ask(client, QUESTION, {**ACTOR, "role": "mentor"}, conversation_id="conversation-1")
        history = [
            {"role": "user", "content": "Какую инструкцию открыть?"},
            {"role": "assistant", "content": first.json()["answer"],
             "source_ids": [source["id"] for source in first.json()["sources"]]},
        ]
        second = ask(client, QUESTION, conversation_id="conversation-1", history=history)

    assert first.status_code == second.status_code == 200
    for index, response, expected in (
        (0, first, ["demo-mentor-environment", "demo-dev-environment"]),
        (2, second, ["demo-dev-environment"]),
    ):
        context = first_context(model, index)
        assert context == {}
        assert [source["id"] for source in tool_output(model, index + 1)["hits"]] == expected
        assert [source["id"] for source in response.json()["sources"]] == expected
    for _, messages, _ in model.requests[2:]:
        assert history[1]["content"] not in json.dumps(messages, ensure_ascii=False)
        assert history[0]["content"] in json.dumps(messages, ensure_ascii=False)


def test_missing_request_profile_cannot_reuse_previous_profile(settings, monkeypatch):
    model = ScriptedModel(
        call("find_instruction", query="наставник"), answer("Ответ наставнику. [source:demo-dev-environment]"),
        call("find_instruction", query="стажёр"), answer("Ответ стажёру. [source:demo-dev-environment]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        assert ask(client, QUESTION, {**ACTOR, "role": "mentor"}, conversation_id="123").status_code == 200
        with monkeypatch.context() as failure:
            def unexpected_search(*args, **kwargs):
                pytest.fail("Search attempted without a complete profile")

            failure.setattr(Embeddings, "embed", unexpected_search)
            response = ask(client, QUESTION, {"user_id": "42"}, conversation_id="123")
        assert response.status_code == 422
        assert len(model.requests) == 2
        assert ask(client, QUESTION, conversation_id="123").status_code == 200
    assert [hit["id"] for hit in tool_output(model, 3)["hits"]] == ["demo-dev-environment"]


def test_knowledge_failure_after_planning_returns_gateway_error(settings, monkeypatch):
    from ai_service.repositories.knowledge import KnowledgeError

    async def search(*args, **kwargs):
        raise KnowledgeError("Search failed", reason="connection")

    monkeypatch.setattr(KnowledgeRepository, "search", search)
    model = searched_model(answer("Нет данных.", kind="no_knowledge"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502
    assert len(model.requests) == 2


def test_embedding_failure_after_planning_uses_lexical_sources(settings, monkeypatch):
    async def embed(self, texts):
        raise EmbeddingError("Не удалось получить эмбеддинги.")

    monkeypatch.setattr(Embeddings, "embed", embed)
    async def lexical(self, query, profile, actor):
        assert query == QUESTION
        assert actor.model_dump() == ACTOR
        assert len(profile) == 64
        return SearchResult(hits=[SearchHit(id="lexical", title="Инструкция", text="Настройте окружение.")])

    monkeypatch.setattr(KnowledgeRepository, "search_lexical", lexical)
    model = searched_model(answer("Настройте окружение. [source:lexical]"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200
    assert response.json() == {
        "answer": "Настройте окружение. [source:lexical]",
        "sources": [{"id": "lexical", "title": "Инструкция", "text": "Настройте окружение.", "url": None}],
    }
    assert len(model.requests) == 2


@pytest.mark.parametrize('database_failure', [False, True])
def test_embedding_outage_distinguishes_empty_lexical_results_from_database_failure(settings, monkeypatch, database_failure):
    from ai_service.repositories.knowledge import KnowledgeError

    async def embed(self, texts):
        raise EmbeddingError('Embedding unavailable', reason='http_503')

    async def lexical(self, query, profile, actor):
        if database_failure:
            raise KnowledgeError('Database unavailable', reason='connection')
        return SearchResult(hits=[])

    monkeypatch.setattr(Embeddings, 'embed', embed)
    monkeypatch.setattr(KnowledgeRepository, 'search_lexical', lexical)
    model = searched_model(answer('Нет данных.', kind='no_knowledge'))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    if database_failure:
        assert response.status_code == 502
        assert response.json() == {'detail': 'Не удалось выполнить поиск в базе знаний.'}
    else:
        assert response.status_code == 200
        assert response.json() == {'answer': 'Нет данных.', 'sources': []}


def test_wrong_embedding_dimension_falls_back_without_using_vector(monkeypatch, use_mock_http):
    async def unexpected_search(*args, **kwargs):
        pytest.fail("Vector search attempted with an incompatible embedding")

    async def lexical(self, query, profile, actor):
        return SearchResult(hits=[])

    settings = Settings(
        service_token="chat-test-token", knowledge_processing_approved=True,
        embedding_api_key="test-embedding-token",
        knowledge_db_dsn="postgresql://knowledge.test/knowledge"
    )
    use_mock_http(lambda request: httpx.Response(200, json={"data": [{"index": 0, "embedding": [1, 0]}]}))
    monkeypatch.setattr(KnowledgeRepository, "search", unexpected_search)
    monkeypatch.setattr(KnowledgeRepository, "search_lexical", lexical)
    model = searched_model(answer("Нет данных.", kind="no_knowledge"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)

    assert response.status_code == 200
    assert response.json() == {"answer": "Нет данных.", "sources": []}
    assert len(model.requests) == 2


@pytest.mark.parametrize("model_tries_to_write", [False, True])
def test_model_cannot_call_write_tools(settings, model_tries_to_write):
    text = "Подтвердите нужный шаг в интерфейсе адаптации."
    outputs = [answer(text, kind="clarification")]
    if model_tries_to_write:
        outputs.insert(0, call("complete_onboarding_step", step_id="dev-environment"))
    else:
        outputs.insert(0, call("find_instruction", query="подтверждение шага адаптации"))
    model = ScriptedModel(*outputs)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Отметь шаг")
    assert [tool["name"] for tool in model.requests[0][2]] == ["find_instruction"]
    if model_tries_to_write:
        assert response.status_code == 502
    else:
        assert response.status_code == 200
        assert response.json() == {"answer": text, "sources": []}


@pytest.mark.parametrize("path", ["/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
def test_service_requires_auth_and_config(settings, path):
    payload = {"actor": ACTOR, "message": "Привет"}
    with TestClient(create_app(settings)) as client:
        assert client.get("/healthz").json() == {"status": "ok"}
        assert client.post(path, json=payload).status_code == 401
        assert client.post(path, headers=AUTH, json=payload).status_code == 503


def test_user_id_alone_requires_full_profile_from_chat(settings):
    with TestClient(create_app(settings, model=ScriptedModel())) as client:
        assert ask(client, "Привет", {"user_id": ACTOR["user_id"]}).status_code == 422


def test_model_failure_returns_gateway_error(settings):
    with TestClient(create_app(settings, model=ScriptedModel(ModelError("Тайм-аут")))) as client:
        response = ask(client, "Привет")
    assert response.status_code == 502
    assert response.json()["detail"] == "Модель временно недоступна."


def test_invalid_llm_response_returns_gateway_error(settings, use_mock_http):
    def handler(request):
        return httpx.Response(200, json={"status": "completed", "output": [{
            "type": "message", "content": [{"type": "output_text"}],
        }]})

    use_mock_http(handler)
    settings = settings.model_copy(update={"llm_api_key": "llm-test-token", "llm_model": "test-model"})
    with TestClient(create_app(settings)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502
    assert response.json()["detail"] == "Модель вернула ответ в неизвестном формате."


@pytest.mark.parametrize("arguments", [
    pytest.param('{"query":' + '[' * 255 + 'null' + ']' * 255 + '}', id="deeply-nested"),
    pytest.param('{"query":"окружение","extra":[]}', id="extra-field"),
    pytest.param('{"query":"  "}', id="blank-query"),
    pytest.param(json.dumps({"query": "a" * 401}), id="long-query"),
])
def test_invalid_search_tool_arguments_are_rejected_before_search(settings, use_mock_http, monkeypatch, arguments):
    def handler(request):
        return httpx.Response(200, json={"output": [{
            "type": "function_call", "name": "find_instruction", "call_id": "call-id", "arguments": arguments,
        }]})

    async def embed(self, texts):
        pytest.fail("Invalid arguments reached the retriever")

    use_mock_http(handler)
    monkeypatch.setattr(Embeddings, "embed", embed)
    settings = settings.model_copy(update={"llm_api_key": "llm-test-token", "llm_model": "test-model"})
    with TestClient(create_app(settings)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502
    assert response.json()["detail"] == "Модель вернула некорректные аргументы инструмента."


@pytest.mark.parametrize("final_tool_call", [False, True])
def test_final_model_round_receives_search_results_with_tools_disabled(settings, monkeypatch, final_tool_call):
    queries = []

    async def embed(self, texts):
        queries.extend(texts)
        return [EMBEDDING]

    monkeypatch.setattr(Embeddings, "embed", embed)
    tool_calls = [call("find_instruction", call_id=f"call-{i}", query=f"поиск {i}") for i in range(1, 5)]
    text = "Итоговый ответ. [source:demo-dev-environment]"
    final_output = tool_calls[2] if final_tool_call else answer(text)
    model = ScriptedModel(*tool_calls[:2], final_output)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)

    assert queries == ["поиск 1", "поиск 2"]
    assert [len(request[2]) for request in model.requests] == [1, 1, 0]
    assert tool_output(model, request_index=2)["hits"][0]["id"] == "demo-dev-environment"
    if final_tool_call:
        assert response.status_code == 502
    else:
        assert response.status_code == 200
        assert response.json()["answer"] == text


def test_llm_tool_follow_up_preserves_provider_items_and_call_id(settings, use_mock_http):
    model_output = [
        {"type": "reasoning", "id": "rs_1", "summary": [], "encrypted_content": "opaque"},
        {
            "type": "function_call", "id": "fc_1", "status": "completed", "call_id": "provider-call-id",
            "name": "find_instruction", "arguments": '{"query":"окружение"}',
        },
    ]
    requests = []

    def handler(request):
        requests.append(json.loads(request.content))
        assert request.url.path == "/v1/responses"
        if len(requests) == 1:
            return httpx.Response(200, json={"status": "completed", "output": model_output})
        assert len(requests) == 2
        return httpx.Response(200, json={"status": "completed", "output": answer("Ответ. [source:demo-dev-environment]")})

    use_mock_http(handler)
    settings = settings.model_copy(update={"llm_api_key": "llm-test-token", "llm_model": "test-model"})
    with TestClient(create_app(settings)) as client:
        response = ask(client, QUESTION)

    assert response.status_code == 200, response.text
    assert response.json()["answer"] == "Ответ. [source:demo-dev-environment]"
    assert requests[1]["input"][:-1] == requests[0]["input"] + model_output
    output = requests[1]["input"][-1]
    assert output["type"] == "function_call_output"
    assert output["call_id"] == "provider-call-id"
    assert [hit["id"] for hit in json.loads(output["output"])["hits"]] == ["demo-dev-environment"]


@pytest.mark.parametrize("path", ["/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
@pytest.mark.parametrize("body", [
    b'{"message":NaN,"actor":{"user_id":"42"}}',
    b'{"message":"hi","actor":{"user_id":"42"},"extra":{"value":Infinity}}',
    b'{"message":"\\ud800","actor":{"user_id":"42"}}',
])
def test_invalid_json_values_return_serializable_validation_errors(settings, path, body):
    with TestClient(create_app(settings), raise_server_exceptions=False) as client:
        response = client.post(path, headers={**AUTH, "content-type": "application/json"}, content=body)
    assert response.status_code == 422
    assert isinstance(response.json()["detail"][0]["type"], str)


@pytest.mark.parametrize("path", ["/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
@pytest.mark.parametrize("chunked", [False, True])
def test_chat_body_limit(settings, path, chunked):
    from ai_service.api.middleware import MAX_CHAT_BODY_BYTES

    chunks = [b" " * (MAX_CHAT_BODY_BYTES // 2)] * 3
    with TestClient(create_app(settings)) as client:
        response = client.post(path, headers={**AUTH, "content-type": "application/json"},
                               content=iter(chunks) if chunked else b"".join(chunks))
    assert response.status_code == 413


def test_readiness_reports_question_configuration(settings):
    for overrides, code, reason in [
        ({"service_token": ""}, 503, "service_token_missing"),
        ({"knowledge_db_dsn": ""}, 503, "knowledge_dsn_missing"),
        ({"knowledge_processing_approved": False}, 503, "knowledge_processing_unapproved"),
    ]:
        with TestClient(create_app(settings.model_copy(update=overrides), model=ScriptedModel())) as client:
            assert client.get("/healthz").status_code == 200
            response = client.get("/readyz")
            assert response.status_code == code
            assert response.json()["capability"] == "questions"
            assert response.json()["reason"] == reason


def test_chat_discards_unverified_answer_after_empty_tool_search(settings):
    model = searched_model(answer("Внутренний адрес https://invented.example"))
    with TestClient(create_app(settings, model=model)) as client:
        result = ask(client, QUESTION, {**ACTOR, "team": "design"})
    assert result.status_code == 502
    assert "invented.example" not in result.text


@pytest.mark.parametrize("outcome", ["recover", "search-error", "bad-citation", "refusal"])
def test_rag_after_empty_tool_search(settings, monkeypatch, outcome):
    from ai_service.repositories.knowledge import KnowledgeError

    async def embed(self, texts):
        return [[2.0] if texts == ["окружение"] else [1.0]]

    async def search(self, vector, profile, actor, min_similarity, *, query):
        if vector == [1.0]:
            return SearchResult(hits=[])
        if outcome == "search-error":
            raise KnowledgeError("Поиск недоступен.", reason="connection")
        return SearchResult(hits=[SearchHit(id="visible", title="Guide", text="Instructions")])

    monkeypatch.setattr(Embeddings, "embed", embed)
    monkeypatch.setattr(KnowledgeRepository, "search", search)
    text = "Ответ [source:hidden]" if outcome == "bad-citation" else "Ответ [source:visible]"
    outputs = answer(text) if outcome != "refusal" else [
        {"type": "message", "content": [{"type": "refusal", "refusal": "Refused"}]}
    ]
    model = ScriptedModel(
        call("find_instruction", call_id="first", query=QUESTION),
        call("find_instruction", call_id="refined", query="окружение"), outputs,
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    if outcome == "recover":
        assert response.status_code == 200
        assert response.json()["sources"][0]["id"] == "visible"
    else:
        assert response.status_code == 502
        assert {"search-error": "поиск", "bad-citation": "источник", "refusal": "отказалась"}[outcome] in response.json()["detail"]


@pytest.mark.parametrize(("question", "query"), [
    ("Как получить доступ к Git?", "Как получить доступ к Git?"),
    ("Как палучить достпу к Git?", "Как получить доступ к Git?"),
    ("kak poluchit dostup k Git", "Как получить доступ к Git?"),
    ("как poluchit доступ к Git", "Как получить доступ к Git?"),
    ("rfr gjkexbnm ljcneg r Git", "Как получить доступ к Git?"),
    ("How do I get Git access?", "How do I get Git access?"),
])
def test_model_formulates_first_query_before_embeddings(settings, monkeypatch, question, query):
    events = []
    queries = []

    async def embed(self, texts):
        events.append("embeddings")
        queries.extend(texts)
        return [EMBEDDING]

    async def search(self, vector, profile, actor, min_similarity, *, query):
        events.append("database")
        assert actor.model_dump() == ACTOR
        assert vector == EMBEDDING
        return SearchResult(hits=[SearchHit(id="git-guide", title="Git", text="Запросите доступ у наставника.")])

    class PlanningModel(ScriptedModel):
        async def respond(self, **kwargs):
            events.append("llm")
            if not self.requests:
                assert queries == []
                assert kwargs["messages"][-1] == {"role": "user", "content": question}
            return await super().respond(**kwargs)

    monkeypatch.setattr(Embeddings, "embed", embed)
    monkeypatch.setattr(KnowledgeRepository, "search", search)
    model = PlanningModel(
        call("find_instruction", query=query),
        answer("Запросите доступ у наставника. [source:git-guide]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, question)
    assert response.status_code == 200, response.text
    assert queries == [query]
    assert events == ["llm", "embeddings", "database", "llm"]
    assert first_context(model) == {}
    assert tool_output(model)["hits"][0]["id"] == "git-guide"
    assert [source["id"] for source in response.json()["sources"]] == ["git-guide"]


def test_unclear_typo_still_searches_before_free_clarification(settings, monkeypatch):
    queries = mock_search_results(monkeypatch, {"ghj cn": SearchResult(hits=[])})
    text = "Уточните, пожалуйста, о каком сервисе или действии вы спрашиваете?"
    model = searched_model(answer(text, kind="clarification"), query="ghj cn")
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "ghj cn")
    assert response.json() == {"answer": text, "sources": []}
    assert queries == ["ghj cn"]
    assert len(model.requests) == 2


@pytest.mark.parametrize("kind", ["grounded_answer", "no_knowledge"])
def test_non_clarification_requires_actual_search(settings, monkeypatch, kind, caplog):
    async def unexpected_work(*args, **kwargs):
        pytest.fail("There was no valid search call")

    monkeypatch.setattr(Embeddings, "embed", unexpected_work)
    model = ScriptedModel(answer("invented fact [source:invented]", kind=kind))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "kak poluchit dostup k Git")
    assert response.status_code == 502
    assert "без поиска" in response.json()["detail"]
    assert "invented" not in response.text
    assert any("result=search_required" in record.getMessage() for record in caplog.records)
    assert len(model.requests) == 1


def test_empty_model_search_returns_no_knowledge(settings, monkeypatch):
    queries = mock_search_results(monkeypatch, {"Как получить доступ к Git?": SearchResult(hits=[])})
    model = searched_model(answer("Не найдено.", kind="no_knowledge"), query="Как получить доступ к Git?")
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "kak poluchit dostup k Git")
    assert response.status_code == 200, response.text
    assert response.json() == {"answer": "Не найдено.", "sources": []}
    assert queries == ["Как получить доступ к Git?"]
    assert len(model.requests) == 2


def test_irrelevant_first_search_can_be_refined_before_answer(settings, monkeypatch):
    queries = mock_search_results(monkeypatch, {
        "доступ Git": SearchResult(hits=[SearchHit(id="wifi", title="Wi-Fi", text="Подключитесь к офисной сети.")]),
        "получение доступа к репозиторию Git": SearchResult(hits=[
            SearchHit(id="git-guide", title="Git", text="Создайте заявку наставнику."),
        ]),
    })
    model = ScriptedModel(
        call("find_instruction", call_id="first", query="доступ Git"),
        call("find_instruction", call_id="refined", query="получение доступа к репозиторию Git"),
        answer("Создайте заявку наставнику. [source:git-guide]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "kak poluchit dostup k Git")
    assert response.status_code == 200, response.text
    assert queries == ["доступ Git", "получение доступа к репозиторию Git"]
    assert tool_output(model, 1)["hits"][0]["id"] == "wifi"
    assert tool_output(model, 2)["hits"][0]["id"] == "git-guide"
    assert [source["id"] for source in response.json()["sources"]] == ["git-guide"]
    assert len(model.requests) == 3


def test_parallel_tool_calls_are_rejected_before_search(settings, monkeypatch):
    queries = mock_search_results(monkeypatch, {})
    calls = call("find_instruction", call_id="a", query="VPN") + call("find_instruction", call_id="b", query="Wiki")
    model = ScriptedModel(calls)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502
    assert queries == []


def test_duplicate_call_id_across_rounds(settings):
    same_call = call("find_instruction", query="окружение")
    model = ScriptedModel(same_call, same_call)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502
    assert "повторяющиеся" in response.json()["detail"]


def test_chat_deadline_cancels_model(settings, caplog):
    caplog.set_level(logging.INFO, logger=logger.name)
    cancellations = []

    class SlowModel:
        async def respond(self, **_):
            try:
                await asyncio.sleep(30)
            except asyncio.CancelledError:
                cancellations.append("cancelled")
                raise

    with TestClient(create_app(settings.model_copy(update={
        "chat_timeout_seconds": 0.05, "llm_fallback_reserve_seconds": 0,
    }), model=SlowModel())) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 504
    assert cancellations == ["cancelled"]
    assert "event=chat_request_cancelled" not in caplog.text


@pytest.mark.parametrize("path", ["/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
def test_disconnected_client_cancels_in_flight_request(settings, path, caplog):
    caplog.set_level(logging.INFO, logger=logger.name)

    async def run():
        started = asyncio.Event()
        cancelled = asyncio.Event()

        class SlowModel:
            async def respond(self, **_):
                started.set()
                try:
                    await asyncio.sleep(30)
                finally:
                    cancelled.set()

        app = create_app(settings, model=SlowModel())
        events = iter([{
            "type": "http.request", "more_body": False,
            "body": json.dumps({"message": QUESTION, "actor": ACTOR}).encode(),
        }])
        responses = []

        async def receive():
            if event := next(events, None):
                return event
            await started.wait()
            return {"type": "http.disconnect"}

        async def send(message):
            responses.append(message)

        async with app.router.lifespan_context(app):
            await asyncio.wait_for(app({
                "type": "http", "http_version": "1.1", "method": "POST", "path": path,
                "root_path": "", "query_string": b"", "headers": [
                    (b"authorization", AUTH["Authorization"].encode()), (b"content-type", b"application/json"),
                ],
            }, receive, send), timeout=1)
        assert cancelled.is_set()
        response = next(message for message in responses if message["type"] == "http.response.start")
        assert response["status"] == 499
        correlation = dict(response["headers"])[b"x-request-id"].decode()
        records = [record for record in caplog.records if "event=chat_request_cancelled" in record.getMessage()]
        assert len(records) == 1
        assert records[0].levelno == logging.INFO
        assert records[0].getMessage() == (
            f"event=chat_request_cancelled reason=client_disconnect request_id={correlation}"
        )

    asyncio.run(run())


def test_pool_reconnect_failure_logs_only_event_and_pool_name(settings, caplog):
    configured = settings.model_copy(update={
        "knowledge_db_dsn": "postgresql://private-user:private-password@private-host/private-db",
    })
    app = create_app(configured, model=ScriptedModel())

    async def run():
        async with app.router.lifespan_context(app):
            pool = app.state.chat_service.agent.knowledge.repository.pool
            await pool.reconnect_failed()

    with caplog.at_level(logging.WARNING, logger=logger.name):
        asyncio.run(run())
    assert [(record.levelno, record.getMessage()) for record in caplog.records] == [
        (logging.WARNING, "event=db_reconnect_failed pool=ai-knowledge"),
    ]


@pytest.mark.parametrize("path", ["/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
def test_chat_deadline_includes_reading_body(settings, path):
    async def run():
        app = create_app(settings.model_copy(update={"chat_timeout_seconds": 0.05}))
        statuses = []

        async def receive():
            await asyncio.sleep(30)

        async def send(message):
            if message["type"] == "http.response.start":
                statuses.append(message["status"])

        async with app.router.lifespan_context(app):
            await asyncio.wait_for(app({
                "type": "http", "http_version": "1.1", "method": "POST", "path": path,
                "root_path": "", "query_string": b"", "headers": [(b"content-type", b"application/json")],
            }, receive, send), timeout=1)
        assert statuses == [408]

    asyncio.run(run())


def test_chat_rejects_oversized_replay_before_follow_up(settings):
    from ai_service.agents.agent import MAX_CONTEXT_BYTES

    model = ScriptedModel(
        [{"type": "reasoning", "encrypted_content": "x" * MAX_CONTEXT_BYTES}]
        + call("find_instruction", query="окружение")
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502
    assert "бюджет" in response.json()["detail"]
    assert len(model.requests) == 1


def mock_search_results(monkeypatch, results):
    queries = []

    async def search(self, query, actor):
        assert actor.model_dump() == ACTOR
        queries.append(query)
        return results[query]

    monkeypatch.setattr(Retriever, "search", search)
    return queries


def search_hits(prefix, text="Фрагмент инструкции"):
    return SearchResult(hits=[
        SearchHit(id=f"{prefix}-{index}", title="Инструкция", text=text) for index in range(3)
    ])


@pytest.mark.parametrize("refinements", [2])
def test_refined_search_reaches_model_after_six_sources(settings, monkeypatch, refinements):
    results = {f"поиск {index}": search_hits(f"round-{index}") for index in range(1, refinements + 1)}
    queries = mock_search_results(monkeypatch, results)
    cited = [f"round-{refinements}-2", "round-1-0", "round-1-1", "round-1-2", "round-2-0", "round-2-1"]
    text = "Ответ: " + " ".join(f"[source:{id}]" for id in [*cited, cited[0]])
    model = ScriptedModel(*[
        call("find_instruction", call_id=f"call-{index}", query=f"поиск {index}")
        for index in range(1, refinements + 1)
    ], answer(text))

    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)

    assert response.status_code == 200, response.text
    assert queries == list(results)
    assert first_context(model) == {}
    for index in range(1, refinements + 1):
        assert tool_output(model, index)["hits"] == results[f"поиск {index}"].model_dump(mode="json")["hits"]
    assert response.json()["answer"] == text
    assert [source["id"] for source in response.json()["sources"]] == cited


@pytest.mark.parametrize("citation_count", [6, 7])
def test_response_limits_unique_citations_without_truncating_answer(settings, monkeypatch, citation_count):
    results = {QUESTION: search_hits("initial"), "первый": SearchResult(hits=[SearchHit(id=f"first-{i}", title="Инструкция", text="Текст") for i in range(4)])}
    mock_search_results(monkeypatch, results)
    ids = [hit.id for result in results.values() for hit in result.hits][:citation_count]
    model = ScriptedModel(
        call("find_instruction", call_id="initial", query=QUESTION),
        call("find_instruction", call_id="first", query="первый"),
        answer("Ответ. " + " ".join(f"[source:{id}]" for id in ids)),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)

    if citation_count == 6:
        assert response.status_code == 200, response.text
        assert [source["id"] for source in response.json()["sources"]] == ids
    else:
        assert response.status_code == 502
        assert "лимит источников" in response.json()["detail"]


@pytest.mark.parametrize(("text", "status"), [("A" * 1200, 200), ("🧠" * 1200, 502)])
def test_source_byte_budget_applies_to_cited_sources(settings, monkeypatch, text, status):
    results = {QUESTION: search_hits("initial", text), "уточнение": search_hits("refined", text)}
    mock_search_results(monkeypatch, results)
    ids = [hit.id for result in results.values() for hit in result.hits]
    model = ScriptedModel(
        call("find_instruction", call_id="initial", query=QUESTION),
        call("find_instruction", query="уточнение"),
        answer("Ответ. " + " ".join(f"[source:{id}]" for id in ids)),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == status, response.text
    if status == 502:
        assert "лимит источников" in response.json()["detail"]


@pytest.mark.parametrize("citation", ["unseen-0", "small-0", "large-0"])
def test_context_overflow_is_explicit_and_unseen_sources_cannot_be_cited(settings, monkeypatch, citation):
    from ai_service.agents.agent import MAX_CONTEXT_BYTES, json_size

    results = {"большой": search_hits("large", "🧠" * 1200), "короткий": search_hits("small")}
    queries = mock_search_results(monkeypatch, results)
    model = ContextFillingModel(
        [{"type": "reasoning", "encrypted_content": ""}, *call("find_instruction", call_id="first", query="большой")],
        call("find_instruction", call_id="second", query="короткий"),
        answer(f"Ответ. [source:{citation}]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert queries == ["большой", "короткий"]
    assert len(model.requests) == 3
    for instructions, messages, tools in model.requests:
        assert json_size([instructions, messages, tools]) <= MAX_CONTEXT_BYTES
    assert tool_output(model, 1)["error"] == "context_budget_exhausted"
    assert tool_output(model, 2)["hits"] == results["короткий"].model_dump(mode="json")["hits"]
    if citation in {"large-0", "unseen-0"}:
        assert response.status_code == 502
        assert "недоступный источник" in response.json()["detail"]
    else:
        assert response.status_code == 200, response.text
        assert [source["id"] for source in response.json()["sources"]] == [citation]


def test_uncited_search_results_are_not_attached_to_response(settings):
    text = "Какой именно доступ вам нужен?"
    model = searched_model(answer(text, kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200, response.text
    assert tool_output(model)["hits"]
    assert response.json() == {"answer": text, "sources": []}


def test_context_limit_after_empty_initial_search_does_not_claim_no_knowledge(settings, monkeypatch):

    mock_search_results(monkeypatch, {QUESTION: SearchResult(hits=[]), "уточнение": search_hits("large", "🧠" * 1200)})
    model = ContextFillingModel(
        [{"type": "reasoning", "encrypted_content": ""}]
        + call("find_instruction", query="уточнение"),
        answer("Не удалось обработать найденные материалы. Уточните вопрос.", kind="no_knowledge"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200, response.text
    assert tool_output(model)["error"] == "context_budget_exhausted"
    assert response.json() == {"answer": "Не удалось обработать найденные материалы. Уточните вопрос.", "sources": []}


@pytest.mark.parametrize("outcome", ["fixed", "missing", "clarification", "no_knowledge", "bad_citation", "tool", "refusal"])
def test_missing_citation_is_repaired_once_with_tools_disabled(settings, outcome, caplog):
    unverified = "Пароль Wi-Fi — invented-password."
    fixed = "Обратитесь в поддержку. [source:demo-dev-environment]"
    corrections = {
        "fixed": answer(fixed),
        "missing": answer(unverified),
        "clarification": answer("К какой сети вы подключаетесь?", kind="clarification"),
        "no_knowledge": answer("В материалах нет сведений о пароле.", kind="no_knowledge"),
        "bad_citation": answer("Ответ. [source:unavailable]"),
        "tool": call("find_instruction", query="пароль"),
        "refusal": [{"type": "message", "content": [{"type": "refusal", "refusal": "Refused"}]}],
    }
    model = searched_model(answer(unverified), corrections[outcome])
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)

    assert len(model.requests) == 3
    assert model.requests[2][2] == []
    repair_messages = model.requests[2][1]
    assert repair_messages[:len(model.requests[1][1])] == model.requests[1][1]
    assert json.loads(repair_messages[-2]["content"][0]["text"])["answer"] == unverified
    assert "Исправь ответ" in repair_messages[-1]["content"]
    if outcome in {"fixed", "clarification", "no_knowledge"}:
        assert response.status_code == 200, response.text
        expected = {"fixed": fixed, "clarification": "К какой сети вы подключаетесь?", "no_knowledge": "В материалах нет сведений о пароле."}
        assert response.json()["answer"] == expected[outcome]
        ids = [source["id"] for source in response.json()["sources"]]
        assert ids == (["demo-dev-environment"] if outcome == "fixed" else [])
    else:
        assert response.status_code == 502
        assert unverified not in response.text
        if outcome == "missing":
            assert "подтверждающих источников" in response.json()["detail"]
            assert any("result=missing_citation" in record.getMessage() for record in caplog.records)


def test_citation_repair_after_last_tool_round_does_not_extend_search_budget(settings):
    calls = [call("find_instruction", call_id=f"call-{i}", query=f"поиск {i}") for i in range(2)]
    model = ScriptedModel(
        *calls, answer("Ответ без ссылки."),
        answer("Исправленный ответ. [source:demo-dev-environment]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200, response.text
    assert [len(request[2]) for request in model.requests] == [1, 1, 0, 0]
    assert [source["id"] for source in response.json()["sources"]] == ["demo-dev-environment"]


@pytest.mark.parametrize("has_sources", [True, False])
@pytest.mark.parametrize("text", CLARIFICATION_QUESTIONS)
def test_clarification_is_allowed_without_citations(settings, has_sources, text):
    model = searched_model(answer(text, kind="clarification"))
    actor = ACTOR if has_sources else {**ACTOR, "team": "design"}
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, actor)
    assert response.status_code == 200, response.text
    assert response.json() == {"answer": text, "sources": []}
    assert len(model.requests) == 2
    assert "дословно" not in model.requests[0][0]


@pytest.mark.parametrize("repair", [False, True])
def test_free_clarification_is_preserved_after_search_and_citation_repair(settings, repair):
    text = "Какой трек вы имеете в виду: настройку рабочего места или знакомство с компанией?"
    outputs = [answer(text, kind="clarification")]
    if repair:
        outputs.insert(0, answer("Ответ без ссылки."))
    model = searched_model(*outputs)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.json() == {"answer": text, "sources": []}
    assert len(model.requests) == 2 + int(repair)


def test_no_knowledge_preserves_model_wording(settings):
    model = searched_model(answer("Произвольный текст модели.", kind="no_knowledge"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200, response.text
    assert tool_output(model)["hits"]
    assert response.json() == {"answer": "Произвольный текст модели.", "sources": []}
    assert len(model.requests) == 2


@pytest.mark.parametrize("text", [
    "Пароль Wi-Fi — invented-password.",
    '{"answer":"Ответ."}',
    '{"kind":"unknown","answer":"Ответ."}',
    '{"kind":"clarification","answer":" "}',
    '{"kind":"clarification","answer":"\\ud800"}',
    '{"kind":"clarification","answer":"Что нужно?","extra":true}',
])
def test_invalid_final_answer_is_rejected_without_leaking_text(settings, text):
    model = searched_model([{"type": "message", "content": [{"type": "output_text", "text": text}]}])
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502
    assert response.json()["detail"] == "Модель вернула некорректный формат итогового ответа."
    assert len(model.requests) == 2


@pytest.mark.parametrize("budget", ["time", "context"])
def test_missing_citation_does_not_start_retry_without_budget(settings, budget):
    from ai_service.agents.agent import MAX_CONTEXT_BYTES

    output = answer("Неподтверждённое утверждение.")
    if budget == "context":
        output.insert(0, {"type": "reasoning", "encrypted_content": "x" * MAX_CONTEXT_BYTES})
    else:
        settings = settings.model_copy(update={"chat_timeout_seconds": 0.5})
    model = searched_model(output)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502, response.text
    assert "подтверждающих источников" in response.json()["detail"]
    assert len(model.requests) == 2


def test_citation_repair_is_cancelled_by_original_deadline(settings, monkeypatch):
    monkeypatch.setattr("ai_service.agents.agent.MIN_REPAIR_SECONDS", 0)
    cancelled = []

    class SlowRepair:
        def __init__(self):
            self.calls = 0

        async def respond(self, **kwargs):
            self.calls += 1
            if self.calls == 1:
                return ModelResponse(output=call("find_instruction", query=QUESTION))
            if self.calls == 2:
                return ModelResponse(output=answer("Неподтверждённое утверждение."))
            assert kwargs["tools"] == []
            try:
                await asyncio.sleep(30)
            finally:
                cancelled.append(True)

    model = SlowRepair()
    settings = settings.model_copy(update={"chat_timeout_seconds": 0.05, "llm_fallback_reserve_seconds": 0})
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 504
    assert model.calls == 3
    assert cancelled == [True]


@pytest.mark.parametrize("path", ["/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
@pytest.mark.parametrize("content_type", ["text/plain", "application/octet-stream"])
def test_binary_input_returns_safe_validation_error(settings, path, content_type):
    model = ScriptedModel()
    with TestClient(create_app(settings, model=model), raise_server_exceptions=False) as client:
        response = client.post(path, headers={**AUTH, "Content-Type": content_type}, content=b"\xff")
    assert response.status_code == 422, response.text
    assert response.headers["x-request-id"]
    assert len(response.json()["detail"]) == 1
    assert set(response.json()["detail"][0]) == {"type", "loc", "msg"}
    assert model.requests == []


@pytest.mark.parametrize("body", [
    b'{"actor":{"user_id":"42"},"message":"\\ud800"}',
    b'{"actor":{"user_id":"42"},"message":NaN,"extra":Infinity}',
    b'{"actor":{"user_id":"42"},"message":"hello","\\ud800":null}',
    b'{"actor":{"user_id":"42"},"extra":' + b'[' * 1500 + b'null' + b']' * 1500 + b'}',
])
def test_validation_details_omit_unsafe_inputs_and_context(settings, body):
    with TestClient(create_app(settings), raise_server_exceptions=False) as client:
        response = client.post("/api/v1/answer", headers={**AUTH, "Content-Type": "application/json"}, content=body)
    assert response.status_code == 422, response.text
    assert response.json()["detail"]
    assert all(set(issue) == {"type", "loc", "msg"} for issue in response.json()["detail"])


@pytest.mark.parametrize("path", ["/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
@pytest.mark.parametrize("legacy_step", [None, {"id": "vpn", "title": "Настроить VPN"}])
@pytest.mark.parametrize("include_plans", [False, True])
def test_top_level_current_step_is_rejected_before_dependencies(
    settings, monkeypatch, path, legacy_step, include_plans,
):
    def unexpected_work(*args, **kwargs):
        pytest.fail("Legacy onboarding format reached dependencies")

    monkeypatch.setattr(Embeddings, "embed", unexpected_work)
    model = ScriptedModel()
    payload = {"actor": ACTOR, "message": QUESTION, "current_step": legacy_step}
    if include_plans:
        payload["onboarding_plans"] = [{
            "id": "technical", "title": "Технический трек",
            "current_step": {"id": "vpn", "title": "Настроить VPN"},
        }]
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post(path, headers=AUTH, json=payload)
    assert response.status_code == 422, response.text
    assert response.json()["detail"] == [{
        "type": "extra_forbidden", "loc": ["body", "current_step"], "msg": "Extra inputs are not permitted",
    }]
    assert model.requests == []


@pytest.mark.parametrize("path", ["/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
@pytest.mark.parametrize("resource", [{}, {"resource_id": None}, {"resource_id": ""}, {"resource_id": "vpn"}])
def test_plan_step_is_accepted_and_used_for_search_and_model(settings, monkeypatch, use_mock_http, path, resource):
    queries = []

    async def embed(self, texts):
        queries.extend(texts)
        return [EMBEDDING]

    def unexpected_http(request):
        pytest.fail("AI must use the supplied step without calling Onboarding or User")

    monkeypatch.setattr(Embeddings, "embed", embed)
    use_mock_http(unexpected_http)
    step = {"id": "setup-vpn", "title": "Настроить VPN", **resource}
    query = "Настроить VPN setup-vpn"
    model = searched_model(answer("Инструкция. [source:demo-dev-environment]"), query=query)
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post(path, headers=AUTH, json={
            "actor": ACTOR, "message": "Как выполнить этот шаг?",
            "onboarding_plans": [{"id": "technical", "title": "Технический трек", "current_step": step}],
        })

    assert response.status_code == 200, response.text
    assert queries == [query]
    context = first_context(model)
    assert context["onboarding_plans"][0]["current_step"] == {
        "id": "setup-vpn", "title": "Настроить VPN", "resource_id": resource.get("resource_id") or None,
        "action_type": None,
    }
    assert "hits" not in context
    assert tool_output(model)["hits"] == response.json()["sources"]
    assert model.requests[0][1][-1] == {"role": "user", "content": "Как выполнить этот шаг?"}


def test_plan_step_changes_between_requests(settings, monkeypatch):
    queries = []

    async def embed(self, texts):
        queries.extend(texts)
        return [EMBEDDING]

    monkeypatch.setattr(Embeddings, "embed", embed)
    steps = [
        {"id": "vpn-step", "title": "Подключить VPN", "resource_id": "vpn"},
        {"id": "tracker-step", "title": "Открыть трекер", "resource_id": "tracker"},
        None,
    ]
    model = ScriptedModel(*[output for _ in steps for output in (
        call("find_instruction", query=QUESTION), answer("Инструкция. [source:demo-dev-environment]"),
    )])
    with TestClient(create_app(settings, model=model)) as client:
        for step in steps:
            response = ask(client, QUESTION, conversation_id="same-chat", onboarding_plans=[{
                "id": "technical", "title": "Технический трек", "current_step": step,
            }])
            assert response.status_code == 200, response.text
    assert queries == [QUESTION] * 3
    assert first_context(model, 2)["onboarding_plans"][0]["current_step"] == {**steps[1], "action_type": None}
    assert first_context(model, 4)["onboarding_plans"][0]["current_step"] is None


@pytest.mark.parametrize("step", [
    {},
    {"id": "vpn"},
    {"title": "VPN"},
    {"id": "vpn", "title": "  "},
    {"id": "vpn", "title": "A" * 257},
    {"id": "vpn", "title": "\ud800"},
    {"id": "invalid id", "title": "VPN"},
    {"id": "vpn", "title": "VPN", "resource_id": 42},
    {"id": "vpn", "title": "VPN", "status": "completed"},
    {"id": "vpn", "title": "VPN", "role": "mentor"},
])
def test_invalid_plan_step_is_rejected_before_dependencies(settings, monkeypatch, step):
    def unexpected_work(*args, **kwargs):
        pytest.fail("Invalid step reached embeddings or knowledge search")

    monkeypatch.setattr(Embeddings, "embed", unexpected_work)
    model = ScriptedModel()
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post(
            "/api/v1/answer", headers={**AUTH, "Content-Type": "application/json"},
            content=json.dumps({"actor": ACTOR, "message": QUESTION, "onboarding_plans": [{
                "id": "technical", "title": "Технический трек", "current_step": step,
            }]}, ensure_ascii=True),
        )
    assert response.status_code == 422, response.text
    assert response.json()["detail"][0]["loc"][:4] == ["body", "onboarding_plans", 0, "current_step"]
    assert model.requests == []


@pytest.mark.parametrize("citation", ["setup-vpn", "vpn-resource"])
def test_step_and_resource_ids_are_not_knowledge_sources(settings, citation):
    model = searched_model(answer(f"Инструкция. [source:{citation}]"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, onboarding_plans=[{
            "id": "technical", "title": "Технический трек", "current_step": {
                "id": "setup-vpn", "title": "Подключить VPN", "resource_id": "vpn-resource",
            },
        }])
    assert response.status_code == 502
    assert "недоступный источник" in response.json()["detail"]


def test_plan_step_does_not_force_unrelated_refined_search(settings, monkeypatch):
    queries = []

    async def search(self, query, actor):
        assert actor.model_dump() == ACTOR
        queries.append(query)
        return search_hits("dms") if query == "ДМС" else SearchResult(hits=[])

    monkeypatch.setattr(Retriever, "search", search)
    model = ScriptedModel(
        call("find_instruction", query="ДМС"), answer("Инструкция по ДМС. [source:dms-0]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Как оформить ДМС?", onboarding_plans=[{
            "id": "technical", "title": "Технический трек",
            "current_step": {"id": "vpn-step", "title": "Настроить VPN"},
        }])
    assert response.status_code == 200, response.text
    assert queries[-1] == "ДМС"
    assert [source["id"] for source in response.json()["sources"]] == ["dms-0"]


@pytest.mark.parametrize("path", ["/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
def test_chat_onboarding_plans_reach_model_and_refined_search(settings, monkeypatch, use_mock_http, path):
    plans = [
        {"id": "technical", "title": "Технический трек", "current_step": {
            "id": "vpn-step", "title": "Получить VPN", "resource_id": "vpn", "action_type": "request_access",
        }},
        {"id": "company", "title": "Знакомство с компанией", "current_step": {
            "id": "office-step", "title": "Получить пропуск", "action_type": "manual",
        }},
        {"id": "team", "title": "Знакомство с командой", "current_step": None},
    ]
    queries = []

    async def search(self, query, actor):
        assert actor.model_dump() == ACTOR
        queries.append(query)
        return search_hits("vpn") if query == "Получить VPN vpn" else SearchResult(hits=[])

    def unexpected_http(request):
        pytest.fail("Plan snapshots must not trigger calls to Onboarding or User")

    monkeypatch.setattr(Retriever, "search", search)
    use_mock_http(unexpected_http)
    model = ScriptedModel(
        call("find_instruction", query="Получить VPN vpn"),
        answer("Инструкция по VPN. [source:vpn-0]"),
    )
    question = "Как получить доступ в техническом треке?"
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post(path, headers=AUTH, json={
            "actor": ACTOR, "message": question, "onboarding_plans": plans,
        })
    assert response.status_code == 200, response.text
    assert queries == ["Получить VPN vpn"]
    context = first_context(model)
    assert context["onboarding_plans"] == [
        plans[0],
        {**plans[1], "current_step": {**plans[1]["current_step"], "resource_id": None}},
        plans[2],
    ]
    assert "current_step" not in context
    assert [source["id"] for source in response.json()["sources"]] == ["vpn-0"]
    assert set(response.json()) == {"answer", "sources"}


@pytest.mark.parametrize("optional_fields", [{}, {"resource_id": "", "action_type": ""},
                                         {"resource_id": "vpn", "action_type": "request_access"}])
def test_single_active_plan_reaches_planner_before_search(settings, monkeypatch, optional_fields):
    queries = []

    async def embed(self, texts):
        queries.extend(texts)
        return [EMBEDDING]

    monkeypatch.setattr(Embeddings, "embed", embed)
    step = {"id": "vpn-step", "title": "Получить VPN", **optional_fields}
    plans = [
        {"id": "done", "title": "Компания", "current_step": None},
        {"id": "technical", "title": "Технический трек", "current_step": step},
    ]
    model = searched_model(answer("Инструкция. [source:demo-dev-environment]"), query="Получить VPN vpn-step")
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Как выполнить этот шаг?", onboarding_plans=plans)
    assert response.status_code == 200, response.text
    assert queries == ["Получить VPN vpn-step"]
    assert "selected_onboarding_plan_id" not in first_context(model)
    context = first_context(model)
    assert context["onboarding_plans"][1]["current_step"] == {
        **step, "resource_id": optional_fields.get("resource_id") or None,
        "action_type": optional_fields.get("action_type") or None,
    }
    assert "current_step" not in context


def test_model_can_clarify_multiple_active_plans_after_search(settings, monkeypatch):
    def unexpected_work(*args, **kwargs):
        pytest.fail("Ambiguous step question reached embeddings")

    plans = [
        {"id": "technical", "title": "Технический трек", "current_step": {"id": "vpn", "title": "VPN"}},
        {"id": "company", "title": "Компания", "current_step": {"id": "pass", "title": "Пропуск"}},
    ]
    clarification = "О каком треке адаптации идёт речь?"
    model = searched_model(answer(clarification, kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Как выполнить этот шаг?", onboarding_plans=plans)
    assert response.status_code == 200, response.text
    assert response.json() == {"answer": clarification, "sources": []}
    assert len(model.requests) == 2


def test_onboarding_plans_are_not_reused_and_empty_or_finished_plans_leave_search_unchanged(settings, monkeypatch):
    queries = []

    async def embed(self, texts):
        queries.extend(texts)
        return [EMBEDDING]

    monkeypatch.setattr(Embeddings, "embed", embed)
    snapshots = [
        [{"id": "tech", "title": "Технический трек", "current_step": {"id": "vpn", "title": "VPN"}}],
        [{"id": "tech", "title": "Технический трек", "current_step": None}],
        [],
        None,
    ]
    model = ScriptedModel(*[output for _ in snapshots for output in (
        call("find_instruction", query=QUESTION), answer("Инструкция. [source:demo-dev-environment]"),
    )])
    with TestClient(create_app(settings, model=model)) as client:
        for plans in snapshots:
            fields = {} if plans is None else {"onboarding_plans": plans}
            response = ask(client, QUESTION, conversation_id="same-chat", **fields)
            assert response.status_code == 200, response.text
    assert queries == [QUESTION] * 4
    assert first_context(model, 2)["onboarding_plans"] == snapshots[1]
    assert all("onboarding_plans" not in first_context(model, i) for i in (4, 6))


@pytest.mark.parametrize("plans", [
    None,
    {},
    [{}],
    [{"id": "invalid id", "title": "Трек"}],
    [{"id": "tech", "title": " "}],
    [{"id": "tech", "title": "A" * 257}],
    [{"id": "tech", "title": "\ud800"}],
    [{"id": "tech", "title": "Трек", "role": "mentor"}],
    [{"id": "tech", "title": "Трек", "current_step": {"id": "vpn"}}],
    [{"id": "tech", "title": "Трек", "current_step": {"id": "vpn", "title": "VPN", "action_type": 42}}],
    [{"id": "tech", "title": "Трек", "current_step": {"id": "vpn", "title": "VPN", "status": "done"}}],
])
def test_invalid_onboarding_plans_are_rejected_before_dependencies(settings, monkeypatch, plans):
    def unexpected_work(*args, **kwargs):
        pytest.fail("Invalid onboarding plans reached dependencies")

    monkeypatch.setattr(Embeddings, "embed", unexpected_work)
    model = ScriptedModel()
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post(
            "/api/v1/answer", headers={**AUTH, "Content-Type": "application/json"},
            content=json.dumps({"actor": ACTOR, "message": QUESTION, "onboarding_plans": plans}, ensure_ascii=True),
        )
    assert response.status_code == 422, response.text
    assert response.json()["detail"][0]["loc"][:2] == ["body", "onboarding_plans"]
    assert model.requests == []


@pytest.mark.parametrize("citation", ["technical", "vpn-step", "vpn-resource", "request_access"])
def test_onboarding_plan_metadata_cannot_be_cited_as_sources(settings, citation):
    model = searched_model(answer(f"Инструкция. [source:{citation}]"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, onboarding_plans=[{
            "id": "technical", "title": "Технический трек", "current_step": {
                "id": "vpn-step", "title": "VPN", "resource_id": "vpn-resource", "action_type": "request_access",
            },
        }])
    assert response.status_code == 502
    assert "недоступный источник" in response.json()["detail"]


ONBOARDING_TRACKS = [
    {"id": "technical", "title": "Технический трек", "current_step": {
        "id": "vpn-step", "title": "Настроить VPN", "resource_id": "vpn",
    }},
    {"id": "company", "title": "Компания", "current_step": {
        "id": "pass-step", "title": "Получить пропуск", "resource_id": "office-pass",
    }},
]


@pytest.mark.parametrize("question", [
    "Как выполнить этот шаг?", "Помоги с этим шагом", "Как завершить текущий этап?",
    "Как выполнить шаг?", "Что дальше?", "Что мне делать дальше?", "Продолжим",
    "Как мне его сделать?",
    "Помоги выполнить шаг", "Нужна помощь с выполнением шага",
    "Как выполнить этот шаг: VPN или office-pass?", "Этот шаг про avpn?",
])
def test_model_can_clarify_ambiguous_step_references_after_search(settings, monkeypatch, question):
    def unexpected_work(*args, **kwargs):
        pytest.fail("Ambiguous step reference reached search")

    model = searched_model(answer("О каком треке адаптации идёт речь?", kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, question, onboarding_plans=ONBOARDING_TRACKS)
    assert response.status_code == 200, response.text
    assert response.json() == {"answer": "О каком треке адаптации идёт речь?", "sources": []}
    assert len(model.requests) == 2


@pytest.mark.parametrize(("question", "history", "selected"), [
    ("Этот шаг в technical", [], "technical"),
    ("Как выполнить этот шаг — Технический трек?", [], "technical"),
    ("Как выполнить этот шаг в техническом треке?", [], "technical"),
    ("Помоги выполнить шаг технического трека", [], "technical"),
    ("Помоги с шагом компании", [{"role": "user", "content": "VPN"}], "company"),
    ("Этот шаг: настроить VPN", [], "technical"),
    ("Как выполнить этот шаг для VPN?", [], "technical"),
    ("Как выполнить этот шаг для office-pass?", [{"role": "user", "content": "VPN"}], "company"),
    ("Как выполнить этот шаг?", [{"role": "user", "content": "Мне нужен VPN"}], "technical"),
    ("Что дальше?", [
        {"role": "user", "content": "Хочу получить пропуск"},
        {"role": "assistant", "content": "Инструкция. [source:demo-dev-environment]",
         "source_ids": ["demo-dev-environment"]},
        {"role": "user", "content": "Как выполнить этот шаг?"},
    ], "company"),
])
def test_explicit_track_choices_and_step_followups_reach_model(settings, monkeypatch, question, history, selected):
    step = next(plan["current_step"] for plan in ONBOARDING_TRACKS if plan["id"] == selected)
    query = f'{question} Текущий шаг: {step["title"]} ({step["id"]})'
    queries = mock_search_results(monkeypatch, {query: search_hits("guide")})
    model = searched_model(answer("Инструкция. [source:guide-0]"), query=query)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, question, history=history, onboarding_plans=ONBOARDING_TRACKS)
    assert response.status_code == 200, response.text
    assert queries == [query]
    assert "selected_onboarding_plan_id" not in first_context(model)


@pytest.mark.parametrize("history", [
    [{"role": "user", "content": "VPN"}, {"role": "user", "content": "Как оформить отпуск?"}],
    [{"role": "assistant", "content": "VPN"}],
    [{"role": "user", "content": "VPN"},
     {"role": "assistant", "content": "О каком треке адаптации идёт речь?"}],
    [{"role": "user", "content": "VPN и office-pass"}],
])
def test_model_can_clarify_unresolved_or_outdated_history(settings, history):
    model = searched_model(answer("О каком треке адаптации идёт речь?", kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Как выполнить этот шаг?", history=history, onboarding_plans=ONBOARDING_TRACKS)
    assert response.status_code == 200, response.text
    assert response.json() == {"answer": "О каком треке адаптации идёт речь?", "sources": []}
    assert len(model.requests) == 2


def test_model_can_clarify_request_for_another_track(settings):
    model = searched_model(answer("О каком треке адаптации идёт речь?", kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Как выполнить этот шаг в другом треке?", onboarding_plans=ONBOARDING_TRACKS,
                       history=[{"role": "user", "content": "VPN"}])
    assert response.status_code == 200, response.text
    assert response.json()["answer"] == "О каком треке адаптации идёт речь?"
    assert len(model.requests) == 2


@pytest.mark.parametrize("question", [
    "Как выполнить этот шаг в новом треке analytics?",
    "Как выполнить этот шаг в аналитическом треке?",
    "Как выполнить этот шаг для unknown-resource?",
])
@pytest.mark.parametrize("plans", [ONBOARDING_TRACKS[:1], ONBOARDING_TRACKS])
def test_model_receives_unknown_destination_and_filtered_history(settings, monkeypatch, question, plans):
    def unexpected_work(*args, **kwargs):
        pytest.fail("Unknown destination reached dependencies")

    model = searched_model(answer("О каком треке адаптации идёт речь?", kind="clarification"))
    history = [
        {"role": "user", "content": "Мне нужен VPN"},
        {"role": "assistant", "content": "Инструкция. [source:demo-dev-environment]",
         "source_ids": ["demo-dev-environment"]},
    ]
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, question, onboarding_plans=plans, history=history)
    assert response.status_code == 200, response.text
    assert response.json() == {"answer": "О каком треке адаптации идёт речь?", "sources": []}
    assert len(model.requests) == 2


def test_model_can_clarify_unresolved_destination_in_history(settings):
    model = searched_model(answer("О каком треке адаптации идёт речь?", kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Как выполнить этот шаг?", onboarding_plans=ONBOARDING_TRACKS, history=[
            {"role": "user", "content": "VPN"},
            {"role": "user", "content": "Как выполнить этот шаг в новом треке analytics?"},
        ])
    assert response.status_code == 200, response.text
    assert response.json()["answer"] == "О каком треке адаптации идёт речь?"
    assert len(model.requests) == 2






@pytest.mark.parametrize("question", [
    "Какой у меня шаг в треке analytics?", "Какой шаг в аналитическом треке?", "Какой шаг для unknown?",
])
def test_model_can_clarify_unknown_track_in_state_question(settings, question):
    model = searched_model(answer("О каком треке адаптации идёт речь?", kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, question, onboarding_plans=ONBOARDING_TRACKS,
                       history=[{"role": "user", "content": "VPN"}])
    assert len(model.requests) == 2
    assert response.status_code == 200, response.text
    assert response.json() == {"answer": "О каком треке адаптации идёт речь?", "sources": []}






def test_snapshot_answers_still_require_service_auth_and_active_internship():
    with TestClient(create_app(Settings(service_token="chat-test-token"))) as client:
        unauthorized = client.post("/api/v1/answer", json={
            "actor": ACTOR, "message": "Какой у меня текущий шаг?", "onboarding_plans": ONBOARDING_TRACKS,
        })
        ended = ask(client, "Какой у меня текущий шаг?", {**ACTOR, "actual_end_date": "2026-01-01"},
                    onboarding_plans=ONBOARDING_TRACKS)
    assert unauthorized.status_code == 401
    assert ended.status_code == 422




@pytest.mark.parametrize("question", [
    "Как подключиться на текущем шаге?", "Покажи инструкцию к текущему шагу",
    "Какой у меня текущий шаг и как его выполнить?", "Какие доступы нужны на текущем шаге?",
    "Какой у меня текущий шаг и где находится офис?", "Где находится офис и какой у меня текущий шаг?",
    "Помоги выполнить текущий шаг, только не VPN",
])
def test_instruction_requests_are_not_replaced_with_snapshot_summary(settings, monkeypatch, question):
    query = f"{question} Текущий шаг: Настроить VPN (vpn-step)"
    queries = mock_search_results(monkeypatch, {query: search_hits("guide")})
    model = searched_model(answer("Инструкция. [source:guide-0]"), query=query)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, question, onboarding_plans=ONBOARDING_TRACKS[:1])
    assert response.status_code == 200, response.text
    assert queries == [query]
    assert response.json()["answer"] == "Инструкция. [source:guide-0]"
    assert model.requests[0][1][-1] == {"role": "user", "content": question}


@pytest.mark.parametrize("track_name", ["backend", "бекенд", "бэкенда"])
def test_backend_step_instruction_searches_for_environment_setup(settings, monkeypatch, track_name):
    plans = [
        {"id": "backend", "title": "Backend", "current_step": {
            "id": "dev-environment", "title": "Настрой рабочее окружение",
        }},
        ONBOARDING_TRACKS[1],
    ]
    question = f"Как выполнить текущий шаг в треке {track_name}?"
    query = f"{question} Текущий шаг: Настрой рабочее окружение (dev-environment)"
    source = SearchHit(id="synth-software-dev-environment", title="Настройка окружения", text="Запусти сервис по README.")
    queries = mock_search_results(monkeypatch, {query: SearchResult(hits=[source])})
    model = searched_model(answer("Запусти сервис по README. [source:synth-software-dev-environment]"), query=query)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, question, onboarding_plans=plans)
    assert response.status_code == 200, response.text
    assert queries == [query]
    assert "selected_onboarding_plan_id" not in first_context(model)
    assert [hit["id"] for hit in response.json()["sources"]] == ["synth-software-dev-environment"]


def test_model_can_clarify_shared_resource(settings):
    plans = [
        {**plan, "current_step": {**plan["current_step"], "resource_id": "shared"}}
        for plan in ONBOARDING_TRACKS
    ]
    model = searched_model(answer("О каком треке адаптации идёт речь?", kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Как выполнить этот шаг для shared?", onboarding_plans=plans)
    assert response.status_code == 200, response.text
    assert response.json()["answer"] == "О каком треке адаптации идёт речь?"
    assert len(model.requests) == 2


def test_explicit_track_disambiguates_shared_resource(settings):
    plans = [
        {**plan, "current_step": {**plan["current_step"], "resource_id": "shared"}}
        for plan in ONBOARDING_TRACKS
    ]
    model = searched_model(answer("Инструкция. [source:demo-dev-environment]"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Как выполнить этот шаг в company для shared?", onboarding_plans=plans)
    assert response.status_code == 200, response.text
    assert "selected_onboarding_plan_id" not in first_context(model)


@pytest.mark.parametrize("plans", [[], ONBOARDING_TRACKS[:1], ONBOARDING_TRACKS])
@pytest.mark.parametrize("question", ["Как оформить отпуск?", "Как выполнить шаг в GitLab CI?", "Какие есть треки?"])
def test_unrelated_questions_search_only_the_question(settings, monkeypatch, plans, question):
    queries = mock_search_results(monkeypatch, {question: search_hits("leave")})
    model = searched_model(answer("Инструкция по отпуску. [source:leave-0]"), query=question)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, question, onboarding_plans=plans)
    assert response.status_code == 200, response.text
    assert queries == [question]
    assert "selected_onboarding_plan_id" not in first_context(model)


@pytest.mark.parametrize("overflow", ["count", "bytes", "escaped_bytes"])
def test_oversized_onboarding_plans_are_rejected_before_dependencies(settings, monkeypatch, overflow):
    from ai_service.api.schemas import MAX_ONBOARDING_PLANS

    count = MAX_ONBOARDING_PLANS + 1 if overflow == "count" else MAX_ONBOARDING_PLANS
    title = {"count": "Трек", "bytes": "😀" * 256, "escaped_bytes": "\\" * 256}[overflow]
    plans = [{"id": f"track-{index}", "title": title, "current_step": {
        "id": f"step-{index}", "title": title,
    }} for index in range(count)]

    def unexpected_work(*args, **kwargs):
        pytest.fail("Oversized onboarding plans reached dependencies")

    monkeypatch.setattr(Embeddings, "embed", unexpected_work)
    model = ScriptedModel()
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, onboarding_plans=plans)
    assert response.status_code == 422, response.text
    detail = response.json()["detail"][0]
    assert detail["loc"] == ["body", "onboarding_plans"]
    if overflow == "count":
        assert detail["type"] == "too_long"
    else:
        assert "128 КиБ" in detail["msg"]
    assert model.requests == []


def test_large_supported_plans_and_history_fit_existing_model_context(settings):
    from ai_service.agents.agent import MAX_CONTEXT_BYTES, json_size
    from ai_service.api.schemas import MAX_ONBOARDING_PLANS

    plans = [{"id": f"track-{index}", "title": "Т" * 256, "current_step": {
        "id": f"step-{index}", "title": "S" * 256, "resource_id": "vpn", "action_type": "request_access",
    }} for index in range(MAX_ONBOARDING_PLANS)]
    history = [{"role": "user", "content": f"{index}:" + "Ж" * 6000} for index in range(30)]
    model = ScriptedModel(
        call("find_instruction", query="Инструкция по окружению"),
        answer("Инструкция. [source:demo-dev-environment]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, onboarding_plans=plans, history=history)
    assert response.status_code == 200, response.text
    assert first_context(model)["onboarding_plans"] == plans
    assert previous_messages(model)
    assert len(previous_messages(model)) < len(history)
    for instructions, messages, tools in model.requests:
        assert json_size([instructions, messages, tools]) <= MAX_CONTEXT_BYTES


def previous_messages(model, request_index=0):
    block = model.requests[request_index][1][0]
    assert block["content"].startswith("Предыдущие сообщения")
    return json.loads(block["content"].split("\n", 1)[1])


@pytest.mark.parametrize(("selection", "query", "source"), [
    ("Первый вариант", "получение VPN", "vpn-guide"),
    ("Второй вариант", "получение офисного пропуска", "pass-guide"),
])
def test_clarification_survives_next_request_without_sources(settings, monkeypatch, selection, query, source):
    question = "Мне нужен доступ"
    clarification = "Уточните: VPN или офисный пропуск?"
    queries = mock_search_results(monkeypatch, {
        question: SearchResult(hits=[]), selection: SearchResult(hits=[]),
        query: SearchResult(hits=[SearchHit(id=source, title="Инструкция", text="Создайте заявку.")]),
    })
    model = ScriptedModel(
        call("find_instruction", call_id="initial", query=question),
        answer(clarification, kind="clarification"),
        call("find_instruction", query=query),
        answer(f"Создайте заявку. [source:{source}]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        first = ask(client, question, conversation_id="access-chat")
        assert first.status_code == 200, first.text
        assert first.json() == {"answer": clarification, "sources": []}
        history = [
            {"role": "user", "content": question},
            {"role": "assistant", "content": first.json()["answer"],
             "source_ids": [hit["id"] for hit in first.json()["sources"]]},
        ]
        second = ask(client, selection, history=history, conversation_id="access-chat")
    assert second.status_code == 200, second.text
    assert previous_messages(model, request_index=2) == [
        {**history[0], "source_ids": []}, history[1],
    ]
    assert queries == [question, query]
    assert [hit["id"] for hit in second.json()["sources"]] == [source]


def test_free_clarification_history_is_context_not_current_evidence(settings):
    text = "Какой ресурс вы имели в виду: Wiki или VPN?"
    model = searched_model(answer("Уточните нужный ресурс.", kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Первый вариант", history=[{"role": "assistant", "content": text}])
    assert response.status_code == 200
    assert previous_messages(model) == [{"role": "assistant", "content": text, "source_ids": []}]
    assert "не источники фактов" in model.requests[0][1][0]["content"]
    assert response.json()["sources"] == []


@pytest.mark.parametrize("path", ["/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
@pytest.mark.parametrize("citation", ["fresh-vpn-duration", "demo-dev-environment"])
def test_follow_up_uses_history_and_requires_sources_from_current_search(settings, monkeypatch, path, citation):
    from ai_service.agents.agent import MAX_CONTEXT_BYTES, json_size

    history = [
        {"role": "user", "content": "Как получить VPN?"},
        {"role": "assistant", "content": "Создайте заявку. [source:demo-dev-environment]",
         "source_ids": ["demo-dev-environment"]},
    ]
    query = "срок выдачи доступа к VPN"
    queries = []

    async def search(self, text, actor):
        assert actor.model_dump() == ACTOR
        queries.append(text)
        return SearchResult(hits=[SearchHit(
            id="fresh-vpn-duration", title="Выдача VPN", text="Доступ выдаётся за два рабочих дня.",
        )]) if text == query else SearchResult(hits=[])

    monkeypatch.setattr(Retriever, "search", search)
    model = ScriptedModel(call("find_instruction", query=query), answer(f"Два рабочих дня. [source:{citation}]"))
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post(path, headers=AUTH, json={
            "actor": ACTOR, "conversation_id": "vpn-chat", "history": history, "message": "А сколько ждать?",
        })
    assert queries == [query]
    assert [item["content"] for item in previous_messages(model)] == [item["content"] for item in history]
    assert model.requests[0][1][-1] == {"role": "user", "content": "А сколько ждать?"}
    assert tool_output(model)["hits"][0]["id"] == "fresh-vpn-duration"
    for instructions, messages, tools in model.requests:
        assert json_size([instructions, messages, tools]) <= MAX_CONTEXT_BYTES
    if citation == "fresh-vpn-duration":
        assert response.status_code == 200, response.text
        assert [source["id"] for source in response.json()["sources"]] == ["fresh-vpn-duration"]
    else:
        assert response.status_code == 502
        assert "недоступный источник" in response.json()["detail"]


def test_long_input_history_is_windowed_and_never_cached_between_requests(settings):
    history = [{"role": "user", "content": f"Предыдущий вопрос {index}"} for index in range(60)]
    model = ScriptedModel(
        call("find_instruction", query="Текущий вопрос"),
        answer("Что именно нужно уточнить?", kind="clarification"),
        call("find_instruction", query=QUESTION), answer("Инструкция. [source:demo-dev-environment]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        first = ask(client, "Текущий вопрос", history=history, conversation_id="same-chat")
        second = ask(client, QUESTION, conversation_id="same-chat")
    assert first.status_code == second.status_code == 200
    assert [item["content"] for item in previous_messages(model)] == [item["content"] for item in history[-30:]]
    assert model.requests[0][1][-1]["content"] == "Текущий вопрос"
    assert len(model.requests[2][1]) == 2
    assert "Предыдущий вопрос" not in json.dumps(model.requests[2], ensure_ascii=False)


def test_history_yields_to_question_and_sources_within_total_context_budget(settings, monkeypatch):
    from importlib.resources import files
    from ai_service.agents.agent import Agent, FIND_INSTRUCTION_TOOL, json_size

    history = [{"role": "user", "content": "Ж" * 4000} for _ in range(8)]
    # A deliberately small overall budget checks that optional history is evicted first.
    prompt = files("ai_service.agents").joinpath("prompts", "system.txt").read_text(encoding="utf-8")
    budget = json_size([Agent(None, None, prompt).prompt, [FIND_INSTRUCTION_TOOL]]) + 4 * 1024
    monkeypatch.setattr("ai_service.agents.agent.MAX_CONTEXT_BYTES", budget)
    model = searched_model(answer("Инструкция. [source:demo-dev-environment]"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, history=history)
    assert response.status_code == 200, response.text
    instructions, messages, tools = model.requests[0]
    assert json_size([instructions, messages, tools]) <= budget
    assert messages[-1]["content"] == QUESTION
    assert tool_output(model)["hits"] == response.json()["sources"]


def test_history_access_failure_stops_before_embeddings_and_model(settings, monkeypatch):
    from ai_service.repositories.knowledge import KnowledgeError

    async def inaccessible(*args, **kwargs):
        raise KnowledgeError("Проверка истории недоступна.", reason="connection")

    async def unexpected_embed(*args, **kwargs):
        pytest.fail("Unverified history must not reach embeddings or the model")

    monkeypatch.setattr(Retriever, "accessible_source_ids", inaccessible)
    monkeypatch.setattr(Embeddings, "embed", unexpected_embed)
    model = ScriptedModel()
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, history=[{
            "role": "assistant", "content": "Закрытое содержимое.", "source_ids": ["source"],
        }])
    assert response.status_code == 502
    assert model.requests == []


def test_source_free_history_with_unverifiable_citations_is_not_forwarded(settings):
    model = searched_model(answer("Какой доступ нужен?", kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, history=[{
            "role": "assistant", "content": "Старая закрытая инструкция. [source:closed]",
        }])
    assert response.status_code == 200
    assert len(model.requests[0][1]) == 2
    assert "Старая закрытая инструкция" not in json.dumps(model.requests, ensure_ascii=False)


def test_long_assistant_history_is_accepted_without_question_length_limit(settings):
    text = "x" * 5000 + " [source:demo-dev-environment]"
    model = searched_model(answer("Что нужно уточнить?", kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, history=[{
            "role": "assistant", "content": text, "source_ids": ["demo-dev-environment"],
        }])
    assert response.status_code == 200, response.text
    assert previous_messages(model)[0]["content"] == text


def test_expanded_history_and_request_budgets_accept_large_conversation(settings):
    from ai_service.agents.agent import MAX_CONTEXT_BYTES, json_size
    from ai_service.rag.history import MAX_HISTORY_BYTES

    history = [{"role": "user", "content": f"{index:02}" + "Ж" * 6000} for index in range(30)]
    body = json.dumps({"actor": ACTOR, "message": QUESTION, "history": history}, ensure_ascii=False).encode()
    assert len(body) > 256 * 1024
    model = searched_model(answer("Что именно нужно уточнить?", kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post(
            "/api/v1/answer", headers={**AUTH, "Content-Type": "application/json"}, content=body,
        )
    assert response.status_code == 200, response.text
    selected = previous_messages(model)
    assert len(selected) > 8
    assert [item["content"] for item in selected] == [item["content"] for item in history[-len(selected):]]
    instructions, messages, tools = model.requests[0]
    assert 16 * 1024 < json_size(messages[0]) <= MAX_HISTORY_BYTES
    assert json_size([instructions, messages, tools]) <= MAX_CONTEXT_BYTES


@pytest.mark.parametrize("resource", ["https://git.example.test/team/repo", "git-guide", "https://git.example.test/invented/repo"])
def test_access_proposal_matches_chat_contract_and_cited_policy(settings, monkeypatch, resource):
    from ai_service.schemas.contracts import AccessPolicy
    from conversation_examples import REPOSITORY_CLARIFICATION

    target = "https://git.example.test/team/repo"
    hit = SearchHit(id="git-guide", title="Git", text="Запросите Developer.", access_policies=[
        AccessPolicy(repository_host="git.example.test", resource_label="Git", access_levels=["Developer"]),
    ])
    mock_search_results(monkeypatch, {"доступ Git": SearchResult(hits=[hit])})
    model = searched_model(answer("Подтвердите заявку в интерфейсе. [source:git-guide]", proposal={
        "resource_id": resource, "resource_label": "Git", "access_level": "Developer",
    }), answer(REPOSITORY_CLARIFICATION, kind="clarification"), query="доступ Git")
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, f"Как получить доступ к {target}?")
    assert response.status_code == 200, response.text
    if resource == target:
        assert response.json()["proposal"] == {
            "resource_id": target, "resource_label": "Репозиторий team/repo", "access_level": "Developer",
        }
        assert "access_policies" not in response.json()["sources"][0]
        assert tool_output(model)["hits"][0]["access_policies"][0]["access_levels"] == ["Developer"]
    else:
        assert response.json() == {"answer": REPOSITORY_CLARIFICATION, "sources": []}


@pytest.mark.parametrize("kind", ["clarification", "no_knowledge"])
def test_non_grounded_answer_cannot_carry_access_proposal(settings, kind):
    model = searched_model(answer(DEFAULT_CLARIFICATION, kind=kind, proposal={
        "resource_id": "repo", "resource_label": "Git", "access_level": "read",
    }))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Как получить доступ к repo?")
    assert response.status_code == 502
    assert "proposal" not in response.json()


def test_uncited_policy_cannot_authorize_proposal(settings, monkeypatch):
    from ai_service.schemas.contracts import AccessPolicy
    target = "https://git.example.test/team/repo"
    mock_search_results(monkeypatch, {"Git": SearchResult(hits=[
        SearchHit(id="cited", title="Git", text="Общие сведения."),
        SearchHit(id="uncited", title="Git policy", text="Developer.", access_policies=[
            AccessPolicy(repository_host="git.example.test", resource_label="Git", access_levels=["Developer"]),
        ]),
    ])})
    model = searched_model(answer("Предложение. [source:cited]", proposal={
        "resource_id": target, "resource_label": "Git", "access_level": "Developer",
    }), answer("К какому ресурсу вам нужен доступ?", kind="clarification"), query="Git")
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, f"Нужен доступ к {target}")
    assert response.status_code == 200
    assert "proposal" not in response.json()


@pytest.mark.parametrize("question", [
    "Какой у меня текущий шаг и где находится офис?",
    "Где находится офис и какой у меня текущий шаг?",
    "Помоги выполнить текущий шаг, только не VPN",
    "Как выполнить этот шаг в новом треке analytics?",
])
def test_complete_question_reaches_llm_before_any_search(settings, monkeypatch, question):
    query = "Поисковый запрос, сформулированный моделью"
    queries = mock_search_results(monkeypatch, {query: search_hits("guide")})

    class InspectingModel(ScriptedModel):
        async def respond(self, *, instructions, messages, tools):
            if not self.requests:
                assert queries == []
                assert messages[-1] == {"role": "user", "content": question}
                context = json.loads(messages[-2]["content"].split("\n", 1)[1])
                assert context == {"onboarding_plans": [
                    {**plan, "current_step": {**plan["current_step"], "action_type": None}}
                    for plan in ONBOARDING_TRACKS
                ]}
            return await super().respond(instructions=instructions, messages=messages, tools=tools)

    # This checks routing and preservation of input; semantic accuracy needs a real LLM eval.
    text = "Инструкция по вопросу. [source:guide-0]"
    model = InspectingModel(call("find_instruction", query=query), answer(text))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, question, onboarding_plans=ONBOARDING_TRACKS)
    assert response.status_code == 200, response.text
    assert response.json()["answer"] == text
    assert queries == [query]
    assert len(model.requests) == 2


def test_state_questions_require_configured_agent():
    with TestClient(create_app(Settings(service_token="chat-test-token"))) as client:
        response = ask(client, "Какой у меня текущий шаг?", onboarding_plans=ONBOARDING_TRACKS)
    assert response.status_code == 503










@pytest.mark.parametrize("plan_id", ["technical", "invented", "done", "duplicate"])
@pytest.mark.parametrize("with_policy", [True, False])
def test_model_selected_step_proposal_still_requires_valid_plan_and_cited_policy(settings, monkeypatch, plan_id, with_policy):
    from ai_service.schemas.contracts import AccessPolicy

    policies = [AccessPolicy(resource_id="vpn", resource_label="VPN", access_levels=["read"])] if with_policy else []
    mock_search_results(monkeypatch, {"доступ": SearchResult(hits=[
        SearchHit(id="guide", title="Инструкция", text="Получение доступа.", access_policies=policies),
    ])})
    payload = {
        "kind": "grounded_answer", "answer": "Подтвердите заявку в интерфейсе. [source:guide]",
        "access_request": {"onboarding_plan_id": plan_id, "policy_source_id": "guide", "policy_index": 0, "target_status": "selected",
                           "level_status": "explicit", "requested_level": "read"},
    }
    final = [{"type": "message", "content": [{"type": "output_text", "text": json.dumps(payload)}]}]
    model = ScriptedModel(call("find_instruction", query="доступ"), final,
                          answer("К какому ресурсу вам нужен доступ?", kind="clarification"))
    plans = [*ONBOARDING_TRACKS, {"id": "done", "title": "Без шага"},
             {**ONBOARDING_TRACKS[0], "id": "duplicate"}, {**ONBOARDING_TRACKS[1], "id": "duplicate"}]
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Как получить доступ на текущем шаге?", onboarding_plans=plans)
    if plan_id != "technical":
        assert response.status_code == 502, response.text
    elif with_policy:
        assert response.status_code == 200, response.text
        assert response.json()["proposal"] == {"resource_id": "vpn", "resource_label": "VPN", "access_level": "read"}
        assert set(response.json()) == {"answer", "sources", "proposal"}
    else:
        assert response.status_code == 200, response.text
        assert "proposal" not in response.json()


def test_compound_question_can_answer_state_and_office_after_search(settings, monkeypatch):
    question = "Какой у меня текущий шаг и где находится офис?"
    queries = mock_search_results(monkeypatch, {"расположение офиса": SearchResult(hits=[
        SearchHit(id="office", title="Офис", text="Офис находится на улице Примерной, 1."),
    ])})
    text = "Текущий шаг — Настроить VPN. Офис находится на улице Примерной, 1. [source:office]"
    model = searched_model(answer(text), query="расположение офиса")
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, question, onboarding_plans=ONBOARDING_TRACKS[:1])
    assert response.status_code == 200, response.text
    assert queries == ["расположение офиса"]
    assert model.requests[0][1][-1]["content"] == question
    assert response.json()["answer"] == text
    assert [source["id"] for source in response.json()["sources"]] == ["office"]




@pytest.mark.parametrize(("kind", "proposal"), [
    ("clarification", None), ("no_knowledge", None), ("grounded_answer", None),
    ("clarification", {"resource_id": "vpn", "resource_label": "VPN", "access_level": "read"}),
])
def test_plan_metadata_cannot_authorize_a_non_proposal_route(settings, kind, proposal):
    payload = {"kind": kind, "answer": DEFAULT_CLARIFICATION, "onboarding_plan_id": "technical"}
    if proposal is not None:
        payload["access_request"] = {"policy_source_id": "demo-dev-environment", "policy_index": 0,
                                     "target_status": "selected", "level_status": "explicit", "requested_level": "read"}
    final = [{"type": "message", "content": [{"type": "output_text", "text": json.dumps(payload)}]}]
    model = searched_model(final)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, onboarding_plans=ONBOARDING_TRACKS)
    assert response.status_code == 502
