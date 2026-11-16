import json

from pydantic import Field, field_validator

from ..schemas.contracts import (
    ChatActor,
    Identifier,
    Message,
    MessageText,
    OnboardingPlan,
    Payload,
)

MAX_ONBOARDING_PLANS = 128
MAX_ONBOARDING_CONTEXT_BYTES = 128 * 1024


class ChatRequest(Payload):
    conversation_id: Identifier | None = Field(
        default=None, description="Optional chat ID for correlation. AI does not store conversations."
    )
    actor: ChatActor = Field(
        description="Current user ID, role and team verified by Chat. AI uses this profile for document ACL "
        "and does not call User. Forward actual_end_date when the internship has ended.",
    )
    message: MessageText
    onboarding_plans: list[OnboardingPlan] = Field(
        default_factory=list, max_length=MAX_ONBOARDING_PLANS,
        description="Optional onboarding tracks supplied by Chat, each with its current step or null. "
        "Step action_type is context only, not an instruction to execute an action. "
        "The planner formulates a context-aware search query for every message before final generation. "
        "The final model can clarify ambiguous step references after retrieval. "
        "Onboarding context is accepted only through "
        "this field and is limited to 128 KiB of JSON context, including string escaping for the model request.",
    )
    history: list[Message] = Field(
        default_factory=list,
        description="Previous messages in chronological order, oldest first, excluding the current question. "
        "AI considers the last 30 messages, verifies assistant source access and keeps at most 128 KiB of history. "
        "Older messages are dropped whole. The complete request remains limited to 1 MiB.",
    )

    @field_validator("onboarding_plans")
    @classmethod
    def bounded_onboarding_context(cls, plans: list[OnboardingPlan]) -> list[OnboardingPlan]:
        context = json.dumps({"onboarding_plans": [plan.model_dump(mode="json") for plan in plans]}, ensure_ascii=False)
        # Context is embedded in a JSON message string: count its escaping too.
        size = len(json.dumps(context, ensure_ascii=False).encode())
        if size > MAX_ONBOARDING_CONTEXT_BYTES:
            raise ValueError("Контекст onboarding_plans превышает 128 КиБ. Сократите число или объём треков.")
        return plans
