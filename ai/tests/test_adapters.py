import asyncio
import json

import httpx
import pytest

from ai_buddy.contracts import Actor
from ai_buddy.llm import ModelError, ResponsesModel
from ai_buddy.onboarding_client import HttpOnboarding, OnboardingError


ACTOR = Actor(user_id="test-user", role="intern", team="backend")


@pytest.mark.parametrize("status", [200, 429])
def test_llm_request(status):
    def handler(request):
        assert request.url == "https://model.example/v1/responses"
        assert request.headers["authorization"] == "Bearer model-key"
        data = json.loads(request.content)
        assert data["model"] == "test-model"
        assert data["input"] == [{"role": "user", "content": "Вопрос"}]
        assert data["store"] is False
        return httpx.Response(status, json={"output": [
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


@pytest.mark.parametrize(("status", "step_id", "error"), [
    (200, "dev-environment", None),
    (200, "another-step", "другого шага"),
    (403, "dev-environment", "нет права"),
    (404, "dev-environment", "не найден"),
    (503, "dev-environment", "не подтвердил"),
])
def test_onboarding_request(status, step_id, error):
    def handler(request):
        assert request.method == "PUT"
        assert request.url.path == "/v1/users/test-user/onboarding/steps/dev-environment/completion"
        assert request.headers["authorization"] == "Bearer onboard-key"
        return httpx.Response(status, json={"step_id": step_id, "status": "completed"})

    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as http:
            return await HttpOnboarding(http, "https://onboarding.example", "onboard-key").complete_step(
                ACTOR, "dev-environment"
            )

    if error:
        with pytest.raises(OnboardingError, match=error):
            asyncio.run(run())
    else:
        assert asyncio.run(run()).status == "completed"
