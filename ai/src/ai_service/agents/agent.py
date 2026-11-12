import asyncio
import json
from collections.abc import Sequence
from dataclasses import dataclass, field
from typing import Annotated, Literal
from unicodedata import normalize

from pydantic import AfterValidator, BaseModel, ConfigDict, StringConstraints, ValidationError

from ..clients.embeddings import EmbeddingError
from ..clients.llm import FunctionCall, LanguageModel, ModelError, ModelResponse
from ..rag.history import history_message, prepare_history
from ..rag.retriever import Retriever
from ..rag.tools import FIND_INSTRUCTION_DESCRIPTION, FIND_INSTRUCTION_NAME, FindInstructionArguments
from ..repositories.knowledge import KnowledgeError
from ..schemas.citations import SOURCE_REFERENCE
from ..schemas.clarifications import CLARIFICATION_QUESTIONS, safe_clarification
from ..schemas.contracts import Actor, ChatResponse, CurrentStep, MAX_RESPONSE_SOURCES, Message, SearchHit, SearchResult, valid_utf8


MAX_TOOL_CALLS = 3
MAX_CONTEXT_BYTES = 256 * 1024
MAX_SOURCE_BYTES = 18 * 1024
MIN_CITATION_REPAIR_SECONDS = 1
NO_KNOWLEDGE = (
    "В доступных материалах недостаточно сведений, чтобы ответить на вопрос. "
    "Рекомендую обратиться с этим вопросом к вашему наставнику."
)
CONTEXT_LIMIT_ANSWER = "Найденные материалы слишком объёмные. Уточните вопрос, чтобы сузить поиск."
FIND_INSTRUCTION_TOOL = {
    "type": "function",
    "name": FIND_INSTRUCTION_NAME,
    "description": FIND_INSTRUCTION_DESCRIPTION,
    "parameters": FindInstructionArguments.model_json_schema(),
    "strict": False,
}
CONTEXT_BUDGET_ERROR = {
    "error": "context_budget_exhausted",
    "message": "Результаты поиска не помещаются в контекст. Используй уже переданные источники или уточни вопрос.",
}
TOOL_LIMIT_ERROR = {"error": "Исчерпан лимит вызовов инструментов."}
CITATION_REPAIR_INSTRUCTION = (
    "В итоговом ответе grounded_answer отсутствуют ссылки. Исправь ответ: опирайся только на уже "
    "переданные материалы и добавь [source:ID] для использованных источников. Верни JSON с kind и answer. "
    "Если сведений недостаточно, выбери no_knowledge; если нужен уточняющий вопрос — clarification. "
    "Для clarification выбери answer дословно из разрешённых вопросов в системной инструкции. "
    "Инструменты недоступны."
)


class FinalAnswer(BaseModel):
    model_config = ConfigDict(extra="forbid", hide_input_in_errors=True)

    kind: Literal["grounded_answer", "clarification", "no_knowledge"]
    answer: Annotated[
        str, StringConstraints(strip_whitespace=True, min_length=1), AfterValidator(valid_utf8),
    ]


def json_size(value: object) -> int:
    return len(json.dumps(value, ensure_ascii=False).encode())


def tool_output(call: FunctionCall, result: SearchResult | dict) -> dict:
    return {
        "type": "function_call_output",
        "call_id": call.call_id,
        "output": json.dumps(
            result.model_dump(mode="json") if isinstance(result, SearchResult) else result,
            ensure_ascii=False,
        ),
    }


@dataclass
class Turn:
    sources: dict[str, SearchHit] = field(default_factory=dict)
    results: dict[str, SearchResult | EmbeddingError | KnowledgeError] = field(default_factory=dict)
    search_error: EmbeddingError | KnowledgeError | None = None
    context_limited: bool = False

    def add_sources(self, result: SearchResult) -> None:
        # Keep every source actually sent to the model, independently of the response limit.
        for hit in result.hits:
            self.sources.setdefault(hit.id, hit)

    def response(self, response: ModelResponse) -> ChatResponse:
        if response.refused:
            raise ModelError("Модель отказалась отвечать на запрос.", reason="refusal")
        if not response.text:
            raise ModelError("Модель не вернула ответ.", reason="empty_answer")
        try:
            final = FinalAnswer.model_validate_json(response.text)
        except ValidationError as error:
            raise ModelError("Модель вернула некорректный формат итогового ответа.", reason="invalid_answer") from error
        if not self.sources:
            if self.search_error is not None:
                raise self.search_error
            if self.context_limited:
                return ChatResponse(answer=CONTEXT_LIMIT_ANSWER, sources=[])
            if final.kind != "clarification":
                return ChatResponse(answer=NO_KNOWLEDGE, sources=[])
        if final.kind == "no_knowledge":
            return ChatResponse(answer=NO_KNOWLEDGE, sources=[])
        if final.kind == "clarification":
            return ChatResponse(answer=safe_clarification(final.answer), sources=[])
        answer = final.answer
        references = SOURCE_REFERENCE.findall(answer)
        if len(references) != answer.count("[source:") or any(ref not in self.sources for ref in references):
            raise ModelError("Модель сослалась на недоступный источник.", reason="invalid_citation")
        if not references:
            raise ModelError("Ответ модели не содержит подтверждающих источников.", reason="missing_citation")
        sources = [self.sources[ref] for ref in dict.fromkeys(references)]
        if len(sources) > MAX_RESPONSE_SOURCES or json_size([
            source.model_dump(mode="json") for source in sources
        ]) > MAX_SOURCE_BYTES:
            raise ModelError("Модель превысила лимит источников ответа.", reason="source_limit")
        return ChatResponse(answer=answer, sources=sources)


