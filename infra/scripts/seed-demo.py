import json
import os
import sys
from datetime import date, timedelta
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


BASE_URL = os.getenv("DEMO_URL", "http://127.0.0.1:8085").rstrip("/")
EMAIL = os.getenv("DEMO_EMAIL", "reviewer@example.test")
PASSWORD = os.getenv("DEMO_PASSWORD", "LocalDemo-Review-2026!")
TEAM_ID = "900001"
TRACK = "backend"


def request(method, path, *, body=None, token=None, expected=(200,)):
    headers = {"Accept": "application/json"}
    data = None
    if body is not None:
        headers["Content-Type"] = "application/json"
        data = json.dumps(body).encode("utf-8")
    if token:
        headers["Authorization"] = f"Bearer {token}"

    req = Request(BASE_URL + path, data=data, headers=headers, method=method)

    try:
        with urlopen(req, timeout=15) as response:
            status = response.status
            raw = response.read()
    except HTTPError as exc:
        status = exc.code
        raw = exc.read()

    if status not in expected:
        raise RuntimeError(f"{method} {path}: HTTP {status}")

    data = json.loads(raw) if raw and status < 400 else {}
    return status, data


def main():
    credentials = {"email": EMAIL, "password": PASSWORD}

    # 409 означает, что аккаунт уже есть. Вход ниже проверит пароль.
    request("POST", "/v1/auth/register", body=credentials, expected=(201, 409))
    _, session = request("POST", "/v1/auth/login", body=credentials)
    token = session["access_token"]

    _, identity = request("POST", "/v1/auth/validate", token=token)
    if not identity.get("is_valid") or not identity.get("user_id"):
        raise RuntimeError("Auth не подтвердил сессию демо-пользователя")

    user_id = str(identity["user_id"])

    profile_path = f"/v1/user/{user_id}/metadata"
    status, _ = request("GET", profile_path, token=token, expected=(200, 404))
    if status == 404:
        today = date.today()
        request(
            "POST",
            "/v1/user",
            token=token,
            expected=(200, 201),
            body={
                "userId": user_id,
                "firstName": "Демо",
                "lastName": "Стажёр",
                "teamId": TEAM_ID,
                "track": TRACK,
                "startDate": today.isoformat(),
                "plannedEndDate": (today + timedelta(days=90)).isoformat(),
            },
        )
        request("GET", profile_path, token=token)

    plan_path = f"/v1/users/{user_id}/plan"
    status, plan = request("GET", plan_path, token=token, expected=(200, 404))
    if status == 404:
        # Onboarding берёт трек из профиля пользователя.
        request("POST", plan_path, body={}, token=token)
        _, plan = request("GET", plan_path, token=token)

    steps = plan.get("steps") or []
    if plan.get("track", "").lower() != TRACK or not steps:
        raise RuntimeError("Ожидался непустой Backend-план для демо-пользователя")

    print(f"Стенд: {BASE_URL}")
    print(f"Логин: {EMAIL}")
    print(f"Пароль: {PASSWORD}")
    print(f"Roadmap: {TRACK}, шагов: {len(steps)}")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, URLError, ValueError, KeyError) as exc:
        sys.exit(f"Не удалось подготовить демо: {exc}")
