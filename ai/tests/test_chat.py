import json
import sqlite3
from contextlib import closing
from dataclasses import replace

import psycopg
import pytest
from fastapi.testclient import TestClient

from ai_buddy.app import create_app
from ai_buddy.config import Settings
from ai_buddy.contracts import Actor
from ai_buddy.embeddings import EmbeddingError, Embeddings
from ai_buddy.llm import ModelError, ModelResponse
from ai_buddy.user_client import HttpUser, UserError


AUTH = {"Authorization": "Bearer chat-test-token"}
ACTOR = {"user_id": "test-user", "team": "backend", "role": "intern"}
QUESTION = "Как настроить окружение?"


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


def call(name, **arguments):
    return [{
        "type": "function_call",
        "call_id": f"call-{name}",
        "name": name,
        "arguments": json.dumps(arguments),
    }]


def answer(text):
    return [{"type": "message", "content": [{"type": "output_text", "text": text}]}]


@pytest.fixture
def settings(tmp_path, monkeypatch):
    profiles = {}

    async def embed(self, texts):
        assert len(texts) == 1
        return [[1.0, 0.0]]

    class TestConnection:
        async def __aenter__(self):
            return self

        async def __aexit__(self, *_):
            pass

        async def set_isolation_level(self, level):
            assert level == psycopg.IsolationLevel.READ_COMMITTED

        async def execute(self, sql, parameters):
            if "FROM ai_chat_profiles" in sql:
                self.profile = profiles.get(parameters)
                return self
            if "pg_advisory_xact_lock" in sql:
                return self
            if "INSERT INTO ai_chat_profiles" in sql:
                conversation_id, user_id, team, role = parameters
                assert (conversation_id, user_id) not in profiles
                profiles[conversation_id, user_id] = (team, role)
                return self
            model, dimensions, role, team, vector, distance = parameters
            assert model == "text-embedding-3-small"
            assert dimensions == 2 and json.loads(vector) == [1.0, 0.0]
            assert distance == 0.7
            assert "<=>" in sql
            self.rows = {
                ("intern", "backend"): ["demo-dev-environment"],
                ("mentor", "backend"): ["demo-mentor-environment", "demo-dev-environment"],
            }.get((role, team), [])
            return self

        async def fetchall(self):
            return [{"id": key, "title": key, "text": "Фрагмент инструкции"} for key in self.rows]

        async def fetchone(self):
            return self.profile

    async def connect(*args, **kwargs):
        return TestConnection()

    monkeypatch.setattr(Embeddings, "embed", embed)
    monkeypatch.setattr(psycopg.AsyncConnection, "connect", connect)
    return Settings(
        service_token="chat-test-token",
        knowledge_db_dsn="postgresql://knowledge.test/knowledge",
        demo_db_path=tmp_path / "steps.sqlite3",
    )


def ask(client, message, actor=ACTOR, **extra):
    return client.post("/api/v1/chat", headers=AUTH, json={"actor": actor, "message": message, **extra})


def tool_output(model):
    return json.loads(model.requests[1][1][-1]["output"])


def first_context(model):
    return json.loads(model.requests[0][1][-2]["content"].split("\n", 1)[1])


@pytest.mark.parametrize(("actor", "ids"), [
    (ACTOR, ["demo-dev-environment"]),
    ({**ACTOR, "role": "mentor"}, ["demo-mentor-environment", "demo-dev-environment"]),
    ({**ACTOR, "team": "design"}, []),
])
def test_search_returns_only_accessible_sources(settings, actor, ids):
    model = ScriptedModel(call("find_instruction", query="окружение"), answer("Ответ."))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, actor)
    assert response.status_code == 200, response.text
    assert response.json()["answer"] == "Ответ."
    assert sorted(source["id"] for source in response.json()["sources"]) == sorted(ids)
    assert sorted(source["id"] for source in first_context(model)["sources"]) == sorted(ids)
    assert sorted(hit["id"] for hit in tool_output(model)["hits"]) == sorted(ids)
    assert response.json()["action"] is None
    assert [tool["name"] for tool in model.requests[0][2]] == ["find_instruction"]


@pytest.mark.parametrize("actor", [
    {"user_id": ACTOR["user_id"]},
    {**ACTOR, "role": "mentor", "team": "design"},
])
def test_user_profile_and_sources_are_sent_with_first_llm_request(settings, monkeypatch, actor):
    async def get_actor(self, user_id):
        assert user_id == ACTOR["user_id"]
        return Actor.model_validate(ACTOR)

    monkeypatch.setattr(HttpUser, "get_actor", get_actor)
    settings = replace(settings, user_url="https://user.example", user_token="user-test-token")
    model = ScriptedModel(answer("Ответ с первого вызова."))
    async def embed(self, texts):
        assert texts == [QUESTION]
        return [[1.0, 0.0]]

    monkeypatch.setattr(Embeddings, "embed", embed)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, QUESTION, actor, conversation_id="conversation-1")

    assert response.status_code == 200, response.text
    assert len(model.requests) == 1
    instructions, messages, _ = model.requests[0]
    assert "Отвечай кратко, по-русски." in instructions
    assert messages[-1] == {"role": "user", "content": QUESTION}
    context = first_context(model)
    assert context["profile"] == {"team": "backend", "role": "intern"}
    assert [source["id"] for source in context["sources"]] == ["demo-dev-environment"]
    assert response.json()["sources"] == context["sources"]


