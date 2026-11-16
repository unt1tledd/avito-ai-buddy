"""Opt-in external evaluation on the SAME corpus/questions in three disposable schemas.

Requires --live; sends knowledge/documents.json and eval_cases.json to configured
providers. Never writes to production corpus tables. Run with AI_TEST_POSTGRES_DSN.
Prices are optional USD per million tokens; absent prices produce null, not zero.
"""

import argparse
import asyncio
import hashlib
import json
import os
from importlib.resources import files
from pathlib import Path
from statistics import median
from time import perf_counter
from uuid import uuid4

import httpx
import psycopg
from psycopg import sql
from psycopg.conninfo import make_conninfo
from psycopg.rows import dict_row
from psycopg_pool import AsyncConnectionPool
from pydantic import TypeAdapter

from ai_service.agents.agent import Agent
from ai_service.clients.embeddings import Embeddings
from ai_service.clients.llm import ResponsesModel
from ai_service.core.config import Settings, resolve_embedding_profile
from ai_service.core.diagnostics import UpstreamError
from ai_service.core.text_preparation import TEXT_PREPARATION_VERSION
from ai_service.rag.chunking import chunk_documents
from ai_service.rag.import_documents import import_documents
from ai_service.rag.retriever import Retriever
from ai_service.repositories.knowledge import KnowledgeRepository
from ai_service.schemas.contracts import Actor, Document

ROOT = Path(__file__).resolve().parents[1]


def cost(stats, args):
    if not stats:
        return None
    context, embedding = stats['context'], stats['embedding']
    if (not context['usage_complete'] or not embedding['usage_complete'] or args.embedding_usd is None
            or (context['calls'] and (args.llm_input_usd is None or args.llm_output_usd is None))):
        return None
    return (embedding['input_tokens'] * args.embedding_usd +
            context['input_tokens'] * (args.llm_input_usd or 0) +
            context['output_tokens'] * (args.llm_output_usd or 0)) / 1_000_000


def percentiles(samples):
    values = sorted(samples)
    return {'p50_ms': round(median(values), 2), 'p95_ms': round(values[min(len(values)-1, int(.95 * len(values)))], 2)}


