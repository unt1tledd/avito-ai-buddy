import hashlib
import json
from dataclasses import dataclass, field
from typing import Literal
from urllib.parse import urlsplit

import httpx
from pydantic import BaseModel, ConfigDict, Field
from pydantic_settings import BaseSettings, SettingsConfigDict

from .text_preparation import CONTEXT_PROMPT, CONTEXT_PROMPT_VERSION, TEXT_PREPARATION_VERSION


DEFAULT_KNOWLEDGE_MIN_SIMILARITY = 0.3
EMBEDDING_DIMENSIONS = 1536
SILICONFLOW_DIMENSIONS = 1024
SILICONFLOW_QWEN_MODELS = {
    "Qwen/Qwen3-Embedding-8B", "Qwen/Qwen3-Embedding-4B", "Qwen/Qwen3-Embedding-0.6B",
}


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
    knowledge_db_dsn: str = ""
    knowledge_processing_approved: bool = False
    chat_timeout_seconds: float = Field(default=60, gt=0, le=60)
    knowledge_min_similarity: float = DEFAULT_KNOWLEDGE_MIN_SIMILARITY
    embedding_model: str = "text-embedding-3-small"
    embedding_base_url: str = ""
    embedding_api_key: str = ""
    embedding_dimensions: int | None = None
    embedding_provider: str = Field(default="", pattern=r"^[a-z0-9/_-]{0,100}$")
    embedding_index_version: str = Field(default="1", pattern=r"^[A-Za-z0-9._-]{1,64}$")
    retrieval_mode: Literal["vector", "hybrid"] = "hybrid"
    search_candidates: int = Field(default=10, ge=1, le=100)
    search_top_k: int = Field(default=3, ge=1, le=3)
    rrf_k: float = Field(default=60, gt=0, le=1000)
    rrf_bm25_weight: float = Field(default=1, gt=0, le=100)
    rrf_vector_weight: float = Field(default=1, gt=0, le=100)
    bm25_k1: float = Field(default=1.2, gt=0, le=10)
    bm25_b: float = Field(default=0.75, ge=0, le=1)
    contextualization_enabled: bool = True
    context_prompt_version: str = Field(default=CONTEXT_PROMPT_VERSION, pattern=r"^[A-Za-z0-9._-]{1,64}$")
    context_timeout_seconds: float = Field(default=25, gt=0, le=120)
    context_concurrency: int = Field(default=4, ge=1, le=32)
    context_min_interval_seconds: float = Field(default=0, ge=0, le=60)
    context_cache_path: str = ".data/context-cache.sqlite3"
    import_timeout_seconds: float = Field(default=900, gt=0, le=7200)

    @classmethod
    def from_env(cls) -> "Settings":
        return cls.model_validate(_EnvironmentSettings().model_dump())


def _environment_name(field_name: str) -> str:
    return {
        "service_token": "AI_SERVICE_TOKEN",
    }.get(field_name, field_name.upper())


class _EnvironmentSettings(Settings, BaseSettings):
    model_config = SettingsConfigDict(case_sensitive=True, alias_generator=_environment_name)


@dataclass(frozen=True)
class EmbeddingProfile:
    url: str
    api_key: str = field(repr=False)
    model: str
    provider: str
    openrouter: bool
    request_dimensions: int | None
    dimensions: int
    index_id: str


def resolve_embedding_profile(settings: Settings) -> EmbeddingProfile:
    """Validate providers and version the complete text/embedding preparation."""
    base_url = settings.embedding_base_url or settings.llm_base_url
    validate_base_url("EMBEDDING_BASE_URL" if settings.embedding_base_url else "LLM_BASE_URL", base_url)
    url = str(httpx.URL(f"{base_url.rstrip('/')}/embeddings"))
    api_key = settings.embedding_api_key or (settings.llm_api_key if not settings.embedding_base_url else "")
    if api_key:
        validate_bearer_token("EMBEDDING_API_KEY" if settings.embedding_api_key else "LLM_API_KEY", api_key)
    model = settings.embedding_model
    if not model.strip():
        raise RuntimeError("EMBEDDING_MODEL must be configured")
    host = urlsplit(url).hostname or ""
    openrouter = host == "openrouter.ai" or host.endswith(".openrouter.ai")
    provider = settings.embedding_provider
    if openrouter and not provider:
        raise RuntimeError("EMBEDDING_PROVIDER must pin an OpenRouter upstream")
    siliconflow = (
        host.removeprefix("api.") in {"siliconflow.com", "siliconflow.cn"}
        or provider.split("/", 1)[0] == "siliconflow"
    )
    request_dimensions = settings.embedding_dimensions
    qwen_model = (
        model == "qwen/qwen3-embedding-8b" if openrouter else model in SILICONFLOW_QWEN_MODELS
    )
    if siliconflow:
        if not qwen_model or request_dimensions not in (None, SILICONFLOW_DIMENSIONS):
            raise RuntimeError("The SiliconFlow Qwen profile requires EMBEDDING_DIMENSIONS=1024")
        request_dimensions = SILICONFLOW_DIMENSIONS
    elif request_dimensions not in (None, EMBEDDING_DIMENSIONS):
        raise RuntimeError(f"EMBEDDING_DIMENSIONS must be {EMBEDDING_DIMENSIONS} for the default profile")
    if openrouter and qwen_model and not siliconflow and provider != "deepinfra":
        raise RuntimeError("The supported OpenRouter Qwen providers are deepinfra (1536) and siliconflow (1024)")
    if openrouter and qwen_model and not siliconflow:
        request_dimensions = EMBEDDING_DIMENSIONS
    if host == "api.openai.com":
        if model not in {"text-embedding-3-small", "text-embedding-3-large", "text-embedding-ada-002"}:
            raise RuntimeError("EMBEDDING_MODEL is incompatible with the OpenAI embeddings endpoint")
        if model == "text-embedding-3-large" and request_dimensions is None:
            raise RuntimeError("EMBEDDING_DIMENSIONS=1536 is required for text-embedding-3-large")
        if model == "text-embedding-ada-002" and request_dimensions is not None:
            raise RuntimeError("Leave EMBEDDING_DIMENSIONS unset for text-embedding-ada-002")
    if (openrouter or siliconflow or host == "api.openai.com") and not api_key:
        raise RuntimeError("EMBEDDING_API_KEY must be configured for the hosted embeddings endpoint")
    dimensions = request_dimensions or EMBEDDING_DIMENSIONS
    preparation = (TEXT_PREPARATION_VERSION, settings.contextualization_enabled)
    if settings.contextualization_enabled:
        preparation += (settings.context_prompt_version, hashlib.sha256(CONTEXT_PROMPT.encode()).hexdigest(),
                        settings.llm_base_url.rstrip("/"), settings.llm_model)
    identity = (url, provider, model, dimensions, settings.embedding_index_version, preparation)
    return EmbeddingProfile(
        url=url, api_key=api_key, model=model, provider=provider, openrouter=openrouter,
        request_dimensions=request_dimensions, dimensions=dimensions,
        index_id=hashlib.sha256(json.dumps(identity).encode()).hexdigest(),
    )
