import asyncio

from ..core.circuit_breaker import CircuitBreaker
from ..core.config import Settings
from .llm import LanguageModel, ModelError, ModelResponse


class ModelUnavailable(ModelError):
    """Operational model failure eligible for deterministic lexical recovery."""


def is_unavailable(error: ModelError) -> bool:
    return error.reason in {"transport", "timeout", "http_408", "http_429", "http_402"} or (
        error.reason.startswith("http_") and error.reason[5:].isdigit()
        and 500 <= int(error.reason[5:]) < 600
    )


class ResilientModel:
    def __init__(self, model: LanguageModel, settings: Settings):
        self.model = model
        self.timeout = settings.llm_timeout_seconds
        self.circuit = CircuitBreaker(settings.llm_circuit_failure_threshold, settings.llm_circuit_reset_seconds)

    async def respond(
        self, *, instructions: str, messages: list[dict], tools: list[dict], deadline: float | None = None,
    ) -> ModelResponse:
        timeout = self.timeout
        if deadline is not None:
            timeout = min(timeout, deadline - asyncio.get_running_loop().time())
        if timeout <= 0:
            raise ModelUnavailable("Исчерпано время ожидания модели.", reason="model_budget_exhausted")
        generation = self.circuit.acquire()
        if generation is None:
            raise ModelUnavailable("Модель временно недоступна.", reason="circuit_open")
        try:
            async with asyncio.timeout(timeout):
                result = await self.model.respond(instructions=instructions, messages=messages, tools=tools)
        except TimeoutError as error:
            self.circuit.failed(generation)
            raise ModelUnavailable("Истекло время ожидания модели.", reason="timeout") from error
        except ModelError as error:
            if is_unavailable(error):
                self.circuit.failed(generation)
                raise ModelUnavailable("Модель временно недоступна.", reason=error.reason) from error
            # A refusal or invalid output proves reachability, without authorizing fallback.
            self.circuit.succeeded(generation)
            raise
        except BaseException:
            self.circuit.cancelled(generation)
            raise
        self.circuit.succeeded(generation)
        return result
