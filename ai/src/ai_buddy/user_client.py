import httpx
from pydantic import ValidationError

from .contracts import Actor


class UserError(Exception):
    pass


class HttpUser:
    def __init__(self, http: httpx.AsyncClient, base_url: str, token: str):
        self.http = http
        self.base_url = base_url.rstrip("/")
        self.token = token

    async def get_actor(self, user_id: str) -> Actor:
        try:
            response = await self.http.get(
                f"{self.base_url}/v1/users/{user_id}",
                headers={"Authorization": f"Bearer {self.token}"},
                timeout=5,
            )
            response.raise_for_status()
            actor = Actor.model_validate_json(response.content)
        except (httpx.HTTPError, ValidationError) as error:
            raise UserError("Не удалось получить профиль из сервиса user.") from error
        if actor.user_id != user_id:
            raise UserError("Сервис user вернул профиль другого пользователя.")
        return actor
