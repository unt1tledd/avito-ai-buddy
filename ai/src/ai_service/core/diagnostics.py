import json
import logging
from datetime import UTC, datetime
from contextlib import contextmanager
from contextvars import ContextVar
from time import monotonic

import httpx


request_id: ContextVar[str] = ContextVar("request_id", default="-")
logger = logging.getLogger("uvicorn.error.ai_service.operations")


class JSONFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        fields = {
            "time": datetime.fromtimestamp(record.created, UTC).isoformat(),
            "level": record.levelname.lower(),
            "msg": record.getMessage(),
            "service": "ai",
        }
        correlation = request_id.get()
        if correlation != "-":
            fields["request_id"] = correlation
        for key in ("operation", "result", "duration_ms", "method", "route", "status", "reason"):
            if hasattr(record, key):
                fields[key] = getattr(record, key)
        return json.dumps(fields, ensure_ascii=True)


class UpstreamError(Exception):
    def __init__(self, message: str, *, reason: str = "failed"):
        super().__init__(message)
        self.reason = reason


def http_failure(error: httpx.HTTPError) -> str:
    if isinstance(error, httpx.TimeoutException):
        return "timeout"
    if isinstance(error, httpx.HTTPStatusError):
        return f"http_{error.response.status_code}"
    return "transport"


@contextmanager
def operation(name: str):
    started = monotonic()
    reason = "ok"
    try:
        yield
    except UpstreamError as error:
        reason = error.reason
        raise
    except TimeoutError:
        reason = "timeout"
        raise
    except BaseException as error:
        reason = type(error).__name__
        raise
    finally:
        elapsed_ms = round((monotonic() - started) * 1000, 1)
        logger.log(
            logging.INFO if reason == "ok" else logging.WARNING,
            "operation=%s result=%s duration_ms=%.1f request_id=%s",
            name, reason, elapsed_ms, request_id.get(),
            extra={"operation": name, "result": reason, "duration_ms": elapsed_ms},
        )
