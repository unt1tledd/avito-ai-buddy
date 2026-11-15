"""Bounded recovery for import preparation; never retry an index replacement."""

import random
from asyncio import sleep
from collections.abc import Awaitable, Callable
from typing import TypeVar

from ..core.diagnostics import UpstreamError, logger


T = TypeVar("T")
IMPORT_MAX_ATTEMPTS = 3
TRANSIENT_STATUSES = {429, 500, 502, 503, 504}


async def retry_import_request(request: Callable[[], Awaitable[T]]) -> T:
    # The caller's import deadline also bounds waits, including Retry-After.
    for attempt in range(IMPORT_MAX_ATTEMPTS):
        try:
            return await request()
        except UpstreamError as error:
            if error.status_code not in TRANSIENT_STATUSES or attempt == IMPORT_MAX_ATTEMPTS - 1:
                raise
            delay = max(2 ** attempt + random.uniform(0, 0.5), error.retry_after_seconds or 0)
            logger.warning(
                "event=import_retry operation=%s attempt=%d delay_seconds=%.1f reason=%s",
                error.domain, attempt + 2, delay, error.reason,
            )
            await sleep(delay)
    raise AssertionError("Import retry attempts exhausted without a result")
