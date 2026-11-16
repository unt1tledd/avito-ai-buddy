"""Free LLM answers in the mandatory RAG pipeline; semantics need live model evals."""

import json

import pytest
from fastapi.testclient import TestClient
from test_chat import (
    ACTOR,
    AUTH,
    ONBOARDING_TRACKS,
    ScriptedModel,
    answer,
    ask,
    call,
    first_context,
    mock_search_results,
    previous_messages,
    searched_model,
)

from ai_service.clients.llm import ModelError
from ai_service.main import create_app
from ai_service.repositories.knowledge import KnowledgeError
from ai_service.schemas.contracts import SearchResult


@pytest.mark.parametrize(("question", "kind", "text"), [
    ("Привет", "conversation", "Здравствуйте! С чем помочь в адаптации?"),
    ("Спасибо", "conversation", "Рад, что удалось разобраться!"),
    ("Мне грустно", "conversation", "Хотите рассказать, что сейчас больше всего беспокоит?"),
    ("Не хочу спрашивать наставника", "conversation", "Можно начать с короткого сообщения. Помочь сформулировать его?"),
    ("Нужен доступ", "clarification", "К какой системе вы хотите получить доступ и для какой задачи?"),
    ("Какой у меня шаг?", "onboarding_state", "В техническом треке текущий шаг — настроить VPN."),
    ("Сколько шагов завершено?", "onboarding_state", "По имеющимся данным нельзя определить число завершённых шагов."),
    ("Где офис?", "no_knowledge", "В доступных материалах я не нашёл адрес офиса. Уточните его у команды."),
])
def test_each_message_searches_before_preserving_model_wording(settings, monkeypatch, question, kind, text):
    queries = mock_search_results(monkeypatch, {question: SearchResult(hits=[])})
    model = searched_model(answer(text, kind=kind), query=question)
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, question, onboarding_plans=ONBOARDING_TRACKS)
    assert response.status_code == 200, response.text
    assert response.json() == {"answer": text, "sources": []}
    assert queries == [question]
    assert len(model.requests) == 2
    assert model.requests[0][0] != model.requests[1][0]
    assert first_context(model)["onboarding_plans"][0]["id"] == "technical"
    assert model.requests[0][1][-1] == {"role": "user", "content": question}
    assert model.requests[1][1][-1]["type"] == "function_call_output"


@pytest.mark.parametrize("kind", ["conversation", "onboarding_state", "clarification", "no_knowledge", "grounded_answer"])
def test_no_answer_kind_can_bypass_initial_search(settings, monkeypatch, kind):
    queries = mock_search_results(monkeypatch, {})
    model = ScriptedModel(answer("Преждевременный ответ", kind=kind))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Привет")
    assert response.status_code == 502
    assert queries == []
    assert "Преждевременный ответ" not in response.text


@pytest.mark.parametrize("kind", ["conversation", "onboarding_state", "clarification", "no_knowledge"])
def test_free_answer_cannot_hide_database_failure(settings, monkeypatch, kind):
    async def unavailable(*args, **kwargs):
        raise KnowledgeError("Unavailable", reason="connection")

    from ai_service.rag.retriever import Retriever
    monkeypatch.setattr(Retriever, "search", unavailable)
    model = searched_model(answer("Свободный ответ", kind=kind))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Привет")
    assert response.status_code == 502
    assert response.json() == {"detail": "Не удалось выполнить поиск в базе знаний."}


@pytest.mark.parametrize("kind", ["conversation", "onboarding_state", "clarification", "no_knowledge"])
def test_free_answer_still_rejects_unavailable_citations_and_proposals(settings, kind):
    model = searched_model(answer("Ответ [source:closed]", kind=kind))
    with TestClient(create_app(settings, model=model)) as client:
        assert ask(client, "Привет").status_code == 502
    model = searched_model(answer("Ответ", kind=kind, proposal={
        "resource_id": "vpn", "resource_label": "VPN", "access_level": "read",
    }))
    with TestClient(create_app(settings, model=model)) as client:
        assert ask(client, "Нужен доступ").status_code == 502


def test_free_clarification_preserves_order_in_both_model_stages(settings):
    clarification = "Что выбираете: Wiki, офисный пропуск или VPN?"
    history = [{"role": "assistant", "content": clarification}, {"role": "user", "content": "второй"}]
    model = searched_model(answer("Какую проблему с пропуском нужно решить?", kind="clarification"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Помоги с ним", history=history, onboarding_plans=list(reversed(ONBOARDING_TRACKS)))
    assert response.status_code == 200
    expected = [{**message, "source_ids": []} for message in history]
    assert previous_messages(model, 0) == previous_messages(model, 1) == expected


def test_partial_answer_and_specific_question_are_not_replaced(settings):
    text = "Следуйте инструкции настройки. [source:demo-dev-environment] А какой трек вы хотите продолжить?"
    model = searched_model(answer(text))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Как настроить окружение и выполнить текущий шаг?", onboarding_plans=ONBOARDING_TRACKS)
    assert response.status_code == 200
    assert response.json()["answer"] == text
    assert [source["id"] for source in response.json()["sources"]] == ["demo-dev-environment"]


def test_legacy_template_output_is_not_a_final_answer(settings):
    payload = {"kind": "support", "template_id": "support_choices"}
    model = searched_model([{"type": "message", "content": [{"type": "output_text", "text": json.dumps(payload)}]}])
    with TestClient(create_app(settings, model=model)) as client:
        assert ask(client, "Мне грустно").status_code == 502


def test_safety_fallback_never_recasts_work_after_search(settings):
    model = ScriptedModel(call("find_instruction", query="опасный шаг VPN"),
                          ModelError("Provider failed", status_code=400, reason="http_400"))
    with TestClient(create_app(settings, model=model)) as client:
        response = ask(client, "Опасно ли настраивать VPN?")
    assert response.status_code == 502


def test_ordinary_emotion_does_not_turn_model_failure_into_unverified_response(settings):
    model = ScriptedModel(ModelError("Provider failed", status_code=400, reason="http_400"))
    with TestClient(create_app(settings, model=model)) as client:
        assert ask(client, "Мне грустно").status_code == 502


@pytest.mark.parametrize("invalid", ["auth", "ended"])
def test_support_preserves_auth_and_internship_checks(settings, invalid):
    model = ScriptedModel(answer("Давайте разберёмся.", kind="conversation"))
    with TestClient(create_app(settings, model=model)) as client:
        response = client.post("/api/v1/answer", headers={} if invalid == "auth" else AUTH, json={
            "actor": ACTOR if invalid == "auth" else {**ACTOR, "actual_end_date": "2026-01-01"},
            "message": "Мне грустно",
        })
    assert response.status_code == (401 if invalid == "auth" else 422)
    assert model.requests == []
