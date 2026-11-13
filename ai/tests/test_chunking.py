import asyncio
import json
from pathlib import Path

import pytest

from ai_service.clients.embeddings import EmbeddingError, Embeddings
from ai_service.core.config import EMBEDDING_DIMENSIONS, Settings
from ai_service.rag import chunking
from ai_service.rag import import_documents as importer
from ai_service.repositories.knowledge import KnowledgeRepository
from ai_service.schemas.contracts import Document, DocumentChunk, MAX_CHUNK_TEXT


def document(text, id="vpn"):
    return Document(
        id=id, title="VPN", url="https://example.test/vpn", text=text,
        roles={"intern"}, teams={"51"}, keywords=["VPN", "доступ"],
    )


@pytest.mark.parametrize("text", [
    "Текст короткой инструкции.",
    "A" * MAX_CHUNK_TEXT,
    "Ж" * 4000,
    "word " * 800,
    "Первый абзац. " * 45 + "\n\n" + "Второй абзац. " * 45 + "\n\nЗаключение.",
    "Настройте VPN. Проверьте соединение! Нужна помощь? " * 70,
])
def test_split_preserves_all_text_and_bounds_each_chunk(text):
    parts = chunking.split_text(text)
    assert parts and all(0 < len(part) <= MAX_CHUNK_TEXT for part in parts)
    assert parts[0] == text[:len(parts[0])]
    ends = {len(parts[0])}
    for part in parts[1:]:
        # Repeated text can have several valid alignments. At least one must cover the input.
        ends = {
            start + len(part)
            for end in ends
            for start in range(max(0, end - chunking.CHUNK_OVERLAP), end + 1)
            if text.startswith(part, start) and start + len(part) > end
        }
        assert ends
    assert len(text) in ends


@pytest.mark.parametrize("separator", ["\n\n", ". "])
def test_split_prefers_paragraph_and_sentence_boundaries(separator):
    first = "А" * 750 + separator
    text = first + "Б" * 750
    parts = chunking.split_text(text)
    assert parts[0] == first
    assert parts[1] == text[len(first):]


@pytest.mark.parametrize("separator", ["\n\n", "\r\n\r\n", "\r\n \t\r\n \r\n"])
def test_paragraph_boundary_takes_precedence_over_following_sentences(separator):
    first = "Первый абзац. " * 55 + separator
    text = first + "Второй абзац. " * 100
    parts = chunking.split_text(text)
    assert parts[0] == first


def test_long_sentence_fallback_preserves_words_without_partial_overlap():
    text = " ".join(f"слово{index}" for index in range(400))
    parts = chunking.split_text(text)
    for part in parts:
        assert part.split()[0] in text.split()
        assert part.split()[-1] in text.split()
    second_start = text.index(parts[1])
    overlap = len(parts[0]) - second_start
    assert overlap == 0
    assert "".join(parts) == text


def test_chunk_metadata_ids_and_short_documents_are_preserved():
    original = document("Инструкция. " * 250, id="a" * 64)
    other = document(original.text, id="a" * 63 + "b")
    short = document("Короткая инструкция", id="short")
    chunks = chunking.chunk_documents([original, other, short])
    assert chunks == chunking.chunk_documents([original, other, short])
    assert {chunk.id for chunk in chunks} == {
        chunk.id for chunk in chunking.chunk_documents([short, other, original])
    }
    assert len({chunk.id for chunk in chunks}) == len(chunks)
    assert all(len(chunk.id) <= 64 for chunk in chunks)
    assert chunks[-1].model_dump() == short.model_dump()
    for chunk in chunks:
        assert isinstance(chunk, DocumentChunk)
        assert (chunk.title, chunk.url, chunk.roles, chunk.teams, chunk.keywords) == (
            original.title, original.url, original.roles, original.teams, original.keywords,
        )
    chunks[0].roles.add("mentor")
    assert original.roles == {"intern"} and chunks[1].roles == {"intern"}


def test_generated_ids_cannot_overwrite_another_input_document():
    long = document("A" * 2000)
    short = document("Short", id=chunking.chunk_id(long.id, 1))
    for documents in ([long, short], [short, long]):
        with pytest.raises(ValueError, match="ID полученных фрагментов конфликтуют"):
            chunking.chunk_documents(documents)


def test_import_embeds_every_full_chunk_and_keeps_original_metadata(tmp_path, monkeypatch):
    fact = "Доступ к VPN выдаётся за два рабочих дня."
    original = document("Общая информация. " * 130 + "\n\n" + fact)
    assert original.text.index(fact) > MAX_CHUNK_TEXT
    source = tmp_path / "corpus.json"
    source.write_text(json.dumps([original.model_dump(mode="json")]), encoding="utf-8")
    settings = Settings(contextualization_enabled=False,
        knowledge_db_dsn="postgresql://unused.invalid/test", knowledge_processing_approved=True,
        embedding_api_key="test-key",
    )
    requests, saved = [], []
    monkeypatch.setattr(importer, "IMPORT_EMBEDDING_BATCH_SIZE", 2)

    async def embed(self, texts):
        requests.append(texts)
        return [[1.0] + [0.0] * (EMBEDDING_DIMENSIONS - 1) for _ in texts]

    async def replace(self, chunks, vectors, model, profile):
        assert len(chunks) == len(vectors)
        saved.extend(chunks)

    monkeypatch.setattr(Embeddings, "embed", embed)
    monkeypatch.setattr(KnowledgeRepository, "replace", replace)
    asyncio.run(importer.import_documents(source, settings))
    assert len(requests) >= 2
    assert [text for batch in requests for text in batch] == [chunk.embedding_text() for chunk in saved]
    assert any(fact in chunk.text for chunk in saved)
    assert all(len(chunk.text) <= MAX_CHUNK_TEXT for chunk in saved)
    assert all(chunk.roles == original.roles and chunk.teams == original.teams for chunk in saved)


