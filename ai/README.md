# AI-buddy · AI-сервис

Внутренний Python-сервис для `chat`. Агент вызывает модель через Responses API, запускает инструменты через MCP в том же процессе и возвращает текст, источники и подтверждённый результат действия. Инструмент поиска использует локальный корпус, а инструмент изменения статуса обращается к адаптеру `onboarding`.

## Локальный запуск

Из каталога `ai/` выполни:

```sh
cp .env.example .env
uv sync --locked
```

Заполни в `.env` три поля:

- `AI_SERVICE_TOKEN` — общий секрет между `chat` и `ai`.
- `LLM_API_KEY` — ключ провайдера.
- `LLM_MODEL` — ID доступной модели с поддержкой вызова инструментов через Responses API.

`LLM_BASE_URL` по умолчанию указывает на OpenAI. Для другого адреса нужен совместимый Responses API.

```sh
uv run --locked --env-file .env uvicorn ai_buddy.app:create_app --factory --port 8000
curl http://localhost:8000/healthz
```

Для Docker из того же каталога:

```sh
docker build -t ai-buddy-ai .
docker run --rm --env-file .env -p 127.0.0.1:8000:8000 \
  -v ai-buddy-demo:/app/.data ai-buddy-ai
```

## Контракт с сервисом chat

`POST /v1/chat` принимает запрос с заголовком `Authorization: Bearer <AI_SERVICE_TOKEN>`. Поля `actor`, `history` и `confirmed_step_id` формирует сам сервис `chat` из проверенной сессии и состояния диалога. Значения `actor` определяют, какие документы попадут в контекст модели. Поле `confirmed_step_id` передавай после подтверждения конкретного шага в интерфейсе.

Пример запроса на поиск:

```json
{
  "actor": {"user_id": "demo-intern", "team": "backend", "role": "intern"},
  "message": "Как настроить окружение?"
}
```

Для действия используй тот же `actor`, сообщение «Отметь шаг настройки окружения выполненным» и поле `"confirmed_step_id": "dev-environment"`. Интерфейс берёт статус из поля `action` ответа. В `sources` возвращаются ID, заголовок, фрагмент и URL при наличии. Тестовые источники можно показать в интерфейсе по заголовку и фрагменту.

Контракт сервиса описан в [OpenAPI](api/ai/v1/openapi.yaml). Дополнительно запущенный сервис отдаёт схему по `/openapi.json`.

## Интеграция с onboarding

При пустом `ONBOARDING_URL` адаптер использует локальную SQLite-имитацию. Для команды `backend` с ролью `intern` шаг `dev-environment` сохраняется в файле `DEMO_DB_PATH`. Повторный запрос к тому же шагу возвращает тот же статус.

Для интеграции укажи `ONBOARDING_URL` и `ONBOARDING_SERVICE_TOKEN`. Предлагаемый контракт, который нужно согласовать с владельцем сервиса:

```http
PUT /v1/users/{user_id}/onboarding/steps/{step_id}/completion
Authorization: Bearer <ONBOARDING_SERVICE_TOKEN>
```

Сервис проверяет права на шаг и отвечает `200` с телом `{"step_id":"dev-environment","status":"completed"}`, либо `403` или `404`. Повторный PUT для выполненного шага должен возвращать его текущий статус.

## Тесты

```sh
uv run --locked pytest --cov=ai_buddy --cov-report=term-missing --cov-fail-under=81
```

Тесты запускаются без ключа LLM. Сквозные тесты проходят через HTTP-вход, агент, реальный MCP-клиент и сервер в памяти, поиск и SQLite-имитацию. Вызовы модели и внешнего `onboarding` проверяются на тестовых ответах.
