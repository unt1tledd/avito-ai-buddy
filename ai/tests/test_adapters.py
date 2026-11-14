import asyncio
import json

import httpx
import pytest

from ai_service.clients.embeddings import EmbeddingError, Embeddings
from ai_service.clients.llm import ModelError, ResponsesModel
from ai_service.core.config import EMBEDDING_DIMENSIONS, SILICONFLOW_DIMENSIONS, Settings, resolve_embedding_profile


FIRST_VECTOR = [1, 0] + [0] * (EMBEDDING_DIMENSIONS - 2)
SECOND_VECTOR = [0, 1] + [0] * (EMBEDDING_DIMENSIONS - 2)


@pytest.mark.parametrize(("status", "items", "expected"), [
    (200, [(1, SECOND_VECTOR), (0, FIRST_VECTOR)], [FIRST_VECTOR, SECOND_VECTOR]),
    (200, [(0, FIRST_VECTOR), (0, SECOND_VECTOR)], None),
    (200, [(0, FIRST_VECTOR), (1, [0] * EMBEDDING_DIMENSIONS)], None),
    (200, [(0, [1, 0]), (1, [0, 1])], None),
    (429, [], None),
])
def test_embeddings_request(status, items, expected):
    def handler(request):
        assert request.url == "https://embeddings.example/v1/embeddings"
        assert "authorization" not in request.headers
        assert json.loads(request.content) == {
            "model": "test-embedding", "input": ["Первый", "Второй"], "encoding_format": "float",
        }
        return httpx.Response(status, json={
            "data": [{"index": index, "embedding": vector} for index, vector in items],
        })

    async def run():
        settings = Settings(
            embedding_base_url="https://embeddings.example/v1",
            embedding_model="test-embedding",
            llm_api_key="llm-secret",
        )
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as http:
            return await Embeddings(http, resolve_embedding_profile(settings)).embed(["Первый", "Второй"])

    if expected is None:
        with pytest.raises(EmbeddingError):
            asyncio.run(run())
    else:
        assert asyncio.run(run()) == expected


def test_qwen_embeddings_use_separate_credentials_and_database_dimensions(monkeypatch):
    monkeypatch.setenv("EMBEDDING_BASE_URL", "https://api.siliconflow.cn/v1")
    monkeypatch.setenv("EMBEDDING_API_KEY", "qwen-test-key")
    monkeypatch.setenv("EMBEDDING_MODEL", "Qwen/Qwen3-Embedding-8B")
    monkeypatch.setenv("EMBEDDING_DIMENSIONS", str(SILICONFLOW_DIMENSIONS))
    monkeypatch.setenv("LLM_API_KEY", "different-llm-key")

    qwen_vector = FIRST_VECTOR[:SILICONFLOW_DIMENSIONS]

    def handler(request):
        assert request.url == "https://api.siliconflow.cn/v1/embeddings"
        assert request.headers["authorization"] == "Bearer qwen-test-key"
        assert json.loads(request.content) == {
            "model": "Qwen/Qwen3-Embedding-8B",
            "input": ["Вопрос"],
            "encoding_format": "float",
            "dimensions": SILICONFLOW_DIMENSIONS,
        }
        return httpx.Response(200, json={"data": [{"index": 0, "embedding": qwen_vector}]})

    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as http:
            return await Embeddings(http, resolve_embedding_profile(Settings.from_env())).embed(["Вопрос"])

    assert asyncio.run(run()) == [qwen_vector]


def test_siliconflow_rejects_1536_before_network():
    settings = Settings(
        embedding_model="Qwen/Qwen3-Embedding-8B",
        embedding_base_url="https://api.siliconflow.com/v1",
        embedding_dimensions=1536,
    )
    with pytest.raises(RuntimeError, match="1024"):
        resolve_embedding_profile(settings)


@pytest.mark.parametrize(("status", "response_fields"), [(200, {}), (200, {"status": "completed"}), (429, {})])
def test_llm_request(status, response_fields):
    def handler(request):
        assert request.url == "https://model.example/v1/responses"
        assert request.headers["authorization"] == "Bearer model-key"
        data = json.loads(request.content)
        assert data["model"] == "test-model"
        assert data["input"] == [{"role": "user", "content": "Вопрос"}]
        assert data["store"] is False
        return httpx.Response(status, json={**response_fields, "output": [
            {"type": "message", "content": [{"type": "output_text", "text": "Ответ"}]}
        ]})

    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as http:
            model = ResponsesModel(http, "https://model.example/v1", "model-key", "test-model")
            return await model.respond(
                instructions="Правила", messages=[{"role": "user", "content": "Вопрос"}], tools=[]
            )

    if status == 200:
        assert asyncio.run(run()).text == "Ответ"
    else:
        with pytest.raises(ModelError):
            asyncio.run(run())


