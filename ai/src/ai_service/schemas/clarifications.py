"""Source-free questions whose complete text is controlled by the service."""


DEFAULT_CLARIFICATION = "Что именно вы хотите уточнить?"
CLARIFICATION_QUESTIONS = (
    DEFAULT_CLARIFICATION,
    "Какой именно доступ вам нужен?",
    "О каком шаге адаптации идёт речь?",
    "Какой шаг вы хотите подтвердить?",
    "К какой сети вы подключаетесь?",
    "Какой операционной системой вы пользуетесь?",
    "Что вы уже попробовали и на каком этапе возникла проблема?",
    "Уточните: VPN или офисный пропуск?",
    "Уточните: Windows, macOS или Linux?",
)


def safe_clarification(text: str) -> str:
    # A question mark or a model-provided kind cannot prove absence of claims.
    return text if text in CLARIFICATION_QUESTIONS else DEFAULT_CLARIFICATION
