import json
import logging
from contextlib import AsyncExitStack, asynccontextmanager
from importlib.resources import files

import httpx
from fastapi import FastAPI, Request, Response
from fastapi.encoders import jsonable_encoder
from fastapi.exceptions import RequestValidationError
from psycopg.rows import dict_row
from psycopg_pool import AsyncConnectionPool

from .agents.agent import Agent
from .api.middleware import RequestBoundary
from .api.routes import create_router
from .clients.embeddings import Embeddings
from .clients.llm import LanguageModel, ResponsesModel
from .clients.onboarding import HttpOnboarding, Onboarding
from .clients.user import HttpUser
from .core.config import Settings, validate_bearer_token
from .rag.retriever import Retriever
from .repositories.knowledge import KnowledgeRepository
from .services.chat_service import ChatService
from .services.onboarding_service import DemoOnboarding


HTTP_CLIENT_TIMEOUT_SECONDS = 25
DB_POOL_TIMEOUT_SECONDS = 2
DB_POOL_MAX_SIZE = 4


def create_app(settings: Settings | None = None, *, model: LanguageModel | None = None) -> FastAPI:
    settings = settings or Settings.from_env()

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        settings.validate_mode()
        if settings.service_token:
            validate_bearer_token("AI_SERVICE_TOKEN", settings.service_token)
        async with AsyncExitStack() as stack:
            http = await stack.enter_async_context(httpx.AsyncClient(timeout=HTTP_CLIENT_TIMEOUT_SECONDS))
            user: HttpUser | None = None
            onboarding: Onboarding
            if settings.mode == "integration":
                if settings.user_url and not settings.confirmations_only:
                    user = HttpUser(http, settings.user_url, settings.user_token)
                onboarding = HttpOnboarding(http, settings.onboarding_url, settings.onboarding_token)
            else:
                onboarding = DemoOnboarding(settings.demo_db_path)

            agent = None
            reason = None
            if not settings.confirmations_only:
                if not settings.knowledge_db_dsn:
                    reason = "knowledge_dsn_missing"
                elif not (model or (settings.llm_api_key and settings.llm_model.strip())):
                    reason = "llm_config_missing"
                elif not settings.knowledge_processing_approved:
                    reason = "knowledge_processing_unapproved"
                else:
                    language_model = model or ResponsesModel(
                        http, settings.llm_base_url, settings.llm_api_key, settings.llm_model
                    )
                    embeddings = Embeddings(http, settings)
                    # Pool retries can include connection details. Emit only classified errors from the repository.
                    logging.getLogger("psycopg.pool").disabled = True
                    pool = await stack.enter_async_context(AsyncConnectionPool(
                        settings.knowledge_db_dsn,
                        kwargs={"row_factory": dict_row, "connect_timeout": 3, "autocommit": True},
                        min_size=0, max_size=DB_POOL_MAX_SIZE, max_waiting=16,
                        timeout=DB_POOL_TIMEOUT_SECONDS, open=False, name="ai-knowledge",
                    ))
                    retriever = Retriever(
                        KnowledgeRepository(settings.knowledge_db_dsn, pool),
                        embeddings,
                        settings.knowledge_min_similarity,
                    )
                    prompt = files("ai_service.agents").joinpath("prompts", "system.txt").read_text(encoding="utf-8")
                    agent = Agent(language_model, retriever, prompt)

            app.state.chat_service = ChatService(
                agent, user, onboarding, settings.mode == "integration", reason
            )
            yield

    app = FastAPI(title="AI-buddy internal API", version="1.0.0", lifespan=lifespan)
    app.add_middleware(RequestBoundary, chat_timeout=settings.chat_timeout_seconds)
    app.include_router(create_router(settings.service_token, settings.confirmations_only))

    @app.exception_handler(RequestValidationError)
    async def invalid_body(_request: Request, error: RequestValidationError) -> Response:
        try:
            detail = json.dumps(jsonable_encoder(error.errors()))
            # Python's JSON decoder accepts NaN and Infinity, which aren't JSON values.
            safe_detail = json.loads(detail, parse_constant=lambda _: None)
        except RecursionError:
            safe_detail = [
                {key: issue[key] for key in ("type", "loc", "msg")}
                for issue in error.errors()
            ]
        return Response(json.dumps({"detail": safe_detail}), status_code=422, media_type="application/json")

    return app
