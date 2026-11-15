"""Validate drafts against current cited policies and an explicitly identified target."""

import re
from collections.abc import Sequence
from typing import Literal
from urllib.parse import unquote, urlsplit, urlunsplit

from pydantic import BaseModel, ConfigDict, Field

from ..schemas.contracts import AccessLevel, AccessProposal, Identifier, Message, ProposalText, SearchHit


def repository_url(value: str) -> str | None:
    try:
        url = urlsplit(value)
        path = url.path.rstrip("/").removesuffix(".git")
        decoded_path = unquote(path)
        segments = decoded_path.strip("/").split("/")
        if (url.scheme != "https" or not url.hostname or url.username or url.password
                or url.port not in (None, 443) or url.query or url.fragment
                or any(ord(character) < 32 for character in decoded_path)
                or len(segments) < 2 or any(part in ("", ".", "..") for part in segments)):
            return None
        return urlunsplit(("https", url.hostname, path, "", ""))
    except ValueError:
        return None


def explicit_urls(text: str) -> set[str]:
    return {
        canonical for value in re.findall(r"https://[^\s<>\"'`]+", text)
        if (canonical := repository_url(value.rstrip(".,;!?)»"))) is not None
    }


class AccessSelection(BaseModel):
    """Model interpretation after retrieval; policy validation does not prove its semantics."""

    model_config = ConfigDict(extra="forbid", hide_input_in_errors=True)

    onboarding_plan_id: Identifier | None = None
    policy_source_id: Identifier
    policy_index: int = Field(ge=0, lt=8, strict=True)
    target_status: Literal["selected", "ambiguous", "excluded"]
    level_status: Literal["explicit", "unspecified", "negated", "ambiguous"]
    requested_level: AccessLevel | None = None
    repository_url: ProposalText | None = Field(
        default=None, description="Required for a repository_host policy: copy the concrete HTTPS repository URL from a user message.",
    )
    repository_message_index: int | None = Field(
        default=None, ge=0, lt=30, strict=True,
        description="Zero-based index within retained history ONLY, and the message must be user. Omit or null for a URL in the CURRENT question; current question is not in history.",
    )
    excluded_resource_ids: list[ProposalText] = Field(default_factory=list, max_length=16)
    excluded_access_levels: list[AccessLevel] = Field(default_factory=list, max_length=16)


class ProposalValidationError(ValueError):
    """A fixed validation explanation for the model, never a user-facing answer."""


def confirmed_proposal(
    selection: AccessSelection, sources: Sequence[SearchHit], *, message: str,
    history: Sequence[Message], selected_resource_id: str | None, source_ids: set[str],
) -> AccessProposal:
    if selection.target_status != "selected":
        raise ProposalValidationError("The target is ambiguous or excluded. Ask the user to choose a resource.")
    hits = [source for source in sources if source.id == selection.policy_source_id]
    if len(hits) != 1 or selection.policy_index >= len(hits[0].access_policies):
        raise ProposalValidationError("The policy must exist in a cited current search hit. Ask for the resource or omit the draft.")
    policy = hits[0].access_policies[selection.policy_index]
    if policy.repository_host:
        resource_id = repository_url(selection.repository_url or "")
        origin = message
        if selection.repository_message_index is not None:
            index = selection.repository_message_index
            if index >= len(history) or history[index].role != "user":
                raise ProposalValidationError("Repository index must refer to a retained user message. For a URL in the CURRENT question omit repository_message_index or set it to null; current question is not in history.")
            origin = history[index].content
        if (resource_id is None or resource_id not in explicit_urls(origin)
                or urlsplit(resource_id).hostname != policy.repository_host):
            raise ProposalValidationError("Set access_request.repository_url to the concrete user-provided HTTPS URL on the policy host. For the CURRENT question omit repository_message_index or set it to null. Ask for a URL only if the user has not provided one.")
        label = f"Репозиторий {urlsplit(resource_id).path.lstrip('/')}"
    else:
        if selection.repository_url is not None or selection.repository_message_index is not None:
            raise ProposalValidationError("A fixed-resource policy cannot be used for a repository URL.")
        resource_id, label = policy.resource_id, policy.resource_label
    excluded = {repository_url(value) or value for value in selection.excluded_resource_ids}
    if resource_id in source_ids or resource_id in excluded:
        raise ProposalValidationError("The resource is excluded or is a source ID. Ask for the actual requested resource.")
    if not policy.repository_host and selected_resource_id is not None and selected_resource_id != resource_id:
        raise ProposalValidationError("The resource does not match the selected onboarding step. Clarify the intended resource.")
    levels = {level.casefold(): level for level in policy.access_levels}
    if selection.level_status == "explicit":
        if selection.requested_level is None or selection.requested_level.casefold() not in levels:
            raise ProposalValidationError("The explicitly requested access level is not supported. Explain this and ask for an allowed level; do not substitute one.")
        level = selection.requested_level.casefold()
    elif (selection.level_status == "unspecified" and selection.requested_level is None
          and not selection.excluded_access_levels and len(levels) == 1):
        level = next(iter(levels))
    else:
        raise ProposalValidationError("The access level is missing, negated or ambiguous. Ask which allowed level the user wants.")
    if level in {value.casefold() for value in selection.excluded_access_levels}:
        raise ProposalValidationError("The selected access level is explicitly excluded. Ask for the desired allowed level.")
    return AccessProposal(resource_id=resource_id, resource_label=label, access_level=levels[level])
