import httpx
from pydantic import BaseModel, FiniteFloat, ValidationError

from ..core.config import EmbeddingProfile
from ..core.diagnostics import UpstreamError, operation


class EmbeddingError(UpstreamError):
    domain = "embeddings"

    def __init__(self, message: str, *, expected_dimensions: int | None = None, **kwargs):
        super().__init__(message, **kwargs)
        self.expected_dimensions = expected_dimensions


class EmbeddingItem(BaseModel):
    index: int
    embedding: list[FiniteFloat]


class EmbeddingResponse(BaseModel):
    data: list[EmbeddingItem]
    usage: dict[str, object] | None = None


class Embeddings:
    def __init__(self, http: httpx.AsyncClient, profile: EmbeddingProfile):
        self.http = http
        self.config = profile
        self.calls = 0
        self.input_tokens = 0
        self.usage_complete = True

    @property
    def model(self) -> str:
        return self.config.model

    @property
    def profile(self) -> str:
        return self.config.index_id

    async def embed(self, texts: list[str]) -> list[list[float]]:
        with operation("embeddings.embed"):
            return await self._embed(texts)

    async def _embed(self, texts: list[str]) -> list[list[float]]:
        headers = {"Authorization": f"Bearer {self.config.api_key}"} if self.config.api_key else {}
        payload = {
            "model": self.model,
            "input": texts,
            "encoding_format": "float",
        }
        if self.config.openrouter:
            payload["provider"] = {"only": [self.config.provider], "allow_fallbacks": False, "require_parameters": True}
        if self.config.request_dimensions is not None:
            payload["dimensions"] = self.config.request_dimensions
        try:
            response = await self.http.post(self.config.url, headers=headers, json=payload)
            response.raise_for_status()
            parsed = EmbeddingResponse.model_validate_json(response.content)
            items = parsed.data
        except httpx.HTTPError as error:
            raise EmbeddingError.from_http_error(error, "Failed to obtain embeddings.") from error
        except ValidationError as error:
            raise EmbeddingError("Failed to obtain embeddings.", reason="invalid_response") from error

        items.sort(key=lambda item: item.index)
        vectors = [item.embedding for item in items]
        if not vectors or [item.index for item in items] != list(range(len(texts))):
            raise EmbeddingError("Embedding service returned invalid vectors.", reason="invalid_indexes")
        if any(len(vector) != self.config.dimensions for vector in vectors):
            raise EmbeddingError(
                f"Embedding service must return {self.config.dimensions}-dimensional vectors.",
                reason="invalid_dimensions", expected_dimensions=self.config.dimensions,
            )
        if any(not any(vector) for vector in vectors):
            raise EmbeddingError("Embedding service returned invalid vectors.", reason="zero_vector")
        self.calls += 1
        usage = parsed.usage or {}
        tokens = usage.get("prompt_tokens", usage.get("total_tokens"))
        if not isinstance(tokens, int) or tokens < 0:
            self.usage_complete = False
        else:
            self.input_tokens += tokens
        return vectors
