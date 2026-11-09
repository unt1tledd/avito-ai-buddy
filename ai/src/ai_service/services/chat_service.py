from dataclasses import dataclass

from ..agents.agent import Agent
from ..clients.onboarding import Onboarding
from ..clients.user import HttpUser
from ..schemas.contracts import Actor, ActorRef, ChatResponse
from .onboarding_service import complete_step


class ChatUnavailable(Exception):
    pass


class ConversationRequired(Exception):
    pass


@dataclass(frozen=True)
class ChatService:
    agent: Agent | None
    user: HttpUser | None
    onboarding: Onboarding
    knowledge_configured: bool

    async def answer(
        self,
        *,
        conversation_id: str | None,
        actor: ActorRef,
        message: str,
        confirmed_step_id: str | None,
    ) -> ChatResponse:
        if confirmed_step_id is None and self.agent is None:
            if not self.knowledge_configured:
                raise ChatUnavailable("Не настроено подключение к базе знаний.")
            raise ChatUnavailable("Не настроены ключ и модель LLM.")

        if self.user is not None:
            if conversation_id is None:
                raise ConversationRequired("Требуется conversation_id.")
            actor = await self.user.get_actor(actor.user_id)
        elif not isinstance(actor, Actor):
            raise ChatUnavailable("В деморежиме требуется полный профиль actor.")

        if confirmed_step_id is not None:
            return await complete_step(self.onboarding, actor, confirmed_step_id)

        assert self.agent is not None
        return await self.agent.answer(message, actor)
