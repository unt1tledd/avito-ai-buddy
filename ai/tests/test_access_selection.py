"""Post-retrieval selection checks; scripted decisions do not test NLU accuracy."""

import asyncio
import json
from importlib.resources import files

import pytest
from fastapi.testclient import TestClient

from ai_service.agents.agent import Agent
from ai_service.clients.llm import ModelResponse
from ai_service.main import create_app
from ai_service.schemas.contracts import AccessPolicy, Actor, SearchHit, SearchResult
from test_chat import ScriptedModel, answer, ask, call, mock_search_results, searched_model, settings


def selected(**changes):
    return {
        "policy_source_id": "policy-guide", "policy_index": 0, "target_status": "selected",
        "level_status": "explicit", "requested_level": "read", **changes,
    }


def output(selection, *, citation=True):
    payload = {
        "kind": "grounded_answer", "answer": "Подтвердите черновик в интерфейсе." + (" [source:policy-guide]" if citation else ""),
        "access_request": selection,
    }
    return [{"type": "message", "content": [{"type": "output_text", "text": json.dumps(payload)}]}]


def policy_hit(levels=("read", "write")):
    return SearchHit(id="policy-guide", title="Доступ", text="Оформление доступа.", access_policies=[
        AccessPolicy(resource_id="wiki", resource_label="Wiki", access_levels=list(levels)),
        AccessPolicy(resource_id="vpn", resource_label="VPN", access_levels=list(levels)),
    ])


@pytest.mark.parametrize("selection", [
    selected(level_status="negated", requested_level="write"),
    selected(level_status="explicit", requested_level="admin"),
    selected(level_status="ambiguous"),
    selected(policy_index=1, excluded_resource_ids=["vpn"]),
    selected(excluded_access_levels=["read"]),
    selected(policy_index=7),
])
def test_invalid_selection_is_corrected_by_model_without_canned_response_or_extra_search(settings, monkeypatch, selection):
    queries = mock_search_results(monkeypatch, {"доступ Wiki": SearchResult(hits=[policy_hit()])})
    clarification = "Уточните, какой разрешённый уровень нужен именно для Wiki: read или write?"
    model = searched_model(output(selection), answer(clarification, kind="clarification"), query="доступ Wiki")
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Нужен доступ к Wiki")
    assert response.status_code == 200, response.text
    assert response.json() == {"answer": clarification, "sources": []}
    assert queries == ["доступ Wiki"]
    assert len(model.requests) == 3
    assert model.requests[-1][2] == []
    assert "Проверка заявки отклонила выбор" in model.requests[-1][1][-1]["content"]


def test_single_level_is_proposed_only_when_selection_declares_it_unspecified(settings, monkeypatch):
    mock_search_results(monkeypatch, {"Wiki": SearchResult(hits=[policy_hit(("read",))])})
    model = searched_model(output(selected(level_status="unspecified", requested_level=None)), query="Wiki")
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Нужен доступ к Wiki")
    assert response.status_code == 200, response.text
    assert response.json()["proposal"] == {"resource_id": "wiki", "resource_label": "Wiki", "access_level": "read"}
    assert "access_request" not in response.json()
    assert "access_policies" not in response.json()["sources"][0]
    assert len(model.requests) == 2


@pytest.mark.parametrize("correction", [output(selected(requested_level="admin")), call("find_instruction", query="другое")])
def test_selection_correction_is_bounded_and_cannot_start_another_search(settings, monkeypatch, correction):
    queries = mock_search_results(monkeypatch, {"Wiki": SearchResult(hits=[policy_hit()])})
    model = searched_model(output(selected(requested_level="admin")), correction, query="Wiki")
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Нужен admin для Wiki")
    assert response.status_code == 502
    assert "proposal" not in response.json()
    assert queries == ["Wiki"]
    assert len(model.requests) == 3


def test_citation_and_selection_correction_share_one_retry(settings, monkeypatch):
    mock_search_results(monkeypatch, {"Wiki": SearchResult(hits=[policy_hit()])})
    model = searched_model(output(selected(), citation=False), output(selected(requested_level="admin")), query="Wiki")
    with TestClient(create_app(settings, model=model)) as client:
        assert ask(client, "Нужен read для Wiki").status_code == 502
    assert len(model.requests) == 3


def test_uncited_policy_does_not_authorize_draft(settings, monkeypatch):
    public = SearchHit(id="public", title="Общие сведения", text="Общая инструкция.")
    mock_search_results(monkeypatch, {"Wiki": SearchResult(hits=[public, policy_hit()])})
    payload = {"kind": "grounded_answer", "answer": "Инструкция [source:public]", "access_request": selected()}
    final = [{"type": "message", "content": [{"type": "output_text", "text": json.dumps(payload)}]}]
    model = searched_model(final, answer("Какой доступ вам нужен?", kind="clarification"), query="Wiki")
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Нужен read для Wiki")
    assert response.status_code == 200
    assert "proposal" not in response.json()


def test_malformed_unicode_query_is_rejected_before_embeddings(settings, monkeypatch):
    queries = mock_search_results(monkeypatch, {})
    model = ScriptedModel(call("find_instruction", query="\ud800"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Вопрос")
    assert response.status_code == 502
    assert queries == []


def test_concurrent_turns_do_not_share_query_cache_or_sources():
    class Knowledge:
        async def search(self, query, actor):
            await asyncio.sleep(0)
            assert query == actor.user_id
            return SearchResult(hits=[SearchHit(id=actor.user_id, title=actor.user_id, text="Текст")])

    class Model:
        async def respond(self, *, instructions, messages, tools):
            await asyncio.sleep(0)
            results = [item for item in messages if item.get("type") == "function_call_output"]
            if not results:
                return ModelResponse(output=call("find_instruction", query=messages[-1]["content"]))
            source = json.loads(results[-1]["output"])["hits"][0]["id"]
            return ModelResponse(output=answer(f"Ответ [source:{source}]"))

    async def run():
        prompt = files("ai_service.agents").joinpath("prompts", "system.txt").read_text()
        agent = Agent(Model(), Knowledge(), prompt)
        return await asyncio.gather(*[
            agent.answer(user, Actor(user_id=user, role="intern", team=user)) for user in ("first", "second")
        ])

    first, second = asyncio.run(run())
    assert [source.id for source in first.sources] == ["first"]
    assert [source.id for source in second.sources] == ["second"]
