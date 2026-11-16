import json
import logging
import math
from contextlib import contextmanager
from contextvars import ContextVar
from dataclasses import asdict, dataclass, fields
from datetime import UTC, datetime
from email.utils import parsedate_to_datetime
from enum import StrEnum
from time import monotonic
from typing import Self

import httpx

request_id: ContextVar[str] = ContextVar("request_id", default="-")
logger = logging.getLogger("uvicorn.error.ai_service.operations")


@dataclass(kw_only=True)
class LogContext:
    """Only declared operation metadata may leave the process in a JSON log."""

    operation: str | None = None
    result: str | None = None
    duration_ms: float | None = None
    method: str | None = None
    route: str | None = None
    status: int | None = None
    reason: str | None = None
    retrieval_mode: str | None = None
    hit_count: int | None = None


@dataclass(kw_only=True)
class LogEvent(LogContext):
    time: str
    level: str
    msg: str
    service: str = "ai"
    request_id: str | None = None

    @classmethod
    def from_record(cls, record: logging.LogRecord) -> "LogEvent":
        return cls(
            time=datetime.fromtimestamp(record.created, UTC).isoformat(),
            level=record.levelname.lower(),
            msg=record.getMessage(),
            request_id=request_id.get() if request_id.get() != "-" else None,
            **{field.name: getattr(record, field.name) for field in fields(LogContext) if hasattr(record, field.name)},
        )


class JSONFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        event = LogEvent.from_record(record)
        return json.dumps({key: value for key, value in asdict(event).items() if value is not None}, ensure_ascii=True)


class FailureKind(StrEnum):
    OTHER = "other"
    TRANSPORT = "transport"
    TIMEOUT = "timeout"
    HTTP = "http"


class UpstreamError(Exception):
    domain = "upstream"

    def __init__(
        self, message: str, *, reason: str = "failed", code: str | None = None,
        kind: FailureKind = FailureKind.OTHER, status_code: int | None = None,
        retry_after_seconds: float | None = None,
    ):
        super().__init__(message)
        self.reason = reason
        self.code = code or reason
        self.kind = FailureKind.HTTP if status_code is not None else kind
        self.status_code = status_code
        self.retry_after_seconds = retry_after_seconds

    @classmethod
    def from_http_error(cls, error: httpx.HTTPError, message: str) -> Self:
        if isinstance(error, httpx.TimeoutException):
            return cls(message, reason="timeout", kind=FailureKind.TIMEOUT)
        if isinstance(error, httpx.HTTPStatusError):
            status_code = error.response.status_code
            return cls(
                message, reason=f"http_{status_code}", status_code=status_code,
                retry_after_seconds=retry_after_delay(error.response.headers.get("Retry-After")),
            )
        return cls(message, reason="transport", kind=FailureKind.TRANSPORT)


def retry_after_delay(value: str | None) -> float | None:
    """Keep only a validated delay; never retain provider bodies or headers."""
    if value is None:
        return None
    try:
        seconds = float(value)
    except ValueError:
        try:
            date = parsedate_to_datetime(value)
            if date.tzinfo is None:
                return None
            seconds = (date - datetime.now(UTC)).total_seconds()
        except (ValueError, TypeError, OverflowError):
            return None
    return max(0.0, seconds) if math.isfinite(seconds) else None


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