def test_user_profile_is_loaded_once_per_conversation_and_user(settings, monkeypatch):
    lookups = []

    async def get_actor(self, user_id):
        lookups.append(user_id)
        role = "intern" if len(lookups) == 1 else "mentor"
        return Actor(user_id=user_id, team="backend", role=role)

    monkeypatch.setattr(HttpUser, "get_actor", get_actor)
    settings = replace(settings, user_url="https://user.example", user_token="user-test-token")
    model = ScriptedModel(*[answer("Ответ.")] * 4)
    with TestClient(create_app(settings, model=model)) as client:
        assert ask(client, QUESTION, conversation_id="conversation-1").status_code == 200
        assert ask(client, QUESTION, conversation_id="conversation-2").status_code == 200
        assert ask(client, QUESTION, {"user_id": "other-user"}, conversation_id="conversation-1").status_code == 200
        assert ask(client, QUESTION, conversation_id="conversation-1").status_code == 200

    assert lookups == [ACTOR["user_id"], ACTOR["user_id"], "other-user"]
    contexts = [json.loads(messages[-2]["content"].split("\n", 1)[1]) for _, messages, _ in model.requests]
    assert [context["profile"]["role"] for context in contexts] == ["intern", "mentor", "mentor", "intern"]


def test_user_lookup_failure_allows_retry(settings, monkeypatch):
    profiles = iter([UserError("Сервис user недоступен."), Actor.model_validate(ACTOR)])

    async def get_actor(self, user_id):
        profile = next(profiles)
        if isinstance(profile, UserError):
            raise profile
        return profile

    monkeypatch.setattr(HttpUser, "get_actor", get_actor)
    settings = replace(settings, user_url="https://user.example", user_token="user-test-token")
    model = ScriptedModel(answer("Ответ."), answer("Ещё ответ."))
    with TestClient(create_app(settings, model=model)) as client:
        assert ask(client, QUESTION).status_code == 422
        assert ask(client, QUESTION, conversation_id="conversation-1").status_code == 502
        assert model.requests == []
        assert ask(client, QUESTION, conversation_id="conversation-1").status_code == 200
        assert ask(client, QUESTION, conversation_id="conversation-1").status_code == 200


def test_knowledge_failure_prevents_llm_call(settings, monkeypatch):
    async def connect(*args, **kwargs):
        raise psycopg.OperationalError("connection failed")

    monkeypatch.setattr(psycopg.AsyncConnection, "connect", connect)
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


def test_confirmed_step_is_saved_and_repeat_is_safe(settings):
    for _ in range(2):
        model = ScriptedModel(
            call("complete_onboarding_step", step_id="dev-environment"), answer("Готово.")
        )
        with TestClient(create_app(settings, model=model)) as client:
            response = ask(client, "Отметь шаг", confirmed_step_id="dev-environment")
        assert response.status_code == 200, response.text
        assert response.json()["action"] == {"step_id": "dev-environment", "status": "completed"}
        assert tool_output(model)["status"] == "completed"

    with closing(sqlite3.connect(settings.demo_db_path)) as db:
        assert db.execute("SELECT user_id, step_id FROM completed_steps").fetchall() == [
            ("test-user", "dev-environment"),
        ]


@pytest.mark.parametrize(("actor", "confirmed_step", "tool_step", "expected"), [
    (ACTOR, None, "dev-environment", "недоступен"),
    (ACTOR, "dev-environment", "other-step", "не был подтверждён"),
    ({**ACTOR, "role": "mentor"}, "dev-environment", "dev-environment", "нет права"),
    (ACTOR, "unknown-step", "unknown-step", "не найден"),
])
def test_unconfirmed_or_forbidden_action_is_not_saved(settings, actor, confirmed_step, tool_step, expected):
    model = ScriptedModel(call("complete_onboarding_step", step_id=tool_step), answer("Шаг не изменён."))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Отметь шаг", actor, confirmed_step_id=confirmed_step)
    assert response.status_code == 200, response.text
    assert response.json()["action"] is None
    assert expected in tool_output(model)["error"]
    assert not settings.demo_db_path.exists()


def test_model_error_after_action_preserves_confirmed_result(settings):
    model = ScriptedModel(
        call("complete_onboarding_step", step_id="dev-environment"), ModelError("Тайм-аут")
    )
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Отметь шаг", confirmed_step_id="dev-environment")
    assert response.status_code == 200, response.text
    assert response.json()["action"]["status"] == "completed"


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
