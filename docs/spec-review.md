# AI Buddy: вопросы и расхождения для согласования спецификаций

Дата анализа: 16.11.2026. Статус: открытый review начальной базы спецификаций.

База review сверена с коммитом `bb579fb34bc8f8cc9bf7f08b8700fee8bdf7809f` (`fix: consumer task, frontend api, token onboarding`) ветки `infra/local-stack`. Последующие локальные изменения реализуют оставшийся путь D2; проверка на настоящем PostgreSQL ещё не выполнена. D1 исправлен; D3 исправлен частично; D4–D7 остаются открытыми. Исправление чтения Kafka учтено в D8.

## Как читать базу

Новые спецификации описывают подтверждённые контрактами и реализацией правила. Это начальная база для обсуждения, а не свидетельство приёмки продукта. `SHALL` фиксирует правило в указанной области; наличие такого правила не означает, что весь сквозной пользовательский сценарий проверен или лишён дефектов.

В этом документе разделены:

- **D — дефекты и противоречия:** наблюдаемая несогласованность кода, контрактов, тестовых ожиданий или документации. Ошибочное поведение не включено в спецификации как желаемое.
- **R — решения команды:** существующий код не позволяет вывести согласованное продуктовое правило. Варианты не считаются принятыми.
- **A — неподтверждённые предположения:** утверждения, которые нельзя использовать как основание для требований или готовности продукта.

Первичное изучение и подготовка документации не включали запуск приложения, стенда или тестов приложения. При последующей реализации D2 выполнены тесты логики Chat/Task с `-race`, frontend-тесты, lint и сборка. Объём и ограничения проверки D2 указаны ниже; остальные ссылки на тесты сами по себе не означают свежий успешный межсервисный прогон.

## Дефекты и противоречия

### D1. JSON Task несовместим с клиентом frontend — исправлено по коду

**Исходное расхождение.** Task включает `UseProtoNames: true` и возвращает поля `task_id`, `proposal_id`, `resource_id`, `access_level`, `created_at`, `external_request_id`. До коммита frontend читал только camelCase, поэтому терял реквизиты и не мог получить следующий курсор из ответа со snake_case.

**Подтверждённое исправление.** `TaskDto` и `fetchUserRequests` поддерживают оба написания, отдавая приоритет snake_case. Это распространяется на ID, ресурс, уровень, дату и внешний номер. Для отсутствующего внешнего номера используется ID задачи; для отсутствующего ID предложения — ID задачи. Курсор берётся из `task_id` либо `taskId`, проверяется и сравнивается через `BigInt`, без преобразования int64 в `Number`.

**Проверки в коде.** Добавлен тест `user requests render the fields emitted by the Task gateway`: snake_case-ответ преобразуется, сортируется по дате и рендерится через `RequestList` с ресурсом, уровнем, номером и статусом без `undefined`. Тест пагинации переведён на snake_case и сохраняет проверку ID `9007199254740993`; прежний тест camelCase остаётся проверкой совместимости. Это прочитанные тесты, не свежий прогон и не браузерная проверка.

Основания: [Task marshaler](../task/internal/app/app.go), [Task protobuf](../task/api/task/v1/task.proto), [frontend mapping](../frontend/src/lib/api.ts), [fixtures frontend](../frontend/tests/onboarding.test.mjs), [интеграционное чтение Task](../integration-tests/task_test.go).

**Отражение в базе.** Несовместимость формата больше не является открытым дефектом; серверный HTTP-контракт Task не менялся. Упоминание D1 как текущей ошибки в [product](product.md) устарело и требует отдельной синхронизации. В этой актуализации изменён только review.

### D2. Статусы доставки и создания заявки — исправление реализовано, проверка БД ожидается

**Реализованные этапы.** Подтверждение пользователя сохраняет `pending_dispatch`. `SUCCESS` локального outbox подтверждает только публикацию команды в Kafka. После успешного `Create` Task публикует фактический статус и ID сохранённой заявки; событие `pending` переводит карточку в ожидание решения. Одобрение и отказ по-прежнему приходят отдельными событиями решений. Публикация команды не приравнивается к созданию Task.

