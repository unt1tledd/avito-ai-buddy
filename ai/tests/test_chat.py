import json
import sqlite3
from contextlib import closing
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from ai_buddy.app import create_app
from ai_buddy.config import Settings
from ai_buddy.llm import ModelError, ModelResponse


CORPUS = Path(__file__).resolve().parents[1] / "knowledge/documents.json"
AUTH = {"Authorization": "Bearer chat-test-token"}
ACTOR = {"user_id": "test-user", "team": "backend", "role": "intern"}


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
def settings(tmp_path):
    return Settings(
        service_token="chat-test-token", knowledge_path=CORPUS, demo_db_path=tmp_path / "steps.sqlite3"
    )


def ask(client, message, actor=ACTOR, **extra):
    return client.post("/v1/chat", headers=AUTH, json={"actor": actor, "message": message, **extra})


def tool_output(model):
    return json.loads(model.requests[1][1][-1]["output"])


@pytest.mark.parametrize(("actor", "ids"), [
    (ACTOR, ["demo-dev-environment"]),
    ({**ACTOR, "role": "mentor"}, ["demo-mentor-environment", "demo-dev-environment"]),
    ({**ACTOR, "team": "design"}, []),
])
def test_search_returns_only_accessible_sources(settings, actor, ids):
    model = ScriptedModel(call("find_instruction", query="окружение"), answer("Ответ."))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Как настроить окружение?", actor)
    assert response.status_code == 200, response.text
    assert response.json()["answer"] == "Ответ."
    assert sorted(source["id"] for source in response.json()["sources"]) == sorted(ids)
    assert sorted(hit["id"] for hit in tool_output(model)["hits"]) == sorted(ids)
    assert response.json()["action"] is None
    assert [tool["name"] for tool in model.requests[0][2]] == ["find_instruction"]


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


def test_service_requires_auth_and_config(settings):
    with TestClient(create_app(settings)) as client:
        assert client.get("/healthz").json() == {"status": "ok"}
        assert client.post("/v1/chat", json={"actor": ACTOR, "message": "Привет"}).status_code == 401
        assert ask(client, "Привет").status_code == 503


def test_model_failure_without_action_returns_gateway_error(settings):
    with TestClient(create_app(settings, model=ScriptedModel(ModelError("Тайм-аут")))) as client:
        response = ask(client, "Привет")
    assert response.status_code == 502
    assert response.json()["detail"] == "Тайм-аут"
