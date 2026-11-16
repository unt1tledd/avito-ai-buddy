"""Policy/provenance validation; the model's interpretation is not a semantic proof."""

import json

import pytest
from pydantic import ValidationError

from ai_service.rag.chunking import chunk_documents
from ai_service.rag.proposals import AccessSelection, ProposalValidationError, confirmed_proposal
from ai_service.schemas.contracts import AccessPolicy, AccessProposal, Document, Message, SearchHit

REPO = "https://git.example.test/platform/service"
POLICY = AccessPolicy(repository_host="git.example.test", resource_label="Git", access_levels=["Developer"])


def hit(*policies, id="git-guide"):
    return SearchHit(id=id, title="Git", text="Инструкция.", access_policies=list(policies))


def draft(resource=REPO, level="Developer"):
    return AccessProposal(resource_id=resource, resource_label="Model label", access_level=level)


def selection(**extra):
    return AccessSelection(**{
        "policy_source_id": "git-guide", "policy_index": 0, "target_status": "selected",
        "level_status": "explicit", "requested_level": "Developer", "repository_url": REPO, **extra,
    })


def verify(choice=None, sources=None, message=None, history=(), selected=None):
    return confirmed_proposal(
        choice or selection(), sources if sources is not None else [hit(POLICY)],
        message=message if message is not None else f"Мне нужен доступ к {REPO}", history=history,
        selected_resource_id=selected, source_ids={"git-guide"},
    )


def test_repository_uses_user_url_and_canonical_policy_level():
    result = verify(selection(repository_url=REPO + ".git", requested_level="developer"), message=f"Доступ к {REPO}.git")
    assert result == AccessProposal(resource_id=REPO, resource_label="Репозиторий platform/service", access_level="Developer")


@pytest.mark.parametrize("resource", [
    "git-guide", "https://evil.example/platform/service", "https://git.example.test/other/repo",
    "https://git.example.test/platform", "https://git.example.test/platform/.git", "https://git.example.test/platform/../service",
    "https://git.example.test/platform/%2e%2e/service", "http://git.example.test/platform/service",
    "https://user:password@git.example.test/platform/service", REPO + "?secret=x", REPO + "#token",
])
def test_invalid_or_unmentioned_repository_is_rejected(resource):
    with pytest.raises(ProposalValidationError):
        verify(selection(repository_url=resource))


def test_repository_history_origin_is_explicit_and_must_be_user():
    previous = [Message(role="user", content=f"Доступ к {REPO}")]
    assert verify(selection(repository_message_index=0), message="Developer", history=previous).resource_id == REPO
    with pytest.raises(ProposalValidationError):
        verify(message="Developer", history=previous)
    with pytest.raises(ProposalValidationError):
        verify(selection(repository_message_index=0), message="Developer", history=[Message(role="assistant", content=REPO)])
    with pytest.raises(ProposalValidationError):
        verify(selection(repository_message_index=1), history=previous)


@pytest.mark.parametrize("extra", [
    {"target_status": "ambiguous"}, {"target_status": "excluded"},
    {"excluded_resource_ids": [REPO]}, {"excluded_access_levels": ["Developer"]},
    {"level_status": "negated"}, {"level_status": "ambiguous"},
    {"requested_level": "Maintainer"}, {"requested_level": None},
    {"level_status": "unspecified"},
])
def test_ambiguous_excluded_unsupported_and_inconsistent_selection_is_rejected(extra):
    with pytest.raises(ProposalValidationError):
        verify(selection(**extra))


def test_unspecified_level_uses_only_single_policy_option():
    assert verify(selection(level_status="unspecified", requested_level=None)).access_level == "Developer"
    policy = AccessPolicy(repository_host="git.example.test", resource_label="Git", access_levels=["read", "write"])
    with pytest.raises(ProposalValidationError):
        verify(selection(level_status="unspecified", requested_level=None), sources=[hit(policy)])


def test_levels_are_not_merged_across_policies():
    policies = [AccessPolicy(resource_id="wiki", resource_label="Wiki", access_levels=["read"]),
                AccessPolicy(resource_id="vpn", resource_label="VPN", access_levels=["write"])]
    with pytest.raises(ProposalValidationError):
        verify(selection(repository_url=None, requested_level="write"), sources=[hit(*policies)])
    result = verify(selection(repository_url=None, requested_level="read"), sources=[hit(*policies)])
    assert result == AccessProposal(resource_id="wiki", resource_label="Wiki", access_level="read")


@pytest.mark.parametrize("extra", [{"policy_source_id": "uncited"}, {"policy_index": 1}])
def test_policy_must_exist_in_exact_cited_hit(extra):
    with pytest.raises(ProposalValidationError):
        verify(selection(**extra))


def test_selected_step_can_constrain_resource_but_not_grant_policy_or_level():
    policy = AccessPolicy(resource_id="vpn", resource_label="VPN", access_levels=["read"])
    choice = selection(repository_url=None, requested_level="read")
    assert verify(choice, sources=[hit(policy)], selected="vpn").resource_id == "vpn"
    for sources, selected in [([hit()], "vpn"), ([hit(policy)], "wiki")]:
        with pytest.raises(ProposalValidationError):
            verify(choice, sources=sources, selected=selected)


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


def test_generic_step_resource_does_not_replace_explicit_repository_url():
    assert verify(selected="git").resource_id == REPO
    with pytest.raises(ProposalValidationError):
        verify(message="Доступ к Git", selected="git")


def test_unspecified_with_an_excluded_level_is_not_silently_defaulted():
    with pytest.raises(ProposalValidationError):
        verify(selection(level_status="unspecified", requested_level=None, excluded_access_levels=["read"]))