**Ошибки и неизвестный результат.** Сохраняется SQL-проекция локального `FAILED` outbox в `failed` для `GetReply` и `ListByConversation`. Дополнительно consumer Task после подтверждённой записи команды в DLQ повторно проверяет наличие заявки. Если её нет, публикует `failed` для предложения и попытки; если она уже создана, сообщает её действительный статус вместо ложной ошибки. При ошибке чтения БД результат продолжает выясняться. Offset команды фиксируется после подтверждения публикации результата; неидентифицируемый payload остаётся в DLQ без изменения пользовательского состояния.

**Повтор и запоздалые события.** История сохраняет `failed`, а карточка предлагает повтор. ConfirmProposal до истечения TTL сбрасывает ту же запись outbox и сохраняет новую команду с новым `dispatch_id`, в том числе когда прежний outbox уже имел `SUCCESS`, но Task сообщил ошибку. Chat сверяет промежуточные события с текущей попыткой и сериализует их применение с повторным подтверждением. Старые pending/failed и повторная доставка не должны откатывать новую попытку или approved/rejected. Событие ошибки без ID созданной задачи не заменяет уже подтверждённый `pending` с ID Task.

Основания: [ConfirmProposal](../chat/internal/usecase/action/action.go), [контракт событий](../chat/internal/port/clients.go), [применение события и блокировка](../chat/internal/repository/action/postgres.go), [SQL переходов](../chat/internal/repository/action/queries.sql), [outbox SQL](../chat/internal/repository/outbox/queries.sql), [history SQL](../chat/internal/repository/message/queries.sql), [обработчик результата Task](../task/internal/consumer/kafka/create.go), [consumer и commit](../task/internal/consumer/kafka/consumer.go), [Chat TaskStatus](../chat/internal/usecase/taskstatus/taskstatus.go), [карточка](../frontend/src/pages/AccessProposalCard.tsx).

**Проверки.** Добавлены проверки результата Create и восстановления после неоднозначного исхода, порядка «DLQ → результат → commit», повторного подтверждения после ошибки Task, смены ID попытки и ключей дедупликации. Frontend-тест проверяет `failed → кнопка → confirm → pending_dispatch → pending` через API-клиент и рендер карточки. В межсервисном lifecycle-тесте добавлено ожидание `pending` с ID Task до решения. Отдельный PostgreSQL-тест удалён по замечанию ревью.

Основания: [Task result tests](../task/internal/consumer/kafka/create_test.go), [consumer tests](../task/internal/consumer/kafka/consumer_test.go), [action tests](../chat/internal/usecase/action/action_test.go), [TaskStatus tests](../chat/internal/usecase/taskstatus/taskstatus_test.go), [frontend tests](../frontend/tests/onboarding.test.mjs), [межсервисный тест](../integration-tests/task_test.go).

**Пределы проверки.** Проверка SQL на настоящем PostgreSQL и запуск межсервисного стенда не выполнены. Отдельный SQL lifecycle-тест удалён по замечанию ревью; остаются unit-тесты и сценарии в общем интеграционном наборе. Их компиляция не означает успешный прогон на БД. Реальный браузерный сценарий не запускался.

**Отражение в базе и совместимость.** [access-requests](../openspec/specs/access-requests/spec.md) обновлён: определены этапы и пользовательский повтор до TTL. `dispatch_id` добавлен в межсервисные команды и события как необязательное поле; миграции БД не требуются. При обновлении работающего окружения сначала нужен Chat, понимающий новые pending/failed, затем Task, публикующий их. Организационная ответственность за DLQ остаётся R3; новый пользовательский повтор отправляет новую попытку, а не удаляет исходную запись из DLQ.

### D3. Назначение бадди не соответствует доступному процессу — исправлено частично

**Подтверждённое исправление.** AssignPlan перенесён в группу операций, требующих `ONBOARDING_SERVICE_TOKEN`, вместе с изменением шаблона. Пользовательский access-токен больше не позволяет назначить план ни себе, ни другому пользователю; отсутствие токена также отклоняется. Для чтения и изменения прогресса сохраняются пользовательская авторизация и проверка владельца. Новые варианты `TestAuthorizeRequests` закрепляют сервисное назначение и отказы пользовательским запросам без обращения к Auth для проверки сервисного токена.

**Что остаётся.** Setup и Home по-прежнему сообщают, что план назначит бадди. Frontend только читает план; после создания профиля POST назначения не вызывается. Сервисная точка назначения теперь доступна доверенному клиенту, но подключённый кабинет бадди, вызывающая интеграция или автоматический триггер назначения в рассмотренном пути не появились. Сервисный токен сам по себе не устанавливает личность и полномочия конкретного бадди. Продуктовый инициатор и момент назначения остаются R1.

