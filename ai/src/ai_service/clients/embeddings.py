import hashlib
import json
from urllib.parse import urlsplit

import httpx
from pydantic import BaseModel, FiniteFloat, ValidationError

from ..core.config import Settings, validate_base_url, validate_bearer_token
from ..core.diagnostics import UpstreamError, http_failure, operation


EMBEDDING_DIMENSIONS = 1536
SILICONFLOW_DIMENSIONS = 1024
SILICONFLOW_QWEN_MODELS = {
    "Qwen/Qwen3-Embedding-8B", "Qwen/Qwen3-Embedding-4B", "Qwen/Qwen3-Embedding-0.6B",
}


class EmbeddingError(UpstreamError):
    pass


class EmbeddingItem(BaseModel):
    index: int
    embedding: list[FiniteFloat]


class EmbeddingResponse(BaseModel):
    data: list[EmbeddingItem]


class Embeddings:
    def __init__(self, http: httpx.AsyncClient, settings: Settings):
        self.http = http
        base_url = settings.embedding_base_url or settings.llm_base_url
        validate_base_url("EMBEDDING_BASE_URL" if settings.embedding_base_url else "LLM_BASE_URL", base_url)
        self.url = str(httpx.URL(f"{base_url.rstrip('/')}/embeddings"))
        self.api_key = settings.embedding_api_key or (settings.llm_api_key if not settings.embedding_base_url else "")
        if self.api_key:
            validate_bearer_token("EMBEDDING_API_KEY" if settings.embedding_api_key else "LLM_API_KEY", self.api_key)
        self.model = settings.embedding_model
        if not self.model.strip():
            raise RuntimeError("EMBEDDING_MODEL must be configured")
        host = urlsplit(self.url).hostname or ""
        self.openrouter = host == "openrouter.ai" or host.endswith(".openrouter.ai")
        self.provider = settings.embedding_provider
        if self.openrouter and not self.provider:
            raise RuntimeError("EMBEDDING_PROVIDER must pin an OpenRouter upstream")
        siliconflow = (
            host.removeprefix("api.") in {"siliconflow.com", "siliconflow.cn"}
            or self.provider.split("/", 1)[0] == "siliconflow"
        )
        self.request_dimensions = settings.embedding_dimensions
        qwen_model = (
            self.model == "qwen/qwen3-embedding-8b" if self.openrouter else self.model in SILICONFLOW_QWEN_MODELS
        )
        if siliconflow:
            if not qwen_model or self.request_dimensions not in (None, SILICONFLOW_DIMENSIONS):
                raise RuntimeError("The SiliconFlow Qwen profile requires EMBEDDING_DIMENSIONS=1024")
            self.request_dimensions = SILICONFLOW_DIMENSIONS
        elif self.request_dimensions not in (None, EMBEDDING_DIMENSIONS):
            raise RuntimeError(f"EMBEDDING_DIMENSIONS must be {EMBEDDING_DIMENSIONS} for the default profile")
        if self.openrouter and qwen_model and not siliconflow and self.provider != "deepinfra":
            raise RuntimeError("The supported OpenRouter Qwen providers are deepinfra (1536) and siliconflow (1024)")
        if self.openrouter and qwen_model and not siliconflow:
            self.request_dimensions = EMBEDDING_DIMENSIONS
        if host == "api.openai.com":
            if self.model not in {"text-embedding-3-small", "text-embedding-3-large", "text-embedding-ada-002"}:
                raise RuntimeError("EMBEDDING_MODEL is incompatible with the OpenAI embeddings endpoint")
            if self.model == "text-embedding-3-large" and self.request_dimensions is None:
                raise RuntimeError("EMBEDDING_DIMENSIONS=1536 is required for text-embedding-3-large")
            if self.model == "text-embedding-ada-002" and self.request_dimensions is not None:
                raise RuntimeError("Leave EMBEDDING_DIMENSIONS unset for text-embedding-ada-002")
        if (self.openrouter or siliconflow or host == "api.openai.com") and not self.api_key:
            raise RuntimeError("EMBEDDING_API_KEY must be configured for the hosted embeddings endpoint")
        self.dimensions = self.request_dimensions or EMBEDDING_DIMENSIONS
        identity = (self.url, self.provider, self.model, self.dimensions, settings.embedding_index_version)
        self.profile = hashlib.sha256(json.dumps(identity).encode()).hexdigest()

    async def embed(self, texts: list[str]) -> list[list[float]]:
        with operation("embeddings.embed"):
            return await self._embed(texts)

    async def _embed(self, texts: list[str]) -> list[list[float]]:
        headers = {"Authorization": f"Bearer {self.api_key}"} if self.api_key else {}
        payload = {
            "model": self.model,
            "input": texts,
            "encoding_format": "float",
        }
        if self.openrouter:
            payload["provider"] = {"only": [self.provider], "allow_fallbacks": False, "require_parameters": True}
        if self.request_dimensions is not None:
            payload["dimensions"] = self.request_dimensions
        try:
            response = await self.http.post(self.url, headers=headers, json=payload)
            response.raise_for_status()
            items = EmbeddingResponse.model_validate_json(response.content).data
        except httpx.HTTPError as error:
            raise EmbeddingError("Не удалось получить эмбеддинги.", reason=http_failure(error)) from error
        except ValidationError as error:
            raise EmbeddingError("Не удалось получить эмбеддинги.", reason="invalid_response") from error

        items.sort(key=lambda item: item.index)
        vectors = [item.embedding for item in items]
        if not vectors or [item.index for item in items] != list(range(len(texts))):
            raise EmbeddingError("Сервис эмбеддингов вернул некорректные векторы.", reason="invalid_indexes")
        if any(len(vector) != self.dimensions for vector in vectors):
            raise EmbeddingError(
                f"Сервис эмбеддингов должен возвращать векторы размерности {self.dimensions}.",
                reason="invalid_dimensions",
            )
        if any(not any(vector) for vector in vectors):
            raise EmbeddingError("Сервис эмбеддингов вернул некорректные векторы.", reason="zero_vector")
        return vectors
