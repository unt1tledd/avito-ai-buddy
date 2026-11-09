from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, HttpUrl, StringConstraints


Identifier = Annotated[str, StringConstraints(pattern=r"^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$")]
MessageText = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=4000)]


class Payload(BaseModel):
    model_config = ConfigDict(extra="forbid")


class Actor(Payload):
    user_id: Identifier
    team: Identifier
    role: Identifier


class Message(Payload):
    role: Literal["user", "assistant"]
    content: MessageText


class ChatRequest(Payload):
    actor: Actor
    message: MessageText
    history: list[Message] = Field(default_factory=list, max_length=12)
    confirmed_step_id: Identifier | None = None


class Source(Payload):
    id: Identifier
    title: str
    url: HttpUrl | None = None


class SearchHit(Source):
    text: str


class SearchResult(Payload):
    hits: list[SearchHit]


class ActionResult(Payload):
    step_id: Identifier
    status: Literal["completed"]


class ChatResponse(Payload):
    answer: str
    sources: list[SearchHit]
    action: ActionResult | None = None
