import os
from pathlib import Path

import pytest
from pydantic import ValidationError

from ai_service.core.config import Settings


def test_loads_existing_environment_names_and_types(monkeypatch):
    monkeypatch.setattr(os, "environ", {
        "AI_MODE": "integration",
        "AI_SERVICE_TOKEN": "chat-test-token",
        "USER_SERVICE_TOKEN": "user-test-token",
        "ONBOARDING_SERVICE_TOKEN": "onboarding-test-token",
        "QUESTIONS_ONLY": "true",
        "LLM_MODEL": "configured-model",
        "KNOWLEDGE_MIN_SIMILARITY": "0.45",
        "DEMO_DB_PATH": "demo/state.sqlite3",
    })

    settings = Settings.from_env()

    assert settings.mode == "integration"
    assert settings.service_token == "chat-test-token"
    assert settings.user_token == "user-test-token"
    assert settings.onboarding_token == "onboarding-test-token"
    assert settings.questions_only is True
    assert settings.llm_model == "configured-model"
    assert settings.knowledge_min_similarity == 0.45
    assert settings.demo_db_path == Path("demo/state.sqlite3")


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


def test_empty_environment_values_do_not_fall_back_to_defaults(monkeypatch):
    monkeypatch.setattr(os, "environ", {"AI_MODE": "", "LLM_BASE_URL": "", "DEMO_DB_PATH": ""})

    settings = Settings.from_env()

    assert settings.llm_base_url == ""
    assert settings.demo_db_path == Path(".")
    with pytest.raises(RuntimeError, match="^AI_MODE must be integration or demo$"):
        settings.validate_mode()


@pytest.mark.parametrize("value", ["", "example-sensitive-value"])
def test_invalid_similarity_reports_the_setting_without_its_value(monkeypatch, value):
    monkeypatch.setattr(os, "environ", {"KNOWLEDGE_MIN_SIMILARITY": value})

    with pytest.raises(ValidationError, match="KNOWLEDGE_MIN_SIMILARITY") as error:
        Settings.from_env()

    assert "input_value" not in str(error.value)
    assert "example-sensitive-value" not in str(error.value)
