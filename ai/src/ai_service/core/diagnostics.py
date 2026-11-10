import logging
from contextlib import contextmanager
from contextvars import ContextVar
from time import monotonic

import httpx


request_id: ContextVar[str] = ContextVar("request_id", default="-")
logger = logging.getLogger("uvicorn.error.ai_service.operations")


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
        logger.log(
            logging.INFO if reason == "ok" else logging.WARNING,
            "operation=%s result=%s duration_ms=%.1f request_id=%s",
            name, reason, (monotonic() - started) * 1000, request_id.get(),
        )
