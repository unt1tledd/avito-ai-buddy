import asyncio
import json
import sqlite3
from contextlib import closing

import httpx
import psycopg
import pytest
from fastapi.testclient import TestClient

from ai_service.agents.agent import NO_KNOWLEDGE
from ai_service.clients.embeddings import EMBEDDING_DIMENSIONS, EmbeddingError, Embeddings
from ai_service.clients.llm import ModelError, ModelResponse
from ai_service.clients.user import HttpUser, UserError
from ai_service.core.config import Settings
from ai_service.main import create_app
from ai_service.repositories.knowledge import KnowledgeRepository
from ai_service.schemas.contracts import SearchHit, SearchResult
from ai_service.schemas.contracts import Actor


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


def answer(text):
    return [{"type": "message", "content": [{"type": "output_text", "text": text}]}]


@pytest.fixture
def settings(tmp_path, monkeypatch):
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
        mode="demo",
        service_token="chat-test-token",
        knowledge_db_dsn="postgresql://knowledge.test/knowledge",
        knowledge_processing_approved=True,
        embedding_api_key="test-embedding-token",
        demo_db_path=tmp_path / "steps.sqlite3",
    )


@pytest.fixture
def integration_settings(settings):
    return settings.model_copy(update={
        "mode": "integration",
        "user_url": "https://user.example",
        "user_token": "user-test-token",
        "onboarding_url": "https://onboarding.example",
        "onboarding_token": "onboarding-test-token",
    })


@pytest.fixture
def use_mock_http(monkeypatch):
    async_client = httpx.AsyncClient

    def use(handler):
        def client(**kwargs):
            return async_client(transport=httpx.MockTransport(handler), **kwargs)

        monkeypatch.setattr(httpx, "AsyncClient", client)

    return use


@pytest.mark.parametrize("variable", ["USER_URL", "USER_SERVICE_TOKEN", "ONBOARDING_URL", "ONBOARDING_SERVICE_TOKEN"])
@pytest.mark.parametrize("missing", [None, " \t"])
def test_default_mode_requires_each_integration_setting(monkeypatch, variable, missing):
    monkeypatch.delenv("KNOWLEDGE_MIN_SIMILARITY", raising=False)
    for name, value in {
        "USER_URL": "https://user.example",
        "USER_SERVICE_TOKEN": "user-test-token",
        "ONBOARDING_URL": "https://onboarding.example",
        "ONBOARDING_SERVICE_TOKEN": "onboarding-test-token",
    }.items():
        monkeypatch.setenv(name, value)
    monkeypatch.delenv("AI_MODE", raising=False)
    if missing is None:
        monkeypatch.delenv(variable)
    else:
        monkeypatch.setenv(variable, missing)
    with pytest.raises(RuntimeError, match=rf"^AI_MODE=integration requires {variable}$"):
        with TestClient(create_app()):
            pytest.fail("Incomplete integration mode started")


@pytest.mark.parametrize(("field", "variable", "value"), [
    ("user_url", "USER_URL", "user.example"),
    ("onboarding_url", "ONBOARDING_URL", "https://[invalid-secret"),
    ("user_url", "USER_URL", "https://💩.example"),
    ("onboarding_url", "ONBOARDING_URL", "https://💩.example"),
    ("user_url", "USER_URL", "https://user:embedded-secret@user.example"),
    ("onboarding_url", "ONBOARDING_URL", "https://user:embedded-secret@onboarding.example"),
])
def test_integration_rejects_invalid_service_url(integration_settings, field, variable, value):
    with pytest.raises(RuntimeError, match=rf"^{variable} must be an HTTP\(S\) base URL$"):
        with TestClient(create_app(integration_settings.model_copy(update={field: value}))):
            pytest.fail("Invalid integration mode started")


