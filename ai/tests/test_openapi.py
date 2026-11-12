from pathlib import Path

import yaml

from ai_service.core.config import Settings
from ai_service.main import create_app


def test_checked_in_openapi_matches_app():
    spec = Path(__file__).resolve().parents[1] / "api" / "ai" / "v1" / "openapi.yaml"
    assert yaml.safe_load(spec.read_text(encoding="utf-8")) == create_app(Settings()).openapi()


def test_chat_contract_only_exposes_answers_and_sources():
    spec = create_app(Settings()).openapi()
    schemas = spec["components"]["schemas"]

    assert set(spec["paths"]) == {"/healthz", "/readyz", "/api/v1/answer"}
    assert spec["paths"]["/api/v1/answer"]["post"]["operationId"] == "answer"
    assert schemas["ChatRequest"]["properties"]["actor"]["$ref"].endswith("/ChatActor")
    assert set(schemas["ChatActor"]["required"]) == {"user_id", "team", "role"}
    assert "ActorRef" not in schemas
    assert set(schemas["ChatRequest"]["properties"]) == {"conversation_id", "actor", "message", "current_step", "history"}
    assert "current_step" not in schemas["ChatRequest"]["required"]
    assert set(schemas["CurrentStep"]["required"]) == {"id", "title"}
    assert set(schemas["ChatResponse"]["properties"]) == {"answer", "sources"}
    assert "ActionResult" not in schemas
