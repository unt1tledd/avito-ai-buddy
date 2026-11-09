import json
from dataclasses import dataclass, field

from mcp import Client, MCPError
from mcp.types import TextContent
from pydantic import ValidationError

from .contracts import ActionResult, Actor, ChatRequest, ChatResponse, SearchHit, SearchResult
from .llm import LanguageModel, ModelError
from .mcp_server import build_server
from .onboarding_client import Onboarding
from .rag import Knowledge


@dataclass
class Turn:
    sources: dict[str, SearchHit] = field(default_factory=dict)
    action: ActionResult | None = None

    def response(self, answer: str) -> ChatResponse:
        return ChatResponse(answer=answer, sources=list(self.sources.values()), action=self.action)


class Agent:
    def __init__(self, model: LanguageModel, knowledge: Knowledge, onboarding: Onboarding, prompt: str):
        self.model = model
        self.knowledge = knowledge
        self.onboarding = onboarding
        self.prompt = prompt

    async def answer(self, request: ChatRequest, actor: Actor) -> ChatResponse:
        retrieved = await self.knowledge.search(request.message, actor)
        turn = Turn(sources={hit.id: hit for hit in retrieved.hits})
        context = {
            "profile": {"team": actor.team, "role": actor.role},
            "sources": retrieved.model_dump(mode="json")["hits"],
        }
        messages = [message.model_dump() for message in request.history]
        messages.extend([
            {"role": "user", "content": "Справочный контекст:\n" + json.dumps(context, ensure_ascii=False)},
            {"role": "user", "content": request.message},
        ])
        instructions = self.prompt
        if request.confirmed_step_id:
            instructions += f"\nПользователь подтвердил шаг с ID {request.confirmed_step_id}."
        server = build_server(self.knowledge, self.onboarding, actor, request.confirmed_step_id)

        failure: ModelError | MCPError
        try:
            async with Client(server) as client:
                # Resolve application errors before closing MCP's task group.
                try:
                    return await self._run(client, instructions, messages, turn)
                except (ModelError, MCPError) as error:
                    failure = error
        except MCPError as error:
            failure = error

        if turn.action is not None:
            return turn.response("Шаг адаптации отмечен как выполненный.")
        if isinstance(failure, MCPError):
            raise ModelError("Инструменты AI-buddy временно недоступны.") from failure
        raise failure

    async def _run(self, client: Client, instructions: str, messages: list[dict], turn: Turn) -> ChatResponse:
        tools = [
            {
                "type": "function",
                "name": tool.name,
                "description": tool.description or "",
                "parameters": tool.input_schema,
                "strict": False,
            }
            for tool in (await client.list_tools()).tools
        ]
        allowed_tools = {tool["name"] for tool in tools}
        for _ in range(4):
            response = await self.model.respond(instructions=instructions, messages=messages, tools=tools)
            calls = [item for item in response.output if item.get("type") == "function_call"]
            if not calls:
                if response.text:
                    return turn.response(response.text)
                raise ModelError("Модель не вернула ответ.")
            messages.extend(response.output)
            for call in calls:
                call_id = call.get("call_id")
                if not isinstance(call_id, str) or not call_id:
                    raise ModelError("Модель вернула вызов инструмента без идентификатора.")
                output = await self._call_tool(client, call, allowed_tools, turn)
                messages.append({
                    "type": "function_call_output",
                    "call_id": call_id,
                    "output": json.dumps(output, ensure_ascii=False),
                })
        raise ModelError("Модель не завершила обработку запроса.")

    @staticmethod
    async def _call_tool(client: Client, call: dict, allowed_tools: set[str], turn: Turn) -> dict:
        name = call.get("name")
        if not isinstance(name, str) or name not in allowed_tools:
            return {"error": "Инструмент недоступен для этого запроса."}
        try:
            arguments = json.loads(call.get("arguments", ""))
            if not isinstance(arguments, dict):
                return {"error": "Ожидался объект с аргументами инструмента."}
        except (TypeError, ValueError):
            return {"error": "Некорректные аргументы инструмента."}

        result = await client.call_tool(name, arguments)
        if result.is_error:
            error = next((block.text for block in result.content if isinstance(block, TextContent)), "")
            return {"error": error or "Инструмент не смог выполнить запрос."}

        payload = result.structured_content
        try:
            if name == "find_instruction":
                for hit in SearchResult.model_validate(payload).hits:
                    turn.sources[hit.id] = hit
            else:
                turn.action = ActionResult.model_validate(payload)
        except ValidationError:
            return {"error": "Инструмент вернул результат в неизвестном формате."}
        return payload
