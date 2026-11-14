"""Corpus-level checks: source preservation, ACL, provenance and usable citations."""

import importlib.util
import json
import shutil
from collections import Counter
from pathlib import Path

from pydantic import TypeAdapter

from ai_service.rag.chunking import chunk_documents
from ai_service.rag.import_documents import MAX_IMPORT_BYTES, MAX_IMPORT_DOCUMENTS
from ai_service.schemas.contracts import Document, MAX_CHUNK_TEXT
from test_rag import pg_schema

ROOT = Path(__file__).resolve().parents[1] / 'knowledge'


def load(name):
    return json.loads((ROOT / name).read_text())


def test_complete_corpus_matches_manifest_and_import_limits():
    raw = load('documents.json')
    documents = TypeAdapter(list[Document]).validate_python(raw)
    manifest = load('manifest.json')
    chunks = chunk_documents(documents)
    assert len({d.id for d in documents}) == len(documents) == manifest['document_count']
    assert len(documents) <= MAX_IMPORT_DOCUMENTS
    assert (ROOT / 'documents.json').stat().st_size <= MAX_IMPORT_BYTES
    assert len({c.id for c in chunks}) == len(chunks) == manifest['import_stats']['chunks']
    assert all(0 < len(c.text) <= MAX_CHUNK_TEXT for c in chunks)
    assert sum(len(c.roles) + len(c.teams) for c in chunks) == manifest['import_stats']['document_access_rows']
    assert sum(manifest['content_counts'].values()) == len(documents)
    parents = {d.id: d for d in documents}
    for chunk in chunks:
        parent = parents[chunk.original_document_id]
        assert chunk.text in parent.text
        assert chunk.roles == parent.roles and chunk.teams == parent.teams
        assert chunk.generated_context == ''  # Generated only by the real importer.
    assert sum(len(chunk_documents([d])) > 1 for d in documents) == manifest['import_stats']['multi_chunk_documents']
    listed = [id for section in manifest['sections'] for topic in section['topics'] for id in topic['document_ids']]
    assert set(listed) == set(parents) and all(count == 1 for count in Counter(listed).values())


def test_authored_learning_has_explicit_origin_and_preserves_role_boundary():
    source = load('sources/learning-expansion-v1.3.json')
    documents = {d['id']: d for d in load('documents.json')}
    assert source['origin'] == 'authored_demo'
    catalog_ids = set()
    for section in source['sections']:
        for topic in section['topics']:
            for article in topic['articles']:
                doc = documents[article['id']]
                catalog_ids.add(article['id'])
                assert doc['roles'] == section['roles']
                assert doc['title'].startswith('[ДЕМО]') and 'не утверждённая политика' in doc['text']
                assert doc['url'] is None and not doc.get('access_policies') and not doc['teams']
                for field in ('procedure', 'evidence', 'failure'):
                    assert article[field] in doc['text']
                if article.get('worked_example'):
                    assert article['worked_example'] in doc['text']
    assert catalog_ids == {id for id in documents if id.startswith(('learn-i-', 'learn-m-'))}
    assert all(d['roles'] == ['mentor'] for id, d in documents.items() if id.startswith('learn-m-'))


def test_eval_sources_are_actual_chunks_and_respect_actor_acl():
    documents = TypeAdapter(list[Document]).validate_python(load('documents.json'))
    chunks = {c.id: c for c in chunk_documents(documents)}
    evaluation = load('eval_cases.json')
    manifest = load('manifest.json')
    assert len({case['id'] for case in evaluation['cases']}) == len(evaluation['cases']) == manifest['eval_case_count']
    assert dict(Counter(c['kind'] for c in evaluation['cases'])) == manifest['import_stats']['case_counts']
    for case in evaluation['cases']:
        actor = case['actor']
        if case['kind'] == 'answerable':
            assert case['expected_source_ids']
        for id in case['expected_source_ids']:
            chunk = chunks[id]
            assert actor['role'] in chunk.roles
            assert not chunk.teams or actor['team'] in chunk.teams
        for id in case['forbidden_source_ids']:
            chunk = chunks[id]
            assert actor['role'] not in chunk.roles or (chunk.teams and actor['team'] not in chunk.teams)
    # Each new closed article has an explicit intern-denial case for ALL its chunks.
    cases = {case['id']: case for case in evaluation['cases']}
    for document in documents:
        if document.id.startswith('learn-m-'):
            acl_case = cases['qa-v13-acl-' + document.id]
            assert set(acl_case['forbidden_source_ids']) == {c.id for c in chunk_documents([document])}
            assert acl_case['actor']['role'] == 'intern'


