import asyncio
import json

import httpx
import psycopg
import pytest
from fastapi.testclient import TestClient

from ai_service.agents.agent import NO_KNOWLEDGE
from ai_service.clients.embeddings import EMBEDDING_DIMENSIONS, EmbeddingError, Embeddings
from ai_service.clients.llm import ModelError, ModelResponse
from ai_service.core.config import Settings
from ai_service.main import create_app
from ai_service.rag.retriever import Retriever
from ai_service.repositories.knowledge import KnowledgeRepository
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


def call(name, *, call_id=None, **arguments):
    return [{
        "type": "function_call",
        "call_id": call_id or f"call-{name}",
        "name": name,
        "arguments": json.dumps(arguments),
    }]


def answer(text, *, kind="grounded_answer"):
    final = json.dumps({"kind": kind, "answer": text}, ensure_ascii=False)
    return [{"type": "message", "content": [{"type": "output_text", "text": final}]}]


@pytest.fixture
def settings(monkeypatch):
    async def embed(self, texts):
        assert len(texts) == 1
        return [EMBEDDING]

    async def search(self, vector, profile, actor, min_similarity):
        assert len(profile) == 64
        assert vector == EMBEDDING
        assert min_similarity == 0.3
        rows = {
            ("intern", "backend"): ["demo-dev-environment"],
            ("intern", "51"): ["demo-dev-environment"],
            ("mentor", "backend"): ["demo-mentor-environment", "demo-dev-environment"],
        }.get((actor.role, actor.team), [])
        return SearchResult(hits=[
            SearchHit(id=key, title=key, text="Фрагмент инструкции") for key in rows
        ])

    monkeypatch.setattr(Embeddings, "embed", embed)
    monkeypatch.setattr(KnowledgeRepository, "search", search)
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
    return client.post("/api/v1/get_answer", headers=AUTH, json={"actor": actor, "message": message, **extra})


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
    model = ScriptedModel(call("find_instruction", query="окружение"), answer(text))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, actor)
    assert response.status_code == 200, response.text
    assert response.json()["answer"] == (text if ids else NO_KNOWLEDGE)
    assert sorted(source["id"] for source in response.json()["sources"]) == sorted(ids)
    assert sorted(source["id"] for source in first_context(model)["hits"]) == sorted(ids)
    assert sorted(hit["id"] for hit in tool_output(model)["hits"]) == sorted(ids)
    assert set(response.json()) == {"answer", "sources"}
    assert [tool["name"] for tool in model.requests[0][2]] == ["find_instruction"]


def test_get_answer_uses_chat_profile_without_calling_user(settings, monkeypatch, use_mock_http):
    def unexpected_http(request):
        pytest.fail(f"Unexpected outgoing HTTP request: {request.method} {request.url}")

    async def check_ready(self, profile):
        assert len(profile) == 64

    use_mock_http(unexpected_http)
    monkeypatch.setattr(KnowledgeRepository, "check_ready", check_ready)
    model = ScriptedModel(answer("Ответ с первого вызова. [source:demo-dev-environment]"))
    payload = {
        "conversation_id": "123",
        "actor": {"user_id": "42", "team": "51", "role": "intern", "actual_end_date": None},
        "message": QUESTION,
    }
    with TestClient(create_app(settings, model=model)) as client:
        assert client.get("/readyz").status_code == 200
        response = client.post("/api/v1/get_answer", headers=AUTH, json=payload)

    assert response.status_code == 200, response.text
    assert set(response.json()) == {"answer", "sources"}
    assert [source["id"] for source in response.json()["sources"]] == ["demo-dev-environment"]
    assert len(model.requests) == 1
    _, messages, _ = model.requests[0]
    assert messages[-1] == {"role": "user", "content": QUESTION}
    assert set(first_context(model)) == {"hits"}
    assert response.json()["sources"] == first_context(model)["hits"]
    assert "actual_end_date" not in json.dumps(messages)


