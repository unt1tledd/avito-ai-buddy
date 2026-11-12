from pydantic import Field

from ..schemas.contracts import ChatActor, Identifier, Message, MessageText, Payload


MAX_CHAT_HISTORY_MESSAGES = 12


class ChatRequest(Payload):
    conversation_id: Identifier | None = Field(
        default=None, description="Optional chat ID for correlation. AI does not store conversations."
    )
    actor: ChatActor = Field(
        description="Current user ID, role and team verified by Chat. AI uses this profile for document ACL "
        "and does not call User. Forward actual_end_date when the internship has ended.",
    )
    message: MessageText
    history: list[Message] = Field(
        default_factory=list,
        max_length=MAX_CHAT_HISTORY_MESSAGES,
        description="Accepted for compatibility. Each answer uses the current message and current search results.",
    )
