from pathlib import Path
from typing import Literal
from urllib.parse import urlsplit

import httpx
from pydantic import BaseModel, ConfigDict, Field
from pydantic_settings import BaseSettings, SettingsConfigDict


DEFAULT_KNOWLEDGE_MIN_SIMILARITY = 0.3
Capability = Literal["chat", "questions", "confirmations"]


def validate_base_url(name: str, value: str) -> None:
    try:
        url = urlsplit(value)
        httpx.URL(value)
        if (
            url.scheme in {"http", "https"}
            and url.hostname
            and url.port != 0
            and url.username is None
            and not any(char.isspace() or char in "?#\\" for char in value)
        ):
            return
    except (ValueError, httpx.InvalidURL):
        pass
    raise RuntimeError(f"{name} must be an HTTP(S) base URL")


def validate_bearer_token(name: str, value: str) -> None:
    if not value or not all("!" <= char <= "~" for char in value):
        raise RuntimeError(f"{name} must be an ASCII Bearer token without whitespace")


class Settings(BaseModel):
    model_config = ConfigDict(frozen=True, extra="forbid", hide_input_in_errors=True)

    service_token: str = ""
    llm_api_key: str = ""
    llm_model: str = ""
    llm_base_url: str = "https://api.openai.com/v1"
    onboarding_url: str = ""
    onboarding_token: str = ""
    knowledge_db_dsn: str = ""
    knowledge_processing_approved: bool = False
    confirmations_only: bool = False
    questions_only: bool = False
    chat_timeout_seconds: float = Field(default=15, gt=0, le=25)
    knowledge_min_similarity: float = DEFAULT_KNOWLEDGE_MIN_SIMILARITY
    embedding_model: str = "text-embedding-3-small"
    embedding_base_url: str = ""
    embedding_api_key: str = ""
    embedding_dimensions: int | None = None
    embedding_provider: str = Field(default="", pattern=r"^[a-z0-9/_-]{0,100}$")
    embedding_index_version: str = Field(default="1", pattern=r"^[A-Za-z0-9._-]{1,64}$")
    demo_db_path: Path = Path(".data/onboarding.sqlite3")
    user_url: str = ""
    user_token: str = ""
    mode: str = "integration"

    @property
    def capability(self) -> Capability:
        if self.questions_only:
            return "questions"
        if self.confirmations_only:
            return "confirmations"
        return "chat"

    def validate_mode(self) -> None:
        if self.questions_only and self.confirmations_only:
            raise RuntimeError("QUESTIONS_ONLY and CONFIRMATIONS_ONLY cannot both be enabled")
        integration = {
            "USER_URL": self.user_url,
            "USER_SERVICE_TOKEN": self.user_token,
            "ONBOARDING_URL": self.onboarding_url,
            "ONBOARDING_SERVICE_TOKEN": self.onboarding_token,
        }
        if self.mode == "demo":
            configured = [name for name, value in integration.items() if value]
            if configured:
                raise RuntimeError(f"AI_MODE=demo cannot be combined with {', '.join(configured)}")
            return
        if self.mode != "integration":
            raise RuntimeError("AI_MODE must be integration or demo")
        if self.confirmations_only and not (self.user_url or self.user_token):
            del integration["USER_URL"], integration["USER_SERVICE_TOKEN"]
        if self.questions_only:
            del integration["ONBOARDING_URL"], integration["ONBOARDING_SERVICE_TOKEN"]
        missing = [name for name, value in integration.items() if not value.strip()]
        if missing:
            raise RuntimeError(f"AI_MODE=integration requires {', '.join(missing)}")
        for name in integration.keys() & {"USER_URL", "ONBOARDING_URL"}:
            validate_base_url(name, integration[name])
        for name in integration.keys() & {"USER_SERVICE_TOKEN", "ONBOARDING_SERVICE_TOKEN"}:
            validate_bearer_token(name, integration[name])

    @classmethod
    def from_env(cls) -> "Settings":
        return cls.model_validate(_EnvironmentSettings().model_dump())


def _environment_name(field_name: str) -> str:
    return {
        "mode": "AI_MODE",
        "service_token": "AI_SERVICE_TOKEN",
        "user_token": "USER_SERVICE_TOKEN",
        "onboarding_token": "ONBOARDING_SERVICE_TOKEN",
    }.get(field_name, field_name.upper())


class _EnvironmentSettings(Settings, BaseSettings):
    model_config = SettingsConfigDict(case_sensitive=True, alias_generator=_environment_name)
