from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, HttpUrl, StringConstraints


MAX_CHAT_MESSAGE_LENGTH = 4000

Identifier = Annotated[str, StringConstraints(pattern=r"^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$")]
MessageText = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=MAX_CHAT_MESSAGE_LENGTH)]


class Payload(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ActorRef(Payload):
    user_id: Identifier


class Actor(ActorRef):
    team: Identifier
    role: Identifier


class Message(Payload):
    role: Literal["user", "assistant"]
    content: MessageText


class Source(Payload):
    id: Identifier
    title: str
    url: HttpUrl | None = None


class SearchHit(Source):
    text: str


class Document(SearchHit):
    roles: set[Identifier] = Field(min_length=1)
    teams: set[Identifier] = Field(default_factory=set)
    keywords: list[str] = Field(default_factory=list)

    def embedding_text(self) -> str:
        return "\n".join([self.title, self.text, *self.keywords])


class SearchResult(Payload):
    hits: list[SearchHit]


class ActionResult(Payload):
    step_id: Identifier
    status: Literal["completed"]


class ChatResponse(Payload):
    answer: str
    sources: list[SearchHit]
    action: ActionResult | None = None
