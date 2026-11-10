import asyncio
import sqlite3
from pathlib import Path

from ..clients.onboarding import Onboarding, OnboardingRejected, OnboardingUnknown
from ..core.diagnostics import operation
from ..repositories.demo_onboarding import save_completion
from ..schemas.contracts import ActionResult, Actor, ActorRef, ChatResponse


def unknown_completion() -> ChatResponse:
    return ChatResponse(answer="Результат завершения шага пока неизвестен.", sources=[])


async def complete_step(onboarding: Onboarding, actor: ActorRef, step_id: str) -> ChatResponse:
    try:
        action = await onboarding.complete_step(actor, step_id)
    except OnboardingUnknown:
        return unknown_completion()
    except OnboardingRejected as error:
        return ChatResponse(answer=str(error), sources=[])
    return ChatResponse(answer="Шаг адаптации отмечен как выполненный.", sources=[], action=action)


class DemoOnboarding:
    def __init__(self, path: Path):
        self.path = path

    async def complete_step(self, actor: ActorRef, step_id: str) -> ActionResult:
        with operation("onboarding.demo"):
            if not isinstance(actor, Actor) or actor.role != "intern" or actor.team != "backend":
                raise OnboardingRejected("У пользователя нет права завершить этот шаг.", reason="forbidden")
            if step_id != "dev-environment":
                raise OnboardingRejected("Шаг адаптации не найден.", reason="step_missing")
            try:
                await asyncio.to_thread(save_completion, self.path, actor.user_id, step_id)
            except (OSError, sqlite3.Error) as error:
                raise OnboardingUnknown("Тестовый сервис не смог сохранить результат.", reason="sqlite") from error
            return ActionResult(step_id=step_id, status="completed")
