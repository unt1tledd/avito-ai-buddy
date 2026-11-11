import asyncio
import json
import os
from pathlib import Path
from uuid import uuid4

import httpx
import psycopg
import pytest
from mcp import Client
from psycopg import sql
from psycopg.conninfo import make_conninfo
from psycopg.rows import dict_row
from psycopg_pool import AsyncConnectionPool

from ai_service.clients.embeddings import EMBEDDING_DIMENSIONS, Embeddings
from ai_service.core.config import Settings
from ai_service.mcp.server import build_server
from ai_service.rag.import_documents import import_documents
from ai_service.rag.retriever import Retriever
from ai_service.repositories.knowledge import KnowledgeError, KnowledgeRepository
from ai_service.schemas.contracts import Actor, Document, SearchHit, SearchResult


@pytest.mark.parametrize(
    ("payload", "error", "message"),
    [
        pytest.param("[]", ValueError, "хотя бы один фрагмент", id="empty-array"),
        pytest.param("[", ValueError, "json_invalid", id="invalid-json"),
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


MIGRATIONS = Path(__file__).resolve().parents[1] / "migrations"
PROFILE = "a" * 64
VECTOR = [1.0] + [0.0] * (EMBEDDING_DIMENSIONS - 1)
ACTOR = Actor(user_id="42", role="intern", team="51")


@pytest.fixture
def pg_schema():
    dsn = os.environ.get("AI_TEST_POSTGRES_DSN")
    if not dsn:
        pytest.skip("Set AI_TEST_POSTGRES_DSN to a test database with pgvector installed in public")
    schema = f"ai_acl_{uuid4().hex}"
    identifier = sql.Identifier(schema)
    with psycopg.connect(dsn, autocommit=True) as db:
        db.execute(sql.SQL("CREATE SCHEMA {}").format(identifier))
        try:
            yield make_conninfo(dsn, options=f"-csearch_path={schema},public")
        finally:
            db.execute(sql.SQL("DROP SCHEMA {} CASCADE").format(identifier))


def migration_sql(version, up=True):
    migration = next(MIGRATIONS.glob(f"{version:06d}_*.sql"))
    return migration.read_text(encoding="utf-8").split("-- +goose Down", 1)[0 if up else 1]


def migrate(dsn):
    with psycopg.connect(dsn) as db:
        db.execute(migration_sql(1))
        db.execute(migration_sql(2))


def pool_for(dsn):
    return AsyncConnectionPool(dsn, min_size=0, max_size=2, open=False, kwargs={"row_factory": dict_row})


def doc(id, *, role="intern", team=None):
    return Document(id=id, title=id, text="A" * 2000, roles={role}, teams={team} if team else set())


def test_postgres_search_filters_by_role_team_threshold_and_profile(pg_schema):
    migrate(pg_schema)
    documents = [
        doc("backend-intern", team="51"),
        doc("any-intern"),
        doc("mentor", role="mentor", team="51"),
        doc("distant"),
    ]
    far = [0.0, 1.0, *VECTOR[2:]]

    async def run():
        async with pool_for(pg_schema) as pool:
            repository = KnowledgeRepository(pg_schema, pool)
            with pytest.raises(KnowledgeError, match="не проиндексирована"):
                await repository.check_ready(PROFILE)
            await repository.replace(documents, [VECTOR] * 3 + [far], "test-model", PROFILE)
            await repository.check_ready(PROFILE)
            for role, team, expected in [
                ("intern", "51", ["any-intern", "backend-intern"]),
                ("intern", "52", ["any-intern"]),
                ("mentor", "51", ["mentor"]),
                ("mentor", "52", []),
                ("other", "51", []),
            ]:
                actor = ACTOR.model_copy(update={"role": role, "team": team})
                result = await repository.search(VECTOR, PROFILE, actor, 0.3)
                assert [hit.id for hit in result.hits] == expected
                assert all(len(hit.text) == 1200 for hit in result.hits)
            assert len((await repository.search(VECTOR, PROFILE, ACTOR, 0)).hits) == 3
            with pytest.raises(KnowledgeError) as error:
                await repository.search(VECTOR, "b" * 64, ACTOR, 0.3)
            assert error.value.reason == "profile_missing"

    asyncio.run(run())


def test_qwen_import_query_and_atomic_reindex(pg_schema, tmp_path, monkeypatch):
    migrate(pg_schema)
    source = tmp_path / "corpus.json"
    source.write_text(json.dumps([doc("qwen", team="51").model_dump(mode="json")]), encoding="utf-8")
    settings = Settings(
        knowledge_db_dsn=pg_schema, knowledge_processing_approved=True,
        embedding_model="Qwen/Qwen3-Embedding-8B", embedding_base_url="https://api.siliconflow.com/v1",
        embedding_api_key="test-key", llm_api_key="other-key",
    )
    requests = []

    def handler(request):
        assert request.headers["authorization"] == "Bearer test-key"
        payload = json.loads(request.content)
        assert payload["dimensions"] == 1024
        requests.append(payload["input"])
        return httpx.Response(200, json={"data": [
            {"index": index, "embedding": VECTOR[:1024]} for index, _ in enumerate(payload["input"])
        ]})

    client = httpx.AsyncClient
    transport = httpx.MockTransport(handler)
    monkeypatch.setattr(httpx, "AsyncClient", lambda **kwargs: client(transport=transport, **kwargs))

    async def run():
        await import_documents(source, settings)
        async with pool_for(pg_schema) as pool, client(transport=transport) as http:
            repo = KnowledgeRepository(pg_schema, pool)
            embeddings = Embeddings(http, settings)
            retriever = Retriever(repo, embeddings, 0.3)
            assert [hit.id for hit in (await retriever.search("Question", ACTOR)).hits] == ["qwen"]
            changed = Embeddings(http, settings.model_copy(update={"embedding_index_version": "2"}))
            with pytest.raises(KnowledgeError, match="не проиндексирована"):
                await Retriever(repo, changed, 0.3).search("Question", ACTOR)
            assert Embeddings(http, settings.model_copy(update={
                "embedding_base_url": "https://api.siliconflow.cn/v1",
            })).profile != embeddings.profile
            with pytest.raises(KnowledgeError, match="заменить"):
                await repo.replace([doc("broken")], [[1.0]], changed.model, changed.profile)
            assert [hit.id for hit in (await retriever.search("Question", ACTOR)).hits] == ["qwen"]

            async with await psycopg.AsyncConnection.connect(pg_schema) as writer:
                await writer.execute("LOCK TABLE documents IN SHARE ROW EXCLUSIVE MODE")
                imports = [
                    asyncio.create_task(repo.replace([doc(name)], [VECTOR[:1024]], changed.model, changed.profile))
                    for name in ("first", "second")
                ]
                try:
                    assert [hit.id for hit in (await retriever.search("Question", ACTOR)).hits] == ["qwen"]
                    assert not any(job.done() for job in imports)
                finally:
                    await writer.commit()
                    await asyncio.gather(*imports)

            new_result = await Retriever(repo, changed, 0.3).search("Question", ACTOR)
            assert len(new_result.hits) == 1 and new_result.hits[0].id in {"first", "second"}
            with pytest.raises(KnowledgeError, match="не проиндексирована"):
                await retriever.search("Question", ACTOR)

    asyncio.run(run())
    assert len(requests[0][0]) > 2000


def test_profile_migration_preserves_legacy_and_blocks_unsafe_rollback(pg_schema):
    with psycopg.connect(pg_schema, autocommit=True) as db:
        db.execute(migration_sql(1))
        document_id = db.execute("INSERT INTO documents (title) VALUES ('legacy') RETURNING id").fetchone()[0]
        db.execute(
            """INSERT INTO document_chunks (document_id, source_id, body, embedding_model, embedding)
               VALUES (%s, 'legacy', 'text', 'legacy-model', %s::vector)""", (document_id, json.dumps(VECTOR)),
        )
        db.execute(migration_sql(2))
        assert db.execute("SELECT embedding_profile, vector_dims(embedding) FROM document_chunks").fetchone() == (None, 1536)
        with db.transaction():
            db.execute(migration_sql(2, up=False))
        assert db.execute("SELECT vector_dims(embedding) FROM document_chunks").fetchone() == (1536,)
        db.execute(migration_sql(2))
        db.execute("UPDATE document_chunks SET embedding_profile = %s, embedding = %s::vector",
                   (PROFILE, json.dumps(VECTOR[:1024])))
        with pytest.raises(psycopg.errors.CheckViolation), db.transaction():
            db.execute(migration_sql(2, up=False))
        assert db.execute("SELECT embedding_profile, vector_dims(embedding) FROM document_chunks").fetchone() == (PROFILE, 1024)


def test_readiness_detects_unapplied_profile_migration(pg_schema):
    with psycopg.connect(pg_schema) as db:
        db.execute(migration_sql(1))

    async def run():
        async with pool_for(pg_schema) as pool:
            with pytest.raises(KnowledgeError) as error:
                await KnowledgeRepository(pg_schema, pool).check_ready(PROFILE)
            assert error.value.reason == "schema_missing"

    asyncio.run(run())


def test_import_rejects_oversized_input_before_network(tmp_path):
    settings = Settings(knowledge_db_dsn="postgresql://unused.invalid/db", knowledge_processing_approved=True)
    source = tmp_path / "large.json"
    source.write_bytes(b" " * (4 * 1024 * 1024 + 1))
    with pytest.raises(ValueError, match="4 МиБ"):
        asyncio.run(import_documents(source, settings))
    source.write_text(json.dumps([{
        "id": f"doc-{i}", "title": "Synthetic", "text": "Test", "roles": ["intern"],
    } for i in range(1001)]))
    with pytest.raises(ValueError, match="1000 фрагментов"):
        asyncio.run(import_documents(source, settings))


def test_readiness_checks_real_pool_and_active_profile(pg_schema):
    from fastapi.testclient import TestClient

    from ai_service.main import create_app

    settings = Settings(
        mode="demo", service_token="chat-key", knowledge_db_dsn=pg_schema,
        knowledge_processing_approved=True, llm_api_key="llm-key", llm_model="test-model",
    )
    profile = Embeddings(None, settings).profile

    with TestClient(create_app(settings)) as client:
        response = client.get("/readyz")
        assert response.status_code == 503
        assert response.json()["reason"] == "schema_missing"
        migrate(pg_schema)
        response = client.get("/readyz")
        assert response.status_code == 503
        assert response.json()["reason"] == "profile_missing"
        asyncio.run(KnowledgeRepository(pg_schema).replace([doc("active")], [VECTOR], settings.embedding_model, profile))
        assert client.get("/readyz").status_code == 200
