from http import HTTPStatus
from typing import Protocol

import httpx
from pydantic import ValidationError

from ..schemas.contracts import ActionResult, Actor


class OnboardingError(Exception):
    """The operation did not produce a verified success."""


class OnboardingRejected(OnboardingError):
    """A known refusal that is safe to show to the user."""


class OnboardingUnknown(OnboardingError):
    """The result of the write could not be confirmed."""


class Onboarding(Protocol):
    async def complete_step(self, actor: Actor, step_id: str) -> ActionResult: ...


class HttpOnboarding:
    def __init__(self, http: httpx.AsyncClient, base_url: str, token: str):
        self.http = http
        self.base_url = base_url.rstrip("/")
        self.token = token

    async def complete_step(self, actor: Actor, step_id: str) -> ActionResult:
        try:
            response = await self.http.put(
                f"{self.base_url}/v1/users/{actor.user_id}/onboarding/steps/{step_id}/completion",
                headers={"Authorization": f"Bearer {self.token}"},
            )
        except httpx.RequestError as error:
            raise OnboardingUnknown("Сервис адаптации временно недоступен.") from error

        if response.status_code == HTTPStatus.FORBIDDEN:
            raise OnboardingRejected("У пользователя нет права завершить этот шаг.")
        if response.status_code == HTTPStatus.NOT_FOUND:
            raise OnboardingRejected("Шаг адаптации не найден.")
        if response.status_code != HTTPStatus.OK:
            raise OnboardingUnknown("Сервис адаптации не подтвердил выполнение шага.")
        try:
            result = ActionResult.model_validate_json(response.content)
        except ValidationError as error:
            raise OnboardingUnknown("Сервис адаптации не подтвердил выполнение шага.") from error
        if result.step_id != step_id:
            raise OnboardingUnknown("Сервис адаптации вернул результат для другого шага.")
        return result
