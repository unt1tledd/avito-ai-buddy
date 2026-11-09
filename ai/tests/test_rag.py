import asyncio
import os
from pathlib import Path
from uuid import uuid4

import psycopg
import pytest
from mcp import Client
from psycopg import sql
from psycopg.conninfo import make_conninfo
from pydantic import ValidationError

from ai_service.clients.embeddings import EMBEDDING_DIMENSIONS, Embeddings
from ai_service.core.config import Settings
from ai_service.mcp.server import build_server
from ai_service.rag.import_documents import import_documents
from ai_service.repositories.knowledge import KnowledgeRepository
from ai_service.schemas.contracts import Actor, Document, SearchHit, SearchResult


@pytest.mark.parametrize(
    ("payload", "error", "message"),
    [
        pytest.param("[]", ValueError, "хотя бы один фрагмент", id="empty-array"),
        pytest.param("[", ValidationError, "Invalid JSON", id="invalid-json"),
    ],
)
def test_import_rejects_invalid_source_before_external_calls(tmp_path, monkeypatch, payload, error, message):
    source = tmp_path / "documents.json"
    source.write_text(payload, encoding="utf-8")
    settings = Settings(knowledge_db_dsn="postgresql://unused.invalid/knowledge")

    def unexpected_embed(*args, **kwargs):
        pytest.fail("Import attempted to request embeddings")

    def unexpected_connect(*args, **kwargs):
        pytest.fail("Import attempted to connect to PostgreSQL")

    monkeypatch.setattr(Embeddings, "embed", unexpected_embed)
    monkeypatch.setattr(psycopg.AsyncConnection, "connect", unexpected_connect)

    with pytest.raises(error, match=message):
        asyncio.run(import_documents(source, settings))


def test_mcp_search_uses_bound_actor():
    actor = Actor(user_id="test-user", role="intern", team="backend")

    class Knowledge:
        async def search(self, query, requested_actor):
            assert query == "окружение"
            assert requested_actor == actor
            return SearchResult(hits=[SearchHit(id="setup", title="Setup", text="Setup instructions")])

    async def run():
        async with Client(build_server(Knowledge(), actor)) as client:
            return await client.call_tool("find_instruction", {"query": " окружение "})

    result = asyncio.run(run())
    assert not result.is_error
    assert result.structured_content["hits"][0]["id"] == "setup"


def test_postgres_search_filters_by_role_and_team():
    dsn = os.environ.get("AI_TEST_POSTGRES_DSN")
    if not dsn:
        pytest.skip("Set AI_TEST_POSTGRES_DSN to a test database with pgvector installed in public")

    schema = f"ai_acl_{uuid4().hex}"
    identifier = sql.Identifier(schema)
    migration = Path(__file__).resolve().parents[1] / "migrations" / "000002_create_document_tables.sql"
    migration_up = migration.read_text(encoding="utf-8").split("-- +goose Down", 1)[0]
    vector = [1.0] + [0.0] * (EMBEDDING_DIMENSIONS - 1)
    documents = [
        Document(id="backend-intern", title="Backend", text="Backend setup", roles={"intern"}, teams={"backend"}),
        Document(id="any-intern", title="Intern", text="Common setup", roles={"intern"}),
        Document(id="mentor", title="Mentor", text="Mentor setup", roles={"mentor"}),
    ]

    async def run():
        async with await psycopg.AsyncConnection.connect(dsn, autocommit=True) as db:
            await db.execute(sql.SQL("CREATE SCHEMA {}").format(identifier))
            try:
                scoped_dsn = make_conninfo(dsn, options=f"-csearch_path={schema},public")
                async with await psycopg.AsyncConnection.connect(scoped_dsn) as scoped_db:
                    await scoped_db.execute(migration_up)

                repository = KnowledgeRepository(scoped_dsn)
                await repository.replace(documents, [vector] * len(documents), "test-model")
                for role, team, expected in [
                    ("intern", "backend", ["any-intern", "backend-intern"]),
                    ("intern", "design", ["any-intern"]),
                    ("mentor", "backend", ["mentor"]),
                    ("other", "backend", []),
                ]:
                    actor = Actor(user_id="test-user", role=role, team=team)
                    result = await repository.search(vector, "test-model", actor, 0.3)
                    assert [hit.id for hit in result.hits] == expected, (role, team)
            finally:
                await db.execute(sql.SQL("DROP SCHEMA {} CASCADE").format(identifier))

    asyncio.run(run())
