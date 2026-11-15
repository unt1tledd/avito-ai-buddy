"""Public Russian messages are chosen at the API boundary, never from exception text."""

from ..clients.embeddings import EmbeddingError
from ..core.diagnostics import UpstreamError


DEFAULT_MESSAGES = {
    "model": "Модель временно недоступна.",
    "embeddings": "Не удалось получить эмбеддинги.",
    "knowledge": "Не удалось выполнить поиск в базе знаний.",
}
PUBLIC_MESSAGES = {
    "embeddings.invalid_indexes": "Сервис эмбеддингов вернул некорректные векторы.",
    "embeddings.invalid_response": "Не удалось получить эмбеддинги.",
    "embeddings.zero_vector": "Сервис эмбеддингов вернул некорректные векторы.",
    "knowledge.history_access_failed": "Не удалось проверить доступ к источникам истории.",
    "knowledge.import_failed": "Не удалось заменить базу знаний.",
    "knowledge.not_ready": "База знаний не готова.",
    "knowledge.outdated_index": (
        "Версия подготовки базы знаний устарела или содержит длинные фрагменты; выполните повторный импорт."
    ),
    "knowledge.profile_missing": "База знаний не проиндексирована для активного профиля.",
    "knowledge.reindex_required": "Требуется полный повторный импорт базы знаний.",
    "knowledge.search_failed": "Не удалось выполнить поиск в базе знаний.",
    "model.bad_status": "Модель не завершила обработку запроса.",
    "model.cache_failed": "Кеш контекстуализации недоступен; индекс не изменён.",
    "model.circuit_open": "Модель временно недоступна.",
    "model.context_failed": "Контекстуализация не завершена; индекс не изменён.",
    "model.context_limit": "Превышен бюджет контекста модели.",
    "model.context_timeout": "Истекло время контекстуализации; индекс не изменён.",
    "model.duplicate_call_id": "Модель вернула повторяющиеся идентификаторы вызовов инструментов.",
    "model.empty_answer": "Модель не вернула ответ.",
    "model.import_timeout": "Истекло время импорта; индекс не изменён.",
    "model.incomplete": "Модель вернула неполный ответ.",
    "model.invalid_answer": "Модель вернула некорректный формат итогового ответа.",
    "model.invalid_citation": "Модель сослалась на недоступный источник.",
    "model.invalid_clarification": "Уточнение должно совпадать с разрешённым вопросом.",
    "model.invalid_context": "Некорректный контекст фрагмента; индекс не изменён.",
    "model.invalid_proposal": "Предложение заявки допустимо только с подтверждённым ответом.",
    "model.invalid_response": "Модель вернула ответ в неизвестном формате.",
    "model.invalid_route": "Модель вернула недопустимое сочетание типа ответа и полей.",
    "model.invalid_tool_args": "Модель вернула некорректные аргументы инструмента.",
    "model.missing_citation": "Ответ модели не содержит подтверждающих источников.",
    "model.model_budget_exhausted": "Исчерпано время ожидания модели.",
    "model.refusal": "Модель отказалась отвечать на запрос.",
    "model.repair_tool_call": "Модель вызвала инструмент при исправлении ответа.",
    "model.search_required": "Модель завершила ответ без поиска по базе знаний.",
    "model.source_limit": "Модель превысила лимит источников ответа.",
    "model.timeout": "Истекло время ожидания модели.",
    "model.unexpected_call": "Модель не завершила обработку запроса.",
}


def public_error_message(error: UpstreamError) -> str:
    if isinstance(error, EmbeddingError) and error.code == "invalid_dimensions" and error.expected_dimensions is not None:
        return f"Сервис эмбеддингов должен возвращать векторы размерности {error.expected_dimensions}."
    return PUBLIC_MESSAGES.get(
        f"{error.domain}.{error.code}", DEFAULT_MESSAGES.get(error.domain, "Не удалось обработать запрос AI."),
    )
