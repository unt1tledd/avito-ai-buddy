"""Internal routing decisions and complete, source-free service texts.

These models are never part of the HTTP or history contract. No user or model
text is interpolated into the catalogue, including the question-writing aid.
"""

import re
from typing import Literal

from pydantic import BaseModel, ConfigDict, model_validator


TEMPLATES = {
    "support_choices": (
        "Мне жаль, что вам сейчас тяжело. Можно сначала обсудить, что случилось, "
        "или выбрать небольшой следующий шаг. Что вам ближе: первое или второе?"
    ),
    "neutral_clarification": "Похоже, общение оставило неприятное впечатление. Что именно произошло?",
    "offer_question": (
        "Спрашивать бывает волнительно. Можно начать с короткого черновика. "
        "Хотите, помогу сформулировать вопрос?"
    ),
    "draft_question": (
        "Можно начать так: «Я пока не разобрался с задачей и хочу уточнить следующий шаг». "
        "Что именно хочется прояснить?"
    ),
    "small_step": (
        "Давайте начнём с малого: можно сделать короткую паузу и записать одну трудность. "
        "Что сейчас больше всего мешает?"
    ),
    "explore_feeling": "Давайте спокойно разберёмся. Что случилось перед тем, как стало тяжело?",
    "greeting": "Здравствуйте! Помогу разобраться с адаптацией и рабочими вопросами. С чего начнём?",
    "thanks": "Пожалуйста! Если появятся вопросы по адаптации, обращайтесь.",
    "role_boundary": (
        "Я помогаю с адаптацией и связанными с ней рабочими трудностями. "
        "Можем вернуться к вопросу о стажировке."
    ),
    "immediate_safety": (
        "Мне жаль, что вы оказались в такой ситуации. Если опасность непосредственная, "
        "обратитесь в местную экстренную службу и, если это безопасно, отойдите от того, "
        "что может причинить вред. Есть ли рядом человек, которого можно попросить побыть с вами?"
    ),
}

TEMPLATE_KINDS = {
    **{key: "support" for key in (
        "support_choices", "neutral_clarification", "offer_question", "draft_question", "small_step", "explore_feeling",
    )},
    "greeting": "social", "thanks": "social",
    "role_boundary": "off_topic", "immediate_safety": "immediate_danger",
}
SOURCE_FREE_TEXTS = frozenset(TEMPLATES.values())
EMPATHY_PREFIXES = {
    "difficulty": "Похоже, это отняло немало сил. Давайте разберёмся по шагам.",
    "worry": "Похоже, это вас тревожит. Давайте разберёмся спокойно.",
}
EmpathyID = Literal["difficulty", "worry"]


class TemplateAnswer(BaseModel):
    model_config = ConfigDict(extra="forbid", hide_input_in_errors=True)

    kind: Literal["support", "social", "off_topic", "immediate_danger"]
    template_id: Literal[
        "support_choices", "neutral_clarification", "offer_question", "draft_question", "small_step", "explore_feeling",
        "greeting", "thanks", "role_boundary", "immediate_safety",
    ]

    @model_validator(mode="after")
    def matching_kind(self):
        if TEMPLATE_KINDS[self.template_id] != self.kind:
            raise ValueError("Template does not match message kind.")
        return self


def needs_safety_routing(message: str) -> bool:
    """Keep potential danger out of deterministic onboarding shortcuts.

    This is only a conservative prefilter, never a classification or answer:
    the LLM still interprets negations, quotations and the complete context.
    """
    return bool(re.search(
        r"суицид|самоуб|уби[тйв]|убью|поконч|навред|вред|опасн|угро[зж]|насили|режу|порез|"
        r"прыгн|таблет|не хочу жить|suicid|kill|harm|danger|threat|overdose",
        message.casefold(),
    ))
