from typing import Annotated

from pydantic import StringConstraints

from ..schemas.contracts import Payload


FIND_INSTRUCTION_NAME = "find_instruction"
FIND_INSTRUCTION_DESCRIPTION = (
    "Ищет доступные стажёру инструкции. Передавай ключевые слова, например «окружение»."
)
MAX_INSTRUCTION_QUERY_LENGTH = 400

InstructionQuery = Annotated[
    str, StringConstraints(strip_whitespace=True, min_length=1, max_length=MAX_INSTRUCTION_QUERY_LENGTH)
]


class FindInstructionArguments(Payload):
    query: InstructionQuery
