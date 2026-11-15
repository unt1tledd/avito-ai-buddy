import json
import logging
from contextlib import AsyncExitStack, asynccontextmanager
from importlib.resources import files

import httpx
from fastapi import FastAPI, Request, Response
from fastapi.exceptions import RequestValidationError
from psycopg.rows import dict_row
from psycopg_pool import AsyncConnectionPool

from .agents.agent import Agent
from .api.middleware import RequestBoundary
from .api.routes import create_router
from .clients.embeddings import Embeddings
from .clients.llm import LanguageModel, ResponsesModel
from .core.config import Settings, resolve_embedding_profile, validate_bearer_token
from .core.diagnostics import logger
from .rag.retriever import Retriever
from .repositories.knowledge import KnowledgeRepository
from .services.chat_service import ChatService


HTTP_CLIENT_TIMEOUT_SECONDS = 60
DB_POOL_TIMEOUT_SECONDS = 2
DB_POOL_MAX_SIZE = 4


async def db_reconnect_failed(pool: AsyncConnectionPool) -> None:
    logger.warning("event=db_reconnect_failed pool=%s", pool.name)


def create_app(settings: Settings | None = None, *, model: LanguageModel | None = None) -> FastAPI:
    settings = settings or Settings.from_env()

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        if settings.service_token:
            validate_bearer_token("AI_SERVICE_TOKEN", settings.service_token)
        async with AsyncExitStack() as stack:
            http = await stack.enter_async_context(httpx.AsyncClient(timeout=HTTP_CLIENT_TIMEOUT_SECONDS))
            agent = None
            reason = None
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
                embeddings = Embeddings(http, resolve_embedding_profile(settings))
                # Pool retries can include connection details. Use only safe application events.
                logging.getLogger("psycopg.pool").disabled = True
                pool = await stack.enter_async_context(AsyncConnectionPool(
                    settings.knowledge_db_dsn,
                    kwargs={"row_factory": dict_row, "connect_timeout": 3, "autocommit": True},
                    min_size=0, max_size=DB_POOL_MAX_SIZE, max_waiting=16,
                    timeout=DB_POOL_TIMEOUT_SECONDS, open=False, name="ai-knowledge",
                    reconnect_failed=db_reconnect_failed,
                ))
                retriever = Retriever(
                    KnowledgeRepository(settings.knowledge_db_dsn, pool, settings),
                    embeddings,
                    settings.knowledge_min_similarity,
                    settings=settings,
                )
                prompt = files("ai_service.agents").joinpath("prompts", "system.txt").read_text(encoding="utf-8")
                agent = Agent(language_model, retriever, prompt, settings=settings)

            app.state.chat_service = ChatService(agent, reason)
            yield

    app = FastAPI(title="AI-buddy internal API", version="1.0.0", lifespan=lifespan)
    app.add_middleware(RequestBoundary, chat_timeout=settings.effective_chat_timeout_seconds)
    app.include_router(create_router(settings.service_token))

    @app.exception_handler(RequestValidationError)
    async def invalid_body(_request: Request, error: RequestValidationError) -> Response:
        # Raw input may contain binary data, NaN, deep nesting or exception objects.
        detail = [{key: issue[key] for key in ("type", "loc", "msg")} for issue in error.errors()]
        # Escaping also handles unpaired surrogates in untrusted field names.
        return Response(
            json.dumps({"detail": detail}, ensure_ascii=True, allow_nan=False),
            status_code=422, media_type="application/json",
        )

    return app
