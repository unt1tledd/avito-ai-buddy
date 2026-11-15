import asyncio
from collections.abc import Sequence
from dataclasses import dataclass

from ..agents.agent import Agent, CONTEXT_LIMIT_ANSWER, MAX_SOURCE_BYTES, NO_KNOWLEDGE, json_size
from ..clients.resilient_llm import ModelUnavailable
from ..core.diagnostics import logger, operation
from ..rag.onboarding import snapshot_answer
from ..schemas.contracts import (
    Actor, ChatActor, ChatResponse, CitedSource, MAX_RESPONSE_SOURCES, Message, OnboardingPlan, SearchResult,
)
from ..schemas.support import TEMPLATES, needs_safety_routing


LEXICAL_FALLBACK_NOTICE = "AI временно недоступен."
MAX_EXCERPT_CHARS = 400


def lexical_answer(result: SearchResult) -> ChatResponse:
    sources: list[CitedSource] = []
    seen: set[str] = set()
    for hit in result.hits:
        source = CitedSource.model_validate(hit.model_dump(exclude={"access_policies"}))
        if source.id in seen:
            continue
        if len(sources) == MAX_RESPONSE_SOURCES:
            break
        if json_size([item.model_dump(mode="json") for item in [*sources, source]]) <= MAX_SOURCE_BYTES:
            sources.append(source)
            seen.add(source.id)
    if not sources:
        text = CONTEXT_LIMIT_ANSWER if result.hits else NO_KNOWLEDGE
        return ChatResponse(answer=LEXICAL_FALLBACK_NOTICE + "\n\n" + text, sources=[])
    parts = [LEXICAL_FALLBACK_NOTICE + " Ниже — найденные материалы, которые могут помочь:"]
    for index, source in enumerate(sources, 1):
        # Treat excerpts as quoted data. Embedded citation markers cannot add references.
        title = " ".join(source.title.split()).replace("[source:", "[source\u200b:")
        excerpt = " ".join(source.text.split())
        if len(excerpt) > MAX_EXCERPT_CHARS:
            excerpt = excerpt[:MAX_EXCERPT_CHARS].rstrip() + "…"
        excerpt = excerpt.replace("[source:", "[source\u200b:")
        parts.append(f"{index}. {title} [source:{source.id}]\n> {excerpt}")
    return ChatResponse(answer="\n\n".join(parts), sources=sources)


class ChatUnavailable(Exception):
    pass


class InvalidChat(Exception):
    def __init__(self, field: tuple[str, ...], message: str, kind: str):
        super().__init__(message)
        self.field = field
        self.kind = kind


@dataclass(frozen=True)
class ChatService:
    agent: Agent | None
    unavailable_reason: str | None

    async def answer(
        self,
        *,
        conversation_id: str | None,
        actor: ChatActor,
        message: str,
        deadline: float,
        onboarding_plans: Sequence[OnboardingPlan] = (),
        history: Sequence[Message] = (),
    ) -> ChatResponse:
        if actor.actual_end_date is not None:
            raise InvalidChat(("actor", "actual_end_date"), "Стажировка завершена.", "value_error")

        if asyncio.get_running_loop().time() >= deadline:
            raise TimeoutError
        with operation("chat.answer"):
            async with asyncio.timeout_at(deadline):
                snapshot = None if needs_safety_routing(message) else snapshot_answer(message, onboarding_plans)
                if snapshot is not None:
                    return snapshot
                if self.agent is None:
                    raise ChatUnavailable("Knowledge chat is unavailable.")
                profile = Actor(user_id=actor.user_id, team=actor.team, role=actor.role)
                try:
                    return await self.agent.answer(
                        message, profile, deadline=deadline,
                        onboarding_plans=onboarding_plans, history=history,
                    )
                except ModelUnavailable as error:
                    if asyncio.get_running_loop().time() >= deadline:
                        raise TimeoutError from error
                    if needs_safety_routing(message):
                        return ChatResponse(answer=TEMPLATES["immediate_safety"], sources=[])
                    logger.warning("event=chat_fallback reason=%s", error.reason, extra={"reason": error.reason})
                    # Query is the original user question, never model output or unverified history.
                    return lexical_answer(await self.agent.knowledge.search_lexical(message, profile))
