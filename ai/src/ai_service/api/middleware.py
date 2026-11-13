import asyncio
import logging
import re
from time import monotonic
from uuid import uuid4

from starlette.exceptions import HTTPException
from starlette.responses import JSONResponse
from starlette.types import ASGIApp, Message, Receive, Scope, Send

from ..core.diagnostics import logger, request_id


MAX_CHAT_BODY_BYTES = 1024 * 1024
CHAT_PATHS = {"/api/v1/answer", "/api/v1/get_answer", "/api/v1/chat", "/v1/chat"}


class RequestBoundary:
    def __init__(self, app: ASGIApp, chat_timeout: float):
        self.app = app
        self.chat_timeout = chat_timeout

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return

        correlation = next((value for name, value in scope["headers"] if name == b"x-request-id"), b"")
        correlation = correlation.decode("ascii") if re.fullmatch(rb"[0-9a-f]{32}", correlation) else uuid4().hex
        started = monotonic()
        status = 500
        token = request_id.set(correlation)

        async def send_with_id(message: Message) -> None:
            nonlocal status
            if message["type"] == "http.response.start":
                status = message["status"]
                message.setdefault("headers", []).append((b"x-request-id", correlation.encode()))
            await send(message)

        try:
            if scope["method"] == "POST" and scope["path"] in CHAT_PATHS:
                total = 0
                body_complete = False
                deadline = asyncio.get_running_loop().time() + self.chat_timeout
                scope.setdefault("state", {})["chat_deadline"] = deadline

                async def bounded_receive() -> Message:
                    nonlocal total, body_complete
                    if body_complete:
                        return await receive()
                    try:
                        async with asyncio.timeout_at(deadline):
                            message = await receive()
                    except TimeoutError as error:
                        raise HTTPException(408, "Истекло время приёма запроса.") from error
                    body_complete = not message.get("more_body", False)
                    total += len(message.get("body", b""))
                    if total > MAX_CHAT_BODY_BYTES:
                        # FastAPI propagates HTTPException from request.body().
                        raise HTTPException(413, "Тело запроса превышает 1 МиБ.")
                    return message

                for name, length in scope["headers"]:
                    if name == b"content-length" and length.isdigit() and len(length) < 20:
                        if int(length) > MAX_CHAT_BODY_BYTES:
                            response = JSONResponse({"detail": "Тело запроса превышает 1 МиБ."}, status_code=413)
                            await response(scope, receive, send_with_id)
                            return
                await self.app(scope, bounded_receive, send_with_id)
            else:
                await self.app(scope, receive, send_with_id)
        except asyncio.CancelledError:
            status = 499
            raise
        finally:
            if status >= 400 or scope["path"] in CHAT_PATHS:
                route = getattr(scope.get("route"), "path", None) or "unmatched"
                logger.log(
                    logging.ERROR if status >= 500 else logging.INFO,
                    "HTTP request completed",
                    extra={
                        "method": scope["method"], "route": route, "status": status,
                        "duration_ms": round((monotonic() - started) * 1000, 1),
                    },
                )
            request_id.reset(token)
