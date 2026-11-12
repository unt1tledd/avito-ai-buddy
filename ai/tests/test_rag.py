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
from ai_service.clients.llm import ModelResponse
from ai_service.core.config import Settings
from ai_service.mcp.server import build_server
from ai_service.rag.import_documents import import_documents
from ai_service.rag.chunking import chunk_documents
from ai_service.rag.retriever import Retriever
from ai_service.repositories.knowledge import KnowledgeError, KnowledgeRepository
from ai_service.schemas.contracts import Actor, Document, MAX_CHUNK_TEXT, SearchHit, SearchResult


@pytest.mark.parametrize(
    ("payload", "error", "message"),
    [
        pytest.param("[]", ValueError, "хотя бы один документ", id="empty-array"),
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
    return Document(id=id, title=id, text="A" * MAX_CHUNK_TEXT, roles={role}, teams={team} if team else set())


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
                assert all(hit.text == "A" * MAX_CHUNK_TEXT for hit in result.hits)
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
    assert requests[0] == [doc("qwen", team="51").embedding_text()]


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
    with pytest.raises(ValueError, match="1000 документов"):
        asyncio.run(import_documents(source, settings))


def test_readiness_checks_real_pool_and_active_profile(pg_schema):
    from fastapi.testclient import TestClient

    from ai_service.main import create_app

    settings = Settings(
        service_token="chat-key", knowledge_db_dsn=pg_schema,
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


def test_long_document_tail_reaches_model_and_preserves_acl_after_reimport(pg_schema, tmp_path, monkeypatch):
    from fastapi.testclient import TestClient

    from ai_service.main import create_app

    migrate(pg_schema)
    question = "Сколько ждать получения VPN?"
    fact = "Доступ к VPN выдаётся за два рабочих дня."
    original = Document(
        id="vpn-guide", title="Настройка VPN", url="https://example.test/vpn",
        text="Общая информация о подключении. " * 90 + "\n\n" + fact,
        roles={"intern"}, teams={"51"}, keywords=["VPN"],
    )
    assert original.text.index(fact) > MAX_CHUNK_TEXT
    chunks = chunk_documents([original])
    by_id = {chunk.id: chunk for chunk in chunks}
    source = tmp_path / "long.json"
    source.write_text(json.dumps([original.model_dump(mode="json")]), encoding="utf-8")
    settings = Settings(
        service_token="chat-key", knowledge_db_dsn=pg_schema,
        knowledge_processing_approved=True, embedding_api_key="test-key",
    )
    embedded = []

    async def embed(self, texts):
        embedded.extend(texts)
        other = [0.0, 1.0, *VECTOR[2:]]
        return [VECTOR if text == question or fact in text else other for text in texts]

    monkeypatch.setattr(Embeddings, "embed", embed)
    asyncio.run(import_documents(source, settings))
    with psycopg.connect(pg_schema) as db:
        first_ids = db.execute("SELECT source_id FROM document_chunks ORDER BY source_id").fetchall()
    asyncio.run(import_documents(source, settings))
    with psycopg.connect(pg_schema) as db:
        assert db.execute("SELECT source_id FROM document_chunks ORDER BY source_id").fetchall() == first_ids
        assert len(first_ids) == len(chunks)
    assert embedded[:len(chunks)] == [chunk.embedding_text() for chunk in chunks]
    contexts = []

    class Model:
        async def respond(self, *, instructions, messages, tools):
            context = json.loads(messages[0]["content"].split("\n", 1)[1])
            contexts.append(context)
            hits = context["hits"]
            if hits:
                assert fact in hits[0]["text"]
                assert all(hit["text"] == by_id[hit["id"]].text for hit in hits)
                text = f"Два рабочих дня. [source:{hits[0]['id']}]"
            else:
                text = "Нет данных."
            final = json.dumps({"kind": "grounded_answer" if hits else "no_knowledge", "answer": text})
            return ModelResponse(output=[{"type": "message", "content": [{"type": "output_text", "text": final}]}])

    headers = {"Authorization": "Bearer chat-key"}
    with TestClient(create_app(settings, model=Model())) as client:
        assert client.get("/readyz").status_code == 200
        payload = {"message": question, "actor": {"user_id": "42", "role": "intern", "team": "51"}}
        reply = client.post("/api/v1/get_answer", headers=headers, json=payload)
        assert reply.status_code == 200, reply.text
        assert "Два рабочих дня" in reply.json()["answer"]
        assert reply.json()["sources"] == contexts[0]["hits"][:1]
        assert all(hit["title"] == original.title and hit["url"] == str(original.url) for hit in reply.json()["sources"])
        for actor in ({"user_id": "42", "role": "mentor", "team": "51"},
                      {"user_id": "42", "role": "intern", "team": "52"}):
            reply = client.post("/api/v1/get_answer", headers=headers, json={**payload, "actor": actor})
            assert reply.status_code == 200, reply.text
            assert reply.json()["sources"] == []
            assert contexts[-1]["hits"] == []


def test_readiness_requires_reimport_of_legacy_long_chunks(pg_schema):
    migrate(pg_schema)
    with psycopg.connect(pg_schema) as db:
        id = db.execute("INSERT INTO documents (title) VALUES ('Legacy') RETURNING id").fetchone()[0]
        db.execute("""
            INSERT INTO document_chunks (document_id, source_id, body, embedding_model, embedding_profile, embedding)
            VALUES (%s, 'legacy', %s, 'test-model', %s, %s::vector)
        """, (id, "A" * (MAX_CHUNK_TEXT + 1), PROFILE, json.dumps(VECTOR)))

    async def run():
        async with pool_for(pg_schema) as pool:
            repository = KnowledgeRepository(pg_schema, pool)
            with pytest.raises(KnowledgeError) as error:
                await repository.check_ready(PROFILE)
            assert error.value.reason == "reindex_required"
            await repository.replace([doc("fixed")], [VECTOR], "test-model", PROFILE)
            await repository.check_ready(PROFILE)

    asyncio.run(run())


def test_current_step_resource_cannot_bypass_document_acl(pg_schema, monkeypatch):
    from fastapi.testclient import TestClient

    from ai_service.main import create_app

    migrate(pg_schema)
    settings = Settings(
        service_token="chat-key", knowledge_db_dsn=pg_schema,
        knowledge_processing_approved=True, embedding_api_key="test-key",
    )
    profile = Embeddings(None, settings).profile
    asyncio.run(KnowledgeRepository(pg_schema).replace(
        [doc("intern-guide", team="51"), doc("mentor-vpn", role="mentor", team="52")],
        [VECTOR, VECTOR], settings.embedding_model, profile,
    ))
    step = {"id": "vpn-step", "title": "Получить VPN", "resource_id": "mentor-vpn"}

    async def embed(self, texts):
        assert len(texts) == 1 and step["title"] in texts[0]
        return [VECTOR]

    monkeypatch.setattr(Embeddings, "embed", embed)
    contexts = []

    class Model:
        async def respond(self, *, instructions, messages, tools):
            context = json.loads(messages[0]["content"].split("\n", 1)[1])
            assert context["current_step"] == step
            contexts.append(context)
            hits = context["hits"]
            final = {
                "kind": "grounded_answer" if hits else "no_knowledge",
                "answer": f"Инструкция. [source:{hits[0]['id']}]" if hits else "Нет данных.",
            }
            return ModelResponse(output=[{
                "type": "message", "content": [{"type": "output_text", "text": json.dumps(final)}],
            }])

    with TestClient(create_app(settings, model=Model())) as client:
        for role, team, expected in [
            ("intern", "51", ["intern-guide"]),
            ("intern", "52", []),
            ("mentor", "52", ["mentor-vpn"]),
        ]:
            response = client.post("/api/v1/get_answer", headers={"Authorization": "Bearer chat-key"}, json={
                "actor": {"user_id": "42", "role": role, "team": team},
                "message": "Как выполнить этот шаг?", "current_step": step,
            })
            assert response.status_code == 200, response.text
            assert [hit["id"] for hit in contexts[-1]["hits"]] == expected
            assert [hit["id"] for hit in response.json()["sources"]] == expected
