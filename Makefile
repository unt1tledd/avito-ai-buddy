GO_MODULES := auth user onboarding task chat pkg integration-tests

.PHONY: deps verify fmt lint test vet tools-check load-test load-test-one load-test-list integration-test-one local-up local-up-new local-monitoring local-status local-logs local-stop local-up local-up-new local-monitoring local-status local-logs local-stop help integration-test-list update-docker update-docker-one docker-status lint-py lint-fr
deps: ## Скачать зависимости Go
	@set -e; for module in $(GO_MODULES); do (cd $$module && GOWORK=off go mod download all); done
verify: ## Проверить контрольные суммы зависимостей Go
	@set -e; for module in $(GO_MODULES); do (cd $$module && GOWORK=off go mod verify); done
fmt: ## Отформатировать Go-код
	@set -e; for module in $(GO_MODULES); do (cd $$module && golangci-lint fmt --config ../.golangci.yaml); done
lint-go: ## Go-линтер, включая исходники интеграционных тестов
	@status=0; \
	for module in $(GO_MODULES); do \
		printf '\n=== Go lint: %s ===\n' "$$module"; \
		( \
			cd "$$module" || exit 1; \
			if [ "$$module" = integration-tests ]; then \
				golangci-lint run --config ../.golangci.yaml --build-tags=integration ./...; \
			else \
				golangci-lint run --config ../.golangci.yaml ./...; \
			fi \
		) || status=1; \
	done; \
	exit "$$status"


lint-py: ## Python-линтер (Ruff)
	cd ai && uv run --locked --group dev ruff check src tests

lint-fr: ## Фронтенд-линтер (ESLint)
	cd frontend && npm run lint	
test: ## Запустить Go-тесты с проверкой гонок
	@set -e; for module in $(GO_MODULES); do ( \
		cd $$module; \
		packages=$$(go list ./...); \
		if [ -n "$$packages" ]; then go test -race -count=1 ./...; fi \
	); done
vet: ## Запустить go vet
	@set -e; for module in $(GO_MODULES); do (cd $$module && go vet ./...); done
tools-check: ## Показать версии инструментов Go
	go version
	golangci-lint version


DOCKER_USE_SUDO ?= 0
LOCAL_DOCKER = $(if $(filter 1,$(DOCKER_USE_SUDO)),sudo docker,docker)
SERVICES ?= chat ai auth task


local-up: ## Поднять приложение с сохранёнными данными
	DOCKER_USE_SUDO="$(DOCKER_USE_SUDO)" bash infra/scripts/up-local.sh

local-up-new: ## Полностью пересобрать приложение вместе с базой данных и ее индексами 
	DOCKER_USE_SUDO="$(DOCKER_USE_SUDO)" bash infra/scripts/up-local.sh --import-ai	

local-monitoring: ## Запустить Grafana, Loki, Alloy, Blackbox и Prometheus
	$(LOCAL_DOCKER) compose --profile observability up -d loki alloy blackbox prometheus grafana
	@addr=$$($(LOCAL_DOCKER) compose --profile observability port grafana 3000) && \
		printf '\nGrafana: http://127.0.0.1:%s/\n' "$${addr##*:}"	

local-ai-status: ## Готовность AI
	curl -siS --max-time 10 http://127.0.0.1:8000/readyz

local-logs: ## Смотреть логи сервисов, например SERVICES=auth
	$(LOCAL_DOCKER) compose --profile observability logs -f --tail 50 $(SERVICES)

local-stop: ## Остановить приложение и мониторинг, сохранив данные
	$(LOCAL_DOCKER) compose --profile observability stop

SOAK_SECONDS ?= 60
LOAD_ARGS ?=


load-test: ## Полное нагрузочное тестирование 
	@DOCKER_USE_SUDO="$(DOCKER_USE_SUDO)" python3 tests/load/run.py all \
		--soak-seconds "$(SOAK_SECONDS)" $(LOAD_ARGS)