@pytest.mark.parametrize(("field", "variable"), [
    ("user_token", "USER_SERVICE_TOKEN"),
    ("onboarding_token", "ONBOARDING_SERVICE_TOKEN"),
])
@pytest.mark.parametrize("token", ["тестовый-токен", "test-token\r\n", "test token"])
def test_integration_rejects_unusable_bearer_token(integration_settings, field, variable, token):
    with pytest.raises(RuntimeError, match=rf"^{variable} must be an ASCII Bearer token without whitespace$"):
        with TestClient(create_app(integration_settings.model_copy(update={field: token}))):
            pytest.fail("Integration mode started with an invalid Bearer token")


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


@pytest.mark.parametrize(("field", "variable", "value"), [
    ("user_url", "USER_URL", "https://user.example"),
    ("user_token", "USER_SERVICE_TOKEN", "user-test-token"),
    ("onboarding_url", "ONBOARDING_URL", "https://onboarding.example"),
    ("onboarding_token", "ONBOARDING_SERVICE_TOKEN", "onboarding-test-token"),
])
def test_demo_mode_rejects_any_integration_setting(settings, field, variable, value):
    with pytest.raises(RuntimeError, match=rf"^AI_MODE=demo cannot be combined with {variable}$"):
        with TestClient(create_app(settings.model_copy(update={field: value}))):
            pytest.fail("Mixed mode started")


def test_unknown_mode_does_not_start(settings):
    with pytest.raises(RuntimeError, match="^AI_MODE must be integration or demo$"):
        with TestClient(create_app(settings.model_copy(update={"mode": "local"}))):
            pytest.fail("Unknown mode started")


def test_questions_and_confirmations_only_cannot_both_start(integration_settings):
    configured = integration_settings.model_copy(update={"questions_only": True, "confirmations_only": True})
    with pytest.raises(RuntimeError, match="QUESTIONS_ONLY and CONFIRMATIONS_ONLY cannot both be enabled"):
        with TestClient(create_app(configured)):
            pytest.fail("Conflicting capabilities started")


def ask(client, message, actor=ACTOR, **extra):
    return client.post("/api/v1/chat", headers=AUTH, json={"actor": actor, "message": message, **extra})


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
    model = ScriptedModel(call("find_instruction", query="окружение"), answer("Ответ."))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, actor)
    assert response.status_code == 200, response.text
    assert response.json()["answer"] == ("Ответ." if ids else NO_KNOWLEDGE)
    assert sorted(source["id"] for source in response.json()["sources"]) == sorted(ids)
    assert sorted(source["id"] for source in first_context(model)["hits"]) == sorted(ids)
    assert sorted(hit["id"] for hit in tool_output(model)["hits"]) == sorted(ids)
    assert response.json()["action"] is None
    assert [tool["name"] for tool in model.requests[0][2]] == ["find_instruction"]


@pytest.mark.parametrize("actor", [
    {"user_id": ACTOR["user_id"]},
    {**ACTOR, "role": "mentor", "team": "design"},
])
def test_user_profile_and_sources_are_sent_with_first_llm_request(integration_settings, monkeypatch, use_mock_http, actor):
    def handler(request):
        assert request.url == "https://user.example/v1/user/42/metadata"
        assert request.headers["authorization"] == "Bearer user-test-token"
        return httpx.Response(200, json={
            "intern": {"user_id": "42", "first_name": "Synthetic"},
            "team": {"team_id": "51", "name": "Backend Platform"},
            "track": "design",
        })

    use_mock_http(handler)
    model = ScriptedModel(answer("Ответ с первого вызова."))
    async def embed(self, texts):
        assert texts == [QUESTION]
        return [EMBEDDING]

    monkeypatch.setattr(Embeddings, "embed", embed)
    with TestClient(create_app(integration_settings, model=model)) as client:
        response = ask(client, QUESTION, actor, conversation_id="conversation-1")

    assert response.status_code == 200, response.text
    assert len(model.requests) == 1
    instructions, messages, _ = model.requests[0]
    assert "Отвечай кратко, по-русски." in instructions
    assert messages[-1] == {"role": "user", "content": QUESTION}
    context = first_context(model)
    assert set(context) == {"hits"}
    assert [source["id"] for source in context["hits"]] == ["demo-dev-environment"]
    assert response.json()["sources"] == context["hits"]