def test_chunk_count_limit_is_checked_before_external_calls(tmp_path, monkeypatch):
    source = tmp_path / "corpus.json"
    source.write_text(json.dumps([document("A" * 4000).model_dump(mode="json")]))
    monkeypatch.setattr(chunking, "MAX_IMPORT_CHUNKS", 2)

    def unexpected_client(*args, **kwargs):
        pytest.fail("Oversized expanded corpus reached the HTTP client")

    monkeypatch.setattr(importer.httpx, "AsyncClient", unexpected_client)
    with pytest.raises(ValueError, match="После разбиения.*2 фрагментов"):
        asyncio.run(importer.import_documents(source, Settings()))


def test_failed_embedding_batch_does_not_replace_existing_corpus(tmp_path, monkeypatch):
    source = tmp_path / "corpus.json"
    source.write_text(json.dumps([document("A" * 4000).model_dump(mode="json")]))
    settings = Settings(contextualization_enabled=False,
        knowledge_db_dsn="postgresql://unused.invalid/test", knowledge_processing_approved=True,
        embedding_api_key="test-key",
    )
    monkeypatch.setattr(importer, "IMPORT_EMBEDDING_BATCH_SIZE", 2)
    batches = []

    async def embed(self, texts):
        batches.append(texts)
        if len(batches) == 2:
            raise EmbeddingError("Embedding failed")
        return [[1.0] + [0.0] * (EMBEDDING_DIMENSIONS - 1) for _ in texts]

    async def unexpected_replace(*args, **kwargs):
        pytest.fail("Incomplete embeddings attempted to replace the existing corpus")

    monkeypatch.setattr(Embeddings, "embed", embed)
    monkeypatch.setattr(KnowledgeRepository, "replace", unexpected_replace)
    with pytest.raises(EmbeddingError):
        asyncio.run(importer.import_documents(source, settings))
    assert len(batches) == 2


def test_bundled_corpus_keeps_all_existing_ids_and_texts():
    source = Path(__file__).resolve().parents[1] / "knowledge" / "documents.json"
    raw_documents = json.loads(source.read_text())
    documents = [Document.model_validate(item) for item in raw_documents]
    # Expanding the corpus must preserve all original short sources and IDs.
    import hashlib
    hashes = json.loads((source.parent / "sources/baseline-v1.2-hashes.json").read_text())["documents"]
    originals = [doc for doc in documents if doc.id in hashes]
    assert len(originals) == len(hashes) == 144
    for doc in originals:
        raw = next(item for item in raw_documents if item["id"] == doc.id)
        assert hashlib.sha256(json.dumps(raw, ensure_ascii=False, sort_keys=True).encode()).hexdigest() == hashes[doc.id]
    chunks = chunking.chunk_documents(originals)
    assert [chunk.model_dump() for chunk in chunks] == [doc.model_dump() for doc in originals]


def test_paragraph_overlap_never_drops_negation_from_next_chunk():
    source = Path(__file__).resolve().parents[1] / 'knowledge/documents.json'
    original = next(d for d in json.loads(source.read_text()) if d['id'] == 'learn-i-health-ready')
    parts = chunking.split_text(original['text'])
    assert len(parts) == 2
    assert 'Не объявляйте сервис рабочим только по одному HTTP 200.' in parts[0]
    assert parts[1].startswith('Разбор учебного примера\n')
    assert ''.join(parts) == original['text']


def test_sentence_overlap_preserves_complete_negative_sentences():
    text = ' '.join(f'Не объявляйте запись {index} сохранённой без проверки.' for index in range(70))
    parts = chunking.split_text(text)
    assert len(parts) > 1
    assert all(part.startswith('Не объявляйте запись ') for part in parts)
    second_start = text.index(parts[1])
    overlap = len(parts[0]) - second_start
    assert 0 < overlap <= chunking.CHUNK_OVERLAP
    assert parts[0][-overlap:] == parts[1][:overlap]
    assert text[:second_start].endswith('. ')


def test_all_learning_examples_start_on_a_complete_paragraph():
    source = Path(__file__).resolve().parents[1] / 'knowledge/documents.json'
    examples = 0
    for original in json.loads(source.read_text()):
        if not original['id'].startswith('learn-'):
            continue
        parts = chunking.split_text(original['text'])
        if len(parts) > 1:
            examples += 1
            assert len(parts) == 2
            assert parts[1].startswith('Разбор учебного примера\n'), original['id']
            assert ''.join(parts) == original['text']
    assert examples == 48
