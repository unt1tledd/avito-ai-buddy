from contextlib import asynccontextmanager
from importlib.resources import files

import httpx
from fastapi import FastAPI

from .agents.agent import Agent
from .api.routes import create_router
from .clients.embeddings import Embeddings
from .clients.llm import LanguageModel, ResponsesModel
from .clients.onboarding import HttpOnboarding, Onboarding
from .clients.user import HttpUser
from .core.config import Settings
from .rag.retriever import Retriever
from .repositories.knowledge import KnowledgeRepository
from .services.chat_service import ChatService
from .services.onboarding_service import DemoOnboarding


HTTP_CLIENT_TIMEOUT_SECONDS = 25


def create_app(settings: Settings | None = None, *, model: LanguageModel | None = None) -> FastAPI:
    settings = settings or Settings.from_env()

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        settings.validate_mode()
        prompt = files("ai_service.agents").joinpath("prompts", "system.txt").read_text(encoding="utf-8")
        async with httpx.AsyncClient(timeout=HTTP_CLIENT_TIMEOUT_SECONDS) as http:
            user: HttpUser | None = None
            onboarding: Onboarding
            if settings.mode == "integration":
                user = HttpUser(http, settings.user_url, settings.user_token)
                onboarding = HttpOnboarding(http, settings.onboarding_url, settings.onboarding_token)
            else:
                onboarding = DemoOnboarding(settings.demo_db_path)

            language_model = model
            if (
                settings.knowledge_db_dsn
                and language_model is None
                and settings.llm_api_key
                and settings.llm_model
            ):
                language_model = ResponsesModel(http, settings.llm_base_url, settings.llm_api_key, settings.llm_model)

            agent = None
            if language_model and settings.knowledge_db_dsn:
                retriever = Retriever(
                    KnowledgeRepository(settings.knowledge_db_dsn),
                    Embeddings(http, settings),
                    settings.knowledge_min_similarity,
                )
                agent = Agent(language_model, retriever, prompt)

            app.state.chat_service = ChatService(agent, user, onboarding, bool(settings.knowledge_db_dsn))
            yield

    app = FastAPI(title="AI-buddy internal API", version="1.0.0", lifespan=lifespan)
    app.include_router(create_router(settings.service_token))
    return app