def test_questions_only_uses_user_without_onboarding(integration_settings, monkeypatch, use_mock_http):
    requests = []

    def handler(request):
        requests.append((request.method, str(request.url), request.headers["authorization"]))
        return httpx.Response(200, json={"intern": {"user_id": "42"}, "team": {"team_id": "51"}})

    async def check_ready(self, profile):
        assert len(profile) == 64

    use_mock_http(handler)
    monkeypatch.setattr(KnowledgeRepository, "check_ready", check_ready)
    configured = integration_settings.model_copy(update={
        "questions_only": True, "onboarding_url": "", "onboarding_token": "",
    })
    payload = {
        "conversation_id": "123", "actor": {"user_id": "42"}, "message": QUESTION,
    }
    with TestClient(create_app(configured, model=ScriptedModel(answer("Ответ.")))) as client:
        readiness = client.get("/readyz")
        reply = client.post("/api/v1/chat", headers=AUTH, json=payload)
        confirmation = client.post("/v1/chat", headers=AUTH, json={**payload, "confirmed_step_id": "setup"})

    assert readiness.status_code == 200
    assert readiness.json() == {"status": "ready", "capability": "questions", "reason": None}
    assert reply.status_code == 200
    assert [source["id"] for source in reply.json()["sources"]] == ["demo-dev-environment"]
    assert reply.json()["action"] is None
    assert confirmation.status_code == 503
    assert requests == [("GET", "https://user.example/v1/user/42/metadata", "Bearer user-test-token")]


def test_chat_uses_current_profile_and_excludes_history_after_role_change(integration_settings, monkeypatch):
    lookups = []

    async def get_actor(self, user_id):
        lookups.append(user_id)
        role = "mentor" if len(lookups) == 1 else "intern"
        return Actor(user_id=user_id, team="backend", role=role)

    monkeypatch.setattr(HttpUser, "get_actor", get_actor)
    model = ScriptedModel(call("find_instruction", query="окружение"), answer("Закрытая инструкция."),
                          call("find_instruction", query="окружение"), answer("Ответ."))
    with TestClient(create_app(integration_settings, model=model)) as client:
        first = ask(client, QUESTION, conversation_id="conversation-1")
        history = [
            {"role": "assistant", "content": first.json()["answer"]},
            {"role": "user", "content": "Старая цитата из инструкции для наставников."},
        ]
        second = ask(client, QUESTION, conversation_id="conversation-1", history=history)

    assert lookups == [ACTOR["user_id"], ACTOR["user_id"]]
    assert first.status_code == second.status_code == 200
    for index, response, role, expected in (
        (0, first, "mentor", ["demo-mentor-environment", "demo-dev-environment"]),
        (2, second, "intern", ["demo-dev-environment"]),
    ):
        context = first_context(model, index)
        assert set(context) == {"hits"}
        assert [source["id"] for source in context["hits"]] == expected
        assert [source["id"] for source in tool_output(model, index + 1)["hits"]] == expected
        assert [source["id"] for source in response.json()["sources"]] == expected
    for _, messages, _ in model.requests[2:]:
        assert all(entry["content"] not in json.dumps(messages, ensure_ascii=False) for entry in history)


