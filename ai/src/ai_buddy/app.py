import secrets
from contextlib import asynccontextmanager
from importlib.resources import files
from typing import Annotated

import httpx
from fastapi import Depends, FastAPI, HTTPException, Request
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from .agent import Agent
from .chat_profiles import ChatProfiles
from .config import Settings
from .contracts import Actor, ChatRequest, ChatResponse
from .embeddings import Embeddings
from .llm import LanguageModel, ModelError, ResponsesModel
from .onboarding_client import DemoOnboarding, HttpOnboarding
from .rag import Knowledge, KnowledgeError
from .user_client import HttpUser, UserError


def create_app(settings: Settings | None = None, *, model: LanguageModel | None = None) -> FastAPI:
    settings = settings or Settings.from_env()

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        if settings.onboarding_url and not settings.onboarding_token:
            raise RuntimeError("ONBOARDING_SERVICE_TOKEN is required when ONBOARDING_URL is configured")
        if settings.user_url and not settings.user_token:
            raise RuntimeError("USER_SERVICE_TOKEN is required when USER_URL is configured")
        prompt = files("ai_buddy").joinpath("prompts/system.txt").read_text(encoding="utf-8")
        async with httpx.AsyncClient(timeout=25) as http:
            app.state.profiles = None
            if settings.user_url:
                user = HttpUser(http, settings.user_url, settings.user_token)
                app.state.profiles = ChatProfiles(settings.chat_profile_db_dsn or settings.knowledge_db_dsn, user)
            language_model = model
            if language_model is None and settings.llm_api_key and settings.llm_model:
                language_model = ResponsesModel(http, settings.llm_base_url, settings.llm_api_key, settings.llm_model)
            onboarding = (
                HttpOnboarding(http, settings.onboarding_url, settings.onboarding_token)
                if settings.onboarding_url
                else DemoOnboarding(settings.demo_db_path)
            )
            app.state.agent = None
            if language_model and settings.knowledge_db_dsn:
                knowledge = Knowledge(settings, Embeddings(http, settings))
                app.state.agent = Agent(language_model, knowledge, onboarding, prompt)
            yield

    app = FastAPI(title="AI-buddy internal API", version="1.0.0", lifespan=lifespan)
    bearer = HTTPBearer(auto_error=False)

    def authenticate(credentials: Annotated[HTTPAuthorizationCredentials | None, Depends(bearer)]) -> None:
        if not settings.service_token:
            raise HTTPException(status_code=503, detail="Не настроен доступ к AI-сервису.")
        if credentials is None or not secrets.compare_digest(
            credentials.credentials.encode(), settings.service_token.encode()
        ):
            raise HTTPException(status_code=401, detail="Требуется токен сервиса chat.",
                                headers={"WWW-Authenticate": "Bearer"})

    @app.get("/healthz")
    async def health() -> dict[str, str]:
        return {"status": "ok"}

    @app.post(
        "/v1/chat",
        response_model=ChatResponse,
        dependencies=[Depends(authenticate)],
        include_in_schema=False,
    )
    @app.post(
        "/api/v1/chat",
        response_model=ChatResponse,
        dependencies=[Depends(authenticate)],
        responses={
            401: {"description": "Invalid or missing chat service token"},
            502: {"description": "User, embeddings, knowledge base, model or tool orchestration error"},
            503: {"description": "AI service configuration is incomplete"},
        },
    )
    async def chat(payload: ChatRequest, request: Request) -> ChatResponse:
        agent: Agent | None = request.app.state.agent
        if agent is None:
            if not settings.knowledge_db_dsn:
                raise HTTPException(status_code=503, detail="Не настроено подключение к базе знаний.")
            raise HTTPException(status_code=503, detail="Не настроены ключ и модель LLM.")
        profiles: ChatProfiles | None = request.app.state.profiles
        try:
            if profiles is not None:
                if payload.conversation_id is None:
                    raise HTTPException(status_code=422, detail="Требуется conversation_id.")
                actor = await profiles.get_actor(payload.conversation_id, payload.actor.user_id)
            elif isinstance(payload.actor, Actor):
                actor = payload.actor
            else:
                raise HTTPException(status_code=503, detail="Не настроен адрес сервиса user.")
            return await agent.answer(payload, actor)
        except (UserError, KnowledgeError, ModelError) as error:
            raise HTTPException(status_code=502, detail=str(error)) from error

    return app
