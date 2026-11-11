import re
from datetime import date
from http import HTTPStatus

import httpx
from pydantic import BaseModel, StrictStr, ValidationError

from ..core.diagnostics import UpstreamError, http_failure, operation
from ..schemas.contracts import Actor


USER_LOOKUP_TIMEOUT_SECONDS = 5


class UserError(UpstreamError):
    pass


def is_user_id(value: str) -> bool:
    return re.fullmatch(r"[1-9][0-9]{0,18}", value) is not None and int(value) <= 2**63 - 1


class Intern(BaseModel):
    user_id: StrictStr


class Team(BaseModel):
    team_id: StrictStr


class Metadata(BaseModel):
    intern: Intern
    team: Team
    actual_end_date: date | None = None


class HttpUser:
    def __init__(self, http: httpx.AsyncClient, base_url: str, token: str):
        self.http = http
        self.base_url = base_url.rstrip("/")
        self.token = token

    async def get_actor(self, user_id: str) -> Actor:
        with operation("user.metadata"):
            if not is_user_id(user_id):
                raise UserError("Некорректный идентификатор пользователя.", reason="invalid_id")
            try:
                response = await self.http.get(
                    f"{self.base_url}/v1/user/{user_id}/metadata",
                    headers={"Authorization": f"Bearer {self.token}"},
                    timeout=USER_LOOKUP_TIMEOUT_SECONDS,
                )
                if response.status_code == HTTPStatus.NOT_FOUND:
                    raise UserError("Профиль стажёра не найден.", reason="profile_missing")
                response.raise_for_status()
            except httpx.HTTPError as error:
                raise UserError("Не удалось получить профиль из сервиса user.", reason=http_failure(error)) from error
            try:
                profile = Metadata.model_validate_json(response.content)
            except ValidationError as error:
                raise UserError("Сервис user не вернул профиль стажёра.", reason="invalid_profile") from error
            if profile.intern.user_id != user_id:
                raise UserError("Сервис user вернул профиль другого пользователя.", reason="wrong_user")
            if profile.actual_end_date is not None:
                raise UserError("У пользователя нет действующего профиля стажёра.", reason="internship_ended")
            if not is_user_id(profile.team.team_id):
                raise UserError("Сервис user вернул некорректную команду.", reason="invalid_team")
            # The gateway describes an internship. Only its verified intern gets the intern ACL.
            return Actor(user_id=user_id, team=profile.team.team_id, role="intern")
