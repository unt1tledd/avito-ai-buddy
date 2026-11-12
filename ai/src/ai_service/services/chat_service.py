import asyncio
from dataclasses import dataclass

from ..agents.agent import Agent
from ..core.diagnostics import operation
from ..schemas.contracts import Actor, ChatActor, ChatResponse


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
    ) -> ChatResponse:
        if actor.actual_end_date is not None:
            raise InvalidChat(("actor", "actual_end_date"), "Стажировка завершена.", "value_error")

        if asyncio.get_running_loop().time() >= deadline:
            raise TimeoutError
        with operation("chat.answer"):
            async with asyncio.timeout_at(deadline):
                if self.agent is None:
                    raise ChatUnavailable("Чат с базой знаний недоступен.")
                profile = Actor(user_id=actor.user_id, team=actor.team, role=actor.role)
                return await self.agent.answer(message, profile, deadline=deadline)
