import asyncio
import json

import pytest

from ai_service.rag.history import MAX_HISTORY_BYTES, history_message, prepare_history
from ai_service.schemas.clarifications import CLARIFICATION_QUESTIONS
from ai_service.schemas.contracts import Actor, Message


ACTOR = Actor(user_id="42", role="intern", team="51")


class Access:
    def __init__(self, allowed=()):
        self.allowed = set(allowed)
        self.calls = []

    async def accessible_source_ids(self, source_ids, actor):
        self.calls.append((source_ids, actor))
        return source_ids & self.allowed


def test_only_last_thirty_messages_are_considered_in_chronological_order():
    history = [Message(role="user", content=f"Сообщение {index}") for index in range(40)]
    access = Access()
    selected = asyncio.run(prepare_history(history, ACTOR, access))
    assert selected == history[-30:]
    assert access.calls == []
    assert len(history) == 40


@pytest.mark.parametrize("symbol", ["a", "Ж", "🧠"])
def test_utf8_history_budget_removes_oldest_messages_whole(symbol):
    history = [Message(role="user", content=f"{index:02}" + symbol * 8190) for index in range(30)]
    selected = asyncio.run(prepare_history(history, ACTOR, Access()))
    assert 0 < len(selected) < 30
    assert selected == history[-len(selected):]
    assert all(len(message.content) == 8192 for message in selected)
    block = history_message(selected)
    assert len(json.dumps(block, ensure_ascii=False).encode()) <= MAX_HISTORY_BYTES
    one_more = history[-len(selected) - 1:]
    assert len(json.dumps(history_message(one_more), ensure_ascii=False).encode()) > MAX_HISTORY_BYTES


def test_assistant_replies_require_all_declared_sources_and_consistent_citations():
    history = [
        Message(role="user", content="Как получить VPN?"),
        Message(role="assistant", content="Legacy reply without source metadata."),
        Message(role="assistant", content="Доступный ответ. [source:public]", source_ids=["public"]),
        Message(role="assistant", content="Частично закрытый ответ. [source:public]", source_ids=["public", "closed"]),
        Message(role="assistant", content="Удалённый источник. [source:deleted]", source_ids=["deleted"]),
        Message(role="assistant", content="Неполные метаданные. [source:closed]", source_ids=["public"]),
        Message(role="assistant", content="Повреждённая ссылка. [source:public", source_ids=["public"]),
    ]
    access = Access(["public"])
    selected = asyncio.run(prepare_history(history, ACTOR, access))
    assert selected == [history[0], history[2]]
    assert access.calls == [({"public", "closed", "deleted"}, ACTOR)]
    assert len(history) == 7


@pytest.mark.parametrize("question", CLARIFICATION_QUESTIONS)
def test_service_questions_are_kept_without_source_metadata(question):
    history = [Message(role="assistant", content=question), Message(role="user", content="Первый вариант")]
    access = Access()
    assert asyncio.run(prepare_history(history, ACTOR, access)) == history
    assert access.calls == []


def test_clarification_exception_does_not_bypass_acl_or_allow_extra_text():
    question = "Уточните: VPN или офисный пропуск?"
    history = [
        Message(role="assistant", content=question),
        Message(role="assistant", content=question, source_ids=["closed"]),
        Message(role="assistant", content=question + " Пароль — invented-password?"),
        Message(role="assistant", content=question + " [source:public]"),
        Message(role="assistant", content="Ответ. [source:public]", source_ids=["public"]),
        Message(role="assistant", content="Закрытый ответ. [source:closed]", source_ids=["closed"]),
    ]
    access = Access(["public"])
    assert asyncio.run(prepare_history(history, ACTOR, access)) == [history[0], history[4]]
    assert access.calls == [({"closed", "public"}, ACTOR)]


def test_old_messages_outside_window_are_not_checked_or_backfilled():
    history = [Message(role="assistant", content="Old reply.", source_ids=["old-source"])]
    history.extend(Message(role="user", content=f"Последний вопрос {index}") for index in range(30))
    access = Access()
    assert asyncio.run(prepare_history(history, ACTOR, access)) == history[-30:]
    assert access.calls == []


def test_single_message_larger_than_history_budget_is_removed_whole():
    size = MAX_HISTORY_BYTES + 1024
    history = [Message(role="user", content="Предыдущий вопрос"), Message(role="user", content="x" * size)]
    assert asyncio.run(prepare_history(history, ACTOR, Access())) == []
    assert len(history[1].content) == size