Основания: [Setup](../frontend/src/pages/SetupPage.tsx), [Home](../frontend/src/pages/HomePage.tsx), [frontend API](../frontend/src/lib/api.ts), [тест отсутствия назначения](../frontend/tests/onboarding.test.mjs), [авторизация Onboarding](../onboarding/internal/app/auth.go), [тесты авторизации](../onboarding/internal/app/auth_test.go), [Plan API](../onboarding/api/plan/v1/plan.proto).

**Отражение в базе.** Описание пользовательского запроса назначения в требовании «Владение планом и изменение шаблонов» [onboarding](../openspec/specs/onboarding/spec.md) требует отдельной синхронизации: текущий доступ к AssignPlan — только сервисный. Готовность назначения бадди или автоматического назначения этим не подтверждена.

### D4. Направления Setup шире набора штатных шаблонов

**Подтверждено.** Форма предлагает Backend, Frontend, Data Analytics, Platform и QA. Штатный seed содержит `common` и `backend`. Назначение требует профессионального шаблона для выбранного трека. Наличие строки QA в display names или подставного QA в unit-тестах не создаёт шаблоны в БД.

Основания: [Setup](../frontend/src/pages/SetupPage.tsx), [команды User](../user/migrations/demo.sql), [шаблоны](../onboarding/migrations/demo.sql), [проверка шаблона](../onboarding/internal/repository/plan/queries.sql), [Plan tests](../onboarding/internal/usecase/plan/plan_test.go).

**Отражение в базе.** Спецификация не обещает готовые планы для всех пяти направлений и не требует скрыть какие-либо направления без решения R2.

### D5. Устаревшее утверждение о флаге передачи планов

**Подтверждено.** `ai/specs/chat.md` описывает `AI_SEND_ONBOARDING_PLANS=true` как условие передачи планов. В текущей Chat config такого поля нет; новый ответ всегда запрашивает контекст Onboarding и передаёт полученные планы AI. Поддержка пустого контекста не равнозначна поддержке отказа сервиса Onboarding.

Основания: [существующая спецификация AI](../ai/specs/chat.md), [Chat config](../chat/internal/config/config.go), [MessageService](../chat/internal/usecase/message/message.go), [AI adapter](../chat/internal/adapter/ai/http/client.go).

**Отражение в базе.** Флаг не перенесён в требования. Правило доступности при отказе контекста вынесено в R8. Исходный документ сохранён.

### D6. Тесты старых экранов не подтверждают кабинет руководителя

**Подтверждено.** `pages.test.mjs` тестирует `TasksPage`, `ProfilePage`, локальную роль `teamlead` и callback одобрения. Эти экраны не подключены из текущих App/ChatPage. Backend Chat получает профиль с ролью `intern`; Task принимает решения с tracker credentials, а не по локальной роли интерфейса. Корпус отдельно содержит закрытые материалы `mentor`.

Основания: [старые тесты страниц](../frontend/tests/pages.test.mjs), [TasksPage](../frontend/src/pages/TasksPage.tsx), [ProfilePage](../frontend/src/pages/ProfilePage.tsx), [локальная роль](../frontend/src/lib/cookies.ts), [App](../frontend/src/App.tsx), [ChatPage](../frontend/src/pages/ChatPage.tsx), [роль actor](../chat/internal/adapter/user/grpc/client.go), [Task credentials](../task/internal/controller/task/task.go), [ACL корпуса](../ai/knowledge/README.md).

**Отражение в базе.** Кабинет руководителя, самостоятельное переключение прав и одобрение обычной пользовательской сессией не объявлены частью работающего MVP. Продуктовая модель ролей — R4.

### D7. Устаревшие ссылки и параметры в существующей документации

**Подтверждено.** Локальный README упоминает удалённый `AI_MODE=demo`, 304 исходных документа, 352 чанка и 608 ACL-строк. Текущий manifest/Knowledge README описывают версию 1.3.2 с 312 документами, 360 чанками и 624 ACL-строками. Это содержимое репозитория, а не проверка импортированной БД. Frontend README ссылается на отсутствующий `frontend/CHAT.md`.

