import hashlib
import re

from ..schemas.contracts import MAX_CHUNK_TEXT, Document, DocumentChunk

CHUNK_OVERLAP = 150
MAX_IMPORT_CHUNKS = 4000
PARAGRAPH_END = re.compile(r"\r?\n(?:[ \t]*\r?\n)+")
SENTENCE_END = re.compile(r"[.!?…][\"»”')\]]*\s+")
WORD_END = re.compile(r"\s+")


def split_text(text: str) -> list[str]:
    """Keep the original text, preferring paragraph, sentence and word boundaries."""
    parts = []
    start = 0
    while start < len(text):
        end = min(start + MAX_CHUNK_TEXT, len(text))
        sentence_split = False
        if end < len(text):
            # Avoid tiny chunks when an early paragraph is followed by a long one.
            minimum = start + MAX_CHUNK_TEXT // 2
            for boundary in (PARAGRAPH_END, SENTENCE_END, WORD_END):
                matches = list(boundary.finditer(text, minimum, end))
                if matches:
                    end = matches[-1].end()
                    sentence_split = boundary is SENTENCE_END
                    break
        parts.append(text[start:end])
        if end == len(text):
            break
        previous_start = start
        start = end
        if sentence_split:
            # Repeat only complete sentences, never an arbitrary word suffix:
            # dropping the first word can turn "Не объявляйте" into "объявляйте".
            minimum_start = max(previous_start + 1, end - CHUNK_OVERLAP)
            for boundary in SENTENCE_END.finditer(text, previous_start, end):
                if minimum_start <= boundary.end() < end:
                    start = boundary.end()
                    break
        # A finished paragraph needs no overlap. Word/character fallback also
        # continues at end rather than copying a partial sentence.
    return parts


def chunk_id(document_id: str, part: int) -> str:
    digest = hashlib.sha256(f"{document_id}:{part}".encode()).hexdigest()[:12]
    suffix = f"-part-{part}-{digest}"
    return document_id[:64 - len(suffix)] + suffix


def chunk_documents(documents: list[Document]) -> list[DocumentChunk]:
    chunks = []
    ids = set()
    for document in documents:
        parts = split_text(document.text)
        for index, text in enumerate(parts, start=1):
            identifier = document.id if len(parts) == 1 else chunk_id(document.id, index)
            if identifier in ids:
                raise ValueError("ID полученных фрагментов конфликтуют; измените ID исходных документов.")
            ids.add(identifier)
            chunks.append(DocumentChunk.model_validate({
                **document.model_dump(), "id": identifier, "text": text, "original_document": document.text, "original_document_id": document.id,
            }))
            if len(chunks) > MAX_IMPORT_CHUNKS:
                raise ValueError(f"После разбиения база знаний превышает {MAX_IMPORT_CHUNKS} фрагментов.")
    return chunks