def test_expansion_builder_is_idempotent_and_rejects_changed_baseline(tmp_path, monkeypatch):
    spec = importlib.util.spec_from_file_location('learning_builder', ROOT / 'build_learning_corpus.py')
    builder = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(builder)
    for name in ('documents.json', 'eval_cases.json', 'manifest.json', 'knowledge.md',
                 'sources/learning-expansion-v1.3.json', 'sources/baseline-v1.2-hashes.json'):
        target = tmp_path / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / name, target)
    monkeypatch.setattr(builder, 'ROOT', tmp_path)
    outputs = ['documents.json', 'eval_cases.json', 'manifest.json', 'knowledge.md']
    before = {name: (tmp_path / name).read_bytes() for name in outputs}
    builder.build()
    assert {name: (tmp_path / name).read_bytes() for name in outputs} == before
    builder.build()
    assert {name: (tmp_path / name).read_bytes() for name in outputs} == before
    raw = json.loads((tmp_path / 'documents.json').read_text())
    raw[0]['text'] += 'Unexpected edit'
    (tmp_path / 'documents.json').write_text(json.dumps(raw, ensure_ascii=False))
    import pytest
    with pytest.raises(ValueError, match='Original document changed'):
        builder.build()


def test_expanded_corpus_round_trip_and_role_acl(pg_schema):
    import asyncio
    import psycopg
    from ai_service.repositories.knowledge import KnowledgeRepository
    from ai_service.schemas.contracts import Actor
    from test_rag import ACTOR, PROFILE, VECTOR, migrate, pool_for

    migrate(pg_schema)
    documents = TypeAdapter(list[Document]).validate_python(load('documents.json'))
    chunks = chunk_documents(documents)
    by_id = {c.id: c for c in chunks}
    closed = {c.id for c in chunks if c.roles == {'mentor'}}
    mentor = Actor(user_id='mentor-check', role='mentor', team='101')
    stats = load('manifest.json')['import_stats']

    async def run():
        async with pool_for(pg_schema) as pool:
            repository = KnowledgeRepository(pg_schema, pool)
            # Deliberately controlled test vectors; these are never published as
            # production embeddings. Lexical queries exercise the actual corpus.
            far = [0.0, 1.0, *VECTOR[2:]]
            await repository.replace(chunks, [far] * len(chunks), 'corpus-test', PROFILE)
            await repository.check_ready(PROFILE)
            assert await repository.accessible_source_ids(closed, PROFILE, ACTOR) == set()
            assert await repository.accessible_source_ids(closed, PROFILE, mentor) == closed
            for question in ['Дозирование подсказок без готового решения',
                             'Недостаточно данных для оценки навыка',
                             'Передача результата стажировки команде',
                             'Оценка собственной доступности наставника']:
                for actor in (ACTOR, mentor):
                    hits = (await repository.search(VECTOR, PROFILE, actor, .9, query=question)).hits
                    if actor.role == 'mentor':
                        assert hits
                    for hit in hits:
                        assert actor.role in by_id[hit.id].roles
                        assert hit.text == by_id[hit.id].text
            with psycopg.connect(pg_schema) as db:
                assert db.execute('SELECT count(*) FROM document_chunks').fetchone()[0] == stats['chunks']
                assert db.execute('SELECT count(*) FROM document_access').fetchone()[0] == stats['document_access_rows']
                for source_id, body, original, context in db.execute('''
                    SELECT c.source_id, c.body, d.original_body, c.generated_context
                    FROM document_chunks c JOIN documents d ON c.document_id = d.id
                ''').fetchall():
                    assert body == by_id[source_id].text
                    assert original == by_id[source_id].original_document
                    assert context == ''
    asyncio.run(run())
