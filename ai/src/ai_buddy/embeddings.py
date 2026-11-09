import httpx
from pydantic import BaseModel, FiniteFloat, ValidationError

from .config import Settings


class EmbeddingError(Exception):
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
        self.url = f"{base_url.rstrip('/')}/embeddings"
        self.api_key = settings.embedding_api_key or (settings.llm_api_key if not settings.embedding_base_url else "")
        self.model = settings.embedding_model

    async def embed(self, texts: list[str]) -> list[list[float]]:
        headers = {"Authorization": f"Bearer {self.api_key}"} if self.api_key else {}
        try:
            response = await self.http.post(
                self.url,
                headers=headers,
                json={"model": self.model, "input": texts, "encoding_format": "float"},
            )
            response.raise_for_status()
            items = EmbeddingResponse.model_validate_json(response.content).data
        except (httpx.HTTPError, ValidationError) as error:
            raise EmbeddingError("Не удалось получить эмбеддинги.") from error

        items.sort(key=lambda item: item.index)
        vectors = [item.embedding for item in items]
        if (
            [item.index for item in items] != list(range(len(texts)))
            or len({len(vector) for vector in vectors}) != 1
            or any(not any(vector) for vector in vectors)
        ):
            raise EmbeddingError("Сервис эмбеддингов вернул некорректные векторы.")
        return vectors
