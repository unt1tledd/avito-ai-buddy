import asyncio
from dataclasses import dataclass

from ..agents.agent import Agent
from ..clients.user import HttpUser, is_user_id
from ..core.diagnostics import operation
from ..schemas.contracts import Actor, ActorRef, ChatResponse


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
    user: HttpUser | None
    integration: bool
    unavailable_reason: str | None

    async def answer(
        self,
        *,
        conversation_id: str | None,
        actor: ActorRef,
        message: str,
        deadline: float,
    ) -> ChatResponse:
        if self.integration:
            if conversation_id is None:
                raise InvalidChat(("conversation_id",), "Требуется conversation_id.", "missing")
            if not is_user_id(actor.user_id):
                raise InvalidChat(("actor", "user_id"), "Требуется положительный int64 в виде строки.", "value_error")
        elif not isinstance(actor, Actor):
            raise ChatUnavailable("В деморежиме требуется полный профиль actor.")

        if asyncio.get_running_loop().time() >= deadline:
            raise TimeoutError
        with operation("chat.answer"):
            async with asyncio.timeout_at(deadline):
                if self.agent is None:
                    raise ChatUnavailable("Чат с базой знаний недоступен.")
                if self.user is not None:
                    actor = await self.user.get_actor(actor.user_id)
                assert isinstance(actor, Actor)
                return await self.agent.answer(message, actor)