class Agent:
    def __init__(self, model: LanguageModel, knowledge: Retriever, prompt: str):
        self.model = model
        self.knowledge = knowledge
        self.prompt = (
            prompt + "\n\nРазрешённые вопросы для kind=clarification (выбери answer дословно):\n"
            + json.dumps(CLARIFICATION_QUESTIONS, ensure_ascii=False)
        )

    async def answer(
        self, message: str, actor: Actor, *, deadline: float | None = None, current_step: CurrentStep | None = None,
        history: Sequence[Message] = (),
    ) -> ChatResponse:
        turn = Turn()
        recent_history = await prepare_history(history, actor, self.knowledge)
        query = message
        if current_step is not None:
            query = f"Вопрос: {message}\nТекущий шаг: {current_step.title}"
            if current_step.resource_id is not None:
                query += f"\nРесурс шага: {current_step.resource_id}"
        retrieved = await self._search(query, actor, turn)
        context = retrieved.model_dump(mode="json")
        if current_step is not None:
            context["current_step"] = current_step.model_dump(mode="json")
        messages = [
            {"role": "user", "content": "Справочный контекст:\n" + json.dumps(context, ensure_ascii=False)},
            {"role": "user", "content": message},
        ]
        # History has its own byte limit and yields space to the current question and fresh retrieval.
        while recent_history and json_size([
            self.prompt, [history_message(recent_history), *messages], [FIND_INSTRUCTION_TOOL],
        ]) > MAX_CONTEXT_BYTES:
            recent_history.pop(0)
        if recent_history:
            messages.insert(0, history_message(recent_history))
        turn.add_sources(retrieved)
        seen_call_ids: set[str] = set()
        remaining = MAX_TOOL_CALLS
        while True:
            tools = [FIND_INSTRUCTION_TOOL] if remaining else []
            if json_size([self.prompt, messages, tools]) > MAX_CONTEXT_BYTES:
                raise ModelError("Превышен бюджет контекста модели.", reason="context_limit")
            response = await self.model.respond(instructions=self.prompt, messages=messages, tools=tools)
            calls = response.calls
            if response.refused or not calls:
                try:
                    return turn.response(response)
                except ModelError as error:
                    if error.reason != "missing_citation":
                        raise
                    return await self._repair_citations(turn, messages, response, error, deadline)
            if not remaining:
                raise ModelError("Модель не завершила обработку запроса.", reason="unexpected_call")
            call_ids = {call.call_id for call in calls}
            if len(call_ids) != len(calls) or call_ids & seen_call_ids:
                raise ModelError("Модель вернула повторяющиеся идентификаторы вызовов инструментов.", reason="duplicate_call_id")
            seen_call_ids.update(call_ids)
            items = [item.model_dump(mode="json", exclude_unset=True) for item in response.output]
            messages.extend(items)
            next_tools = [FIND_INSTRUCTION_TOOL] if remaining > len(calls) else []
            # Reserve a result for every call, including calls beyond the tool budget.
            outputs = [
                tool_output(call, CONTEXT_BUDGET_ERROR if index < remaining else TOOL_LIMIT_ERROR)
                for index, call in enumerate(calls)
            ]
            if json_size([self.prompt, messages + outputs, next_tools]) > MAX_CONTEXT_BYTES:
                raise ModelError("Превышен бюджет контекста модели.", reason="context_limit")
            for index, call in enumerate(calls):
                if remaining:
                    remaining -= 1
                    output = await self._call_tool(call, actor, turn)
                    candidate = tool_output(call, output)
                    pending = outputs[:index] + [candidate] + outputs[index + 1:]
                    if json_size([self.prompt, messages + pending, next_tools]) <= MAX_CONTEXT_BYTES:
                        outputs[index] = candidate
                        if isinstance(output, SearchResult):
                            turn.add_sources(output)
                    else:
                        turn.context_limited = True
            messages.extend(outputs)

    async def _repair_citations(
        self, turn: Turn, messages: list[dict], response: ModelResponse,
        error: ModelError, deadline: float | None,
    ) -> ChatResponse:
        if deadline is not None and deadline - asyncio.get_running_loop().time() < MIN_CITATION_REPAIR_SECONDS:
            raise error
        repair_messages = [
            *messages,
            *(item.model_dump(mode="json", exclude_unset=True) for item in response.output),
            {"role": "user", "content": CITATION_REPAIR_INSTRUCTION},
        ]
        if json_size([self.prompt, repair_messages, []]) > MAX_CONTEXT_BYTES:
            raise error
        # This single retry shares the caller's deadline and cannot initiate more searches.
        repaired = await self.model.respond(instructions=self.prompt, messages=repair_messages, tools=[])
        if repaired.calls:
            raise ModelError("Модель вызвала инструмент при исправлении ответа.", reason="unexpected_call")
        return turn.response(repaired)

    async def _search(self, query: str, actor: Actor, turn: Turn) -> SearchResult:
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
        return SearchResult(hits=[turn.sources.get(hit.id, hit) for hit in result.hits])

    async def _call_tool(self, call: FunctionCall, actor: Actor, turn: Turn) -> SearchResult | dict:
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
