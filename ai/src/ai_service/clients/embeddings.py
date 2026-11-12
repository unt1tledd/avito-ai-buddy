import httpx
from pydantic import BaseModel, FiniteFloat, ValidationError

from ..core.config import EmbeddingProfile
from ..core.diagnostics import UpstreamError, http_failure, operation


class EmbeddingError(UpstreamError):
    pass


class EmbeddingItem(BaseModel):
    index: int
    embedding: list[FiniteFloat]


class EmbeddingResponse(BaseModel):
    data: list[EmbeddingItem]


class Embeddings:
    def __init__(self, http: httpx.AsyncClient, profile: EmbeddingProfile):
        self.http = http
        self.config = profile

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
            items = EmbeddingResponse.model_validate_json(response.content).data
        except httpx.HTTPError as error:
            raise EmbeddingError("Не удалось получить эмбеддинги.", reason=http_failure(error)) from error
        except ValidationError as error:
            raise EmbeddingError("Не удалось получить эмбеддинги.", reason="invalid_response") from error

        items.sort(key=lambda item: item.index)
        vectors = [item.embedding for item in items]
        if not vectors or [item.index for item in items] != list(range(len(texts))):
            raise EmbeddingError("Сервис эмбеддингов вернул некорректные векторы.", reason="invalid_indexes")
        if any(len(vector) != self.config.dimensions for vector in vectors):
            raise EmbeddingError(
                f"Сервис эмбеддингов должен возвращать векторы размерности {self.config.dimensions}.",
                reason="invalid_dimensions",
            )
        if any(not any(vector) for vector in vectors):
            raise EmbeddingError("Сервис эмбеддингов вернул некорректные векторы.", reason="zero_vector")
        return vectors