Основания: [локальный README](../README-local-ai.md), [удалённые режимы AI](../ai/specs/chat.md), [Knowledge README](../ai/knowledge/README.md), [manifest](../ai/knowledge/manifest.json), [Frontend README](../frontend/README.md).

**Отражение в базе.** Старые команды/числа не превращены в требования. Существующие документы не исправлялись в этой работе по условию их сохранения.

### D8. Завершение Task consumer при ошибке чтения Kafka — исправлено по коду

**Подтверждено сравнением коммита.** Ранее ошибка `FetchMessage` вне отмены контекста завершала consumer. Теперь чтение использует существующий `retryKafka`: повторяется с увеличением задержки до `RetryMaxBackoff`, пока не получено сообщение либо не отменён контекст. `MaxAttempts` обработки payload не ограничивает эти повторы чтения. Обработка и commit выполняются после успешного чтения; порядок подтверждения обработки/DLQ перед commit сохранён.

`TestConsumeRecoversAfterKafkaFetchEOF` задаёт три последовательных `io.EOF`, затем сообщение; ожидает четыре чтения, одну обработку и один commit. Тест использует подставной reader, поэтому не подтверждает восстановление настоящего брокера или сквозное восстановление заявки. Отдельные существующие тесты проверяют commit после обработки либо подтверждённой записи в DLQ, включая отказ DLQ при остановке.

Основания: [consumer](../task/internal/consumer/kafka/consumer.go), [тест восстановления и порядка commit](../task/internal/consumer/kafka/consumer_test.go). Это подтверждённое изменение устойчивости consumer, но не решение R3 о восстановлении из DLQ и пользовательских этапах заявки.

## Решения, необходимые для согласования

| ID | Вопрос команде | Что известно и какая часть остаётся открытой |
|---|---|---|
| R1 | Какая доверенная интеграция инициирует назначение плана и в какой момент? | AssignPlan теперь требует сервисный токен, самостоятельное назначение пользовательской сессией запрещено (D3). Открыты вызывающий процесс, полномочия конкретного бадди, момент назначения и путь пользователя до него. |
| R2 | Какие направления входят в поддержанный MVP и что делать без шаблонов? | Setup предлагает пять направлений, seed поддерживает Backend (D4). Нужен согласованный набор; наличие команды не означает наличие её плана. |
| R3 | Кто отвечает за операционную обработку и очистку DLQ? | Технические этапы и пользовательский повтор до TTL реализованы в D2; публикация Kafka и создание Task различаются. Повтор не удаляет старую DLQ-запись. Открыты ответственность за её дальнейшую обработку и подтверждение полного пути на стенде; сроки не заданы. |
| R4 | Нужны ли отдельные пользователи/кабинеты наставника и руководителя, и откуда брать их права? | User хранит `lead/member`, корпус — `intern/mentor`, старый UI — `intern/teamlead`, текущий Chat — `intern`. Эти модели не образуют общей системы полномочий (D6). |
| R5 | План относится к пользователю или к конкретной стажировке? | User хранит несколько стажировок и читает последнюю по дате/ID; Onboarding уникален по `user_id`, смена трека через AssignPlan конфликтует. Не определены повторная стажировка, перевод команды и перенос прогресса. |
| R6 | Что доступно после завершения стажировки и надо ли перепроверять уже созданный черновик? | AI отклоняет любую непустую `actual_end_date`, но ConfirmProposal проверяет владельца/TTL без нового чтения профиля и политики; Task получает команду пользователя, но не проверяет дату окончания. Правила отзыва/завершения не выведены из этого автоматически. |
| R7 | Что означает удаление чата для срока хранения, истории заявки и уведомлений? | Chat делает soft delete, Task хранит заявку отдельно, решение не восстанавливает удалённый диалог. Политика физического удаления и доступность связанной переписки не определены. |
| R8 | Должен ли обычный вопрос работать при отказе Onboarding? | Chat сейчас прерывает новую генерацию при ошибке чтения планов; AI умеет принимать пустой массив, но это не подтверждает согласованный режим работы без контекста (D5). |
| R9 | Какие знания разрешены для рабочего использования и какие критерии качества нужны? | Корпус содержит учебные/демонстрационные материалы, внешняя обработка требует настройки согласия. Нет согласованных метрик качества, SLA и полного внешнего прогона текущего корпуса. |
| R10 | Как трактовать logout и защищать cookies за TLS proxy? | Logout отзывает предъявленный refresh, но не все сессии и не уже выпущенные access JWT. `Secure` зависит от `r.TLS`; Nginx передаёт `X-Forwarded-Proto`, но setter cookies его не использует. Требования рабочего TLS-развёртывания и немедленного отзыва требуют отдельного решения. |
| R11 | Должно ли редактирование шаблона менять уже назначенные планы? | `plan_step` ссылается на живой шаблон; чтение использует текущие название, порядок, период и ресурс. Версионирование и фиксированный снимок назначения не реализованы и не добавлены как требование. |