def test_user_lookup_failure_cannot_reuse_previous_profile(integration_settings, monkeypatch):
    actor = Actor.model_validate({**ACTOR, "role": "mentor"})
    profiles = iter([actor, UserError("Сервис user недоступен."), actor])

    async def get_actor(self, user_id):
        profile = next(profiles)
        if isinstance(profile, UserError):
            raise profile
        return profile

    monkeypatch.setattr(HttpUser, "get_actor", get_actor)
    model = ScriptedModel(answer("Ответ."), answer("Ещё ответ."))
    with TestClient(create_app(integration_settings, model=model)) as client:
        missing_id = ask(client, QUESTION)
        assert missing_id.status_code == 422
        assert missing_id.json()["detail"] == [{
            "type": "missing",
            "loc": ["body", "conversation_id"],
            "msg": "Требуется conversation_id.",
            "input": None,
        }]
        assert ask(client, QUESTION, conversation_id="conversation-1").status_code == 200
        with monkeypatch.context() as failure:
            def unexpected_search(*args, **kwargs):
                pytest.fail("Search attempted while the trusted profile was unavailable")

            failure.setattr(Embeddings, "embed", unexpected_search)
            failure.setattr(psycopg.AsyncConnection, "connect", unexpected_search)
            response = ask(client, QUESTION, conversation_id="conversation-1")
        assert response.status_code == 502
        assert response.json()["detail"] == "Сервис user недоступен."
        assert len(model.requests) == 1
        assert ask(client, QUESTION, conversation_id="conversation-1").status_code == 200


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
        mode="demo", service_token="chat-test-token", knowledge_processing_approved=True,
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


def test_confirmed_step_is_saved_without_search_or_llm_and_repeat_is_safe(settings):
    settings = settings.model_copy(update={
        "knowledge_db_dsn": "",
        "llm_model": "test-model",
        "llm_base_url": "unused-url",
        "llm_api_key": "unused-key",
    })
    for _ in range(2):
        with TestClient(create_app(settings)) as client:
            response = ask(client, "Отметь шаг", confirmed_step_id="dev-environment")
        assert response.status_code == 200, response.text
        assert response.json() == {
            "answer": "Шаг адаптации отмечен как выполненный.",
            "sources": [],
            "action": {"step_id": "dev-environment", "status": "completed"},
        }

    with closing(sqlite3.connect(settings.demo_db_path)) as db:
        assert db.execute("SELECT user_id, step_id FROM completed_steps").fetchall() == [
            ("42", "dev-environment"),
        ]


@pytest.mark.parametrize("model_tries_to_write", [False, True])
def test_unconfirmed_step_is_not_saved_even_if_model_claims_success(settings, model_tries_to_write):
    outputs = [answer("Шаг выполнен.")]
    if model_tries_to_write:
        outputs.insert(0, call("complete_onboarding_step", step_id="dev-environment"))
    model = ScriptedModel(*outputs)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Отметь шаг")
    assert response.status_code == 200, response.text
    assert response.json()["action"] is None
    assert [tool["name"] for tool in model.requests[0][2]] == ["find_instruction"]
    if model_tries_to_write:
        assert "недоступен" in tool_output(model)["error"]
    assert not settings.demo_db_path.exists()


@pytest.mark.parametrize(("actor", "step_id", "message"), [
    ({**ACTOR, "role": "mentor"}, "dev-environment", "нет права"),
    (ACTOR, "unknown-step", "не найден"),
])
def test_demo_onboarding_reports_refusal_without_saving(settings, actor, step_id, message):
    model = ScriptedModel(answer("Шаг выполнен."))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Отметь шаг", actor, confirmed_step_id=step_id)
    assert response.status_code == 200, response.text
    assert response.json()["action"] is None
    assert message in response.json()["answer"]
    assert not settings.demo_db_path.exists()
    assert not model.requests


