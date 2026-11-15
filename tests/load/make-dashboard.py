import json
from pathlib import Path

LOKI = {"type": "loki", "uid": "loki"}
FILTER = (
    ' | json rid="request_id" | __error__="" | rid!=""'
    ' | rid=~"^${run_prefix:regex}[0-9a-f]*$"'
)
CHAT = (
    '{service="chat"} |= "RPC completed"' + FILTER
    + ' | json method="rpc", code="code", ms="duration_ms"'
    ' | method=~".*/(GetHistory|SendMessage|ConfirmProposal)"'
)
AI = (
    '{service="ai"}' + FILTER + ' | json op="operation", result="result", ms="duration_ms"'
    ' | op=~"llm[.]respond|embeddings[.]embed|db[.]search"'
)
FAILURE = (
    '{service="chat"} |= "send message failed"' + FILTER + ' | json stage="stage"'
)
ALL = '{service=~"chat|ai|task|user|onboarding|auth"}'


def panel(number, title, query, kind, x, y, width=12, height=8, legend="", unit="short"):
    return {
        "id": number, "title": title, "type": kind, "datasource": LOKI,
        "gridPos": {"x": x, "y": y, "w": width, "h": height},
        "fieldConfig": {"defaults": {"unit": unit}, "overrides": []},
        "targets": [{"refId": "A", "datasource": LOKI, "editorMode": "code", "expr": query,
                     "queryType": "instant" if kind == "bargauge" else "range",
                     "legendFormat": legend, "maxLines": 500}],
        "options": {"showTime": True, "wrapLogMessage": True, "sortOrder": "Descending"} if kind == "logs" else {},
    }


dashboard = {
    "id": None, "uid": "chat-load-suite", "title": "Chat · Нагрузочные тесты",
    "schemaVersion": 39, "version": 1, "timezone": "browser", "refresh": "5s",
    "time": {"from": "now-15m", "to": "now"},
    "templating": {"list": [{
        "name": "run_prefix", "label": "Набор, ступень или аккаунт", "type": "textbox",
        "query": "", "current": {"text": "", "value": ""},
    }]},
    "panels": [
        panel(1, "Завершения RPC по методу и коду за минуту",
              f"sum by (method, code) (count_over_time({CHAT} | drop rid, ms [1m]))",
              "timeseries", 0, 0, legend="{{method}} · {{code}}"),
        panel(2, "p95 успешных RPC Chat",
              f'quantile_over_time(0.95, {CHAT} | code="OK" | drop rid | unwrap ms | __error__="" [2m]) by (method)',
              "timeseries", 12, 0, legend="{{method}}", unit="ms"),
        panel(3, "Ошибки отправки по этапу",
              f"sum by (stage) (count_over_time({FAILURE} | drop rid [$__range]))",
              "bargauge", 0, 8, legend="{{stage}}"),
        panel(4, "p95 успешных операций AI",
              f'quantile_over_time(0.95, {AI} | result="ok" | drop rid | unwrap ms | __error__="" [2m]) by (op)',
              "timeseries", 12, 8, legend="{{op}}", unit="ms"),
        panel(5, "Сбои AI по операции и причине",
              f'sum by (op, result) (count_over_time({AI} | result!="" | result!="ok" | drop rid, ms [$__range]))',
              "bargauge", 0, 16, width=24, height=6, legend="{{op}} · {{result}}"),
        panel(6, "Журнал ошибок и предупреждений",
              ALL + r' |~ `"level"\s*:\s*"(error|warn|warning)"`' + FILTER,
              "logs", 0, 22, width=24, height=10),
        panel(7, "Журнал запросов",
              ALL + FILTER, "logs", 0, 32, width=24, height=12),
    ],
}

Path(__file__).with_name("chat-load-dashboard.json").write_text(
    json.dumps(dashboard, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
