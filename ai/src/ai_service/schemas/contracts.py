from datetime import date
from typing import Annotated, Literal

from pydantic import (
    AfterValidator,
    BaseModel,
    ConfigDict,
    Field,
    HttpUrl,
    StringConstraints,
    field_validator,
    model_serializer,
    model_validator,
)

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


def valid_proposal_text(value: str) -> str:
    valid_utf8(value)
    if any(ord(character) < 32 or ord(character) == 127 for character in value):
        raise ValueError("Control characters are not allowed.")
    return value


ProposalText = Annotated[
    str, StringConstraints(strip_whitespace=True, min_length=1, max_length=1024), AfterValidator(valid_proposal_text),
]
AccessLevel = Annotated[
    str, StringConstraints(strip_whitespace=True, min_length=1, max_length=64), AfterValidator(valid_proposal_text),
]


class AccessProposal(Payload):
    """AI-to-Chat draft; Chat owns its ID, confirmation and dispatch state."""

    resource_id: ProposalText
    resource_label: ProposalText
    access_level: AccessLevel


class AccessPolicy(Payload):
    """Publisher-supplied request policy, not a permission inferred by the model."""

    resource_id: ProposalText | None = None
    resource_label: ProposalText
    repository_host: Annotated[str, StringConstraints(pattern=r"^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$")] | None = None
    access_levels: list[AccessLevel] = Field(min_length=1, max_length=16)

    @model_validator(mode="after")
    def one_target_kind(self):
        if (self.resource_id is None) == (self.repository_host is None):
            raise ValueError("Specify a fixed resource_id or a repository_host, not both.")
        return self


class ActorRef(Payload):
    user_id: Identifier


class Actor(ActorRef):
    team: Identifier
    role: Identifier


class ChatActor(Actor):
    """Current user context supplied by the authenticated Chat service."""

    actual_end_date: date | None = None


class OnboardingStep(Payload):
    """Current step within an onboarding track; action type is context only."""

    id: Identifier
    title: Annotated[
        str, StringConstraints(strip_whitespace=True, min_length=1, max_length=256), AfterValidator(valid_utf8),
    ]
    resource_id: Identifier | None = None
    action_type: Identifier | None = None

    @field_validator("resource_id", mode="before", json_schema_input_type=Identifier | Literal[""] | None)
    @classmethod
    def empty_resource_is_absent(cls, value: object) -> object:
        # Chat's Go string has an empty zero value for steps without a resource.
        return None if value == "" else value

    @field_validator("action_type", mode="before", json_schema_input_type=Identifier | Literal[""] | None)
    @classmethod
    def empty_action_is_absent(cls, value: object) -> object:
        return None if value == "" else value


class OnboardingPlan(Payload):
    """Read-only track snapshot supplied by Chat, including tracks without a current step."""

    id: Identifier
    title: Annotated[
        str, StringConstraints(strip_whitespace=True, min_length=1, max_length=256), AfterValidator(valid_utf8),
    ]
    current_step: OnboardingStep | None = None


class Message(Payload):
    role: Literal["user", "assistant"]
    content: Annotated[
        str, StringConstraints(strip_whitespace=True, min_length=1), AfterValidator(valid_utf8),
    ]
    source_ids: list[Identifier] = Field(
        default_factory=list, max_length=MAX_RESPONSE_SOURCES,
        description="Source IDs stored with a past assistant reply. Every listed source must still be accessible "
        "to the current actor. Source-free replies without citation markers are retained only as untrusted "
        "conversation context, not evidence of facts or access rights. Chat must preserve source metadata.",
    )


class Source(Payload):
    id: Identifier
    title: str = Field(min_length=1, max_length=160)
    url: HttpUrl | None = None


class AccessMetadata(Payload):
    access_policies: list[AccessPolicy] = Field(default_factory=list, max_length=8)

    @model_serializer(mode="wrap")
    def omit_empty_access_policies(self, handler):
        data = handler(self)
        if not self.access_policies:
            data.pop("access_policies", None)
        return data


class CitedSource(Source):
    text: str = Field(min_length=1, max_length=MAX_SOURCE_TEXT)


class SearchHit(CitedSource, AccessMetadata):
    """Search result with publisher metadata that is not part of Chat's source contract."""


class Document(Source, AccessMetadata):
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
    # Internal import metadata; never part of search results or public sources.
    generated_context: str = Field(default="", max_length=600, exclude=True)
    original_document: str = Field(default="", exclude=True)
    original_document_id: str = Field(default="", exclude=True)

    def embedding_text(self) -> str:
        return "\n".join([self.title, *self.keywords, self.generated_context, self.text])


class SearchResult(Payload):
    hits: list[SearchHit]


class ChatResponse(Payload):
    answer: str
    sources: list[CitedSource] = Field(
        max_length=MAX_RESPONSE_SOURCES,
        description="Only sources cited in the answer, in order of first citation, without duplicates.",
    )
    proposal: AccessProposal | None = None

    @model_serializer(mode="wrap")
    def omit_absent_proposal(self, handler):
        data = handler(self)
        if self.proposal is None:
            data.pop("proposal", None)
        return data