Основания для R5: [User schema](../user/migrations/migrations/000002_create_user.sql), [выбор стажировки](../user/internal/repository/user/queries.sql), [Plan schema](../onboarding/migrations/migrations/000001_create_plan.sql), [конфликт назначения](../onboarding/internal/repository/plan/postgres.go).

Основания для R6: [отказ AI](../ai/src/ai_service/services/chat_service.py), [ConfirmProposal](../chat/internal/usecase/action/action.go), [Task User adapter](../task/internal/adapter/user/http/client.go).

Основания для R7: [удаление](../chat/internal/repository/conversation/queries.sql), [обработка решения для удалённого диалога](../chat/internal/repository/action/postgres.go), [Task storage](../task/internal/repository/task/queries.sql).

Основания для R8: [MessageService](../chat/internal/usecase/message/message.go), [AI schema](../ai/src/ai_service/api/schemas.py).

Основания для R9: [Knowledge README](../ai/knowledge/README.md), [настройки AI](../ai/src/ai_service/core/config.py), [ограничения проверки в AI specs](../ai/specs/chat.md).

Основания для R10: [logout](../auth/controller/auth/logout.go), [JWT validation](../auth/usecase/auth/auth.go), [cookie setter](../auth/controller/auth/auth.go), [Nginx](../infra/nginx/nginx.conf).

Основания для R11: [схема шагов](../onboarding/migrations/migrations/000002_create_step.sql), [SQL чтения и обновления](../onboarding/internal/repository/step/queries.sql).

## Неподтверждённые предположения

### A1. Одобрение заявки уже выдало технический доступ

Не подтверждено. Task сохраняет решение и событие; адаптеров предоставления прав в целевой системе в этом пути нет. Название UI «Доступ одобрен» не является доказательством выдачи. Основания: [Task decision](../task/internal/repository/task/postgres.go), [TaskStatus](../chat/internal/usecase/taskstatus/taskstatus.go), [карточка](../frontend/src/pages/AccessProposalCard.tsx).

### A2. Данные в базе конкретного окружения совпадают с файлами

Не подтверждено. Наличие `documents.json`, manifest и demo seed не доказывает их импорт или применение в окружении. Полный импорт заменяет корпус; актуальность проверяется отдельно после разрешённого запуска. Основания: [импортер](../ai/src/ai_service/rag/import_documents.py), [Knowledge README](../ai/knowledge/README.md), [seed](../onboarding/migrations/seed.go).

### A3. Валидация цитат доказывает достоверность ответа и правильное понимание намерения

Не подтверждено. Код проверяет структуру, доступность ID и соответствие proposal политике. Семантическое соответствие каждого утверждения источнику и корректность интерпретации выбора моделью не доказываются этими проверками. Старый текст assistant без sources и маркеров остаётся недоверенным контекстом: при утрате метаданных нельзя восстановить его ACL. Основания: [Agent](../ai/src/ai_service/agents/agent.py), [proposal validator](../ai/src/ai_service/rag/proposals.py), [history](../ai/src/ai_service/rag/history.py), [описанные ограничения](../ai/specs/chat.md).

### A4. Существующие интеграционные тесты подтверждают весь MVP

Не подтверждено. Стенд содержит backend, PostgreSQL и Kafka, но AI заменён детерминированным HTTP fixture, frontend отсутствует, seed Onboarding выключен. Нет отдельного набора межсервисных сценариев назначения и прохождения заполненного плана. Проверки frontend-ответа Task, сервисной авторизации назначения и восстановления consumer с подставным reader не равны сквозному прогону. Для D2 добавлено ожидание подтверждения создания Task в межсервисном сценарии, но выполнение на стенде пока не подтверждено. Отдельный SQL lifecycle-тест удалён по замечанию ревью. Основания: [compose](../integration-tests/compose.yml), [fixture AI](../integration-tests/fixtures/ai.py), [helpers](../integration-tests/helpers_test.go), [Onboarding tests](../onboarding/internal/usecase/plan/plan_test.go), [frontend tests](../frontend/tests/onboarding.test.mjs), [тесты авторизации](../onboarding/internal/app/auth_test.go), [consumer tests](../task/internal/consumer/kafka/consumer_test.go), [межсервисный тест](../integration-tests/task_test.go).

