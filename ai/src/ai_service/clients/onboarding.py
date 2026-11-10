from http import HTTPStatus
from typing import Protocol

import httpx
from pydantic import ValidationError

from ..core.diagnostics import UpstreamError, http_failure, operation
from ..schemas.contracts import ActionResult, ActorRef


class OnboardingError(UpstreamError):
    """The operation did not produce a verified success."""


class OnboardingRejected(OnboardingError):
    """A known refusal that is safe to show to the user."""


class OnboardingUnavailable(OnboardingError):
    """The write was rejected at authentication or could not be sent."""


class OnboardingUnknown(OnboardingError):
    """The result of the write could not be confirmed."""


class Onboarding(Protocol):
    async def complete_step(self, actor: ActorRef, step_id: str) -> ActionResult: ...


class HttpOnboarding:
    def __init__(self, http: httpx.AsyncClient, base_url: str, token: str):
        self.http = http
        self.base_url = base_url.rstrip("/")
        self.token = token

    async def complete_step(self, actor: ActorRef, step_id: str) -> ActionResult:
        with operation("onboarding.complete"):
            return await self._complete_step(actor, step_id)

    async def _complete_step(self, actor: ActorRef, step_id: str) -> ActionResult:
        try:
            response = await self.http.put(
                f"{self.base_url}/v1/users/{actor.user_id}/onboarding/steps/{step_id}/completion",
                headers={"Authorization": f"Bearer {self.token}"},
            )
        except (httpx.ConnectError, httpx.ConnectTimeout, httpx.PoolTimeout) as error:
            raise OnboardingUnavailable("Сервис адаптации временно недоступен.", reason=http_failure(error)) from error
        except httpx.RequestError as error:
            raise OnboardingUnknown("Сервис адаптации временно недоступен.", reason=http_failure(error)) from error

        if response.status_code == HTTPStatus.UNAUTHORIZED:
            raise OnboardingUnavailable("Сервис адаптации отклонил авторизацию AI.", reason="http_401")
        if response.status_code == HTTPStatus.FORBIDDEN:
            raise OnboardingRejected("У пользователя нет права завершить этот шаг.", reason="http_403")
        if response.status_code == HTTPStatus.NOT_FOUND:
            raise OnboardingRejected("Шаг адаптации не найден.", reason="http_404")
        if response.status_code != HTTPStatus.OK:
            raise OnboardingUnknown(
                "Сервис адаптации не подтвердил выполнение шага.", reason=f"http_{response.status_code}"
            )
        try:
            result = ActionResult.model_validate_json(response.content)
        except ValidationError as error:
            raise OnboardingUnknown(
                "Сервис адаптации не подтвердил выполнение шага.", reason="invalid_response"
            ) from error
        if result.step_id != step_id:
            raise OnboardingUnknown("Сервис адаптации вернул результат для другого шага.", reason="wrong_step")
        return result
