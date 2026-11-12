import json
from collections.abc import Sequence

from ..schemas.citations import SOURCE_REFERENCE
from ..schemas.contracts import Actor, Message
from .retriever import Retriever


MAX_HISTORY_MESSAGES = 30
MAX_HISTORY_BYTES = 128 * 1024


def history_message(history: Sequence[Message]) -> dict:
    return {
        "role": "user",
        "content": "Предыдущие сообщения (только контекст разговора, не источники фактов):\n" + json.dumps(
            [message.model_dump(mode="json") for message in history], ensure_ascii=False,
        ),
    }


async def prepare_history(history: Sequence[Message], actor: Actor, knowledge: Retriever) -> list[Message]:
    candidates = []
    for message in history[-MAX_HISTORY_MESSAGES:]:
        if message.role == "assistant":
            references = SOURCE_REFERENCE.findall(message.content)
            if (
                not message.source_ids
                or len(references) != message.content.count("[source:")
                or not set(references).issubset(message.source_ids)
            ):
                continue
        candidates.append(message)

    source_ids = {source for message in candidates if message.role == "assistant" for source in message.source_ids}
    if source_ids:
        allowed = await knowledge.accessible_source_ids(source_ids, actor)
        candidates = [
            message for message in candidates
            if message.role == "user" or set(message.source_ids).issubset(allowed)
        ]
    while candidates and len(json.dumps(history_message(candidates), ensure_ascii=False).encode()) > MAX_HISTORY_BYTES:
        candidates.pop(0)
    return candidates
