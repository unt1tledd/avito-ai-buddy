import asyncio
import json
from collections.abc import Sequence
from dataclasses import dataclass, field
from importlib.resources import files
from typing import Annotated, Literal
from unicodedata import normalize

from pydantic import AfterValidator, BaseModel, ConfigDict, StringConstraints, ValidationError

from ..clients.embeddings import EmbeddingError
from ..clients.llm import FunctionCall, LanguageModel, ModelError, ModelResponse
from ..clients.resilient_llm import ResilientModel
from ..core.config import Settings
from ..rag.history import history_message, prepare_history
from ..rag.proposals import AccessSelection, ProposalValidationError, confirmed_proposal
from ..rag.retriever import Retriever
from ..rag.tools import FIND_INSTRUCTION_DESCRIPTION, FIND_INSTRUCTION_NAME, FindInstructionArguments
from ..repositories.knowledge import KnowledgeError
from ..schemas.citations import SOURCE_REFERENCE
from ..schemas.contracts import (
    Actor, ChatResponse, MAX_RESPONSE_SOURCES, Message, OnboardingPlan,
    SearchHit, SearchResult, valid_utf8,
)
from ..schemas.support import IMMEDIATE_SAFETY_ANSWER, needs_safety_fallback


MAX_TOOL_CALLS = 2
MAX_CONTEXT_BYTES = 256 * 1024
MAX_SOURCE_BYTES = 18 * 1024
MIN_REPAIR_SECONDS = 1
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
CITATION_REPAIR_INSTRUCTION = (
    "В итоговом ответе grounded_answer отсутствуют ссылки. Исправь ответ: опирайся только на уже "
    "переданные материалы и добавь [source:ID] для использованных источников. Верни JSON с kind и answer. "
    "Если сведений недостаточно, выбери no_knowledge; если нужен уточняющий вопрос — clarification. "
    "Сохрани access_request, только если его ресурс и уровень подтверждены переданными access_policies. "
    "Сохрани onboarding_plan_id внутри access_request, если заявка опирается на текущий шаг выбранного трека. "
    "Инструменты недоступны."
)


class FinalAnswer(BaseModel):
    model_config = ConfigDict(extra="forbid", hide_input_in_errors=True)

    kind: Literal["grounded_answer", "clarification", "no_knowledge", "conversation", "onboarding_state"]
    answer: Annotated[
        str, StringConstraints(strip_whitespace=True, min_length=1), AfterValidator(valid_utf8),
    ]
    access_request: AccessSelection | None = None


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
    searched: bool = False
    message: str = ""
    history: Sequence[Message] = ()
    onboarding_plans: Sequence[OnboardingPlan] = ()
    tools_called: bool = False

    def add_sources(self, result: SearchResult) -> None:
        # Keep every source actually sent to the model, independently of the response limit.
        for hit in result.hits:
            self.sources.setdefault(hit.id, hit)

    def response(self, response: ModelResponse) -> ChatResponse:
        if response.refused:
            raise ModelError("Model refused to answer the request.", reason="refusal")
        if not response.text:
            raise ModelError("Model returned an empty answer.", reason="empty_answer")
        try:
            final = FinalAnswer.model_validate_json(response.text)
        except ValidationError as error:
            raise ModelError("Model returned an invalid final answer format.", reason="invalid_answer") from error
        return self._validated_response(final)

    def selected_plan(self, plan_id: str) -> OnboardingPlan:
        matches = [plan for plan in self.onboarding_plans if plan.id == plan_id]
        if len(matches) != 1:
            raise ModelError("Model selected an unavailable or ambiguous plan.", reason="invalid_answer")
        return matches[0]

    def _validated_response(self, final: FinalAnswer) -> ChatResponse:
        if final.access_request is not None and final.kind != "grounded_answer":
            raise ModelError("Access proposals require a grounded answer.", code="invalid_proposal", reason="invalid_answer")
        selected_resource_id = None
        if final.access_request is not None and final.access_request.onboarding_plan_id is not None:
            plan = self.selected_plan(final.access_request.onboarding_plan_id)
            if plan.current_step is None:
                raise ModelError("Selected plan has no current step.", reason="invalid_answer")
            selected_resource_id = plan.current_step.resource_id
        if not self.searched:
            raise ModelError("Model completed an answer without a knowledge search.", reason="search_required")
        # A failed search must not be hidden behind a freely worded conversational reply.
        if not self.sources and self.search_error is not None:
            raise self.search_error
        answer = final.answer
        references = SOURCE_REFERENCE.findall(answer)
        if len(references) != answer.count("[source:") or any(ref not in self.sources for ref in references):
            raise ModelError("Model cited an unavailable source.", reason="invalid_citation")
        if final.kind == "grounded_answer" and not references:
            raise ModelError("Model answer has no supporting citations.", reason="missing_citation")
        sources = [self.sources[ref] for ref in dict.fromkeys(references)]
        proposal = None
        if final.access_request is not None:
            try:
                proposal = confirmed_proposal(
                    final.access_request, sources, message=self.message, history=self.history,
                    selected_resource_id=selected_resource_id, source_ids=set(self.sources),
                )
            except ProposalValidationError as error:
                raise ModelError(str(error), reason="invalid_proposal") from error
        # Policies are internal retrieval metadata; Chat receives its established source fields.
        sources = [source.model_copy(update={"access_policies": []}) for source in sources]
        if len(sources) > MAX_RESPONSE_SOURCES or json_size([
            source.model_dump(mode="json") for source in sources
        ]) > MAX_SOURCE_BYTES:
            raise ModelError("Model exceeded the response source limit.", reason="source_limit")
        return ChatResponse(answer=answer, sources=sources, proposal=proposal)


