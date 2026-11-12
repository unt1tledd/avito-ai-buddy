import asyncio
import secrets
from http import HTTPStatus
from typing import Annotated, Literal

from fastapi import APIRouter, Depends, HTTPException, Request, Response
from fastapi.exceptions import RequestValidationError
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer
from pydantic import BaseModel

from ..core.diagnostics import UpstreamError, logger, request_id
from ..schemas.contracts import ChatResponse
from ..services.chat_service import ChatService, ChatUnavailable, InvalidChat
from .schemas import ChatRequest


class Readiness(BaseModel):
    status: Literal["ready", "unavailable"]
    capability: Literal["questions"] = "questions"
    reason: str | None = None


def create_router(service_token: str) -> APIRouter:
    router = APIRouter()
    bearer = HTTPBearer(auto_error=False)

    def authenticate(credentials: Annotated[HTTPAuthorizationCredentials | None, Depends(bearer)]) -> None:
        if not service_token:
            raise HTTPException(status_code=HTTPStatus.SERVICE_UNAVAILABLE, detail="Не настроен доступ к AI-сервису.")
        if credentials is None or not secrets.compare_digest(
            credentials.credentials.encode(), service_token.encode()
        ):
            raise HTTPException(
                status_code=HTTPStatus.UNAUTHORIZED,
                detail="Требуется токен сервиса chat.",
                headers={"WWW-Authenticate": "Bearer"},
            )

    @router.get("/healthz")
    async def health() -> dict[str, str]:
        return {"status": "ok"}

    @router.get("/readyz", responses={503: {"model": Readiness, "description": "Required configuration or index unavailable"}})
    async def readiness(request: Request, response: Response) -> Readiness:
        service: ChatService = request.app.state.chat_service
        reason = "service_token_missing" if not service_token else service.unavailable_reason
        if reason is None and service.agent is not None:
            try:
                await service.agent.knowledge.check_ready()
            except UpstreamError as error:
                reason = error.reason
        if reason:
            response.status_code = HTTPStatus.SERVICE_UNAVAILABLE
            return Readiness(status="unavailable", reason=reason)
        return Readiness(status="ready")

    @router.post(
        "/v1/chat",
        response_model=ChatResponse,
        dependencies=[Depends(authenticate)],
        include_in_schema=False,
    )
    @router.post(
        "/api/v1/chat",
        response_model=ChatResponse,
        dependencies=[Depends(authenticate)],
        include_in_schema=False,
    )
    @router.post(
        "/api/v1/get_answer",
        response_model=ChatResponse,
        dependencies=[Depends(authenticate)],
        include_in_schema=False,
    )
    @router.post(
        "/api/v1/answer",
        response_model=ChatResponse,
        dependencies=[Depends(authenticate)],
        operation_id="answer",
        description="Get an answer using the current user profile supplied by authenticated Chat. "
        "AI does not call User. Maximum request body is 1 MiB.",
        responses={
            HTTPStatus.BAD_REQUEST: {"description": "Malformed request body"},
            HTTPStatus.UNAUTHORIZED: {"description": "Invalid or missing chat service token"},
            HTTPStatus.REQUEST_TIMEOUT: {"description": "Request body deadline expired"},
            HTTPStatus.REQUEST_ENTITY_TOO_LARGE: {"description": "Request body exceeds 1 MiB"},
            HTTPStatus.BAD_GATEWAY: {"description": "Embeddings, knowledge base, model or tool orchestration error"},
            HTTPStatus.SERVICE_UNAVAILABLE: {"description": "AI service configuration is incomplete"},
            HTTPStatus.GATEWAY_TIMEOUT: {"description": "Chat deadline expired"},
        },
    )
    async def answer(payload: ChatRequest, request: Request) -> ChatResponse:
        service: ChatService = request.app.state.chat_service

        async def wait_for_disconnect() -> None:
            while (await request.receive())["type"] != "http.disconnect":
                pass

        work = asyncio.create_task(service.answer(
            conversation_id=payload.conversation_id,
            actor=payload.actor,
            message=payload.message,
            onboarding_plans=payload.onboarding_plans,
            history=payload.history,
            deadline=request.state.chat_deadline,
        ))
        disconnect = asyncio.create_task(wait_for_disconnect())
        try:
            await asyncio.wait((work, disconnect), return_when=asyncio.FIRST_COMPLETED)
            if not work.done():
                logger.info(
                    "event=chat_request_cancelled reason=client_disconnect request_id=%s",
                    request_id.get(),
                )
                raise HTTPException(status_code=499, detail="Клиент закрыл соединение.")
            return work.result()
        except ChatUnavailable as error:
            raise HTTPException(status_code=HTTPStatus.SERVICE_UNAVAILABLE, detail=str(error)) from error
        except InvalidChat as error:
            raise RequestValidationError([
                {"type": error.kind, "loc": ("body", *error.field), "msg": str(error), "input": None}
            ]) from error
        except UpstreamError as error:
            raise HTTPException(status_code=HTTPStatus.BAD_GATEWAY, detail=str(error)) from error
        except TimeoutError as error:
            raise HTTPException(status_code=HTTPStatus.GATEWAY_TIMEOUT, detail="Истекло время ответа AI.") from error
        finally:
            disconnect.cancel()
            if not work.done():
                work.cancel()
            await asyncio.gather(work, disconnect, return_exceptions=True)

    return router
