import json

import pytest
from pydantic import ValidationError

from ai_service.rag.chunking import chunk_documents
from ai_service.rag.proposals import confirmed_proposal
from ai_service.schemas.clarifications import ACCESS_LEVEL_CLARIFICATION, REPOSITORY_CLARIFICATION, RESOURCE_CLARIFICATION
from ai_service.schemas.contracts import AccessPolicy, AccessProposal, Document, Message, SearchHit


REPO = "https://git.example.test/platform/service"
POLICY = AccessPolicy(repository_host="git.example.test", resource_label="Git", access_levels=["Developer"])


def hit(*policies, id="git-guide"):
    return SearchHit(id=id, title="Git", text="Maintainer не выдаётся. Запросите Developer.", access_policies=list(policies))


def draft(resource=REPO, level="Developer"):
    return AccessProposal(resource_id=resource, resource_label="Model label", access_level=level)


def verify(proposal=None, sources=None, message=None, history=(), selected=None):
    return confirmed_proposal(
        proposal or draft(), sources if sources is not None else [hit(POLICY)],
        message=message if message is not None else f"Мне нужен доступ к {REPO}", history=history,
        selected_resource_id=selected, source_ids={"git-guide"},
    )


def test_repository_uses_explicit_url_and_published_level_with_canonical_label():
    result = verify(draft(REPO + ".git", "developer"), message=f"Мне нужен доступ к {REPO}.git")
    assert result == AccessProposal(resource_id=REPO, resource_label="Репозиторий platform/service", access_level="Developer")
    assert set(result.model_dump()) == {"resource_id", "resource_label", "access_level"}


@pytest.mark.parametrize("resource", [
    "git-guide", "https://evil.example/platform/service", "https://git.example.test/other/repo",
    "https://git.example.test/platform", "https://git.example.test/platform/.git", "https://git.example.test/platform/../service",
    "https://git.example.test/platform/%2e%2e/service", "http://git.example.test/platform/service",
    "https://user:password@git.example.test/platform/service", REPO + "?secret=x", REPO + "#token",
])
def test_invented_source_id_host_or_repository_cannot_become_target(resource):
    assert verify(draft(resource)) in {REPOSITORY_CLARIFICATION, RESOURCE_CLARIFICATION}


def test_unknown_repository_asks_instead_of_inventing_target():
    assert verify(message="Как получить доступ к репозиторию?") == REPOSITORY_CLARIFICATION


def test_only_user_history_can_identify_url_and_current_target_has_priority():
    history = [Message(role="user", content=f"Доступ к {REPO}")]
    assert isinstance(verify(message="Developer", history=history), AccessProposal)
    assert verify(message="А теперь к https://git.example.test/other/repo", history=history) == REPOSITORY_CLARIFICATION
    assert verify(message="Дайте доступ", history=[Message(role="assistant", content=REPO)]) == REPOSITORY_CLARIFICATION


def test_level_in_negative_prose_is_not_an_allowed_level():
    assert verify(draft(level="Maintainer")) == ACCESS_LEVEL_CLARIFICATION
    assert verify(sources=[hit()]) == REPOSITORY_CLARIFICATION


def test_multiple_allowed_levels_require_explicit_choice_and_cannot_escalate_it():
    policy = AccessPolicy(repository_host="git.example.test", resource_label="Git", access_levels=["read", "write"])
    assert verify(draft(level="write"), [hit(policy)]) == ACCESS_LEVEL_CLARIFICATION
    assert verify(draft(level="write"), [hit(policy)], f"read для {REPO}") == ACCESS_LEVEL_CLARIFICATION
    assert verify(draft(level="read"), [hit(policy)], f"read для {REPO}").access_level == "read"


def test_selected_step_identifies_resource_but_does_not_grant_level():
    policy = AccessPolicy(resource_id="repo", resource_label="Репозиторий команды", access_levels=["read"])
    assert verify(draft("repo", "read"), [hit(policy)], "Как получить доступ?", selected="repo").resource_id == "repo"
    assert verify(draft("repo", "read"), [hit()], "Как получить доступ?", selected="repo") == RESOURCE_CLARIFICATION
    assert verify(draft("repo", "read"), [hit(policy)], "Нужен доступ: Репозиторий команды").resource_id == "repo"


@pytest.mark.parametrize("field", ["resource_id", "resource_label", "access_level"])
@pytest.mark.parametrize("value", ["", "   ", "x\x00y", "x\ny"])
def test_proposal_requires_complete_nonempty_control_free_fields(field, value):
    with pytest.raises(ValidationError):
        AccessProposal(**{**draft().model_dump(), field: value})


def test_imported_policy_survives_chunking_without_changing_embedding_text():
    document = Document(id="git-guide", title="Git", text="Инструкция. " * 200, roles={"intern"}, access_policies=[POLICY])
    chunks = chunk_documents([document])
    assert len(chunks) > 1 and all(chunk.access_policies == [POLICY] for chunk in chunks)
    assert "repository_host" not in chunks[0].embedding_text()
    assert Document.model_validate_json(json.dumps(document.model_dump(mode="json"))).access_policies == [POLICY]


@pytest.mark.parametrize("overrides", [{}, {"resource_id": "repo", "repository_host": "git.example.test"}])
def test_policy_requires_one_authoritative_target_type(overrides):
    with pytest.raises(ValidationError):
        AccessPolicy(resource_label="Git", access_levels=["read"], **overrides)
