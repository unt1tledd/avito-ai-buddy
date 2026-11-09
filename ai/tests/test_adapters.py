import asyncio
import json

import httpx
import pytest

from ai_service.clients.embeddings import EMBEDDING_DIMENSIONS, EmbeddingError, Embeddings
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


@pytest.mark.parametrize(("status", "profile", "error"), [
    (200, ACTOR.model_dump(), None),
    (200, {**ACTOR.model_dump(), "user_id": "different-user"}, "другого пользователя"),
    (200, {"user_id": ACTOR.user_id}, "получить профиль"),
    (404, {}, "получить профиль"),
])
def test_user_request(status, profile, error):
    def handler(request):
        assert request.method == "GET"
        assert request.url == "https://user.example/v1/users/test-user"
        assert request.headers["authorization"] == "Bearer user-key"
        return httpx.Response(status, json=profile)

    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as http:
            return await HttpUser(http, "https://user.example/", "user-key").get_actor(ACTOR.user_id)

    if error:
        with pytest.raises(UserError, match=error):
            asyncio.run(run())
    else:
        assert asyncio.run(run()) == ACTOR


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