@pytest.mark.parametrize("path", ["/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
def test_answer_endpoint_and_compatibility_routes(settings, path):
    model = ScriptedModel(answer("Инструкция [source:demo-dev-environment]"))
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post(path, headers=AUTH, json={"actor": ACTOR, "message": QUESTION})
    assert response.status_code == 200, response.text
    assert response.json()["answer"] == "Инструкция [source:demo-dev-environment]"
    assert response.json()["sources"] == first_context(model)["hits"]
    assert response.headers["x-request-id"]


@pytest.mark.parametrize("headers", [{}, {"Authorization": "Bearer wrong-token"}, {"Authorization": "Basic wrong-token"}])
def test_supplied_profile_does_not_bypass_chat_authentication(settings, monkeypatch, headers):
    def unexpected_work(*args, **kwargs):
        pytest.fail("Unauthenticated request reached dependencies")

    monkeypatch.setattr(Embeddings, "embed", unexpected_work)
    model = ScriptedModel()
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post("/api/v1/get_answer", headers=headers, json={"actor": ACTOR, "message": QUESTION})
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


@pytest.mark.parametrize("path", ["/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
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


def test_chat_uses_request_profile_and_excludes_history_after_role_change(settings):
    model = ScriptedModel(
        call("find_instruction", query="окружение"),
        answer("Закрытая инструкция. [source:demo-mentor-environment] [source:demo-dev-environment]"),
        call("find_instruction", query="окружение"), answer("Ответ. [source:demo-dev-environment]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        first = ask(client, QUESTION, {**ACTOR, "role": "mentor"}, conversation_id="conversation-1")
        history = [
            {"role": "assistant", "content": first.json()["answer"]},
            {"role": "user", "content": "Старая цитата из инструкции для наставников."},
        ]
        second = ask(client, QUESTION, conversation_id="conversation-1", history=history)

    assert first.status_code == second.status_code == 200
    for index, response, expected in (
        (0, first, ["demo-mentor-environment", "demo-dev-environment"]),
        (2, second, ["demo-dev-environment"]),
    ):
        context = first_context(model, index)
        assert set(context) == {"hits"}
        assert [source["id"] for source in context["hits"]] == expected
        assert [source["id"] for source in tool_output(model, index + 1)["hits"]] == expected
        assert [source["id"] for source in response.json()["sources"]] == expected
    for _, messages, _ in model.requests[2:]:
        assert all(entry["content"] not in json.dumps(messages, ensure_ascii=False) for entry in history)


def test_missing_request_profile_cannot_reuse_previous_profile(settings, monkeypatch):
    model = ScriptedModel(answer("Ответ наставнику. [source:demo-dev-environment]"),
                          answer("Ответ стажёру. [source:demo-dev-environment]"))
    with TestClient(create_app(settings, model=model)) as client:
        assert ask(client, QUESTION, {**ACTOR, "role": "mentor"}, conversation_id="123").status_code == 200
        with monkeypatch.context() as failure:
            def unexpected_search(*args, **kwargs):
                pytest.fail("Search attempted without a complete profile")

            failure.setattr(Embeddings, "embed", unexpected_search)
            response = ask(client, QUESTION, {"user_id": "42"}, conversation_id="123")
        assert response.status_code == 422
        assert len(model.requests) == 1
        assert ask(client, QUESTION, conversation_id="123").status_code == 200
    assert [hit["id"] for hit in first_context(model, 1)["hits"]] == ["demo-dev-environment"]


def test_knowledge_failure_prevents_llm_call(settings, monkeypatch):
    from ai_service.repositories.knowledge import KnowledgeError

    async def search(*args, **kwargs):
        raise KnowledgeError("Search failed", reason="connection")

    monkeypatch.setattr(KnowledgeRepository, "search", search)
    model = ScriptedModel()
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502
    assert model.requests == []


def test_embedding_failure_prevents_llm_call(settings, monkeypatch):
    async def embed(self, texts):
        raise EmbeddingError("Не удалось получить эмбеддинги.")

    monkeypatch.setattr(Embeddings, "embed", embed)
    model = ScriptedModel()
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502
    assert response.json()["detail"] == "Не удалось получить эмбеддинги."
    assert model.requests == []


def test_wrong_embedding_dimension_returns_gateway_error_before_search(monkeypatch, use_mock_http):
    async def unexpected_connect(*args, **kwargs):
        pytest.fail("Search attempted with an incompatible embedding")

    settings = Settings(
        service_token="chat-test-token", knowledge_processing_approved=True,
        embedding_api_key="test-embedding-token",
        knowledge_db_dsn="postgresql://knowledge.test/knowledge"
    )
    use_mock_http(lambda request: httpx.Response(200, json={"data": [{"index": 0, "embedding": [1, 0]}]}))
    monkeypatch.setattr(psycopg.AsyncConnection, "connect", unexpected_connect)
    model = ScriptedModel()
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)

    assert response.status_code == 502
    assert "размерности 1536" in response.json()["detail"]
    assert model.requests == []


@pytest.mark.parametrize("model_tries_to_write", [False, True])
def test_model_cannot_call_write_tools(settings, model_tries_to_write):
    outputs = [answer("Какой шаг вы хотите подтвердить?", kind="clarification")]
    if model_tries_to_write:
        outputs.insert(0, call("complete_onboarding_step", step_id="dev-environment"))
    model = ScriptedModel(*outputs)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Отметь шаг")
    assert response.status_code == 200, response.text
    assert set(response.json()) == {"answer", "sources"}
    assert [tool["name"] for tool in model.requests[0][2]] == ["find_instruction"]
    if model_tries_to_write:
        assert "недоступен" in tool_output(model)["error"]


@pytest.mark.parametrize("path", ["/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
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
    assert response.json()["detail"] == "Тайм-аут"


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
        assert texts == [QUESTION], "Invalid arguments reached the retriever"
        return [EMBEDDING]

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
    final_output = tool_calls[3] if final_tool_call else answer(text)
    model = ScriptedModel(*tool_calls[:3], final_output)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)

    assert queries == [QUESTION, "поиск 1", "поиск 2", "поиск 3"]
    assert [len(request[2]) for request in model.requests] == [1, 1, 1, 0]
    assert tool_output(model, request_index=3)["hits"][0]["id"] == "demo-dev-environment"
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


@pytest.mark.parametrize("path", ["/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
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


@pytest.mark.parametrize("path", ["/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
@pytest.mark.parametrize("chunked", [False, True])
def test_chat_body_limit(settings, path, chunked):
    chunks = [b" " * (128 * 1024)] * 3
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


def test_chat_discards_unverified_answer_after_empty_search(settings):
    model = ScriptedModel(answer("Внутренний адрес https://invented.example"))
    with TestClient(create_app(settings, model=model)) as client:
        result = ask(client, QUESTION, {**ACTOR, "team": "design"}).json()
    assert result == {"answer": NO_KNOWLEDGE, "sources": []}


@pytest.mark.parametrize("outcome", ["recover", "search-error", "bad-citation", "refusal"])
def test_rag_after_empty_initial_search(settings, monkeypatch, outcome):
    from ai_service.repositories.knowledge import KnowledgeError

    async def embed(self, texts):
        return [[2.0] if texts == ["окружение"] else [1.0]]

    async def search(self, vector, profile, actor, min_similarity):
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
    model = ScriptedModel(call("find_instruction", query="окружение"), outputs)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    if outcome == "recover":
        assert response.status_code == 200
        assert response.json()["sources"][0]["id"] == "visible"
    else:
        assert response.status_code == 502
        assert {"search-error": "Поиск", "bad-citation": "источник", "refusal": "отказалась"}[outcome] in response.json()["detail"]


def test_multiple_tool_calls_are_paired_and_share_normalized_search(settings, monkeypatch):
    queries = []

    async def embed(self, texts):
        queries.extend(texts)
        return [EMBEDDING]

    monkeypatch.setattr(Embeddings, "embed", embed)
    calls = []
    for index, query in enumerate(["  КАК  настроить окружение? ", "настройка", "НАСТРОЙКА", "другой"]):
        calls.extend(call("find_instruction", call_id=f"call-{index}", query=query))
    model = ScriptedModel(calls, answer("Ответ. [source:demo-dev-environment]"))
    with TestClient(create_app(settings, model=model)) as client:
        assert ask(client, QUESTION).status_code == 200
    assert queries == [QUESTION, "настройка"]
    _, messages, tools = model.requests[1]
    assert tools == []
    outputs = [item for item in messages if item.get("type") == "function_call_output"]
    assert [item["call_id"] for item in outputs] == [item["call_id"] for item in calls]
    assert "лимит" in json.loads(outputs[-1]["output"])["error"]


def test_duplicate_call_id_across_rounds(settings):
    same_call = call("find_instruction", query="окружение")
    model = ScriptedModel(same_call, same_call)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502
    assert "повторяющиеся" in response.json()["detail"]


def test_chat_deadline_cancels_model(settings):
    cancellations = []

    class SlowModel:
        async def respond(self, **_):
            try:
                await asyncio.sleep(30)
            except asyncio.CancelledError:
                cancellations.append("cancelled")
                raise

    with TestClient(create_app(settings.model_copy(update={"chat_timeout_seconds": 0.05}), model=SlowModel())) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 504
    assert cancellations == ["cancelled"]


@pytest.mark.parametrize("path", ["/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
def test_disconnected_client_cancels_in_flight_request(settings, path):
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

        async def receive():
            if event := next(events, None):
                return event
            await started.wait()
            return {"type": "http.disconnect"}

        async def send(_):
            pass

        async with app.router.lifespan_context(app):
            await asyncio.wait_for(app({
                "type": "http", "http_version": "1.1", "method": "POST", "path": path,
                "root_path": "", "query_string": b"", "headers": [
                    (b"authorization", AUTH["Authorization"].encode()), (b"content-type", b"application/json"),
                ],
            }, receive, send), timeout=1)
        assert cancelled.is_set()

    asyncio.run(run())


@pytest.mark.parametrize("path", ["/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
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
    model = ScriptedModel(
        [{"type": "reasoning", "encrypted_content": "x" * (64 * 1024)}]
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


@pytest.mark.parametrize("refinements", [2, 3])
def test_refined_search_reaches_model_after_six_sources(settings, monkeypatch, refinements):
    results = {QUESTION: search_hits("initial")}
    results.update({f"поиск {index}": search_hits(f"round-{index}") for index in range(1, refinements + 1)})
    queries = mock_search_results(monkeypatch, results)
    cited = [f"round-{refinements}-2", "initial-0", "round-1-0", "initial-1", "round-1-1", "initial-2"]
    text = "Ответ: " + " ".join(f"[source:{id}]" for id in [*cited, cited[0]])
    model = ScriptedModel(*[
        call("find_instruction", call_id=f"call-{index}", query=f"поиск {index}")
        for index in range(1, refinements + 1)
    ], answer(text))

    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)

    assert response.status_code == 200, response.text
    assert queries == list(results)
    assert first_context(model)["hits"] == results[QUESTION].model_dump(mode="json")["hits"]
    for index in range(1, refinements + 1):
        assert tool_output(model, index)["hits"] == results[f"поиск {index}"].model_dump(mode="json")["hits"]
    assert response.json()["answer"] == text
    assert [source["id"] for source in response.json()["sources"]] == cited


@pytest.mark.parametrize("citation_count", [6, 7])
def test_response_limits_unique_citations_without_truncating_answer(settings, monkeypatch, citation_count):
    results = {QUESTION: search_hits("initial"), "первый": search_hits("first"), "второй": search_hits("second")}
    mock_search_results(monkeypatch, results)
    ids = [hit.id for result in results.values() for hit in result.hits][:citation_count]
    model = ScriptedModel(
        call("find_instruction", call_id="first", query="первый"),
        call("find_instruction", call_id="second", query="второй"),
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
        call("find_instruction", query="уточнение"),
        answer("Ответ. " + " ".join(f"[source:{id}]" for id in ids)),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == status, response.text
    if status == 502:
        assert "лимит источников" in response.json()["detail"]


@pytest.mark.parametrize("citation", ["initial-0", "small-0", "large-0"])
def test_context_overflow_is_explicit_and_unseen_sources_cannot_be_cited(settings, monkeypatch, citation):
    from ai_service.agents.agent import MAX_CONTEXT_BYTES, json_size

    results = {
        QUESTION: search_hits("initial"),
        "большой": search_hits("large", "🧠" * 1200),
        "короткий": search_hits("small"),
    }
    queries = mock_search_results(monkeypatch, results)
    calls = []
    for index, query in enumerate(["большой", "короткий", "большой"]):
        calls.extend(call("find_instruction", call_id=f"call-{index}", query=query))
    model = ScriptedModel(
        [{"type": "reasoning", "encrypted_content": "x" * (52 * 1024)}, *calls],
        answer(f"Ответ. [source:{citation}]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)

    assert queries == [QUESTION, "большой", "короткий"]
    assert len(model.requests) == 2
    for instructions, messages, tools in model.requests:
        assert json_size([instructions, messages, tools]) <= MAX_CONTEXT_BYTES
    outputs = [item for item in model.requests[1][1] if item.get("type") == "function_call_output"]
    assert [item["call_id"] for item in outputs] == [item["call_id"] for item in calls]
    for index in (0, 2):
        output = json.loads(outputs[index]["output"])
        assert output["error"] == "context_budget_exhausted"
        assert "hits" not in output
    assert json.loads(outputs[1]["output"])["hits"] == results["короткий"].model_dump(mode="json")["hits"]
    if citation == "large-0":
        assert response.status_code == 502
        assert "недоступный источник" in response.json()["detail"]
    else:
        assert response.status_code == 200, response.text
        assert [source["id"] for source in response.json()["sources"]] == [citation]


def test_uncited_search_results_are_not_attached_to_response(settings):
    model = ScriptedModel(answer("Какой доступ вам нужен?", kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200, response.text
    assert first_context(model)["hits"]
    assert response.json() == {"answer": "Какой доступ вам нужен?", "sources": []}


def test_context_limit_after_empty_initial_search_does_not_claim_no_knowledge(settings, monkeypatch):
    from ai_service.agents.agent import CONTEXT_LIMIT_ANSWER

    mock_search_results(monkeypatch, {QUESTION: SearchResult(hits=[]), "уточнение": search_hits("large", "🧠" * 1200)})
    model = ScriptedModel(
        [{"type": "reasoning", "encrypted_content": "x" * (52 * 1024)}]
        + call("find_instruction", query="уточнение"),
        answer("Неподтверждённый ответ модели."),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200, response.text
    assert tool_output(model)["error"] == "context_budget_exhausted"
    assert response.json() == {"answer": CONTEXT_LIMIT_ANSWER, "sources": []}


@pytest.mark.parametrize("outcome", ["fixed", "missing", "clarification", "no_knowledge", "bad_citation", "tool", "refusal"])
def test_missing_citation_is_repaired_once_with_tools_disabled(settings, outcome, caplog):
    unverified = "Пароль Wi-Fi — invented-password."
    fixed = "Обратитесь в поддержку. [source:demo-dev-environment]"
    corrections = {
        "fixed": answer(fixed),
        "missing": answer(unverified),
        "clarification": answer("К какой сети вы подключаетесь?", kind="clarification"),
        "no_knowledge": answer(unverified, kind="no_knowledge"),
        "bad_citation": answer("Ответ. [source:unavailable]"),
        "tool": call("find_instruction", query="пароль"),
        "refusal": [{"type": "message", "content": [{"type": "refusal", "refusal": "Refused"}]}],
    }
    model = ScriptedModel(answer(unverified), corrections[outcome])
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)

    assert len(model.requests) == 2
    assert model.requests[1][2] == []
    repair_messages = model.requests[1][1]
    assert repair_messages[:2] == model.requests[0][1]
    assert json.loads(repair_messages[-2]["content"][0]["text"])["answer"] == unverified
    assert "Исправь ответ" in repair_messages[-1]["content"]
    if outcome in {"fixed", "clarification", "no_knowledge"}:
        assert response.status_code == 200, response.text
        expected = {"fixed": fixed, "clarification": "К какой сети вы подключаетесь?", "no_knowledge": NO_KNOWLEDGE}
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
    calls = [call("find_instruction", call_id=f"call-{i}", query=f"поиск {i}") for i in range(3)]
    model = ScriptedModel(
        *calls, answer("Ответ без ссылки."),
        answer("Исправленный ответ. [source:demo-dev-environment]"),
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200, response.text
    assert [len(request[2]) for request in model.requests] == [1, 1, 1, 0, 0]
    assert [source["id"] for source in response.json()["sources"]] == ["demo-dev-environment"]


@pytest.mark.parametrize("has_sources", [True, False])
def test_clarification_is_allowed_without_citations(settings, has_sources):
    text = "Какой именно доступ вам нужен?"
    model = ScriptedModel(answer(text, kind="clarification"))
    actor = ACTOR if has_sources else {**ACTOR, "team": "design"}
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, actor)
    assert response.status_code == 200, response.text
    assert response.json() == {"answer": text, "sources": []}
    assert len(model.requests) == 1


def test_no_knowledge_uses_service_text_even_if_search_returned_sources(settings):
    model = ScriptedModel(answer("Произвольный текст модели.", kind="no_knowledge"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 200, response.text
    assert first_context(model)["hits"]
    assert response.json() == {"answer": NO_KNOWLEDGE, "sources": []}
    assert len(model.requests) == 1


@pytest.mark.parametrize("text", [
    "Пароль Wi-Fi — invented-password.",
    '{"answer":"Ответ."}',
    '{"kind":"unknown","answer":"Ответ."}',
    '{"kind":"clarification","answer":" "}',
    '{"kind":"clarification","answer":"\\ud800"}',
    '{"kind":"clarification","answer":"Что нужно?","extra":true}',
])
def test_invalid_final_answer_is_rejected_without_leaking_text(settings, text):
    model = ScriptedModel([{"type": "message", "content": [{"type": "output_text", "text": text}]}])
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502
    assert response.json()["detail"] == "Модель вернула некорректный формат итогового ответа."
    assert len(model.requests) == 1


@pytest.mark.parametrize("budget", ["time", "context"])
def test_missing_citation_does_not_start_retry_without_budget(settings, budget):
    output = answer("Неподтверждённое утверждение.")
    if budget == "context":
        output.insert(0, {"type": "reasoning", "encrypted_content": "x" * (64 * 1024)})
    else:
        settings = settings.model_copy(update={"chat_timeout_seconds": 0.5})
    model = ScriptedModel(output)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 502, response.text
    assert "подтверждающих источников" in response.json()["detail"]
    assert len(model.requests) == 1


def test_citation_repair_is_cancelled_by_original_deadline(settings, monkeypatch):
    monkeypatch.setattr("ai_service.agents.agent.MIN_CITATION_REPAIR_SECONDS", 0)
    cancelled = []

    class SlowRepair:
        def __init__(self):
            self.calls = 0

        async def respond(self, **kwargs):
            self.calls += 1
            if self.calls == 1:
                return ModelResponse(output=answer("Неподтверждённое утверждение."))
            assert kwargs["tools"] == []
            try:
                await asyncio.sleep(30)
            finally:
                cancelled.append(True)

    model = SlowRepair()
    settings = settings.model_copy(update={"chat_timeout_seconds": 0.05})
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION)
    assert response.status_code == 504
    assert model.calls == 2
    assert cancelled == [True]


@pytest.mark.parametrize("path", ["/api/v1/get_answer", "/api/v1/chat", "/v1/chat"])
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
        response = client.post("/api/v1/get_answer", headers={**AUTH, "Content-Type": "application/json"}, content=body)
    assert response.status_code == 422, response.text
    assert response.json()["detail"]
    assert all(set(issue) == {"type", "loc", "msg"} for issue in response.json()["detail"])