@pytest.mark.parametrize(("outcome", "model_text", "expected", "saved"), [
    ("success", "Шаг не выполнен.", "отмечен как выполненный", True),
    ("denied", "Шаг выполнен.", "нет права", False),
    ("timeout", "Шаг выполнен.", "пока неизвестен", True),
    ("wrong-step", "Шаг выполнен.", "пока неизвестен", True),
    ("accepted", "Шаг выполнен.", "пока неизвестен", False),
])
def test_integrated_confirmation_reports_the_write_outcome(
    integration_settings, use_mock_http, monkeypatch, outcome, model_text, expected, saved
):
    step_id = "confirmed-step"
    path = f"/v1/users/42/onboarding/steps/{step_id}/completion"
    calls = []
    completed_steps = []

    def handler(request):
        calls.append((request.method, str(request.url)))
        assert request.method == "PUT"
        assert request.url.path == path
        assert request.headers["authorization"] == "Bearer onboarding-test-token"
        assert len(calls) == 1
        if outcome == "denied":
            return httpx.Response(403)
        if outcome == "accepted":
            return httpx.Response(202, json={"step_id": step_id, "status": "completed"})
        completed_steps.append((ACTOR["user_id"], step_id))
        if outcome == "timeout":
            raise httpx.ReadTimeout("Response lost after saving", request=request)
        result_id = "other-step" if outcome == "wrong-step" else step_id
        return httpx.Response(200, json={"step_id": result_id, "status": "completed"})

    def unexpected_search(*args, **kwargs):
        pytest.fail("Confirmed action attempted to search")

    use_mock_http(handler)
    monkeypatch.setattr(Embeddings, "embed", unexpected_search)
    monkeypatch.setattr(psycopg.AsyncConnection, "connect", unexpected_search)
    model = ScriptedModel(answer(model_text))
    with TestClient(create_app(integration_settings, model=model)) as client:
        response = ask(client, "Отметь другой шаг", {**ACTOR, "role": "mentor", "team": "design"},
                       conversation_id="conversation-1", confirmed_step_id=step_id)

    assert response.status_code == 200, response.text
    result = response.json()
    assert result["answer"] != model_text
    assert expected in result["answer"]
    assert result["sources"] == []
    assert result["action"] == ({"step_id": step_id, "status": "completed"} if outcome == "success" else None)
    assert completed_steps == ([(ACTOR["user_id"], step_id)] if saved else [])
    assert calls == [("PUT", f"https://onboarding.example{path}")]
    assert not model.requests
    assert not integration_settings.demo_db_path.exists()


@pytest.mark.parametrize("path", ["/api/v1/chat", "/v1/chat"])
def test_service_requires_auth_and_config(settings, path):
    payload = {"actor": ACTOR, "message": "Привет"}
    with TestClient(create_app(settings)) as client:
        assert client.get("/healthz").json() == {"status": "ok"}
        assert client.post(path, json=payload).status_code == 401
        assert client.post(path, headers=AUTH, json=payload).status_code == 503


def test_user_id_alone_requires_user_service(settings):
    with TestClient(create_app(settings, model=ScriptedModel())) as client:
        assert ask(client, "Привет", {"user_id": ACTOR["user_id"]}).status_code == 503


def test_model_failure_without_action_returns_gateway_error(settings):
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
    final_output = tool_calls[3] if final_tool_call else answer("Итоговый ответ.")
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
        assert response.json()["answer"] == "Итоговый ответ."


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
        return httpx.Response(200, json={"status": "completed", "output": answer("Ответ.")})

    use_mock_http(handler)
    settings = settings.model_copy(update={"llm_api_key": "llm-test-token", "llm_model": "test-model"})
    with TestClient(create_app(settings)) as client:
        response = ask(client, QUESTION)

    assert response.status_code == 200, response.text
    assert response.json()["answer"] == "Ответ."
    assert requests[1]["input"][:-1] == requests[0]["input"] + model_output
    output = requests[1]["input"][-1]
    assert output["type"] == "function_call_output"
    assert output["call_id"] == "provider-call-id"
    assert [hit["id"] for hit in json.loads(output["output"])["hits"]] == ["demo-dev-environment"]


@pytest.mark.parametrize("path", ["/api/v1/chat", "/v1/chat"])
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