## Прослеживаемость начальных спецификаций

| Область | Основные контракты и код | Прочитанные проверки |
|---|---|---|
| auth | [routes](../auth/controller/api.go), [usecase](../auth/usecase/auth/auth.go), [frontend session](../frontend/src/lib/api.ts) | [межсервисные](../integration-tests/auth_test.go), [контроллер](../auth/controller/api_test.go), [frontend](../frontend/tests/onboarding.test.mjs) |
| user-profile | [protobuf](../user/api/user/v1/user.proto), [usecase](../user/internal/usecase/user/user.go), [repository](../user/internal/repository/user/postgres.go) | [межсервисные](../integration-tests/user_test.go), [права](../user/internal/app/auth_test.go) |
| onboarding | [plan](../onboarding/api/plan/v1/plan.proto), [step](../onboarding/api/step/v1/step.proto), [usecase](../onboarding/internal/usecase/plan/plan.go) | [план](../onboarding/internal/usecase/plan/plan_test.go), [шаги](../onboarding/internal/usecase/step/step_test.go), [права](../onboarding/internal/app/auth_test.go), [frontend](../frontend/tests/onboarding.test.mjs) |
| chat | [protobuf](../chat/api/chat/v1/chat.proto), [usecase](../chat/internal/usecase/message/message.go), [AI adapter](../chat/internal/adapter/ai/http/client.go) | [межсервисные](../integration-tests/chat_test.go), [диалоги](../chat/internal/controller/conversation/conversation_test.go), [WebSocket](../chat/internal/controller/events/subscription_test.go) |
| ai-knowledge | [OpenAPI](../ai/api/ai/v1/openapi.yaml), [chat.md](../ai/specs/chat.md), [hybrid-search.md](../ai/specs/hybrid-search.md), [Agent](../ai/src/ai_service/agents/agent.py) | [OpenAPI](../ai/tests/test_openapi.py), [чат](../ai/tests/test_chat.py), [RAG](../ai/tests/test_rag.py), [hybrid](../ai/tests/test_hybrid.py), [fallback](../ai/tests/test_llm_fallback.py) |
| access-requests | [Task protobuf](../task/api/task/v1/task.proto), [подтверждение](../chat/internal/usecase/action/action.go), [проекция статуса](../chat/internal/repository/message/queries.sql), [Task usecase](../task/internal/usecase/task/task.go), [consumer](../task/internal/consumer/kafka/consumer.go) | [межсервисные](../integration-tests/task_test.go), [policy validation](../ai/tests/test_proposals.py), [повтор outbox](../chat/internal/usecase/action/action_test.go), [frontend Task response](../frontend/tests/onboarding.test.mjs), [восстановление consumer](../task/internal/consumer/kafka/consumer_test.go) |

## Проверка формата

Для основных specs используется структура установленного OpenSpec 2.0.0: `Purpose`, `Requirements`, `Requirement`, `Scenario`, проверяемые условия `WHEN`/`THEN` и нормативное `SHALL`. Операционные заголовки delta-specs в основную базу не переносятся. Это соответствует [инструкции sync-specs](../.agents/skills/openspec-sync-specs/SKILL.md) и [конфигурации проекта](../openspec/config.yaml).

Команда проверки: `openspec validate --all --strict`. Результат: все 6 спецификаций прошли строгую валидацию без ошибок и предупреждений. Локальные файловые ссылки в восьми созданных документах проверены; отсутствующих целей нет. Контрольные суммы существовавших README и `ai/specs/` сохранены.

Проверка формата и ссылок не заменяет запуск приложения и тестов, согласование оставшихся решений, проверку D2 на БД/стенде, завершение D3 и устранение открытых D4–D7. Границы свежего прогона для D2 указаны выше. Отдельной синхронизации с исходным коммитом требуют упоминание D1 в `docs/product.md` и права назначения в `onboarding/spec.md`; эти части в исправлении D2 не менялись.