async def evaluate(args):
    if not args.live:
        raise ValueError('External evaluation requires explicit --live.')
    settings = Settings.from_env()
    if not settings.knowledge_processing_approved:
        raise ValueError('KNOWLEDGE_PROCESSING_APPROVED must be true.')
    dsn = os.environ['AI_TEST_POSTGRES_DSN']
    source = ROOT / 'knowledge/documents.json'
    questions = ROOT / 'knowledge/eval_cases.json'
    documents = TypeAdapter(list[Document]).validate_json(source.read_bytes())
    cases = json.loads(questions.read_text())['cases']
    report = {'text_preparation_version': TEXT_PREPARATION_VERSION, 'corpus_sha256': hashlib.sha256(source.read_bytes()).hexdigest(),
              'questions_sha256': hashlib.sha256(questions.read_bytes()).hexdigest(),
              'documents': len(documents), 'questions': len(cases),
              'llm_model': settings.llm_model, 'embedding_model': settings.embedding_model,
              'llm_endpoint': settings.llm_base_url.rstrip('/'),
              'embedding_endpoint': resolve_embedding_profile(settings).url,
              'embedding_provider': resolve_embedding_profile(settings).provider,
              'method': 'Exact baseline SQL and legacy embedding text; hybrid raw queries; live agent for insufficient_knowledge.',
              'limitations': ['Retrieval metrics use original questions without agent reformulation.',
                             'USD estimates require explicit current provider prices; cached context costs zero new generation.',
                             'One run; loopback database; retrieval latency excludes query embeddings.'], 'modes': {}}
    if args.output.exists():
        previous = json.loads(args.output.read_text())
        if previous['corpus_sha256'] != report['corpus_sha256'] or previous['questions_sha256'] != report['questions_sha256']:
            raise ValueError('Existing report uses a different corpus/questions; choose a new output path.')
        for field in ('text_preparation_version', 'llm_model', 'embedding_model', 'llm_endpoint', 'embedding_endpoint', 'embedding_provider'):
            if previous.get(field) != report[field]:
                raise ValueError('Existing report uses different providers/models/text preparation; choose a new output path.')
        report['modes'] = previous['modes']
    args.output.parent.mkdir(parents=True, exist_ok=True)

    def save():
        args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')

    # Shared query vectors make retrieval comparisons paired and eliminate API noise.
    async with httpx.AsyncClient(timeout=60) as http:
        query_embeddings = Embeddings(http, resolve_embedding_profile(settings))
        query_vectors = []
        for start in range(0, len(cases), 32):
            query_vectors.extend(await query_embeddings.embed([c['question'] for c in cases[start:start+32]]))
            print(f'Query vectors: {len(query_vectors)}/{len(cases)}', flush=True)
        for mode, contextual in [('current_vector', False), ('hybrid_plain', False), ('hybrid_context', True)]:
            if mode not in args.modes:
                continue
            schema = 'ai_hybrid_eval_' + uuid4().hex
            scoped = make_conninfo(dsn, options=f'-csearch_path={schema},public')
            config = settings.model_copy(update={
                'knowledge_db_dsn': scoped, 'contextualization_enabled': contextual,
                'retrieval_mode': 'vector' if mode == 'current_vector' else 'hybrid',
                'context_cache_path': str(ROOT / '.data/hybrid-eval-context.sqlite3'),
            })
            result = {'status': 'running', 'context_concurrency': config.context_concurrency,
                      'context_min_interval_seconds': config.context_min_interval_seconds}
            report['modes'][mode] = result
            async with await psycopg.AsyncConnection.connect(dsn, autocommit=True) as admin:
                await admin.execute('CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public')
                await admin.execute(sql.SQL('CREATE SCHEMA {}').format(sql.Identifier(schema)))
                try:
                    async with await psycopg.AsyncConnection.connect(scoped) as db:
                        for migration in sorted((ROOT / 'migrations').glob('*.sql')):
                            await db.execute(migration.read_text().split('-- +goose Down')[0])
                    if mode == 'current_vector':
                        # Reproduce old title + original text + keywords order exactly.
                        chunks = chunk_documents(documents)
                        embeddings = Embeddings(http, resolve_embedding_profile(config))
                        started = perf_counter()
                        vectors = []
                        for start in range(0, len(chunks), 32):
                            vectors.extend(await embeddings.embed([Document.embedding_text(c) for c in chunks[start:start+32]]))
                        await KnowledgeRepository(scoped).replace(chunks, vectors, embeddings.model, embeddings.profile)
                        imported = {'duration_seconds': perf_counter()-started,
                                    'embedding': {'calls': embeddings.calls, 'input_tokens': embeddings.input_tokens,
                                                  'usage_complete': embeddings.usage_complete},
                                    'context': {'calls': 0, 'cache_hits': 0, 'input_tokens': 0,
                                                'output_tokens': 0, 'usage_complete': True}}
                    else:
                        imported = await import_documents(source, config)
                    result['import'] = imported
                    result['estimated_import_usd'] = cost(imported, args)
                    profile = resolve_embedding_profile(config).index_id
                    async with AsyncConnectionPool(scoped, open=False, min_size=1, max_size=4,
                                                   kwargs={'row_factory': dict_row, 'autocommit': True}) as pool:
                        repo = KnowledgeRepository(scoped, pool, config)
                        retriever = Retriever(repo, Embeddings(http, resolve_embedding_profile(config)), config.knowledge_min_similarity)
                        measurements, recall, reciprocal, acl_failures, details = [], [], [], [], []
                        for case, vector in zip(cases, query_vectors, strict=True):
                            actor = Actor.model_validate(case['actor'])
                            started = perf_counter()
                            if mode == 'current_vector':
                                # Original SQL: ACL before nearest top-3 and cosine cutoff.
                                from ai_service.repositories.queries import (
                                    DOCUMENT_ACCESS_SQL,
                                )
                                baseline = f'''SELECT c.source_id AS id FROM document_chunks c
                                    JOIN documents d ON d.id=c.document_id
                                    WHERE c.embedding_profile=%(profile)s AND {DOCUMENT_ACCESS_SQL}
                                    AND c.embedding <=> %(vector)s::vector <= %(max_distance)s
                                    ORDER BY c.embedding <=> %(vector)s::vector, c.source_id COLLATE "C" LIMIT 3'''
                                async with pool.connection() as db:
                                    cursor = await db.execute(baseline, {'profile': profile, 'role': actor.role, 'team': actor.team,
                                                               'vector': json.dumps(vector), 'max_distance': 1-config.knowledge_min_similarity})
                                    ids = [row['id'] for row in await cursor.fetchall()]
                            else:
                                ids = [hit.id for hit in (await repo.search(vector, profile, actor, config.knowledge_min_similarity,
                                                                           query=case['question'])).hits]
                            measurements.append((perf_counter()-started)*1000)
                            expected = set(case.get('expected_source_ids', []))
                            if case['kind'] == 'answerable':
                                recall.append(len(expected.intersection(ids)) / len(expected))
                                reciprocal.append(next((1/r for r, id in enumerate(ids, 1) if id in expected), 0))
                            leaked = set(case.get('forbidden_source_ids', [])).intersection(ids)
                            if leaked:
                                acl_failures.append(case['id'])
                            details.append({'id': case['id'], 'source_ids': ids})
                        result['retrieval'] = {'answerable': len(recall), 'recall_at_3': sum(recall)/len(recall),
                                               'mrr_at_3': sum(reciprocal)/len(reciprocal),
                                               'acl_violations': acl_failures, **percentiles(measurements), 'cases': details}
                        result['no_knowledge'] = []
                        prompt = files('ai_service.agents').joinpath('prompts/system.txt').read_text()
                        agent = Agent(ResponsesModel(http, config.llm_base_url, config.llm_api_key, config.llm_model), retriever, prompt)
                        for case in cases:
                            if case['kind'] != 'insufficient_knowledge':
                                continue
                            started = perf_counter()
                            try:
                                async with asyncio.timeout(config.chat_timeout_seconds):
                                    reply = await agent.answer(case['question'], Actor.model_validate(case['actor']),
                                                               deadline=asyncio.get_running_loop().time()+config.chat_timeout_seconds)
                                outcome = {'answer': reply.answer, 'source_ids': [s.id for s in reply.sources],
                                           'source_free_response': not reply.sources and reply.proposal is None}
                            except (UpstreamError, TimeoutError) as error:
                                outcome = {'error': getattr(error, 'reason', 'timeout'), 'correct_abstention': False}
                            result['no_knowledge'].append({'id': case['id'], 'latency_ms': round((perf_counter()-started)*1000), **outcome})
                            save()
                    result['status'] = 'partial' if any('error' in c for c in result['no_knowledge']) else 'complete'
                except (UpstreamError, TimeoutError) as error:
                    result.update(status='unavailable', reason=getattr(error, 'reason', 'timeout'))
                finally:
                    await admin.execute(sql.SQL('DROP SCHEMA {} CASCADE').format(sql.Identifier(schema)))
                    save()
            print(json.dumps({'mode': mode, 'status': result['status'],
                              'retrieval': {k:v for k,v in result.get('retrieval', {}).items() if k != 'cases'}}, ensure_ascii=False), flush=True)
    save()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--live', action='store_true')
    parser.add_argument('--output', type=Path, default=ROOT/'evals/hybrid-search.json')
    parser.add_argument('--modes', nargs='+', choices=['current_vector', 'hybrid_plain', 'hybrid_context'],
                        default=['current_vector', 'hybrid_plain', 'hybrid_context'])
    parser.add_argument('--llm-input-usd', type=float)
    parser.add_argument('--llm-output-usd', type=float)
    parser.add_argument('--embedding-usd', type=float)
    args = parser.parse_args()
    asyncio.run(evaluate(args))