class Agent:
    def __init__(self, model: LanguageModel, knowledge: Retriever, prompt: str, *, settings: Settings | None = None):
        settings = settings or Settings()
        self.model = ResilientModel(model, settings)
        self.fallback_reserve = min(settings.llm_fallback_reserve_seconds, settings.effective_chat_timeout_seconds / 2)
        self.knowledge = knowledge
        self.prompt = prompt + "\n\nJSON-схема окончательного ответа:\n" + json.dumps(
            FinalAnswer.model_json_schema(), ensure_ascii=False,
        )
        self.planner_prompt = files("ai_service.agents").joinpath("prompts", "planning.txt").read_text(encoding="utf-8")

    async def answer(
        self, message: str, actor: Actor, *, deadline: float | None = None,
        onboarding_plans: Sequence[OnboardingPlan] = (),
        history: Sequence[Message] = (),
    ) -> ChatResponse:
        turn = Turn(message=message, onboarding_plans=onboarding_plans)
        safety_fallback = needs_safety_fallback(message)
        recent_history = await prepare_history(history, actor, self.knowledge)
        turn.history = recent_history
        # The model interprets the original question and history before any embedding/search.
        context = {}
        if onboarding_plans:
            context["onboarding_plans"] = [plan.model_dump(mode="json") for plan in onboarding_plans]
        messages = [
            {"role": "user", "content": "Справочный контекст:\n" + json.dumps(context, ensure_ascii=False)},
            {"role": "user", "content": message},
        ]
        # History has its own byte limit and yields space to the current question and plans.
        while recent_history and json_size([
            self.prompt + self.planner_prompt, [history_message(recent_history), *messages], [FIND_INSTRUCTION_TOOL],
        ]) > MAX_CONTEXT_BYTES:
            recent_history.pop(0)
        if recent_history:
            messages.insert(0, history_message(recent_history))
        seen_call_ids: set[str] = set()
        remaining = MAX_TOOL_CALLS
        while True:
            tools = [FIND_INSTRUCTION_TOOL] if remaining else []
            instructions = self.planner_prompt if not turn.tools_called else self.prompt
            if json_size([instructions, messages, tools]) > MAX_CONTEXT_BYTES:
                raise ModelError("Model context budget exceeded.", reason="context_limit")
            try:
                response = await self.model.respond(
                    instructions=instructions, messages=messages, tools=tools,
                    deadline=deadline - self.fallback_reserve if deadline is not None else None,
                )
            except ModelError:
                if safety_fallback and not turn.tools_called:
                    return ChatResponse(answer=IMMEDIATE_SAFETY_ANSWER, sources=[])
                raise
            calls = response.calls
            if response.refused or not calls:
                if not turn.tools_called:
                    if safety_fallback:
                        return ChatResponse(answer=IMMEDIATE_SAFETY_ANSWER, sources=[])
                    raise ModelError("Planner must request a knowledge search.", reason="search_required")
                try:
                    return turn.response(response)
                except ModelError as error:
                    if error.reason not in {"missing_citation", "invalid_proposal"} or not turn.sources:
                        raise
                    return await self._repair_answer(turn, messages, response, error, deadline)
            if len(calls) != 1:
                raise ModelError("Only one search call per model response is allowed.", reason="unexpected_call")
            if not remaining:
                raise ModelError("Model did not complete the request.", reason="unexpected_call")
            call_ids = {call.call_id for call in calls}
            if len(call_ids) != len(calls) or call_ids & seen_call_ids:
                raise ModelError("Model returned duplicate tool call IDs.", reason="duplicate_call_id")
            seen_call_ids.update(call_ids)
            call = calls[0]
            if call.name != FIND_INSTRUCTION_NAME:
                raise ModelError("Model requested an unavailable tool.", reason="unexpected_call")
            turn.tools_called = True
            messages.extend(item.model_dump(mode="json", exclude_unset=True) for item in response.output)
            remaining -= 1
            next_tools = [FIND_INSTRUCTION_TOOL] if remaining else []
            reserved = tool_output(call, CONTEXT_BUDGET_ERROR)
            if json_size([self.prompt, [*messages, reserved], next_tools]) > MAX_CONTEXT_BYTES:
                raise ModelError("Model context budget exceeded.", reason="context_limit")
            result = await self._call_tool(call, actor, turn)
            candidate = tool_output(call, result)
            if json_size([self.prompt, [*messages, candidate], next_tools]) <= MAX_CONTEXT_BYTES:
                messages.append(candidate)
                if isinstance(result, SearchResult):
                    turn.add_sources(result)
            else:
                messages.append(reserved)

    async def _repair_answer(
        self, turn: Turn, messages: list[dict], response: ModelResponse,
        error: ModelError, deadline: float | None,
    ) -> ChatResponse:
        if deadline is not None and deadline - asyncio.get_running_loop().time() < MIN_REPAIR_SECONDS:
            raise error
        repair_messages = [
            *messages,
            *(item.model_dump(mode="json", exclude_unset=True) for item in response.output),
            {"role": "user", "content": (
                CITATION_REPAIR_INSTRUCTION if error.reason == "missing_citation" else
                "Проверка заявки отклонила выбор: " + str(error) + "\n"
                "Исправь ответ по уже переданным материалам. Если нужен выбор пользователя, задай конкретный вопрос "
                "с kind=clarification и без access_request. Не заменяй явно запрошенный неподдерживаемый или "
                "отвергнутый уровень. Верни финальный JSON; инструменты недоступны."
            )},
        ]
        if json_size([self.prompt, repair_messages, []]) > MAX_CONTEXT_BYTES:
            raise error
        # This single retry shares the caller's deadline and cannot initiate more searches.
        repaired = await self.model.respond(
            instructions=self.prompt, messages=repair_messages, tools=[],
            deadline=deadline - self.fallback_reserve if deadline is not None else None,
        )
        if repaired.calls:
            raise ModelError("Model called a tool during citation repair.", code="repair_tool_call", reason="unexpected_call")
        return turn.response(repaired)

    async def _search(self, query: str, actor: Actor, turn: Turn) -> SearchResult:
        turn.searched = True
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
        try:
            arguments = FindInstructionArguments.model_validate_json(call.arguments)
        except ValidationError as error:
            raise ModelError("Model returned invalid tool arguments.", reason="invalid_tool_args") from error
        try:
            return await self._search(arguments.query, actor, turn)
        except (KnowledgeError, EmbeddingError) as error:
            turn.search_error = error
            return {"error": "Поиск временно недоступен."}
