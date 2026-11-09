# AI-buddy · AI-сервис

Внутренний Python-сервис для `chat`. При первом сообщении диалога сервис получает роль и команду из `user` и сохраняет их в PostgreSQL. Для каждого вопроса он использует сохранённый профиль, векторизует вопрос и ищет до трёх доступных фрагментов в базе знаний с pgvector. В LLM передаются вопрос, история, роль, команда и найденные фрагменты. Правила ответа из `src/ai_buddy/prompts/system.txt` передаются отдельно. В HTTP-ответе `chat` получает текст, источники и результат действия, если оно было выполнено. Через MCP модель может уточнить поиск или вызвать инструмент адаптации для подтверждённого шага.

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

`LLM_BASE_URL` по умолчанию указывает на OpenAI. Для другого адреса нужен совместимый Responses API. Для локального PostgreSQL с pgvector выполни из `ai/`:

```sh
docker network create ai-buddy
docker run -d --name ai-buddy-knowledge --network ai-buddy \
  -p 127.0.0.1:5433:5432 -e POSTGRES_DB=ai_knowledge \
  -e POSTGRES_USER=ai_buddy -e POSTGRES_PASSWORD=ai_buddy \
  -v ai-buddy-knowledge:/var/lib/postgresql/data \
  --mount "type=bind,source=$(pwd)/knowledge/schema.sql,target=/docker-entrypoint-initdb.d/01-schema.sql,readonly" \
  --mount "type=bind,source=$(pwd)/sql/chat_profiles.sql,target=/docker-entrypoint-initdb.d/02-chat-profiles.sql,readonly" \
  pgvector/pgvector:pg16
```

Когда PostgreSQL запустится, загрузи тестовый корпус и запусти сервис. `KNOWLEDGE_DB_DSN` из `.env.example` соответствует локальному контейнеру.

```sh
uv run --locked --env-file .env python -m ai_buddy.rag knowledge/documents.json
uv run --locked --env-file .env uvicorn ai_buddy.app:create_app --factory --port 8000
curl http://localhost:8000/healthz
```

Для Docker из того же каталога:

```sh
docker build -t ai-buddy-ai .
docker run --rm --env-file .env --network ai-buddy -p 127.0.0.1:8000:8000 \
  -e KNOWLEDGE_DB_DSN=postgresql://ai_buddy:ai_buddy@ai-buddy-knowledge:5432/ai_knowledge \
  -v ai-buddy-demo:/app/.data ai-buddy-ai
```

## Контракт с сервисом chat

`POST /api/v1/chat` принимает запрос с заголовком `Authorization: Bearer <AI_SERVICE_TOKEN>`. Сервис `chat` берёт `actor.user_id` из проверенной сессии, а `conversation_id` и `history` из диалога. Передавай один и тот же `conversation_id` для всех сообщений диалога. При настроенном `USER_URL` это обязательное поле. Поле `confirmed_step_id` передавай после подтверждения конкретного шага в интерфейсе. Прежний путь `POST /v1/chat` поддерживается для совместимости.

Пример запроса на поиск:

```json
{
  "conversation_id": "demo-conversation",
  "actor": {"user_id": "demo-intern"},
  "message": "Как настроить окружение?"
}
```

Для действия используй тот же `actor`, сообщение «Отметь шаг настройки окружения выполненным» и поле `"confirmed_step_id": "dev-environment"`. Интерфейс берёт статус из поля `action` ответа. В `sources` возвращаются ID, заголовок, фрагмент и URL при наличии. Тестовые источники можно показать в интерфейсе по заголовку и фрагменту.

Контракт сервиса описан в [OpenAPI](api/ai/v1/openapi.yaml). Дополнительно запущенный сервис отдаёт схему по `/openapi.json`.

## Интеграция с user

Укажи `USER_URL` и `USER_SERVICE_TOKEN`. Адаптер ожидает следующий внутренний HTTP-контракт. Его нужно реализовать на стороне `user`.

```http
GET /v1/users/{user_id}
Authorization: Bearer <USER_SERVICE_TOKEN>
```

Ожидаемый ответ `200` с телом `{"user_id":"demo-intern","team":"backend","role":"intern"}`. Сервис сохраняет полученные роль и команду для пары `conversation_id` и `user_id`. Параллельные первые сообщения используют один сохранённый профиль. Для нового диалога профиль загружается заново. При пустом `USER_URL` для локального демо передай полный `actor` из примера ответа. Запрос с одним `user_id` в этом режиме вернёт `503`.

Профили хранятся в `CHAT_PROFILE_DB_DSN`. Если переменная пустая, используется `KNOWLEDGE_DB_DSN`. Если база уже существует, примени схему профилей. Сервису нужны права `SELECT` и `INSERT` на таблицу `ai_chat_profiles`.

```sh
psql "${CHAT_PROFILE_DB_DSN:-$KNOWLEDGE_DB_DSN}" -v ON_ERROR_STOP=1 -f sql/chat_profiles.sql
```

При ошибке получения профиля, эмбеддинга или чтения базы знаний `POST /api/v1/chat` возвращает `502` до вызова LLM. Если поиск завершился без совпадений, модель получает пустой список источников.

## База знаний

Для отдельной PostgreSQL-базы укажи `KNOWLEDGE_DB_DSN` и один раз примени [схему](knowledge/schema.sql) под ролью с правом создать расширение `vector` и таблицу. Во время поиска сервису нужен доступ на чтение.

Импорт принимает массив фрагментов в формате [тестового корпуса](knowledge/documents.json). Каждая запись содержит `id`, `title`, `text` и список разрешённых `roles`. Поля `url`, `keywords` и `teams` необязательны. Пустой список `teams` открывает фрагмент всем командам с разрешённой ролью.

```sh
psql "$KNOWLEDGE_DB_DSN" -v ON_ERROR_STOP=1 -f knowledge/schema.sql
uv run --locked --env-file .env python -m ai_buddy.rag /path/to/company-documents.json
```

Команда векторизует заголовок, текст и ключевые слова через `POST /embeddings`, затем в одной транзакции заменяет весь корпус в `ai_knowledge_chunks`. Загружай полный снимок документов. При смене модели эмбеддингов повтори импорт.

- `EMBEDDING_MODEL` задаёт общую модель для импорта и поиска. По умолчанию используется `text-embedding-3-small`.
- `EMBEDDING_BASE_URL` задаёт отдельный адрес совместимого API эмбеддингов. Если поле пустое, используется `LLM_BASE_URL`.
- `EMBEDDING_API_KEY` задаёт ключ для эмбеддингов. При пустых `EMBEDDING_BASE_URL` и `EMBEDDING_API_KEY` используется `LLM_API_KEY`.
- `KNOWLEDGE_MIN_SIMILARITY` задаёт минимальное косинусное сходство от `-1` до `1`. Значение по умолчанию — `0.3`.

Во время поиска сервис отправляет в API эмбеддингов только текст вопроса. PostgreSQL фильтрует фрагменты по роли, команде, модели и размерности, затем выполняет точный поиск по косинусному расстоянию. В LLM передаются до трёх фрагментов выше порога.

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

Тесты запускаются без ключей и запущенной базы. Они проверяют повторное использование профиля в диалоге, запрос к API эмбеддингов, параметры поиска, контекст первого запроса к LLM и вызовы MCP на тестовых ответах.
