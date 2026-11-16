import json
from pathlib import Path


LOKI = {"type": "loki", "uid": "loki"}
PROMETHEUS = {"type": "prometheus", "uid": "prometheus"}
APP = "auth|user|chat|onboarding|task|ai|frontend"
MONITORING = "prometheus|grafana|loki|alloy|ai-readiness"
PREFIX = (
    ' | json rid="request_id" | __error__="" | rid!=""'
    ' | rid=~"^${run_prefix:regex}[0-9a-f]*$"'
)
CHAT = '{service="chat"} |= "RPC completed"'
AI = '{service="ai"}'
LOG_LEVELS = r' |~ `"level"\s*:\s*"(error|warn|warning|fatal|panic)"`'
READINESS = (
    '(probe_success{job="ai-readiness"} and on(job, instance) '
    '(up{job="ai-readiness"} == 1)) or on() vector(-1)'
)
SCENARIOS = {
    "history": (
        "Чтение истории",
        "GetHistory",
        "chat",
        "Проверки содержимого сообщений смотри в отчёте k6.",
    ),
    "pagination": (
        "Пагинация истории",
        "GetHistory",
        "chat",
        "Порядок, дубли и курсоры проверяет k6. Один сценарий чтения может вызвать несколько RPC.",
    ),
    "ai": (
        "Вопросы к AI",
        "SendMessage|GetHistory",
        "chat|ai",
        "Графики включают контрольные чтения истории. Качество ответа и сохранение пары сообщений проверяет k6.",
    ),
    "context": (
        "Контекст диалога",
        "SendMessage|GetHistory",
        "chat|ai",
        "Зависимость ответа от предыдущих сообщений проверяет k6.",
    ),
    "mixed": (
        "Смешанная нагрузка",
        "GetHistory|SendMessage",
        "chat|ai",
        "Сравнивай задержки чтения истории и отправки сообщений по отдельным рядам.",
    ),
    "mock": (
        "Задержки провайдера",
        "GetHistory|SendMessage",
        "chat|ai",
        "Заглушка меняет задержку ответов. Проверки повторной отправки смотри в отчёте k6.",
    ),
    "faults": (
        "Сбои провайдера",
        "GetHistory|SendMessage",
        "chat|ai",
        "Сбои провайдера намеренные. Прохождение проверок fallback определяет k6.",
    ),
    "task": (
        "Жизненный цикл заявки",
        "ConfirmProposal|GetHistory",
        "chat|task",
        "Доставка, решение и обратное уведомление проверяются в отчёте k6. "
        "Асинхронный лог без request_id может отсутствовать здесь.",
    ),
    "recovery": (
        "Восстановление Task",
        "ConfirmProposal|GetHistory",
        "chat|task",
        "Смотри отдельные ссылки этапов queue и drain. Проверки строк и Kafka DLQ выполняет run.py.",
    ),
    "soak": (
        "Длительная нагрузка",
        "GetHistory|SendMessage",
        "chat|ai",
        "Снимки CPU и памяти лежат в container-stats.log в каталоге ступени.",
    ),
    "suite": (
        "Общий прогон",
        "GetHistory|SendMessage|ConfirmProposal",
        APP,
        "В оглавлении прогона есть ссылки на сценарии, этапы и отчёты k6.",
    ),
}


def target(expr, legend="", source=LOKI, instant=False):
    result = {
        "datasource": source,
        "editorMode": "code",
        "expr": expr,
        "legendFormat": legend,
    }
    if source == LOKI:
        result["queryType"] = "instant" if instant else "range"
    else:
        result.update(instant=instant, range=not instant)
    return result


def panel(
    number, title, grid, *targets, kind="timeseries", unit="short", description=""
):
    x, y, w, h = grid
    queries = [dict(query, refId=chr(ord("A") + i)) for i, query in enumerate(targets)]
    item = {
        "id": number,
        "title": title,
        "description": description,
        "type": kind,
        "datasource": queries[0]["datasource"],
        "gridPos": {"x": x, "y": y, "w": w, "h": h},
        "fieldConfig": {"defaults": {"unit": unit}, "overrides": []},
        "targets": queries,
        "options": {},
    }
    if kind == "logs":
        queries[0]["maxLines"] = 250
        item["options"] = {
            "showTime": True,
            "wrapLogMessage": True,
            "sortOrder": "Descending",
        }
    return item


def chat_query(methods, prefix=""):
    return (
        CHAT + prefix + ' | json method="rpc", code="code", ms="duration_ms"'
        ' | __error__=""' + f' | method=~".*/({methods})"'
    )


def chat_p95(query, window):
    return (
        f'quantile_over_time(0.95, {query} | code="OK" | drop rid'
        f' | unwrap ms | __error__="" [{window}]) by (method)'
    )


