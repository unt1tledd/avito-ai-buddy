import asyncio
from http import HTTPStatus

from ..core.circuit_breaker import CircuitBreaker
from ..core.config import Settings
from ..core.diagnostics import FailureKind
from .llm import LanguageModel, ModelError, ModelResponse


class ModelUnavailable(ModelError):
    """Operational model failure eligible for deterministic lexical recovery."""


def is_unavailable(error: ModelError) -> bool:
    return error.kind in {FailureKind.TRANSPORT, FailureKind.TIMEOUT} or (
        error.kind == FailureKind.HTTP and error.status_code is not None and (
            error.status_code in {HTTPStatus.REQUEST_TIMEOUT, HTTPStatus.TOO_MANY_REQUESTS, HTTPStatus.PAYMENT_REQUIRED}
            or 500 <= error.status_code < 600
        )
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
            raise ModelUnavailable("Model time budget exhausted.", reason="model_budget_exhausted")
        generation = self.circuit.acquire()
        if generation is None:
            raise ModelUnavailable("Model circuit is open.", reason="circuit_open")
        try:
            async with asyncio.timeout(timeout):
                result = await self.model.respond(instructions=instructions, messages=messages, tools=tools)
        except TimeoutError as error:
            self.circuit.failed(generation)
            raise ModelUnavailable("Model request timed out.", reason="timeout", kind=FailureKind.TIMEOUT) from error
        except ModelError as error:
            if is_unavailable(error):
                self.circuit.failed(generation)
                raise ModelUnavailable(
                    "Model is temporarily unavailable.", reason=error.reason,
                    kind=error.kind, status_code=error.status_code,
                ) from error
            # A refusal or invalid output proves reachability, without authorizing fallback.
            self.circuit.succeeded(generation)
            raise
        except BaseException:
            self.circuit.cancelled(generation)
            raise
        self.circuit.succeeded(generation)
        return result
