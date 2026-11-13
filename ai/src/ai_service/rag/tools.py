from typing import Annotated

from pydantic import StringConstraints

from ..schemas.contracts import Payload


FIND_INSTRUCTION_NAME = "find_instruction"
FIND_INSTRUCTION_DESCRIPTION = (
    "Ищет доступные пользователю инструкции с проверкой прав. Автоматического поиска до модели нет: "
    "для понятного вопроса вызови до фактического ответа или no_knowledge. Передавай самостоятельный запрос "
    "с учётом истории и выбранного шага, исправляя очевидные опечатки, русский транслит и раскладку. "
    "Сохраняй смысл, отрицания, названия сервисов и идентификаторы. Если смысл неоднозначен, уточни вопрос. "
    "После достаточного результата отвечай без новых поисков. При непустой выдаче о другой теме "
    "сделай одно уточнение перед no_knowledge; выполненные запросы не повторяй."
)
MAX_INSTRUCTION_QUERY_LENGTH = 400

InstructionQuery = Annotated[
    str, StringConstraints(strip_whitespace=True, min_length=1, max_length=MAX_INSTRUCTION_QUERY_LENGTH)
]


class FindInstructionArguments(Payload):
    query: InstructionQuery