def dashboard(uid, title, panels, *, load=False, description=""):
    item = {
        "uid": uid,
        "title": title,
        "description": description,
        "schemaVersion": 41,
        "version": 1,
        "tags": ["ai-buddy", "load-test" if load else "observability"],
        "timezone": "browser",
        "refresh": "10s",
        "time": {"from": "now-15m" if load else "now-1h", "to": "now"},
        "panels": panels,
    }
    if load:
        item["templating"] = {
            "list": [
                {
                    "name": "run_prefix",
                    "label": "Набор, ступень или аккаунт",
                    "type": "textbox",
                    "query": "__no_run__",
                    "current": {"text": "__no_run__", "value": "__no_run__"},
                }
            ]
        }
    return item


def overview():
    chat = chat_query("GetHistory|SendMessage|ConfirmProposal")
    levels = f'{{service=~"{APP}"}}' + LOG_LEVELS
    app_logs = f'{{service=~"{APP}"}}'
    error_status = panel(
        6,
        "Ошибки и предупреждения · 5 мин",
        (0, 14, 12, 8),
        target(
            f"sum(count_over_time({levels} [5m]))"
            f" or (0 * sum(count_over_time({app_logs} [5m])))"
            " or vector(-1)",
            instant=True,
        ),
        kind="stat",
        description="Количество строк с предупреждениями и ошибками за последние 5 минут.",
    )

    error_status["fieldConfig"]["defaults"].update(
        decimals=0,
        color={"mode": "thresholds"},
        thresholds={
            "mode": "absolute",
            "steps": [
                {"color": "green", "value": None},
                {"color": "orange", "value": 1},
            ],
        },
        mappings=[
            {
                "type": "value",
                "options": {
                    "-1": {"text": "Нет свежих логов", "color": "gray"},
                    "0": {"text": "Нет предупреждений и ошибок", "color": "green"},
                },
            },
        ],
    )

    error_status["options"] = {
        "colorMode": "value",
        "graphMode": "none",
        "textSize": {"value": 22},
        "reduceOptions": {"calcs": ["lastNotNull"], "values": False},
    }
    health = panel(
        1,
        "AI · Готовность",
        (0, 0, 6, 6),
        target(READINESS, source=PROMETHEUS, instant=True),
        kind="stat",
        description="Проверка /readyz через Blackbox Exporter.",
    )
    health["fieldConfig"]["defaults"]["mappings"] = [
        {
            "type": "value",
            "options": {
                "-1": {"text": "Нет данных проверки", "color": "gray", "index": 0},
                "0": {"text": "Не готов", "color": "red", "index": 1},
                "1": {"text": "Готов", "color": "green", "index": 2},
            },
        }
    ]
    health["options"] = {
        "colorMode": "value",
        "graphMode": "none",
        "textSize": {"value": 28},
        "reduceOptions": {
            "calcs": ["lastNotNull"],
            "values": False,
        },
    }
    panels = [
        health,
        panel(
            2,
            "Prometheus · Цели мониторинга",
            (6, 0, 10, 6),
            target(f'up{{job=~"{MONITORING}"}}', "{{job}}", PROMETHEUS),
            description="1 — метрики собраны, 0 — опрос не удался. Для ai-readiness это статус опроса экспортера.",
        ),
        panel(
            3,
            "AI · История /readyz",
            (16, 0, 8, 6),
            target(
                'probe_success{job="ai-readiness"} and on(job, instance) '
                '(up{job="ai-readiness"} == 1)',
                "/readyz",
                PROMETHEUS,
            ),
        ),
        panel(
            4,
            "Chat · Запросы/с по методу",
            (0, 6, 12, 8),
            target(
                f"sum by (method) (rate({chat} | drop ms, code [5m]))", "{{method}}"
            ),
            unit="reqps",
        ),
        panel(
            5,
            "Chat и LLM · p95",
            (12, 6, 12, 8),
            target(chat_p95(chat, "5m"), "{{method}}"),
            target(
                'quantile_over_time(0.95, {service="ai"} |= "llm.respond"'
                ' | json op="operation", result="result", ms="duration_ms"'
                ' | op="llm.respond" | result="ok" | unwrap ms | __error__="" [5m]) by (service)',
                "AI · LLM",
            ),
            unit="ms",
            description="Успешные вызовы за скользящие 5 минут.",
        ),
        panel(
            6,
            "Приложение · Ошибки и предупреждения",
            (0, 14, 12, 8),
            target(f"sum by (service) (count_over_time({levels} [5m]))", "{{service}}"),
            description="Строки логов за 5 минут по сервисам.",
        ),
        panel(
            7,
            "Alloy → Loki · Доставка логов",
            (12, 14, 12, 8),
            target(
                'sum(increase(loki_write_sent_entries_total{job="alloy"}[5m]))',
                "Отправлено",
                PROMETHEUS,
            ),
            target(
                'sum(increase(loki_write_dropped_entries_total{job="alloy"}[5m]))',
                "Отброшено",
                PROMETHEUS,
            ),
            description="Записи за 5 минут. Отброшенные — после исчерпания повторных отправок.",
        ),
        {
            "id": 9,
            "type": "row",
            "title": "Диагностика · журнал ошибок и предупреждений",
            "collapsed": True,
            "gridPos": {"x": 0, "y": 22, "w": 24, "h": 1},
            "panels": [
                panel(
                    8,
                    "Ошибки и предупреждения по сервисам",
                    (0, 23, 24, 10),
                    target(levels),
                    kind="logs",
                ),
            ],
        },
    ]
    for chart in panels:
        if chart["id"] not in (4, 5, 6) or chart["type"] != "timeseries":
            continue

        baseline = "__empty_chart__"
        chart["targets"].append(dict(target("vector(0)", baseline), refId="Z"))
        chart["fieldConfig"]["overrides"].append(
            {
                "matcher": {"id": "byName", "options": baseline},
                "properties": [
                    {"id": "custom.lineWidth", "value": 0},
                    {"id": "custom.fillOpacity", "value": 0},
                    {"id": "custom.showPoints", "value": "never"},
                    {
                        "id": "custom.hideFrom",
                        "value": {"legend": True, "tooltip": True, "viz": False},
                    },
                ],
            }
        )
    item = dashboard("ai-buddy-overview", "AI Buddy — Overview", panels)
    item["preload"] = True
    item["links"] = [
        {
            "type": "dashboards",
            "title": "Нагрузочные тесты",
            "tags": ["load-test"],
            "asDropdown": True,
            "keepTime": False,
            "includeVars": False,
        }
    ]
    return item


