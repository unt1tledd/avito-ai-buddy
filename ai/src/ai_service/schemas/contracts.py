from datetime import date
from typing import Annotated, Literal

from pydantic import AfterValidator, BaseModel, ConfigDict, Field, HttpUrl, StringConstraints, field_validator


MAX_CHAT_MESSAGE_LENGTH = 4000
MAX_DOCUMENT_TEXT = 4000
MAX_CHUNK_TEXT = 1200
MAX_SOURCE_TEXT = MAX_CHUNK_TEXT
MAX_RESPONSE_SOURCES = 6

Identifier = Annotated[str, StringConstraints(pattern=r"^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$")]


def valid_utf8(value: str) -> str:
    try:
        value.encode()
    except UnicodeEncodeError as error:
        raise ValueError("Expected valid Unicode text.") from error
    return value


MessageText = Annotated[
    str, StringConstraints(strip_whitespace=True, min_length=1, max_length=MAX_CHAT_MESSAGE_LENGTH),
    AfterValidator(valid_utf8),
]


class Payload(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ActorRef(Payload):
    user_id: Identifier


class Actor(ActorRef):
    team: Identifier
    role: Identifier


class ChatActor(Actor):
    """Current user context supplied by the authenticated Chat service."""

    actual_end_date: date | None = None


class CurrentStep(Payload):
    """Read-only onboarding step snapshot supplied by authenticated Chat."""

    id: Identifier
    title: Annotated[
        str, StringConstraints(strip_whitespace=True, min_length=1, max_length=256), AfterValidator(valid_utf8),
    ]
    resource_id: Identifier | None = None

    @field_validator("resource_id", mode="before", json_schema_input_type=Identifier | Literal[""] | None)
    @classmethod
    def empty_resource_is_absent(cls, value: object) -> object:
        # Chat's Go string has an empty zero value for steps without a resource.
        return None if value == "" else value


class Message(Payload):
    role: Literal["user", "assistant"]
    content: Annotated[
        str, StringConstraints(strip_whitespace=True, min_length=1), AfterValidator(valid_utf8),
    ]
    source_ids: list[Identifier] = Field(
        default_factory=list, max_length=MAX_RESPONSE_SOURCES,
        description="Source IDs stored with a past assistant reply. Replies without provenance are excluded "
        "from model context; every listed source must still be accessible to the current actor.",
    )


class Source(Payload):
    id: Identifier
    title: str = Field(min_length=1, max_length=160)
    url: HttpUrl | None = None


class SearchHit(Source):
    text: str = Field(min_length=1, max_length=MAX_SOURCE_TEXT)


class Document(Source):
    text: str = Field(min_length=1, max_length=MAX_DOCUMENT_TEXT)
    roles: set[Identifier] = Field(min_length=1, max_length=16)
    teams: set[Identifier] = Field(default_factory=set, max_length=32)
    keywords: list[Annotated[str, StringConstraints(min_length=1, max_length=80)]] = Field(
        default_factory=list, max_length=16
    )

    def embedding_text(self) -> str:
        return "\n".join([self.title, self.text, *self.keywords])


class DocumentChunk(Document):
    """The complete text indexed by one embedding and returned by search."""

    text: str = Field(min_length=1, max_length=MAX_CHUNK_TEXT)


class SearchResult(Payload):
    hits: list[SearchHit]


class ChatResponse(Payload):
    answer: str
    sources: list[SearchHit] = Field(
        max_length=MAX_RESPONSE_SOURCES,
        description="Only sources cited in the answer, in order of first citation, without duplicates.",
    )
