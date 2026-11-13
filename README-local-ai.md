# Локальный AI по инструкции девопса

Рабочий каталог: `/Users/aekimov/ai-buddy/avito-ai-buddy`. `.env` находится здесь же, в корне, и уже заполнен.
Запускаются только PostgreSQL и AI; user/auth для режима AI_MODE=demo не нужны.

## Команды

На Mac с запущенным Docker Desktop sudo не требуется. Выполнять из корня репозитория:

```sh
cd /Users/aekimov/ai-buddy/avito-ai-buddy
docker compose -p ai-rag-check up --wait db
docker compose -p ai-rag-check ps db
docker compose -p ai-rag-check build ai
docker compose -p ai-rag-check --profile tools run --rm ai-migrate
docker compose -p ai-rag-check run --rm ai \
  /app/.venv/bin/python -m ai_service.rag.import_documents \
  /app/knowledge/documents.json
docker compose -p ai-rag-check run --rm -p 127.0.0.1:18000:8080 ai
```

- db — один локальный PostgreSQL с отдельными базами сервисов; AI использует ai_buddy под пользователем ai_app.
- build ai — собирает образ с кодом и ai/knowledge/documents.json.
- ai-migrate — применяет только недостающие миграции AI.
- import_documents — отправляет 144 статьи в OpenRouter/DeepInfra, создаёт эмбеддинги и атомарно заменяет корпус в БД.
- Последний run — запускает сервер. На Mac открывается порт 18000, внутри контейнера сервер слушает 8080. Не запускать второй AI на том же порту.

Флаг -p ai-rag-check задаёт имя проекта и его Docker volume. --rm удаляет завершившийся контейнер, сохраняя данные БД.
Импорт нужен при изменении корпуса или профиля эмбеддингов; для обычного перезапуска сервера повторять импорт не нужно.

## Фоновый запуск

Текущий запуск отличается от последней команды только флагом -d (фон) и именем контейнера:

```sh
docker compose -p ai-rag-check run --rm -d --name ai-rag-check-api -p 127.0.0.1:18000:8080 ai
docker logs --tail 50 ai-rag-check-api
docker stop ai-rag-check-api
```

Для контейнера, созданного через run, остановка выполняется по имени через docker stop. После остановки --rm удалит контейнер; для повторного запуска используется run снова.

## Проверка и доступ

AI: http://127.0.0.1:18000. Готовность: http://127.0.0.1:18000/readyz.
POST /api/v1/chat требует заголовок Authorization: Bearer <AI_SERVICE_TOKEN из .env>.
AI только отвечает на вопросы и возвращает источники. Подтверждения шагов выполняются вне AI; поле confirmed_step_id не принимается. Настройки QUESTIONS_ONLY и ONBOARDING_* для AI не нужны.

```sh
docker compose -p ai-rag-check exec db psql -h db -U ai_app -d ai_buddy
```

Пароль — AI_DB_PASSWORD из .env. PostgreSQL доступен внутри Docker на db:5432; порт на Mac не опубликован.
Корпус: 144 documents, 144 document_chunks, 280 document_access; вектора размерности 1536.
Данные хранятся в volume ai-rag-check_postgres_data. .env содержит секреты и исключён из Git.
