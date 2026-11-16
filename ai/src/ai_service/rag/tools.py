from typing import Annotated

from pydantic import AfterValidator, StringConstraints

from ..schemas.contracts import Payload, valid_utf8

FIND_INSTRUCTION_NAME = "find_instruction"
FIND_INSTRUCTION_DESCRIPTION = (
    "Ищет доступные пользователю материалы с проверкой прав. Первый этап обязан вызвать поиск для каждого сообщения. "
    "Передавай самостоятельный query с учётом истории и текущих планов, исправляя очевидные опечатки, "
    "русский транслит и раскладку. Сохраняй все части вопроса, отрицания, названия, URL и идентификаторы. "
    "После результатов финальная модель может выполнить один дополнительный поиск. Повторять запросы не нужно."
)
MAX_INSTRUCTION_QUERY_LENGTH = 400

InstructionQuery = Annotated[
    str, StringConstraints(strip_whitespace=True, min_length=1, max_length=MAX_INSTRUCTION_QUERY_LENGTH),
    AfterValidator(valid_utf8),
]


class FindInstructionArguments(Payload):
    query: InstructionQuery
