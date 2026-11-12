from pydantic import Field

from ..schemas.contracts import ChatActor, CurrentStep, Identifier, Message, MessageText, Payload


class ChatRequest(Payload):
    conversation_id: Identifier | None = Field(
        default=None, description="Optional chat ID for correlation. AI does not store conversations."
    )
    actor: ChatActor = Field(
        description="Current user ID, role and team verified by Chat. AI uses this profile for document ACL "
        "and does not call User. Forward actual_end_date when the internship has ended.",
    )
    message: MessageText
    current_step: CurrentStep | None = Field(
        default=None,
        description="Optional current onboarding step snapshot supplied by Chat. Used to understand the question "
        "and refine knowledge search; does not grant document access or confirm step completion.",
    )
    history: list[Message] = Field(
        default_factory=list,
        description="Previous messages in chronological order, oldest first, excluding the current question. "
        "AI considers the last 30 messages, verifies assistant source access and keeps at most 128 KiB of history. "
        "Older messages are dropped whole. The complete request remains limited to 1 MiB.",
    )
