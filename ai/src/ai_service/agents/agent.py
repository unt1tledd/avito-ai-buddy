import json
from dataclasses import dataclass

from pydantic import ValidationError

from ..clients.llm import FunctionCall, LanguageModel, ModelError
from ..rag.retriever import Retriever
from ..rag.tools import FIND_INSTRUCTION_DESCRIPTION, FIND_INSTRUCTION_NAME, FindInstructionArguments
from ..repositories.knowledge import KnowledgeError
from ..schemas.contracts import Actor, ChatResponse, SearchHit


MAX_TOOL_ROUNDS = 3
FIND_INSTRUCTION_TOOL = {
    "type": "function",
    "name": FIND_INSTRUCTION_NAME,
    "description": FIND_INSTRUCTION_DESCRIPTION,
    "parameters": FindInstructionArguments.model_json_schema(),
    "strict": False,
}


@dataclass
class Turn:
    sources: dict[str, SearchHit]

    def response(self, answer: str) -> ChatResponse:
        if not answer:
            raise ModelError("Модель не вернула ответ.")
        return ChatResponse(answer=answer, sources=list(self.sources.values()))


class Agent:
    def __init__(self, model: LanguageModel, knowledge: Retriever, prompt: str):
        self.model = model
        self.knowledge = knowledge
        self.prompt = prompt

    async def answer(self, message: str, actor: Actor) -> ChatResponse:
        retrieved = await self.knowledge.search(message, actor)
        turn = Turn(sources={hit.id: hit for hit in retrieved.hits})
        context = {
            "profile": {"team": actor.team, "role": actor.role},
            "sources": retrieved.model_dump(mode="json")["hits"],
        }
        messages = [
            {"role": "user", "content": "Справочный контекст:\n" + json.dumps(context, ensure_ascii=False)},
            {"role": "user", "content": message},
        ]
        for _ in range(MAX_TOOL_ROUNDS):
            response = await self.model.respond(
                instructions=self.prompt, messages=messages, tools=[FIND_INSTRUCTION_TOOL]
            )
            calls = response.calls
            if not calls:
                return turn.response(response.text)
            messages.extend(item.model_dump(mode="json", exclude_unset=True) for item in response.output)
            for call in calls:
                output = await self._call_tool(call, actor, turn)
                messages.append({
                    "type": "function_call_output",
                    "call_id": call.call_id,
                    "output": json.dumps(output, ensure_ascii=False),
                })
        response = await self.model.respond(instructions=self.prompt, messages=messages, tools=[])
        if response.calls:
            raise ModelError("Модель не завершила обработку запроса.")
        return turn.response(response.text)

    async def _call_tool(self, call: FunctionCall, actor: Actor, turn: Turn) -> dict:
        if call.name != FIND_INSTRUCTION_NAME:
            return {"error": "Инструмент недоступен для этого запроса."}
        try:
            arguments = FindInstructionArguments.model_validate_json(call.arguments)
        except ValidationError as error:
            raise ModelError("Модель вернула некорректные аргументы инструмента.") from error

        try:
            result = await self.knowledge.search(arguments.query, actor)
        except KnowledgeError as error:
            return {"error": str(error)}

        turn.sources.update((hit.id, hit) for hit in result.hits)
        return result.model_dump(mode="json")
