from ..clients.embeddings import EmbeddingError, Embeddings
from ..repositories.knowledge import KnowledgeError, KnowledgeRepository
from ..schemas.contracts import Actor, SearchResult


class Retriever:
    def __init__(self, repository: KnowledgeRepository, embeddings: Embeddings, min_similarity: float):
        if not -1 <= min_similarity <= 1:
            raise ValueError("KNOWLEDGE_MIN_SIMILARITY must be between -1 and 1")
        self.repository = repository
        self.embeddings = embeddings
        self.min_similarity = min_similarity

    async def search(self, query: str, actor: Actor) -> SearchResult:
        try:
            [embedding] = await self.embeddings.embed([query])
        except EmbeddingError as error:
            raise KnowledgeError(str(error)) from error
        return await self.repository.search(embedding, self.embeddings.model, actor, self.min_similarity)