@pytest.mark.parametrize(("payload", "error"), [
    ({"output": [{"type": "message", "content": [{"type": "output_text"}]}]}, "неизвестном формате"),
    ({"output": [{"type": "message", "content": [{"type": "output_text", "text": 5}]}]}, "неизвестном формате"),
    ({"status": "incomplete", "output": []}, "неполный ответ"),
    ({"status": "failed", "output": []}, "не завершила"),
    ({"output": [{"type": "message", "content": [{"type": "refusal", "refusal": "No"}]}]}, "отказалась"),
    ({"output": [{
        "type": "message", "status": "incomplete", "content": [{"type": "output_text", "text": "Часть ответа"}],
    }]}, "неполный ответ"),
    ({"output": [{
        "type": "function_call", "status": "in_progress", "call_id": "call-id", "name": "find_instruction",
        "arguments": '{"query":"окружение"}',
    }]}, "неполный ответ"),
])
def test_llm_rejects_invalid_or_unsuccessful_responses(payload, error):
    async def run():
        transport = httpx.MockTransport(lambda request: httpx.Response(200, json=payload))
        async with httpx.AsyncClient(transport=transport) as http:
            model = ResponsesModel(http, "https://model.example/v1", "model-key", "test-model")
            await model.respond(instructions="Правила", messages=[], tools=[])

    with pytest.raises(ModelError, match=error):
        asyncio.run(run())


@pytest.mark.parametrize(("call_ids", "error"), [
    ([{}], "неизвестном формате"),
    ([{"call_id": " "}], "неизвестном формате"),
    ([{"call_id": 5}], "неизвестном формате"),
    ([{"call_id": "repeated"}] * 2, "повторяющиеся идентификаторы"),
])
def test_llm_requires_usable_tool_call_ids(call_ids, error):
    payload = {"output": [
        {"type": "function_call", "name": "find_instruction", "arguments": "{}", **fields}
        for fields in call_ids
    ]}

    async def run():
        transport = httpx.MockTransport(lambda request: httpx.Response(200, json=payload))
        async with httpx.AsyncClient(transport=transport) as http:
            model = ResponsesModel(http, "https://model.example/v1", "model-key", "test-model")
            await model.respond(instructions="Правила", messages=[], tools=[])

    with pytest.raises(ModelError, match=error):
        asyncio.run(run())


def test_openrouter_requires_pinned_embedding_provider():
    settings = Settings(
        embedding_model="qwen/qwen3-embedding-8b",
        embedding_base_url="https://openrouter.ai/api/v1",
        embedding_dimensions=1536,
    )
    with pytest.raises(RuntimeError, match="EMBEDDING_PROVIDER"):
        resolve_embedding_profile(settings)


def test_openrouter_provider_is_sent_and_changes_index_profile():
    settings = Settings(
        embedding_model="qwen/qwen3-embedding-8b",
        embedding_base_url="https://openrouter.ai/api/v1",
        embedding_provider="deepinfra",
        embedding_api_key="test-key",
        embedding_dimensions=1536,
    )

    def handler(request):
        payload = json.loads(request.content)
        assert payload["dimensions"] == 1536
        assert payload["provider"] == {"only": ["deepinfra"], "allow_fallbacks": False, "require_parameters": True}
        return httpx.Response(200, json={"data": [{"index": 0, "embedding": FIRST_VECTOR}]})

    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as http:
            client = Embeddings(http, resolve_embedding_profile(settings))
            other = Embeddings(http, resolve_embedding_profile(settings.model_copy(update={
                "embedding_provider": "siliconflow", "embedding_dimensions": 1024,
            })))
            with pytest.raises(RuntimeError, match="supported OpenRouter Qwen providers"):
                resolve_embedding_profile(settings.model_copy(update={"embedding_provider": "nebius"}))
            assert other.profile != client.profile
            assert await client.embed(["synthetic"]) == [FIRST_VECTOR]

    asyncio.run(run())


@pytest.mark.parametrize('usage', [None, {'prompt_tokens': 11, 'cost': 0.00001, 'details': None},
                                   {'prompt_tokens': 'unavailable'}])
def test_embedding_usage_metadata_does_not_break_valid_vectors(usage):
    async def run():
        handler = lambda request: httpx.Response(200, json={
            'data': [{'index': 0, 'embedding': FIRST_VECTOR}], 'usage': usage,
        })
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as http:
            client = Embeddings(http, resolve_embedding_profile(Settings(embedding_api_key='test')))
            assert await client.embed(['Вопрос']) == [FIRST_VECTOR]
            assert client.calls == 1
            assert client.usage_complete == isinstance((usage or {}).get('prompt_tokens'), int)
    asyncio.run(run())
