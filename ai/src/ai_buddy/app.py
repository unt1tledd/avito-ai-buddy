import secrets
from contextlib import asynccontextmanager
from importlib.resources import files
from typing import Annotated

import httpx
from fastapi import Depends, FastAPI, HTTPException, Request
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from .agent import Agent
from .config import Settings
from .contracts import ChatRequest, ChatResponse
from .llm import LanguageModel, ModelError, ResponsesModel
from .onboarding_client import DemoOnboarding, HttpOnboarding
from .rag import Knowledge


def create_app(settings: Settings | None = None, *, model: LanguageModel | None = None) -> FastAPI:
    settings = settings or Settings.from_env()

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        if settings.onboarding_url and not settings.onboarding_token:
            raise RuntimeError("ONBOARDING_SERVICE_TOKEN is required when ONBOARDING_URL is configured")
        knowledge = Knowledge.load(settings.knowledge_path)
        prompt = files("ai_buddy").joinpath("prompts/system.txt").read_text(encoding="utf-8")
        async with httpx.AsyncClient(timeout=25) as http:
            language_model = model
            if language_model is None and settings.llm_api_key and settings.llm_model:
                language_model = ResponsesModel(http, settings.llm_base_url, settings.llm_api_key, settings.llm_model)
            onboarding = (
                HttpOnboarding(http, settings.onboarding_url, settings.onboarding_token)
                if settings.onboarding_url
                else DemoOnboarding(settings.demo_db_path)
            )
            app.state.agent = Agent(language_model, knowledge, onboarding, prompt) if language_model else None
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
        responses={
            401: {"description": "Invalid or missing chat service token"},
            502: {"description": "Model or tool orchestration error"},
            503: {"description": "AI service configuration is incomplete"},
        },
    )
    async def chat(payload: ChatRequest, request: Request) -> ChatResponse:
        agent: Agent | None = request.app.state.agent
        if agent is None:
            raise HTTPException(status_code=503, detail="Не настроены ключ и модель LLM.")
        try:
            return await agent.answer(payload)
        except ModelError as error:
            raise HTTPException(status_code=502, detail=str(error)) from error

    return app
