from typing import Annotated, Literal, Protocol

import httpx
from pydantic import BaseModel, ConfigDict, Field, ValidationError

from ..core.config import validate_base_url, validate_bearer_token
from ..core.diagnostics import UpstreamError, http_failure, operation


MAX_MODEL_OUTPUT_TOKENS = 1500


class ModelError(UpstreamError):
    pass


class ResponseItem(BaseModel):
    # Preserve provider metadata when replaying items with tool results.
    model_config = ConfigDict(extra="allow")


class OutputText(ResponseItem):
    type: Literal["output_text"]
    text: str


class Refusal(ResponseItem):
    type: Literal["refusal"]
    refusal: str


MessagePart = Annotated[OutputText | Refusal, Field(discriminator="type")]
ItemStatus = Literal["completed", "incomplete", "in_progress"]


class OutputMessage(ResponseItem):
    type: Literal["message"]
    content: list[MessagePart]
    status: ItemStatus = "completed"


class FunctionCall(ResponseItem):
    type: Literal["function_call"]
    name: str = Field(pattern=r"\S", max_length=128)
    arguments: str = Field(max_length=4096)
    call_id: str = Field(pattern=r"\S", max_length=128)
    status: ItemStatus = "completed"


class Reasoning(ResponseItem):
    type: Literal["reasoning"]


ModelOutput = Annotated[OutputMessage | FunctionCall | Reasoning, Field(discriminator="type")]


class ModelResponse(BaseModel):
    output: list[ModelOutput]
    # Older compatible providers omit status on complete responses.
    status: str = "completed"

    @property
    def text(self) -> str:
        return "\n".join(
            part.text
            for item in self.output
            if isinstance(item, OutputMessage)
            for part in item.content
            if isinstance(part, OutputText)
        ).strip()

    @property
    def calls(self) -> list[FunctionCall]:
        return [item for item in self.output if isinstance(item, FunctionCall)]

    @property
    def refused(self) -> bool:
        return any(
            isinstance(part, Refusal)
            for item in self.output
            if isinstance(item, OutputMessage)
            for part in item.content
        )


class LanguageModel(Protocol):
    async def respond(
        self, *, instructions: str, messages: list[dict], tools: list[dict]
    ) -> ModelResponse: ...


class ResponsesModel:
    def __init__(self, http: httpx.AsyncClient, base_url: str, api_key: str, model: str):
        validate_base_url("LLM_BASE_URL", base_url)
        validate_bearer_token("LLM_API_KEY", api_key)
        self.http = http
        self.url = f"{base_url.rstrip('/')}/responses"
        self.api_key = api_key
        self.model = model

    async def respond(
        self, *, instructions: str, messages: list[dict], tools: list[dict]
    ) -> ModelResponse:
        with operation("llm.respond"):
            return await self._respond(instructions=instructions, messages=messages, tools=tools)

    async def _respond(self, *, instructions: str, messages: list[dict], tools: list[dict]) -> ModelResponse:
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
                    "max_output_tokens": MAX_MODEL_OUTPUT_TOKENS,
                    "store": False,
                },
            )
            response.raise_for_status()
        except httpx.HTTPError as error:
            raise ModelError("Модель временно недоступна.", reason=http_failure(error)) from error
        try:
            result = ModelResponse.model_validate_json(response.content)
        except ValidationError as error:
            raise ModelError("Модель вернула ответ в неизвестном формате.", reason="invalid_response") from error
        if result.status == "incomplete" or any(
            isinstance(item, (OutputMessage, FunctionCall)) and item.status != "completed"
            for item in result.output
        ):
            raise ModelError("Модель вернула неполный ответ.", reason="incomplete")
        if result.status != "completed":
            raise ModelError("Модель не завершила обработку запроса.", reason="bad_status")
        if result.refused:
            raise ModelError("Модель отказалась отвечать на запрос.", reason="refusal")
        call_ids = [call.call_id for call in result.calls]
        if len(set(call_ids)) != len(call_ids):
            raise ModelError("Модель вернула повторяющиеся идентификаторы вызовов инструментов.", reason="duplicate_call_id")
        return result
