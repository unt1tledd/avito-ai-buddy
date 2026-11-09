from typing import Any, Protocol

import httpx
from pydantic import BaseModel, ValidationError


class ModelError(Exception):
    pass


class ModelResponse(BaseModel):
    output: list[dict[str, Any]]

    @property
    def text(self) -> str:
        return "\n".join(
            part["text"]
            for item in self.output
            if item["type"] == "message"
            for part in item.get("content", [])
            if part.get("type") == "output_text"
        ).strip()


class LanguageModel(Protocol):
    async def respond(
        self, *, instructions: str, messages: list[dict], tools: list[dict]
    ) -> ModelResponse: ...


class ResponsesModel:
    def __init__(self, http: httpx.AsyncClient, base_url: str, api_key: str, model: str):
        self.http = http
        self.url = f"{base_url.rstrip('/')}/responses"
        self.api_key = api_key
        self.model = model

    async def respond(
        self, *, instructions: str, messages: list[dict], tools: list[dict]
    ) -> ModelResponse:
        try:
            response = await self.http.post(
                self.url,
                headers={"Authorization": f"Bearer {self.api_key}"},
                json={
                    "model": self.model,
                    "instructions": instructions,
                    "input": messages,
                    "tools": tools,
                    "parallel_tool_calls": False,
                    "max_output_tokens": 1500,
                    "store": False,
                },
            )
            response.raise_for_status()
            return ModelResponse.model_validate_json(response.content)
        except (httpx.HTTPError, ValidationError) as error:
            raise ModelError("Модель временно недоступна.") from error
