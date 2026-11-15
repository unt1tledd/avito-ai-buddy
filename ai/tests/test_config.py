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


def test_two_model_rounds_share_bounded_configurable_deadline(monkeypatch):
    monkeypatch.setattr(os, "environ", {})
    assert Settings.from_env().chat_timeout_seconds == 25
    monkeypatch.setenv("CHAT_TIMEOUT_SECONDS", "20")
    assert Settings.from_env().chat_timeout_seconds == 20
    assert Settings.from_env().effective_chat_timeout_seconds == 20
    monkeypatch.setenv("CHAT_TIMEOUT_SECONDS", "60")
    assert Settings.from_env().chat_timeout_seconds == 60
    assert Settings.from_env().effective_chat_timeout_seconds == 25
    monkeypatch.setenv("CHAT_TIMEOUT_SECONDS", "61")
    with pytest.raises(ValidationError):
        Settings.from_env()


def test_programmatic_settings_reject_unknown_fields():
    with pytest.raises(ValidationError, match="knowledge_min_similiarity"):
        Settings(knowledge_min_similiarity=0.2)


def test_embedding_resilience_settings_load_from_environment_without_changing_index(monkeypatch):
    monkeypatch.setattr(os, 'environ', {
        'SEARCH_EMBEDDING_TIMEOUT_SECONDS': '1.5',
        'EMBEDDING_CIRCUIT_FAILURE_THRESHOLD': '5',
        'EMBEDDING_CIRCUIT_RESET_SECONDS': '45',
        'EMBEDDING_API_KEY': 'test-key',
    })
    configured = Settings.from_env()
    assert configured.search_embedding_timeout_seconds == 1.5
    assert configured.embedding_circuit_failure_threshold == 5
    assert configured.embedding_circuit_reset_seconds == 45
    assert resolve_embedding_profile(configured).index_id == resolve_embedding_profile(Settings(embedding_api_key='test-key')).index_id


@pytest.mark.parametrize('field,value', [
    ('search_embedding_timeout_seconds', 0), ('search_embedding_timeout_seconds', 61),
    ('embedding_circuit_failure_threshold', 0), ('embedding_circuit_failure_threshold', 101),
    ('embedding_circuit_reset_seconds', 0), ('embedding_circuit_reset_seconds', 301),
])
def test_embedding_resilience_settings_are_bounded(field, value):
    with pytest.raises(ValidationError):
        Settings(**{field: value})


def test_llm_resilience_settings_do_not_change_embedding_profile(monkeypatch):
    monkeypatch.setattr(os, 'environ', {
        'LLM_TIMEOUT_SECONDS': '12', 'LLM_FALLBACK_RESERVE_SECONDS': '4',
        'LLM_CIRCUIT_FAILURE_THRESHOLD': '5', 'LLM_CIRCUIT_RESET_SECONDS': '45',
        'EMBEDDING_API_KEY': 'test',
    })
    settings = Settings.from_env()
    assert (settings.llm_timeout_seconds, settings.llm_fallback_reserve_seconds,
            settings.llm_circuit_failure_threshold, settings.llm_circuit_reset_seconds) == (12, 4, 5, 45)
    assert resolve_embedding_profile(settings).index_id == resolve_embedding_profile(Settings(embedding_api_key='test')).index_id


@pytest.mark.parametrize('field,value', [
    ('llm_timeout_seconds', 0), ('llm_timeout_seconds', 61),
    ('llm_fallback_reserve_seconds', -1), ('llm_fallback_reserve_seconds', 11),
    ('llm_circuit_failure_threshold', 0), ('llm_circuit_failure_threshold', 101),
    ('llm_circuit_reset_seconds', 0), ('llm_circuit_reset_seconds', 301),
])
def test_llm_resilience_settings_are_bounded(field, value):
    with pytest.raises(ValidationError):
        Settings(**{field: value})


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
def test_embedding_profiles_version_text_preparation(overrides, expected_index_id):
    # Values captured from the client implementation before the configuration refactor.
    settings = Settings(embedding_api_key="test-key", **overrides)
    profile = resolve_embedding_profile(settings)
    assert profile.index_id != expected_index_id
    rotated = resolve_embedding_profile(settings.model_copy(update={"embedding_api_key": "rotated-key"}))
    next_version = resolve_embedding_profile(settings.model_copy(update={"embedding_index_version": "2"}))
    assert rotated.index_id == profile.index_id
    assert next_version.index_id != profile.index_id
    plain = resolve_embedding_profile(settings.model_copy(update={"contextualization_enabled": False}))
    changed_prompt = resolve_embedding_profile(settings.model_copy(update={"context_prompt_version": "next"}))
    assert len({plain.index_id, changed_prompt.index_id, profile.index_id}) == 3
    assert "test-key" not in repr(profile)


def test_sentence_overlap_fix_invalidates_previous_text_profile(monkeypatch):
    from ai_service.core import config

    settings = Settings(embedding_api_key='test-key', contextualization_enabled=False)
    corrected = resolve_embedding_profile(settings).index_id
    monkeypatch.setattr(config, 'TEXT_PREPARATION_VERSION', 'hybrid-text-v1')
    previous = resolve_embedding_profile(settings).index_id
    assert corrected != previous
