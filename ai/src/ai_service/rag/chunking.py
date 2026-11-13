import hashlib
import re

from ..schemas.contracts import Document, DocumentChunk, MAX_CHUNK_TEXT


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
        if end < len(text):
            # Avoid tiny chunks when an early paragraph is followed by a long one.
            minimum = start + MAX_CHUNK_TEXT // 2
            for boundary in (PARAGRAPH_END, SENTENCE_END, WORD_END):
                matches = list(boundary.finditer(text, minimum, end))
                if matches:
                    end = matches[-1].end()
                    break
        parts.append(text[start:end])
        if end == len(text):
            break
        next_start = end - CHUNK_OVERLAP
        # Start at a complete word where possible; unbroken strings use a hard cut.
        boundary = WORD_END.search(text, next_start, end)
        start = boundary.end() if boundary else next_start
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