def load_dashboard(test, title, methods, services, description):
    chat = chat_query(methods, PREFIX)
    ai = (
        AI + PREFIX + ' | json op="operation", result="result", ms="duration_ms"'
        ' | __error__="" | op=~"llm[.]respond|embeddings[.]embed|db[.]search"'
    )
    logs = f'{{service=~"{services}"}}'
    panels = []

    def row(left, right):
        y = sum(item["gridPos"]["x"] == 0 for item in panels) * 8
        for x, (name, expr, legend, unit) in ((0, left), (12, right)):
            panels.append(
                panel(
                    len(panels) + 1,
                    name,
                    (x, y, 12, 8),
                    target(expr, legend),
                    unit=unit,
                )
            )

    row(
        (
            "Завершения RPC за минуту",
            f"sum by (method, code) (count_over_time({chat} | drop rid, ms [1m]))",
            "{{method}} · {{code}}",
            "short",
        ),
        ("p95 успешных RPC", chat_p95(chat, "2m"), "{{method}}", "ms"),
    )
    if "ai" in services.split("|"):
        row(
            (
                "p95 операций AI",
                f'quantile_over_time(0.95, {ai} | result="ok" | drop rid | unwrap ms | __error__="" [2m]) by (op)',
                "{{op}}",
                "ms",
            ),
            (
                "Сбои операций AI за минуту",
                f'sum by (op, result) (count_over_time({ai} | result!="" | result!="ok" | drop rid, ms [1m]))',
                "{{op}} · {{result}}",
                "short",
            ),
        )
    if test in ("faults", "mock", "suite"):
        row(
            (
                "Fallback AI за минуту",
                f'sum by (reason) (count_over_time({AI} |= "event=chat_fallback"{PREFIX}'
                ' | json reason="reason" | __error__="" | drop rid [1m]))',
                "{{reason}}",
                "short",
            ),
            (
                "Ошибки отправки по этапу за минуту",
                f'sum by (stage) (count_over_time({{service="chat"}} |= "send message failed"{PREFIX}'
                ' | json stage="stage" | __error__="" | drop rid [1m]))',
                "{{stage}}",
                "short",
            ),
        )
    y = sum(item["gridPos"]["x"] == 0 for item in panels) * 8
    panels.extend(
        [
            panel(
                len(panels) + 1,
                "Ошибки и предупреждения прогона",
                (0, y, 24, 9),
                target(logs + LOG_LEVELS + PREFIX),
                kind="logs",
            ),
            panel(
                len(panels) + 2,
                "Логи по ID запроса",
                (0, y + 9, 24, 10),
                target(logs + PREFIX),
                kind="logs",
                description=description,
            ),
        ]
    )
    return dashboard(
        f"chat-load-{test}",
        f"Нагрузка · {title}",
        panels,
        load=True,
        description=description,
    )


def main():
    output = Path(__file__).resolve().parents[2] / "infra" / "grafana" / "dashboards"
    output.mkdir(parents=True, exist_ok=True)
    items = [
        ("ai-buddy.json", overview()),
        *[
            (
                "chat-load-dashboard.json"
                if name == "suite"
                else f"chat-load-{name}.json",
                load_dashboard(name, *settings),
            )
            for name, settings in SCENARIOS.items()
        ],
    ]
    for filename, content in items:
        (output / filename).write_text(
            json.dumps(content, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )


if __name__ == "__main__":
    main()
