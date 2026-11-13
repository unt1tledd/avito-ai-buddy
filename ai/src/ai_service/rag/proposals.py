"""Validate drafts against current cited policies and an explicitly identified target."""

import re
from collections.abc import Sequence
from urllib.parse import unquote, urlsplit, urlunsplit

from ..schemas.clarifications import ACCESS_LEVEL_CLARIFICATION, REPOSITORY_CLARIFICATION, RESOURCE_CLARIFICATION
from ..schemas.contracts import AccessProposal, Message, SearchHit


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


def confirmed_proposal(
    proposal: AccessProposal, sources: Sequence[SearchHit], *, message: str,
    history: Sequence[Message], selected_resource_id: str | None, source_ids: set[str],
) -> AccessProposal | str:
    # Only user messages identify a requested target; assistant history is not authority.
    texts = [message]
    if previous := next((item.content for item in reversed(history) if item.role == "user"), None):
        texts.append(previous)
    urls = explicit_urls(message)
    if not urls and len(texts) > 1:
        urls = explicit_urls(texts[1])
    requested_url = repository_url(proposal.resource_id)
    policies = [policy for source in sources for policy in source.access_policies]
    targets = []
    for policy in policies:
        if proposal.resource_id in source_ids:
            continue
        if policy.repository_host:
            if (requested_url not in urls or requested_url is None
                    or urlsplit(requested_url).hostname != policy.repository_host):
                continue
            resource_id = requested_url
            label = f"Репозиторий {urlsplit(resource_id).path.lstrip('/')}"
        else:
            if proposal.resource_id != policy.resource_id:
                continue
            explicitly_named = any(re.search(
                r"(?<![\w/-])" + re.escape(name) + r"(?![\w/-])", text, flags=re.IGNORECASE,
            ) for text in texts for name in (policy.resource_id, policy.resource_label))
            if not explicitly_named and selected_resource_id != policy.resource_id:
                continue
            resource_id, label = policy.resource_id, policy.resource_label
        targets.append((resource_id, label, policy.access_levels))
    if targets:
        levels = {level.casefold(): level for _, _, allowed in targets for level in allowed}
        requested_levels = set()
        for text in texts:
            requested_levels = {key for key in levels if re.search(r"(?<!\w)" + re.escape(key) + r"(?!\w)", text.casefold())}
            if requested_levels:
                break
        selected_level = proposal.access_level.casefold()
        if (selected_level in levels and (requested_levels == {selected_level}
                or (not requested_levels and len(levels) == 1))):
            resource_id, label, _ = targets[0]
            return AccessProposal(resource_id=resource_id, resource_label=label, access_level=levels[selected_level])
        return ACCESS_LEVEL_CLARIFICATION
    if requested_url or any(policy.repository_host for policy in policies):
        return REPOSITORY_CLARIFICATION
    return RESOURCE_CLARIFICATION