load-test-one: ## Запустить один нагрузочный сценарий, например TEST=task
	@test -n "$(TEST)" || { echo 'Пример: make load-test-one TEST=task'; exit 2; }
	@DOCKER_USE_SUDO="$(DOCKER_USE_SUDO)" python3 tests/load/run.py "$(TEST)" $(LOAD_ARGS)


load-test-list: ## Показать нагрузочные сценарии
	@python3 tests/load/run.py --list

integration-test: ## Полное интеграционное тестирование
	bash integration-tests/run.sh	

integration-test-one: ## Запустить один интеграционный тест, например TEST=TestAuthLifecycle
	@test -n "$(TEST)" || { echo 'Пример: make integration-test-one TEST=TestAuthLifecycle'; exit 2; }
	@DOCKER_USE_SUDO="$(DOCKER_USE_SUDO)" bash integration-tests/run.sh \
		go test -tags=integration -race -count=1 -timeout=10m -v \
		-run '^$(TEST)$$' ./...


integration-test-list: ## Показать список интеграционных тестов
	@awk 'BEGIN { \
		desc["TestAuthLifecycle"] = "                 Вход, ротация refresh-токена и выход"; \
		desc["TestAuthInvalidInput"] = "             Отказ при некорректных данных и токенах"; \
		desc["TestConcurrentRegistrationCreatesOneAccount"] = "Один аккаунт при параллельной регистрации"; \
		desc["TestUserPersistsAcrossGRPCAndHTTP"] = "          Создание профиля через gRPC и чтение через HTTP"; \
		desc["TestUserInvalidCreationDoesNotPersist"] = "       Невалидные данные профиля не сохраняются"; \
		desc["TestChatHistoryIdempotencyAndIsolation"] = "      История, пагинация, повторы и изоляция пользователей"; \
		desc["TestChatRejectsUnauthorizedAndInvalidMessages"] = "Доступ к истории и валидация сообщений"; \
		desc["TestConcurrentMessageRetriesSaveOnePair"] = "     Одна пара сообщений при конкурентных повторах"; \
		desc["TestAccessRequestLifecycle"] = "               Заявка, решение и уведомление в Chat через Kafka"; \
		desc["TestConcurrentTaskDecisionsHaveOneWinner"] = " Одно решение при конкурентном одобрении и отклонении"; \
		desc["TestTaskAuthorizationAndValidation"] = "       Авторизация и валидация запросов Task"; \
	} \
	/^func Test[[:alnum:]_]+\(/ { \
		name = $$2; sub(/\(.*/, "", name); \
		printf "%s: %s\n", name, (name in desc ? desc[name] : "Описание пока не добавлено"); \
	}' integration-tests/*_test.go

APP_SERVICES := auth user chat onboarding ai task frontend


update-docker: ## Пересобрать и перезапустить все сервисы приложения
	$(LOCAL_DOCKER) compose up -d --build --force-recreate --no-deps $(APP_SERVICES)

update-docker-one: ## Пересобрать и перезапустить выбранные сервисы
	@test -n "$(strip $(SERVICE))" || { echo 'Пример: make update-docker-one SERVICE=frontend'; exit 2; }
	$(LOCAL_DOCKER) compose up -d --build --force-recreate --no-deps $(SERVICE)

docker-status: ## Показать запущенные и остановленные контейнеры проекта
	$(LOCAL_DOCKER) compose --profile "*" ps -a

help: ## Показать команды проекта и их назначение
	@awk '/^[[:alnum:]_][[:alnum:]_.-]*[[:space:]]*:([[:space:]]|$$)/ { \
		name = $$0; sub(/[[:space:]]*:.*/, "", name); \
		if (seen[name]++) next; \
		desc = $$0; \
		if (desc ~ /##/) sub(/^.*##[[:space:]]*/, "", desc); \
		else desc = "Описание пока не добавлено"; \
		printf "  %-24s %s\n", name, desc; \
	}' $(MAKEFILE_LIST)

		