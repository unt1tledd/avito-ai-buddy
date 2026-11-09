import asyncio
import sqlite3
from contextlib import closing
from pathlib import Path
from typing import Protocol

import httpx
from pydantic import ValidationError

from .contracts import ActionResult, Actor


class OnboardingError(Exception):
    """An onboarding error that is safe to show to the user."""


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
            raise OnboardingError("Сервис адаптации временно недоступен.") from error

        if response.status_code == 403:
            raise OnboardingError("У пользователя нет права завершить этот шаг.")
        if response.status_code == 404:
            raise OnboardingError("Шаг адаптации не найден.")
        try:
            response.raise_for_status()
            result = ActionResult.model_validate_json(response.content)
        except (httpx.HTTPStatusError, ValidationError) as error:
            raise OnboardingError("Сервис адаптации не подтвердил выполнение шага.") from error
        if result.step_id != step_id:
            raise OnboardingError("Сервис адаптации вернул результат для другого шага.")
        return result


class DemoOnboarding:
    def __init__(self, path: Path):
        self.path = path

    async def complete_step(self, actor: Actor, step_id: str) -> ActionResult:
        if actor.role != "intern" or actor.team != "backend":
            raise OnboardingError("У пользователя нет права завершить этот шаг.")
        if step_id != "dev-environment":
            raise OnboardingError("Шаг адаптации не найден.")
        try:
            await asyncio.to_thread(self._save, actor.user_id, step_id)
        except (OSError, sqlite3.Error) as error:
            raise OnboardingError("Тестовый сервис не смог сохранить результат.") from error
        return ActionResult(step_id=step_id, status="completed")

    def _save(self, user_id: str, step_id: str) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with closing(sqlite3.connect(self.path)) as connection, connection:
            connection.execute(
                "CREATE TABLE IF NOT EXISTS completed_steps ("
                "user_id TEXT NOT NULL, step_id TEXT NOT NULL, PRIMARY KEY (user_id, step_id))"
            )
            connection.execute(
                "INSERT INTO completed_steps (user_id, step_id) VALUES (?, ?) "
                "ON CONFLICT (user_id, step_id) DO NOTHING",
                (user_id, step_id),
            )
