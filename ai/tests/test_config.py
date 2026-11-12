import os

import pytest
from pydantic import ValidationError

from ai_service.core.config import Settings, resolve_embedding_profile


def test_loads_existing_environment_names_and_types(monkeypatch):
    monkeypatch.setattr(os, "environ", {
        "AI_SERVICE_TOKEN": "chat-test-token",
        "LLM_MODEL": "configured-model",
        "KNOWLEDGE_MIN_SIMILARITY": "0.45",
    })

    settings = Settings.from_env()

    assert settings.service_token == "chat-test-token"
    assert settings.llm_model == "configured-model"
    assert settings.knowledge_min_similarity == 0.45


def test_ignores_unrecognized_environment_names(monkeypatch):
    monkeypatch.setattr(os, "environ", {"ai_mode": "demo", "MODE": "demo", "mode": "demo", "llm_model": "unexpected"})

    settings = Settings.from_env()

    assert settings == Settings()
    assert settings.llm_model == ""


def test_programmatic_settings_do_not_read_environment(monkeypatch):
    monkeypatch.setattr(os, "environ", {"AI_MODE": "integration", "KNOWLEDGE_MIN_SIMILARITY": "invalid"})

    assert Settings().knowledge_min_similarity == 0.3


def test_programmatic_settings_reject_unknown_fields():
    with pytest.raises(ValidationError, match="knowledge_min_similiarity"):
        Settings(knowledge_min_similiarity=0.2)


def test_legacy_service_environment_does_not_configure_ai(monkeypatch):
    monkeypatch.setattr(os, "environ", {
        "AI_MODE": "demo",
        "USER_URL": "https://unused-user.example",
        "USER_SERVICE_TOKEN": "unused-user-token",
        "ONBOARDING_URL": "https://unused.example",
        "ONBOARDING_SERVICE_TOKEN": "unused-token",
        "QUESTIONS_ONLY": "false",
        "CONFIRMATIONS_ONLY": "true",
        "DEMO_DB_PATH": "unused.sqlite3",
    })

    settings = Settings.from_env()
    assert settings == Settings()


def test_empty_environment_values_do_not_fall_back_to_defaults(monkeypatch):
    monkeypatch.setattr(os, "environ", {"AI_MODE": "", "LLM_BASE_URL": ""})

    settings = Settings.from_env()

    assert settings.llm_base_url == ""


@pytest.mark.parametrize("value", ["", "example-sensitive-value"])
def test_invalid_similarity_reports_the_setting_without_its_value(monkeypatch, value):
    monkeypatch.setattr(os, "environ", {"KNOWLEDGE_MIN_SIMILARITY": value})

    with pytest.raises(ValidationError, match="KNOWLEDGE_MIN_SIMILARITY") as error:
        Settings.from_env()

    assert "input_value" not in str(error.value)
    assert "example-sensitive-value" not in str(error.value)


@pytest.mark.parametrize(("overrides", "expected_index_id"), [
    ({}, "c46ee183cfd9844fd54c26ed815baa1143369075c3cff9213f3e92dfd3726fad"),
    ({"embedding_model": "text-embedding-3-large", "embedding_dimensions": 1536},
     "4041b6fac89818fca29409d0f7f905c7e0e45b1d4ee52c703873592c2b9b15d2"),
    ({"embedding_base_url": "https://api.siliconflow.cn/v1", "embedding_model": "Qwen/Qwen3-Embedding-8B"},
     "de1ad6606f9ed674ca5e347a977e87e910cbee35af9506973c0d44e83f063ca3"),
    ({"embedding_base_url": "https://openrouter.ai/api/v1", "embedding_model": "qwen/qwen3-embedding-8b",
      "embedding_provider": "deepinfra"},
     "9705a3e27ff4fe3b63e63978279271bbd912ca3cb72c5b93e7945f2b18504263"),
    ({"embedding_base_url": "https://openrouter.ai/api/v1", "embedding_model": "qwen/qwen3-embedding-8b",
      "embedding_provider": "siliconflow"},
     "2a1b0bddb119c09bcc8891d2424c607752af034f76a6718b8c188589d6e7fae3"),
])
def test_embedding_profiles_preserve_existing_index_identity(overrides, expected_index_id):
    # Values captured from the client implementation before the configuration refactor.
    settings = Settings(embedding_api_key="test-key", **overrides)
    profile = resolve_embedding_profile(settings)
    assert profile.index_id == expected_index_id
    rotated = resolve_embedding_profile(settings.model_copy(update={"embedding_api_key": "rotated-key"}))
    next_version = resolve_embedding_profile(settings.model_copy(update={"embedding_index_version": "2"}))
    assert rotated.index_id == expected_index_id
    assert next_version.index_id != expected_index_id
    assert "test-key" not in repr(profile)
