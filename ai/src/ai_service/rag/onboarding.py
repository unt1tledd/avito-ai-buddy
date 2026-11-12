"""Select onboarding tracks and answer snapshot questions without inventing state."""

import re
import tomllib
from collections.abc import Sequence
from importlib.resources import files
from unicodedata import normalize

from ..schemas.clarifications import CLARIFICATION_QUESTIONS, ONBOARDING_TRACK_CLARIFICATION
from ..schemas.contracts import ChatResponse, Message, OnboardingPlan


TRACK_ALIASES = tomllib.loads(
    files("ai_service.rag").joinpath("track_aliases.toml").read_text(encoding="utf-8")
)


# This is intentionally a vocabulary for bare follow-ups, not an extractor of names.
# Any extra qualification must resolve in the current question, never in old history.
BARE_REFERENCE_WORDS = frozenset(
    "а и ну как где что мне мой у меня пожалуйста скажи подскажи помоги помогите нужно надо "
    "с со по его это этот этого этому этом этим текущий текущего текущему текущем текущим "
    "следующий следующего следующему следующем следующим шаг шага шагу шаге шагом "
    "этап этапа этапу этапе этапом трек трека треку треке треком адаптации онбординга "
    "сделать делать выполнить выполнением пройти прохождением завершить завершением "
    "подтвердить подтверждением начать закончить продолжить дальше сейчас теперь продолжим".split()
)
STEP_NOUN = re.compile(r"\b(?:шаг(?:а|у|е|ом)?|этап(?:а|у|е|ом)?|трек(?:а|у|е|ом)?)\b")
POINTER = re.compile(r"\b(?:эт\w*|текущ\w*|следующ\w*|мо[йеюя])\b")
FOLLOWUP = re.compile(
    r"\b(?:(?:что|как) (?:мне )?(?:делать )?дальше|"
    r"что (?:мне )?делать (?:сейчас|теперь)|продолжим|"
    r"как (?:мне )?(?:его|это) (?:сделать|выполнить|завершить|подтвердить))\b"
)
RUSSIAN_ENDINGS = tuple(sorted({
    "иями", "ями", "ами", "иях", "ях", "ах", "ыми", "ими", "ого", "его", "ому", "ему",
    "ий", "ый", "ой", "ая", "яя", "ое", "ее", "ые", "ие", "ую", "юю", "ым", "им", "ом", "ем",
    "ов", "ев", "ей", "ам", "ям", "а", "я", "ы", "и", "у", "ю", "е", "о",
}, key=lambda ending: (-len(ending), ending)))


def normalized(text: str) -> str:
    return " ".join(re.findall(r"[\w-]+", normalize("NFKC", text).casefold().replace("ё", "е")))


def title_tokens(text: str) -> str:
    """Compare full titles in common Russian cases; never stem IDs or resource IDs."""
    result = []
    for word in normalized(text).split():
        if re.fullmatch(r"(?:трек|шаг|этап)(?:а|у|е|ом|и|ов|ам|ах|ами)?", word):
            word = re.match(r"трек|шаг|этап", word)[0]
        elif re.fullmatch("[а-я]+", word):
            for ending in RUSSIAN_ENDINGS:
                if word.endswith(ending) and len(word) - len(ending) >= 4:
                    word = word[:-len(ending)]
                    break
        result.append(word)
    return " ".join(result)


def mentioned_plans(text: str, plans: Sequence[OnboardingPlan]) -> list[OnboardingPlan]:
    exact_text = f" {normalized(text)} "
    inflected_text = f" {title_tokens(text)} "
    # A track name has priority over a possibly shared step or resource.
    for level in ("track", "step", "resource"):
        matches = []
        for plan in plans:
            step = plan.current_step
            if level == "track":
                identifier, title = plan.id, plan.title
            elif level == "step" and step is not None:
                identifier, title = step.id, step.title
            elif level == "resource" and step is not None:
                identifier, title = step.resource_id, None
            else:
                continue
            aliases = TRACK_ALIASES.get(plan.id, ()) if level == "track" else ()
            titles = (title, *aliases)
            if ((identifier and f" {normalized(identifier)} " in exact_text)
                    or any(name and f" {title_tokens(name)} " in inflected_text for name in titles)):
                matches.append(plan)
        if matches:
            return matches
    return []


def bare_reference(text: str) -> bool:
    words = normalized(text).split()
    return bool(words) and all(word in BARE_REFERENCE_WORDS for word in words)


def step_reference(text: str, plans: Sequence[OnboardingPlan]) -> bool:
    text = normalized(text)
    if FOLLOWUP.search(text) and bare_reference(text):
        return True
    if not STEP_NOUN.search(text):
        return False
    # An explicitly unrelated technical topic, e.g. a step in GitLab CI, is not
    # an onboarding reference merely because the word "step" occurs in it.
    return bool(POINTER.search(text) or mentioned_plans(text, plans)
                or re.search(r"\b(?:адаптац\w*|онбординг\w*)\b", text)
                or STEP_NOUN.fullmatch(text.split()[-1]))


