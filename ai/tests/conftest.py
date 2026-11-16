"""Shared fixtures discovered by pytest across the AI test suite."""

import os
from uuid import uuid4

import psycopg
import pytest
from psycopg import sql
from psycopg.conninfo import make_conninfo

from ai_service.clients.embeddings import Embeddings
from ai_service.core.config import EMBEDDING_DIMENSIONS, Settings
from ai_service.repositories.knowledge import KnowledgeRepository
from ai_service.schemas.contracts import SearchHit, SearchResult


@pytest.fixture
def settings(monkeypatch):
    embedding = [1.0] + [0.0] * (EMBEDDING_DIMENSIONS - 1)
    visible_sources = {
        ("intern", "backend"): ["demo-dev-environment"],
        ("intern", "51"): ["demo-dev-environment"],
        ("mentor", "backend"): ["demo-mentor-environment", "demo-dev-environment"],
    }

    async def embed(self, texts):
        assert len(texts) == 1
        return [embedding]

    async def search(self, vector, profile, actor, min_similarity, *, query):
        assert len(profile) == 64
        assert vector == embedding
        assert min_similarity == 0.3
        rows = visible_sources.get((actor.role, actor.team), [])
        return SearchResult(hits=[
            SearchHit(id=key, title=key, text="Фрагмент инструкции") for key in rows
        ])

    async def accessible_source_ids(self, source_ids, profile, actor):
        assert len(profile) == 64
        return source_ids.intersection(visible_sources.get((actor.role, actor.team), []))

    monkeypatch.setattr(Embeddings, "embed", embed)
    monkeypatch.setattr(KnowledgeRepository, "search", search)
    monkeypatch.setattr(KnowledgeRepository, "accessible_source_ids", accessible_source_ids)
    return Settings(
        service_token="chat-test-token",
        knowledge_db_dsn="postgresql://knowledge.test/knowledge",
        knowledge_processing_approved=True,
        embedding_api_key="test-embedding-token",
    )


@pytest.fixture
def pg_schema():
    dsn = os.environ.get("AI_TEST_POSTGRES_DSN")
    if not dsn:
        pytest.skip("Set AI_TEST_POSTGRES_DSN to a test database with pgvector installed in public")
    schema = f"ai_acl_{uuid4().hex}"
    identifier = sql.Identifier(schema)
    with psycopg.connect(dsn, autocommit=True) as db:
        db.execute("CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public")
        db.execute(sql.SQL("CREATE SCHEMA {}").format(identifier))
        try:
            yield make_conninfo(dsn, options=f"-csearch_path={schema},public")
        finally:
            db.execute(sql.SQL("DROP SCHEMA {} CASCADE").format(identifier))


