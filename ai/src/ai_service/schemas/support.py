"""Internal routing decisions and complete, source-free service texts.

These models are never part of the HTTP or history contract. No user or model
text is interpolated into the catalogue, including the question-writing aid.
"""

import re
from collections.abc import Sequence
from importlib.resources import files
from typing import Annotated, Literal, get_args

from pydantic import BaseModel, ConfigDict, Field, StringConstraints, model_validator

from .contracts import Message


TemplateID = Literal[
    "support_choices", "neutral_clarification", "offer_question", "draft_question", "small_step", "explore_feeling",
    "greeting", "thanks", "role_boundary", "immediate_safety",
]
EmpathyID = Literal["difficulty", "worry"]
TEMPLATE_KINDS = {
    **{key: "support" for key in (
        "support_choices", "neutral_clarification", "offer_question", "draft_question", "small_step", "explore_feeling",
    )},
    "greeting": "social", "thanks": "social",
    "role_boundary": "off_topic", "immediate_safety": "immediate_danger",
}
CatalogueText = Annotated[str, StringConstraints(strict=True, min_length=1, max_length=400)]
Variants = Annotated[tuple[CatalogueText, ...], Field(min_length=1)]


class SupportCatalogue(BaseModel):
    model_config = ConfigDict(extra="forbid", hide_input_in_errors=True)

    templates: dict[TemplateID, Variants]
    empathy: dict[EmpathyID, Variants]

    @model_validator(mode="after")
    def complete_catalogue(self):
        if set(self.templates) != set(get_args(TemplateID)) or set(self.empathy) != set(get_args(EmpathyID)):
            raise ValueError("The support catalogue must contain every template and empathy ID.")
        for variants in (*self.templates.values(), *self.empathy.values()):
            if len(set(variants)) != len(variants):
                raise ValueError("Catalogue variants must be unique within each ID.")
            if any(text != text.strip() or not text.strip() or "[source:" in text for text in variants):
                raise ValueError("Catalogue texts must be nonblank, trimmed and source-free.")
        if len(self.templates["immediate_safety"]) != 1:
            raise ValueError("The immediate safety response must have exactly one variant.")
        return self


# Validate bundled resources once, before serving requests. The first wording
# remains the canonical response for existing consumers and empty histories.
_catalogue = SupportCatalogue.model_validate_json(
    files("ai_service.schemas").joinpath("resources").joinpath("support.json").read_text(encoding="utf-8")
)
TEMPLATE_VARIANTS = _catalogue.templates
EMPATHY_VARIANTS = _catalogue.empathy
TEMPLATES = {key: variants[0] for key, variants in TEMPLATE_VARIANTS.items()}
EMPATHY_PREFIXES = {key: variants[0] for key, variants in EMPATHY_VARIANTS.items()}
SOURCE_FREE_TEXTS = frozenset(text for variants in TEMPLATE_VARIANTS.values() for text in variants)


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
    template_id: TemplateID

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