def historical_choice(plans: Sequence[OnboardingPlan], history: Sequence[Message]) -> OnboardingPlan | None:
    for previous in reversed(history[-30:]):
        if previous.role == "assistant":
            if previous.content in CLARIFICATION_QUESTIONS:
                break
            continue
        mentions = mentioned_plans(previous.content, plans)
        if mentions:
            return mentions[0] if len(mentions) == 1 else None
        if not bare_reference(previous.content):
            break
    return None


def resolve_step_reference(
    message: str, plans: Sequence[OnboardingPlan], history: Sequence[Message],
) -> tuple[bool, OnboardingPlan | None]:
    if not step_reference(message, plans):
        return False, None
    mentions = mentioned_plans(message, plans)
    if mentions:
        return len(mentions) != 1, mentions[0] if len(mentions) == 1 else None
    active = [plan for plan in plans if plan.current_step is not None]
    # Only a bare continuation may reuse a choice. Unknown names, negations,
    # or destinations in the current request must never inherit an old track.
    destination = re.search(r"\b(?:в|для|по|про|из|о|об)\b", normalized(message))
    if not bare_reference(message) and (len(active) > 1 or destination):
        return True, None
    if len(active) <= 1:
        return False, active[0] if active else None
    selected = historical_choice(plans, history)
    return selected is None, selected


def snapshot_question(message: str) -> str | None:
    """Recognize state questions, keeping instruction requests on the RAG path."""
    text = normalized(message)
    # A compound question must not silently lose its instruction part.
    if re.search(r"\b(?:помоги|помогите|помощь|инструкц\w*|парол\w*|срок\w*|команд\w*|ошибк\w*)\b", text):
        return None
    if "как" in text.split() and not re.search(r"\bкак (?:идет|продвигается)\b", text):
        return None
    onboarding = re.search(r"\b(?:шаг\w*|этап\w*|трек\w*|адаптац\w*|онбординг\w*)\b", text)
    if not onboarding:
        return None
    if re.search(r"\b(?:сколько (?:шагов|этапов|треков)|процент\w*|прогресс\w*|"
                 r"(?:выполнен|завершен)(?:ы|а|о|ные|ных|ного|ном|ному)?)\b", text):
        return "progress"
    state = re.search(
        r"\b(?:какой (?:у меня |мой )?(?:сейчас |текущий |следующий |назначенный )?(?:шаг|этап|трек)|"
        r"какие (?:у меня |мои )?(?:сейчас |текущие |назначенные )?(?:шаги|этапы|треки)|"
        r"на каком (?:я |мы )?(?:сейчас )?(?:шаге|этапе)|"
        r"(?:покажи|покажите|назови|перечисли|список) (?:мои |мне |все |текущие |текущий |назначенные )*"
        r"(?:шаг\w*|этап\w*|трек\w*)|"
        r"(?:статус|состояние) (?:моей |моего )?(?:адаптации|онбординга)|"
        r"как (?:идет|продвигается) (?:моя |мой )?(?:адаптация|онбординг))\b", text,
    )
    if state or re.fullmatch(r"(?:мой |мои |текущий |текущие )+(?:шаг|шаги|этап|трек|треки)", text):
        return "next" if re.search(r"\bследующ\w*\b", text) else "current"
    return None


def display_title(title: str) -> str:
    # Quote snapshot data and escape Markdown so titles cannot become fake links
    # or citations. This path never executes instructions embedded in titles.
    return "«" + re.sub(r"([\\`*_{}\[\]()<>])", r"\\\1", " ".join(title.split())) + "»"


def snapshot_answer(
    message: str, plans: Sequence[OnboardingPlan],
) -> ChatResponse | None:
    kind = snapshot_question(message)
    if kind is None:
        return None
    mentions = mentioned_plans(message, plans)
    # A non-bare question about a named but unknown track cannot borrow history
    # or be answered by listing a different track as if it were requested.
    target_text = re.sub(r"\b(?:по|в) (?:адаптаци\w*|онбординг\w*)\b", "", normalized(message))
    if not mentions and re.search(r"\b(?:в|для|по|из|про|о)\b|\bтрек\w* [a-z0-9_-]+\b", target_text):
        return ChatResponse(answer=ONBOARDING_TRACK_CLARIFICATION, sources=[])
    selected = mentions or list(plans)
    if not selected:
        return ChatResponse(answer="Chat не передал сведения о треках адаптации. Проверьте план в интерфейсе адаптации.", sources=[])
    lines = []
    for plan in selected:
        prefix = f"Трек {display_title(plan.title)}: "
        if plan.current_step is None:
            lines.append(prefix + "текущий шаг в снимке не указан.")
        else:
            lines.append(prefix + f"текущий шаг — {display_title(plan.current_step.title)}.")
    if kind == "progress":
        lines.insert(0, "В снимке нет числа завершённых шагов и общего числа шагов, поэтому процент прогресса и остаток посчитать нельзя.")
    elif kind == "next":
        lines.insert(0, "В снимке есть только текущие шаги; порядок следующих шагов не передан.")
    return ChatResponse(answer="\n".join(lines), sources=[])
