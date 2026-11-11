import json
import re
from dataclasses import dataclass, field
from unicodedata import normalize

from pydantic import ValidationError

from ..clients.embeddings import EmbeddingError
from ..clients.llm import FunctionCall, LanguageModel, ModelError, ModelResponse
from ..rag.retriever import Retriever
from ..rag.tools import FIND_INSTRUCTION_DESCRIPTION, FIND_INSTRUCTION_NAME, FindInstructionArguments
from ..repositories.knowledge import KnowledgeError
from ..schemas.contracts import Actor, ChatResponse, MAX_RESPONSE_SOURCES, SearchHit, SearchResult


MAX_TOOL_CALLS = 3
MAX_CONTEXT_BYTES = 64 * 1024
MAX_SOURCE_BYTES = 18 * 1024
NO_KNOWLEDGE = "В доступных материалах недостаточно сведений, чтобы ответить на вопрос."
SOURCE_REFERENCE = re.compile(r"\[source:([^\]\r\n]*)\]")
FIND_INSTRUCTION_TOOL = {
    "type": "function",
    "name": FIND_INSTRUCTION_NAME,
    "description": FIND_INSTRUCTION_DESCRIPTION,
    "parameters": FindInstructionArguments.model_json_schema(),
    "strict": False,
}


def json_size(value: object) -> int:
    return len(json.dumps(value, ensure_ascii=False).encode())


@dataclass
class Turn:
    sources: dict[str, SearchHit] = field(default_factory=dict)
    results: dict[str, SearchResult | EmbeddingError | KnowledgeError] = field(default_factory=dict)
    search_error: EmbeddingError | KnowledgeError | None = None
    source_bytes: int = 0

    def add_sources(self, result: SearchResult) -> dict:
        hits = []
        for hit in result.hits:
            if hit.id not in self.sources:
                size = json_size(hit.model_dump(mode="json")) + 2
                if len(self.sources) >= MAX_RESPONSE_SOURCES or self.source_bytes + size > MAX_SOURCE_BYTES:
                    continue
                self.sources[hit.id] = hit
                self.source_bytes += size
            hits.append(self.sources[hit.id].model_dump(mode="json"))
        return {"hits": hits}

    def response(self, response: ModelResponse) -> ChatResponse:
        if response.refused:
            raise ModelError("Модель отказалась отвечать на запрос.", reason="refusal")
        answer = response.text
        if not answer:
            raise ModelError("Модель не вернула ответ.", reason="empty_answer")
        if not self.sources:
            if self.search_error is not None:
                raise self.search_error
            return ChatResponse(answer=NO_KNOWLEDGE, sources=[])
        references = SOURCE_REFERENCE.findall(answer)
        if len(references) != answer.count("[source:") or any(ref not in self.sources for ref in references):
            raise ModelError("Модель сослалась на недоступный источник.", reason="invalid_citation")
        return ChatResponse(answer=answer, sources=list(self.sources.values()))


class Agent:
    def __init__(self, model: LanguageModel, knowledge: Retriever, prompt: str):
        self.model = model
        self.knowledge = knowledge
        self.prompt = prompt

    async def answer(self, message: str, actor: Actor) -> ChatResponse:
        turn = Turn()
        retrieved = await self._search(message, actor, turn)
        messages = [
            {"role": "user", "content": "Справочный контекст:\n" + json.dumps(retrieved, ensure_ascii=False)},
            {"role": "user", "content": message},
        ]
        seen_call_ids: set[str] = set()
        remaining = MAX_TOOL_CALLS
        while True:
            tools = [FIND_INSTRUCTION_TOOL] if remaining else []
            if json_size([self.prompt, messages, tools]) > MAX_CONTEXT_BYTES:
                raise ModelError("Превышен бюджет контекста модели.", reason="context_limit")
            response = await self.model.respond(instructions=self.prompt, messages=messages, tools=tools)
            calls = response.calls
            if response.refused or not calls:
                return turn.response(response)
            if not remaining:
                raise ModelError("Модель не завершила обработку запроса.", reason="unexpected_call")
            call_ids = {call.call_id for call in calls}
            if len(call_ids) != len(calls) or call_ids & seen_call_ids:
                raise ModelError("Модель вернула повторяющиеся идентификаторы вызовов инструментов.", reason="duplicate_call_id")
            seen_call_ids.update(call_ids)
            items = [item.model_dump(mode="json", exclude_unset=True) for item in response.output]
            if json_size([messages, items]) > MAX_CONTEXT_BYTES:
                raise ModelError("Превышен бюджет контекста модели.", reason="context_limit")
            messages.extend(items)
            for call in calls:
                if remaining:
                    remaining -= 1
                    output = await self._call_tool(call, actor, turn)
                else:
                    output = {"error": "Исчерпан лимит вызовов инструментов."}
                messages.append({
                    "type": "function_call_output",
                    "call_id": call.call_id,
                    "output": json.dumps(output, ensure_ascii=False),
                })

    async def _search(self, query: str, actor: Actor, turn: Turn) -> dict:
        normalized = " ".join(normalize("NFC", query).split())
        key = normalized.casefold()
        if key not in turn.results:
            try:
                turn.results[key] = await self.knowledge.search(normalized, actor)
            except (EmbeddingError, KnowledgeError) as error:
                turn.results[key] = error
        result = turn.results[key]
        if isinstance(result, (EmbeddingError, KnowledgeError)):
            raise result
        return turn.add_sources(result)

    async def _call_tool(self, call: FunctionCall, actor: Actor, turn: Turn) -> dict:
        if call.name != FIND_INSTRUCTION_NAME:
            return {"error": "Инструмент недоступен для этого запроса."}
        try:
            arguments = FindInstructionArguments.model_validate_json(call.arguments)
        except ValidationError as error:
            raise ModelError("Модель вернула некорректные аргументы инструмента.", reason="invalid_tool_args") from error
        try:
            return await self._search(arguments.query, actor, turn)
        except (KnowledgeError, EmbeddingError) as error:
            turn.search_error = error
            return {"error": "Поиск временно недоступен."}
