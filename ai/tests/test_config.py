import os

import pytest
from pydantic import ValidationError

from ai_service.core.config import Settings


def test_loads_existing_environment_names_and_types(monkeypatch):
    monkeypatch.setattr(os, "environ", {
        "AI_MODE": "integration",
        "AI_SERVICE_TOKEN": "chat-test-token",
        "USER_SERVICE_TOKEN": "user-test-token",
        "LLM_MODEL": "configured-model",
        "KNOWLEDGE_MIN_SIMILARITY": "0.45",
    })

    settings = Settings.from_env()

    assert settings.mode == "integration"
    assert settings.service_token == "chat-test-token"
    assert settings.user_token == "user-test-token"
    assert settings.llm_model == "configured-model"
    assert settings.knowledge_min_similarity == 0.45


def test_ignores_unrecognized_environment_names_and_keeps_http_validation_separate(monkeypatch):
    monkeypatch.setattr(os, "environ", {"ai_mode": "demo", "MODE": "demo", "mode": "demo", "llm_model": "unexpected"})

    settings = Settings.from_env()

    assert settings == Settings()
    assert settings.mode == "integration"
    assert settings.llm_model == ""
    with pytest.raises(RuntimeError, match="AI_MODE=integration requires USER_URL"):
        settings.validate_mode()


def test_programmatic_settings_do_not_read_environment(monkeypatch):
    monkeypatch.setattr(os, "environ", {"AI_MODE": "integration", "KNOWLEDGE_MIN_SIMILARITY": "invalid"})

    assert Settings(mode="demo").mode == "demo"


def test_programmatic_settings_reject_unknown_fields():
    with pytest.raises(ValidationError, match="knowledge_min_similiarity"):
        Settings(knowledge_min_similiarity=0.2)


def test_legacy_onboarding_environment_does_not_configure_ai(monkeypatch):
    monkeypatch.setattr(os, "environ", {
        "AI_MODE": "demo",
        "ONBOARDING_URL": "https://unused.example",
        "ONBOARDING_SERVICE_TOKEN": "unused-token",
        "QUESTIONS_ONLY": "false",
        "CONFIRMATIONS_ONLY": "true",
        "DEMO_DB_PATH": "unused.sqlite3",
    })

    settings = Settings.from_env()
    settings.validate_mode()

    assert settings == Settings(mode="demo")


def test_empty_environment_values_do_not_fall_back_to_defaults(monkeypatch):
    monkeypatch.setattr(os, "environ", {"AI_MODE": "", "LLM_BASE_URL": ""})

    settings = Settings.from_env()

    assert settings.llm_base_url == ""
    with pytest.raises(RuntimeError, match="^AI_MODE must be integration or demo$"):
        settings.validate_mode()


@pytest.mark.parametrize("value", ["", "example-sensitive-value"])
def test_invalid_similarity_reports_the_setting_without_its_value(monkeypatch, value):
    monkeypatch.setattr(os, "environ", {"KNOWLEDGE_MIN_SIMILARITY": value})

    with pytest.raises(ValidationError, match="KNOWLEDGE_MIN_SIMILARITY") as error:
        Settings.from_env()

    assert "input_value" not in str(error.value)
    assert "example-sensitive-value" not in str(error.value)
