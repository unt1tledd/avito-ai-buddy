import secrets
from http import HTTPStatus
from typing import Annotated

from fastapi import APIRouter, Depends, HTTPException, Request
from fastapi.exceptions import RequestValidationError
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from ..clients.llm import ModelError
from ..clients.user import UserError
from ..repositories.knowledge import KnowledgeError
from ..schemas.contracts import ChatResponse
from ..services.chat_service import ChatService, ChatUnavailable, ConversationRequired
from .schemas import ChatRequest


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
        responses={
            HTTPStatus.UNAUTHORIZED: {"description": "Invalid or missing chat service token"},
            HTTPStatus.BAD_GATEWAY: {"description": "User, embeddings, knowledge base, model or tool orchestration error"},
            HTTPStatus.SERVICE_UNAVAILABLE: {"description": "AI service configuration is incomplete"},
        },
    )
    async def chat(payload: ChatRequest, request: Request) -> ChatResponse:
        service: ChatService = request.app.state.chat_service
        try:
            return await service.answer(
                conversation_id=payload.conversation_id,
                actor=payload.actor,
                message=payload.message,
                confirmed_step_id=payload.confirmed_step_id,
            )
        except ChatUnavailable as error:
            raise HTTPException(status_code=HTTPStatus.SERVICE_UNAVAILABLE, detail=str(error)) from error
        except ConversationRequired as error:
            raise RequestValidationError([
                {"type": "missing", "loc": ("body", "conversation_id"), "msg": str(error), "input": None}
            ]) from error
        except (UserError, KnowledgeError, ModelError) as error:
            raise HTTPException(status_code=HTTPStatus.BAD_GATEWAY, detail=str(error)) from error

    return router