@pytest.mark.parametrize("chunked", [False, True])
def test_chat_body_limit(settings, chunked):
    chunks = [b" " * (128 * 1024)] * 3
    with TestClient(create_app(settings)) as client:
        response = client.post("/v1/chat", headers={**AUTH, "content-type": "application/json"},
                               content=iter(chunks) if chunked else b"".join(chunks))
    assert response.status_code == 413


def test_readiness_reports_configuration_and_confirmation_mode(settings):
    for overrides, code, capability, reason in [
        ({"service_token": ""}, 503, "chat", "service_token_missing"),
        ({"knowledge_db_dsn": ""}, 503, "chat", "knowledge_dsn_missing"),
        ({"knowledge_processing_approved": False}, 503, "chat", "knowledge_processing_unapproved"),
        ({"knowledge_db_dsn": "", "confirmations_only": True}, 200, "confirmations", None),
    ]:
        with TestClient(create_app(settings.model_copy(update=overrides), model=ScriptedModel())) as client:
            assert client.get("/healthz").status_code == 200
            response = client.get("/readyz")
            assert response.status_code == code
            assert response.json()["capability"] == capability
            assert response.json()["reason"] == reason


def test_chat_discards_unverified_answer_after_empty_search(settings):
    model = ScriptedModel(answer("Внутренний адрес https://invented.example"))
    with TestClient(create_app(settings, model=model)) as client:
        result = ask(client, QUESTION, {**ACTOR, "team": "design"}).json()
    assert result == {"answer": NO_KNOWLEDGE, "sources": [], "action": None}


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
    model = ScriptedModel(calls, answer("Ответ."))
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


def test_disconnected_client_cancels_in_flight_request(settings):
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
                "type": "http", "http_version": "1.1", "method": "POST", "path": "/v1/chat",
                "root_path": "", "query_string": b"", "headers": [
                    (b"authorization", AUTH["Authorization"].encode()), (b"content-type", b"application/json"),
                ],
            }, receive, send), timeout=1)
        assert cancelled.is_set()

    asyncio.run(run())


def test_confirmation_deadline_is_unknown_and_never_retries(integration_settings, use_mock_http, caplog):
    sent = []

    async def handler(request):
        sent.append(request.method)
        await asyncio.sleep(30)
        raise AssertionError("expected cancellation")

    use_mock_http(handler)
    settings = integration_settings.model_copy(update={
        "chat_timeout_seconds": 0.05, "confirmations_only": True, "user_url": "", "user_token": "",
    })
    with TestClient(create_app(settings)) as client:
        response = ask(client, "PRIVATE CHAT", {"user_id": "42"},
                       conversation_id="conversation-1", confirmed_step_id="dev-environment")
    assert response.status_code == 200
    assert response.json()["action"] is None
    assert "неизвестен" in response.json()["answer"]
    assert sent == ["PUT"]
    assert f"request_id={response.headers['x-request-id']}" in caplog.text
    assert "operation=chat.confirm result=timeout" in caplog.text
    assert "PRIVATE CHAT" not in caplog.text
    assert "onboarding-test-token" not in caplog.text


@pytest.mark.parametrize("outcome", ["connection", "auth", "ambiguous"])
def test_onboarding_distinguishes_unavailable_from_unknown(integration_settings, use_mock_http, outcome):
    def handler(request):
        if outcome == "connection":
            raise httpx.ConnectError("failed", request=request)
        return httpx.Response(401 if outcome == "auth" else 503)

    use_mock_http(handler)
    with TestClient(create_app(integration_settings)) as client:
        response = ask(client, "Подтверждаю", conversation_id="conversation-1", confirmed_step_id="dev-environment")
    assert response.status_code == (200 if outcome == "ambiguous" else 502)


def test_chat_deadline_includes_reading_body(settings):
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
                "type": "http", "http_version": "1.1", "method": "POST", "path": "/v1/chat",
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
