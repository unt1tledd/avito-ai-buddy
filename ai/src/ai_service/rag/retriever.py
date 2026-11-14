import asyncio
from time import monotonic

from ..clients.embeddings import EmbeddingError, Embeddings
from ..core.config import Settings
from ..core.diagnostics import logger, operation
from ..repositories.knowledge import KnowledgeRepository
from ..schemas.contracts import Actor, SearchResult


class _EmbeddingCircuit:
    """One circuit per retriever/process, with a single recovery probe.

    State transitions don't await, so concurrent searches on the application's
    event loop cannot acquire two probes. Generations ignore stale completions.
    """

    def __init__(self, threshold: int, reset_seconds: float):
        self.threshold = threshold
        self.reset_seconds = reset_seconds
        self.failures = 0
        self.open_until: float | None = None
        self.probing = False
        self.generation = 0

    def acquire(self) -> int | None:
        if self.open_until is not None:
            if monotonic() < self.open_until or self.probing:
                return None
            self.probing = True
        return self.generation

    def succeeded(self, generation: int) -> None:
        if generation != self.generation:
            return
        self.failures = 0
        if self.open_until is not None:
            self.open_until = None
            self.probing = False
            self.generation += 1

    def failed(self, generation: int) -> None:
        if generation != self.generation:
            return
        self.failures += 1
        if self.probing or self.failures >= self.threshold:
            self.open_until = monotonic() + self.reset_seconds
            self.probing = False
            self.generation += 1

    def cancelled(self, generation: int) -> None:
        if generation == self.generation:
            self.probing = False


class Retriever:
    def __init__(
        self, repository: KnowledgeRepository, embeddings: Embeddings, min_similarity: float,
        *, settings: Settings | None = None,
    ):
        if not -1 <= min_similarity <= 1:
            raise ValueError("KNOWLEDGE_MIN_SIMILARITY must be between -1 and 1")
        self.repository = repository
        self.embeddings = embeddings
        self.min_similarity = min_similarity
        self.settings = settings or repository.settings
        self._circuit = _EmbeddingCircuit(
            self.settings.embedding_circuit_failure_threshold, self.settings.embedding_circuit_reset_seconds,
        )

    async def search(self, query: str, actor: Actor) -> SearchResult:
        with operation("retrieval.search"):
            generation = self._circuit.acquire()
            reason = "circuit_open"
            if generation is not None:
                try:
                    async with asyncio.timeout(self.settings.search_embedding_timeout_seconds):
                        [embedding] = await self.embeddings.embed([query])
                except (EmbeddingError, TimeoutError) as error:
                    self._circuit.failed(generation)
                    reason = error.reason if isinstance(error, EmbeddingError) else "timeout"
                except BaseException:
                    # Caller cancellation/deadline must propagate, including during a probe.
                    self._circuit.cancelled(generation)
                    raise
                else:
                    self._circuit.succeeded(generation)
                    result = await self.repository.search(
                        embedding, self.embeddings.profile, actor, self.min_similarity, query=query,
                    )
                    return self._completed(result, self.settings.retrieval_mode, "ok")
            logger.warning(
                "event=retrieval_fallback reason=%s", reason, extra={"reason": reason, "retrieval_mode": "lexical"},
            )
            result = await self.repository.search_lexical(query, self.embeddings.profile, actor)
            return self._completed(result, "lexical", reason)

    @staticmethod
    def _completed(result: SearchResult, mode: str, reason: str) -> SearchResult:
        logger.info(
            "event=retrieval_completed mode=%s reason=%s hits=%d", mode, reason, len(result.hits),
            extra={"retrieval_mode": mode, "reason": reason, "hit_count": len(result.hits)},
        )
        return result

    async def check_ready(self) -> None:
        await self.repository.check_ready(self.embeddings.profile)

    async def accessible_source_ids(self, source_ids: set[str], actor: Actor) -> set[str]:
        return await self.repository.accessible_source_ids(source_ids, self.embeddings.profile, actor)
