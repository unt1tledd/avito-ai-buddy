from pydantic import Field

from ..schemas.contracts import Actor, ActorRef, Identifier, Message, MessageText, Payload


MAX_CHAT_HISTORY_MESSAGES = 12


class ChatRequest(Payload):
    conversation_id: Identifier | None = Field(
        default=None, description="Stable chat ID. Required in integration mode."
    )
    actor: Actor | ActorRef
    message: MessageText
    history: list[Message] = Field(
        default_factory=list,
        max_length=MAX_CHAT_HISTORY_MESSAGES,
        description="Accepted for compatibility. Each answer uses the current message and current search results.",
    )
    confirmed_step_id: Identifier | None = None
