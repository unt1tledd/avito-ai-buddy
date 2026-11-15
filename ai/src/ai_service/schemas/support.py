"""Internal routing decisions and complete, source-free service texts.

These models are never part of the HTTP or history contract. No user or model
text is interpolated into the catalogue, including the question-writing aid.
"""

import re
from collections.abc import Sequence
from typing import Literal

from pydantic import BaseModel, ConfigDict, model_validator

from .contracts import Message


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
TEMPLATE_VARIANTS = {
    "support_choices": (
        TEMPLATES["support_choices"],
        "Похоже, сейчас непросто. Можно сначала поговорить о том, что случилось, "
        "или наметить небольшой следующий шаг. Что вам ближе: первое или второе?",
        "Давайте без спешки. Можем сначала обсудить, что вас беспокоит, "
        "или выбрать одно небольшое действие. С чего начнём: с первого или второго?",
        "Можно двигаться в удобном вам темпе. Сначала разберём, что случилось, "
        "или найдём маленький следующий шаг. Что сейчас полезнее: первое или второе?",
    ),
    "neutral_clarification": (
        TEMPLATES["neutral_clarification"],
        "Кажется, разговор вас задел. Можете рассказать, что произошло?",
        "Давайте разберём саму ситуацию. Что в общении оказалось для вас неприятным?",
        "Хочется лучше понять, что случилось. Как прошёл этот разговор?",
    ),
    "offer_question": (
        TEMPLATES["offer_question"],
        "Бывает непросто решиться на вопрос. Можно сначала набросать его в черновике. Помочь с формулировкой?",
        "Можно начать с пары простых фраз. Хотите, вместе подготовим черновик вопроса?",
        "Необязательно сразу подбирать идеальные слова. Попробуем сначала составить короткий вопрос?",
    ),
    "draft_question": (
        TEMPLATES["draft_question"],
        "Например: «Хочу уточнить, как двигаться дальше с задачей». Что именно нужно прояснить?",
        "Вот начало: «Мне нужна небольшая подсказка по следующему шагу». О чём хотите спросить?",
        "Можно написать: «Помогите, пожалуйста, разобраться со следующим действием по задаче». Что хочется уточнить?",
    ),
    "small_step": (
        TEMPLATES["small_step"],
        "Можно ненадолго остановиться и выбрать одну трудность, с которой начнём. Что сейчас мешает больше всего?",
        "Давайте выберем что-то небольшое: сделать паузу и записать, где возникла трудность. На чём вы остановились?",
        "Попробуйте пока выписать один момент, который не получается. С чем хотелось бы разобраться первым?",
    ),
    "explore_feeling": (
        TEMPLATES["explore_feeling"],
        "Можем спокойно это обсудить. После какого момента вы почувствовали себя так?",
        "Расскажите чуть подробнее. Что произошло перед тем, как стало тяжело?",
        "Давайте начнём с того, что случилось. Что вас больше всего задело?",
    ),
    "greeting": (
        TEMPLATES["greeting"],
        "Здравствуйте! Как идут дела со стажировкой?",
        "Привет! Давайте разберёмся с вопросами по адаптации. Что сейчас занимает больше всего?",
        "Рад помочь с адаптацией и рабочими вопросами. С чем нужна помощь?",
    ),
    "thanks": (
        TEMPLATES["thanks"],
        "Рад помочь! Если что-то ещё по стажировке будет непонятно, пишите.",
        "Пожалуйста! Обращайтесь, если понадобится разобраться со следующим шагом.",
        "Пожалуйста, удачи с задачами! Будут вопросы по адаптации — пишите.",
    ),
    "role_boundary": (
        TEMPLATES["role_boundary"],
        "С этим вряд ли помогу. А вот с вопросами по стажировке — да. Что сейчас нужно разобрать?",
        "Моя специализация — адаптация и рабочие вопросы. Есть что-то по стажировке, с чем нужна помощь?",
        "Давайте вернёмся к стажировке. Как у вас сейчас идут дела с задачами?",
        "Я могу помочь с адаптацией и трудностями на стажировке. С чем из этого сейчас нужна помощь?",
    ),
    "immediate_safety": (TEMPLATES["immediate_safety"],),
}
SOURCE_FREE_TEXTS = frozenset(text for variants in TEMPLATE_VARIANTS.values() for text in variants)
EMPATHY_PREFIXES = {
    "difficulty": "Похоже, это отняло немало сил. Давайте разберёмся по шагам.",
    "worry": "Похоже, это вас тревожит. Давайте разберёмся спокойно.",
}
EMPATHY_VARIANTS = {
    "difficulty": (
        EMPATHY_PREFIXES["difficulty"],
        "Похоже, с этим пришлось повозиться. Давайте разберёмся по порядку.",
        "Такие сложности могут утомлять. Попробуем пройти по шагам.",
        "Понимаю, что хочется уже с этим разобраться. Начнём с ближайшего шага.",
    ),
    "worry": (
        EMPATHY_PREFIXES["worry"],
        "Понимаю ваше беспокойство. Давайте спокойно посмотрим, что можно сделать.",
        "Похоже, эта ситуация вызывает волнение. Разберёмся по порядку.",
        "Когда что-то непонятно, бывает тревожно. Давайте начнём с конкретных шагов.",
    ),
}
EmpathyID = Literal["difficulty", "worry"]


def _select_variant(variants: tuple[str, ...], history: Sequence[Message], *, prefix: bool = False) -> str:
    """Prefer an unseen text, then the least recently used one in allowed history.

    Ties keep catalogue order, preserving the original wording without history.
    User messages never count as a service reply. Prefixes match only at the
    start of a complete answer, separated from its working part by a blank line.
    """
    last_seen = dict.fromkeys(variants, -1)
    for index, message in enumerate(history):
        if message.role != "assistant":
            continue
        for text in variants:
            if message.content == text or (prefix and message.content.startswith(text + "\n\n")):
                last_seen[text] = index
    return min(variants, key=last_seen.__getitem__)


def template_text(template_id: str, history: Sequence[Message] = ()) -> str:
    return _select_variant(TEMPLATE_VARIANTS[template_id], history)


def empathy_prefix(empathy_id: EmpathyID, history: Sequence[Message] = ()) -> str:
    return _select_variant(EMPATHY_VARIANTS[empathy_id], history, prefix=True)


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
