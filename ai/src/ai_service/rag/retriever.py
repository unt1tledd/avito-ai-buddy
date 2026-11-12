from ..clients.embeddings import Embeddings
from ..repositories.knowledge import KnowledgeRepository
from ..schemas.contracts import Actor, SearchResult


class Retriever:
    def __init__(self, repository: KnowledgeRepository, embeddings: Embeddings, min_similarity: float):
        if not -1 <= min_similarity <= 1:
            raise ValueError("KNOWLEDGE_MIN_SIMILARITY must be between -1 and 1")
        self.repository = repository
        self.embeddings = embeddings
        self.min_similarity = min_similarity

    async def search(self, query: str, actor: Actor) -> SearchResult:
        [embedding] = await self.embeddings.embed([query])
        return await self.repository.search(embedding, self.embeddings.profile, actor, self.min_similarity)

    async def check_ready(self) -> None:
        await self.repository.check_ready(self.embeddings.profile)

    async def accessible_source_ids(self, source_ids: set[str], actor: Actor) -> set[str]:
        return await self.repository.accessible_source_ids(source_ids, self.embeddings.profile, actor)
