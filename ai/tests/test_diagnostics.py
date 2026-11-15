import json
import logging

import httpx
import pytest
from fastapi.testclient import TestClient

from ai_service.clients.embeddings import EmbeddingError
from ai_service.clients.llm import ModelError
from ai_service.clients.resilient_llm import is_unavailable
from ai_service.core.diagnostics import FailureKind, JSONFormatter, UpstreamError, request_id
from ai_service.main import create_app
from ai_service.repositories.knowledge import KnowledgeError
from test_chat import ScriptedModel, ask, settings  # noqa: F401


def test_json_log_preserves_fields_and_omits_undeclared_data():
    record = logging.makeLogRecord({
        "levelname": "INFO", "msg": "Found %s instructions", "args": (0,),
        "operation": "db.search", "result": "ok", "duration_ms": 1.5,
        "method": "POST", "route": "/api/v1/answer", "status": 200,
        "reason": "ok", "retrieval_mode": "lexical", "hit_count": 0,
        "authorization": "secret", "prompt": "private prompt", "request_id": "untrusted",
        "service": "untrusted",
    })
    token = request_id.set("a" * 32)
    try:
        payload = json.loads(JSONFormatter().format(record))
    finally:
        request_id.reset(token)
    assert payload == {
        "time": payload["time"], "level": "info", "msg": "Found 0 instructions", "service": "ai",
        "request_id": "a" * 32, "operation": "db.search", "result": "ok", "duration_ms": 1.5,
        "method": "POST", "route": "/api/v1/answer", "status": 200,
        "reason": "ok", "retrieval_mode": "lexical", "hit_count": 0,
    }


def test_json_log_omits_absent_context():
    record = logging.makeLogRecord({"levelname": "WARNING", "msg": "Unavailable"})
    token = request_id.set("-")
    try:
        payload = json.loads(JSONFormatter().format(record))
    finally:
        request_id.reset(token)
    assert set(payload) == {"time", "level", "msg", "service"}


@pytest.mark.parametrize(("status", "unavailable"), [
    (400, False), (401, False), (402, True), (403, False), (408, True),
    (429, True), (500, True), (503, True), (599, True), (600, False),
])
@pytest.mark.parametrize("error_class", [ModelError, EmbeddingError])
def test_http_failure_carries_status_separately_from_diagnostic_reason(status, unavailable, error_class):
    response = httpx.Response(status, request=httpx.Request("POST", "https://provider.example.test"))
    with pytest.raises(httpx.HTTPStatusError) as caught:
        response.raise_for_status()
    error = error_class.from_http_error(caught.value, "Upstream request failed.")
    assert error.status_code == status
    assert error.kind == FailureKind.HTTP
    assert error.reason == f"http_{status}"
    if error_class is ModelError:
        assert is_unavailable(error) == unavailable


@pytest.mark.parametrize(("error", "kind"), [
    (httpx.ReadTimeout("private details"), FailureKind.TIMEOUT),
    (httpx.ConnectError("private details"), FailureKind.TRANSPORT),
])
def test_transport_failures_have_typed_categories(error, kind):
    failure = ModelError.from_http_error(error, "Model request failed.")
    assert failure.kind == kind
    assert failure.status_code is None
    assert is_unavailable(failure)
    assert "private" not in str(failure)


def test_availability_uses_metadata_instead_of_parsing_reason():
    assert not is_unavailable(ModelError("Invalid answer", reason="http_503"))
    assert is_unavailable(ModelError("Provider failed", reason="provider_down", status_code=503))
    assert is_unavailable(ModelError("Deadline expired", reason="deadline", kind=FailureKind.TIMEOUT))
    assert not is_unavailable(ModelError("Refused", reason="refusal"))


@pytest.mark.parametrize(("error", "message"), [
    (ModelError("secret internal context", reason="invalid_citation"), "Модель сослалась на недоступный источник."),
    (ModelError("secret internal context", reason="unknown"), "Модель временно недоступна."),
    (KnowledgeError("secret internal context", reason="connection"), "Не удалось выполнить поиск в базе знаний."),
    (EmbeddingError("secret internal context", reason="invalid_response"), "Не удалось получить эмбеддинги."),
    (EmbeddingError("secret internal context", reason="invalid_dimensions", expected_dimensions=1024),
     "Сервис эмбеддингов должен возвращать векторы размерности 1024."),
    (UpstreamError("secret internal context"), "Не удалось обработать запрос AI."),
])
def test_api_localizes_errors_without_exposing_exception_text(settings, error, message):
    with TestClient(create_app(settings, model=ScriptedModel(error))) as client:
        response = ask(client, "Как настроить VPN?")
    assert response.status_code == 502
    assert response.json() == {"detail": message}
    assert "secret" not in response.text
