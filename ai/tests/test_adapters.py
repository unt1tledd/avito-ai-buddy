import asyncio
import json

import httpx
import pytest

from ai_service.clients.embeddings import EMBEDDING_DIMENSIONS, SILICONFLOW_DIMENSIONS, EmbeddingError, Embeddings
from ai_service.clients.llm import ModelError, ResponsesModel
from ai_service.clients.onboarding import HttpOnboarding, OnboardingRejected, OnboardingUnknown
from ai_service.clients.user import HttpUser, UserError
from ai_service.core.config import Settings
from ai_service.schemas.contracts import Actor


ACTOR = Actor(user_id="test-user", role="intern", team="backend")
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
            return await Embeddings(http, settings).embed(["Первый", "Второй"])

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
            return await Embeddings(http, Settings.from_env()).embed(["Вопрос"])

    assert asyncio.run(run()) == [qwen_vector]



def test_siliconflow_rejects_1536_before_network():
    settings = Settings(
        embedding_model="Qwen/Qwen3-Embedding-8B",
        embedding_base_url="https://api.siliconflow.com/v1",
        embedding_dimensions=1536,
    )
    with pytest.raises(RuntimeError, match="1024"):
        Embeddings(None, settings)


PROFILE = {
    "internship_id": "7",
    "intern": {"user_id": "42", "first_name": "Synthetic"},
    "team": {"team_id": "51", "name": "Backend Platform"},
    "track": "infra",
}


@pytest.mark.parametrize(("status", "profile", "reason"), [
    (200, PROFILE, None),
    (200, {**PROFILE, "intern": {"user_id": "43"}}, "wrong_user"),
    (200, {**PROFILE, "intern": None, "mentor": {"user_id": "42"}}, "invalid_profile"),
    (200, {**PROFILE, "team": {"team_id": "0"}}, "invalid_team"),
    (200, {**PROFILE, "actual_end_date": "2026-03-01"}, "internship_ended"),
    (404, {}, "profile_missing"),
    (401, {}, "http_401"),
    (None, {}, "transport"),
])
def test_user_request(status, profile, reason):
    def handler(request):
        assert request.method == "GET"
        assert request.url == "https://user.example/v1/user/42/metadata"
        assert request.headers["authorization"] == "Bearer user-key"
        if status is None:
            raise httpx.ConnectError("Connection refused", request=request)
        return httpx.Response(status, json=profile)

    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as http:
            return await HttpUser(http, "https://user.example/", "user-key").get_actor("42")

    if reason:
        with pytest.raises(UserError) as error:
            asyncio.run(run())
        assert error.value.reason == reason
    else:
        assert asyncio.run(run()) == Actor(user_id="42", role="intern", team="51")


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


@pytest.mark.parametrize(("status", "body", "error", "message"), [
    (200, {"step_id": "dev-environment", "status": "completed"}, None, None),
    (200, {"step_id": "another-step", "status": "completed"}, OnboardingUnknown, "другого шага"),
    (200, {}, OnboardingUnknown, "не подтвердил"),
    (403, {}, OnboardingRejected, "нет права"),
    (404, {}, OnboardingRejected, "не найден"),
    (503, {}, OnboardingUnknown, "не подтвердил"),
])
def test_onboarding_request(status, body, error, message):
    def handler(request):
        assert request.method == "PUT"
        assert request.url.path == "/v1/users/test-user/onboarding/steps/dev-environment/completion"
        assert request.headers["authorization"] == "Bearer onboard-key"
        return httpx.Response(status, json=body)

    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as http:
            return await HttpOnboarding(http, "https://onboarding.example", "onboard-key").complete_step(
                ACTOR, "dev-environment"
            )

    if error:
        with pytest.raises(error, match=message):
            asyncio.run(run())
    else:
        assert asyncio.run(run()).status == "completed"


def test_openrouter_requires_pinned_embedding_provider():
    settings = Settings(
        embedding_model="qwen/qwen3-embedding-8b",
        embedding_base_url="https://openrouter.ai/api/v1",
        embedding_dimensions=1536,
    )
    with pytest.raises(RuntimeError, match="EMBEDDING_PROVIDER"):
        Embeddings(None, settings)


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
            client = Embeddings(http, settings)
            other = Embeddings(http, settings.model_copy(update={
                "embedding_provider": "siliconflow", "embedding_dimensions": 1024,
            }))
            with pytest.raises(RuntimeError, match="supported OpenRouter Qwen providers"):
                Embeddings(http, settings.model_copy(update={"embedding_provider": "nebius"}))
            assert other.profile != client.profile
            assert await client.embed(["synthetic"]) == [FIRST_VECTOR]

    asyncio.run(run())
