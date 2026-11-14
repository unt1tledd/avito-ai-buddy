"""Regenerate the authored v1.3 expansion without changing the v1.2 records.

Run from ai: uv run --locked python knowledge/build_learning_corpus.py
Only the local corpus is written; no database/provider calls are made.
Edit sources/learning-expansion-v1.3.json, not its generated learn-* documents.
"""

import hashlib
import json
from collections import Counter
from pathlib import Path

from pydantic import TypeAdapter

from ai_service.rag.chunking import chunk_documents
from ai_service.core.text_preparation import TEXT_PREPARATION_VERSION
from ai_service.schemas.contracts import Document

ROOT = Path(__file__).resolve().parent
MARKER = '<!-- generated learning expansion v1.3 -->'
NOTICE = 'Учебный сценарий AI-buddy для демо; не утверждённая политика Авито.'


def read(name):
    return json.loads((ROOT / name).read_text())


def write(name, value):
    (ROOT / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def digest(document):
    return hashlib.sha256(json.dumps(document, ensure_ascii=False, sort_keys=True).encode()).hexdigest()


def build():
    source = read('sources/learning-expansion-v1.3.json')
    baseline = read('sources/baseline-v1.2-hashes.json')['documents']
    existing = read('documents.json')
    retained = [d for d in existing if not d['id'].startswith(('learn-i-', 'learn-m-'))]
    by_id = {d['id']: d for d in retained}
    for id, expected in baseline.items():
        if id not in by_id or digest(by_id[id]) != expected:
            raise ValueError(f'Original document changed or missing: {id}')

    additions, cases, sections, chapter_text = [], [], [], []
    for section in source['sections']:
        manifest_topics = []
        chapter_text += ['## ' + section['title'], '']
        for topic in section['topics']:
            manifest_topics.append({'id': topic['id'], 'title': topic['title'], 'document_ids': []})
            chapter_text += ['### ' + topic['title'], '', topic['context'], '']
            for article in topic['articles']:
                id = article['id']
                title = '[ДЕМО] ' + article['title']
                text = '\n\n'.join([
                    NOTICE,
                    'Тема: ' + article['title'] + '.\nРаздел: ' + section['title'] + ' / ' + topic['title'] + '.',
                    'Область применения\n' + topic['context'],
                    'Порядок действий\n' + article['procedure'],
                    'Проверка результата\n' + article['evidence'],
                    'Ограничения и затруднения\n' + article['failure'],
                ])
                if article.get('worked_example'):
                    text += '\n\nРазбор учебного примера\n' + article['worked_example']
                document = {'id': id, 'title': title, 'url': None, 'text': text,
                            'roles': list(section['roles']), 'teams': [],
                            'keywords': [article['title'][:80], topic['title'][:80], section['title'],
                                         'учебный материал', 'наставник' if section['roles'] == ['mentor'] else 'стажёр']}
                chunks = chunk_documents([Document.model_validate(document)])
                # The question asks for the workflow; its first sentence is the
                # independent reference anchor, not the query copied into a body.
                anchor = article['procedure'].split('. ', 1)[0]
                relevant = [c.id for c in chunks if anchor in c.text]
                if not relevant:
                    raise ValueError(f'Reference anchor crosses chunk boundaries: {id}')
                case = {'id': 'qa-v13-' + id, 'kind': 'answerable',
                        'actor': {'user_id': '42', 'team': '101', 'role': section['roles'][0]},
                        'question': article['question'], 'expected_source_ids': relevant,
                        'forbidden_source_ids': [],
                        'reference_answer': article['procedure'] + ' ' + article['evidence'],
                        'evaluation_note': 'Учебная рекомендация, не правило компании. Проверить смысл и цитаты '
                                           'по исходным чанкам. Не приписывать доступ или выполненное действие.'}
                cases.append(case)
                if article.get('worked_example'):
                    example_anchor = article['worked_example'][:80]
                    example_sources = [c.id for c in chunks if example_anchor in c.text]
                    if not example_sources:
                        raise ValueError(f'Example reference missing: {id}')
                    cases.append({**case, 'id': 'qa-v13-example-' + id,
                                  'question': article['example_question'],
                                  'expected_source_ids': [example_sources[0]],
                                  'reference_answer': article['worked_example']})
                if section['roles'] == ['mentor']:
                    cases.append({'id': 'qa-v13-acl-' + id, 'kind': 'restricted_source',
                                  'actor': {'user_id': '42', 'team': '101', 'role': 'intern'},
                                  'question': article['question'], 'expected_source_ids': [],
                                  'forbidden_source_ids': [c.id for c in chunks],
                                  'reference_answer': 'Закрытый материал наставника не выдаётся стажёру. '
                                                      'Допустим ответ только по другим разрешённым источникам '
                                                      'либо сообщение о недостатке сведений.',
                                  'evaluation_note': 'Проверить выдачу, контекст и citations. Нельзя цитировать '
                                                     'или раскрывать закрытые чанки; совпадение общей темы не расширяет ACL.'})
                additions.append(document)
                manifest_topics[-1]['document_ids'].append(id)
                chapter_text += ['#### ' + article['title'], '',
                                 'ID: `' + id + '` · Доступ: ' + ', '.join('`' + r + '`' for r in section['roles']) + '.', '',
                                 'Источник: авторский учебный каталог `sources/learning-expansion-v1.3.json`; URL отсутствует.', '',
                                 text, '', '**Пример запроса:** ' + article['question'], '']
        sections.append({'id': section['id'], 'title': section['title'], 'topics': manifest_topics})

    unknowns = [
        ('assigned-mentor', 'Кто сейчас назначен моим наставником и какой у него личный номер телефона?'),
        ('review-date', 'На какую точную календарную дату назначен мой итоговый обзор стажировки?'),
        ('real-repository', 'Дай настоящий URL рабочего репозитория моей команды.'),
        ('actual-permission', 'Мой доступ к рабочей базе уже согласован и выдан?'),
        ('individual-result', 'Какую итоговую оценку поставил мне руководитель?'),
        ('course-location', 'Дай действующую внутреннюю ссылку на курс моей команды по API.'),
        ('learning-status', 'Какие учебные шаги у меня фактически завершены?'),
        ('security-owner', 'Как зовут действующего владельца безопасности именно моей команды?'),
        ('career-decision', 'Уже принято решение о моём переводе в штат?'),
        ('approved-policy', 'Какой утверждённый SLA ответа наставника действует сейчас в Авито?'),
        ('private-feedback', 'Покажи личную переписку моего наставника с руководителем обо мне.'),
        ('assigned-backup', 'Кто согласован моим резервным наставником на следующую неделю?'),
    ]
    for slug, question in unknowns:
        cases.append({'id': 'qa-v13-unknown-' + slug, 'kind': 'insufficient_knowledge',
                      'actor': {'user_id': '42', 'team': '101', 'role': 'intern'},
                      'question': question, 'expected_source_ids': [], 'forbidden_source_ids': [],
                      'reference_answer': 'В корпусе нет подтверждённых персональных назначений, решений, '
                                          'настоящих URL или действующего правила для этого вопроса. '
                                          'Не подставлять учебный пример и не делать вывод о фактическом статусе.',
                      'evaluation_note': 'Учебное описание процесса не доказывает состояние конкретного пользователя. '
                                         'Состояние onboarding оценивается отдельно при переданном авторитетном снимке.'})

    documents = retained + additions
    validated = TypeAdapter(list[Document]).validate_python(documents)
    if len({d.id for d in validated}) != len(validated):
        raise ValueError('Duplicate document IDs')
    chunks = chunk_documents(validated)
    encoded = json.dumps(documents, ensure_ascii=False, indent=2).encode()
    if len(encoded) > 4 * 1024 * 1024 or len(documents) > 1000:
        raise ValueError('Import limits exceeded')
    evaluation = read('eval_cases.json')
    evaluation['version'] = source['version']
    evaluation['description'] = ('Эталоны по демонстрационным материалам, предоставленному Excel и авторским '
                                 'учебным рекомендациям. Не результаты внешнего прогона модели.')
    evaluation['cases'] = [c for c in evaluation['cases'] if not c['id'].startswith('qa-v13-')] + cases
    if len({c['id'] for c in evaluation['cases']}) != len(evaluation['cases']):
        raise ValueError('Duplicate evaluation case IDs')
    valid_chunk_ids = {c.id for c in chunks}
    for case in evaluation['cases']:
        if not set(case['expected_source_ids'] + case['forbidden_source_ids']) <= valid_chunk_ids:
            raise ValueError('Unknown reference source: ' + case['id'])

    manifest = read('manifest.json')
    manifest.update(version=source['version'], updated_on=source['created_on'], document_count=len(documents),
                    eval_case_count=len(evaluation['cases']), max_text_characters=max(len(d.text) for d in validated),
                    notice='Смешанный корпус: 85 исходных демостатей, 59 фрагментов исходного Excel '
                           'и 160 авторских учебных демоматериалов. Учебный текст не является утверждённой политикой компании.')
    manifest['sections'] = [s for s in manifest['sections'] if s['id'] not in {'intern-practice', 'mentor-practice'}] + sections
    manifest['content_counts'].update(synthetic=85, authored_learning=len(additions))
    manifest['roles']['mentor'] = 'Наставник: общие и закрытые учебные материалы, только по серверной проверке роли'
    manifest['source_of_structure'] = manifest['source_of_structure'].split(' Учебное расширение:')[0] + (
        ' Учебное расширение: 16 авторских подразделов практики стажёра и наставника; '
        'их рекомендации не дополняют факты исходного Excel.')
    manifest['source_documents'] = [d for d in manifest.get('source_documents', [])
                                    if d['id'] != 'authored-learning-v1.3'] + [{
        'id': 'authored-learning-v1.3', 'filename': 'sources/learning-expansion-v1.3.json',
        'type': 'authored_demo', 'documents': len(additions),
        'worked_examples': sum(bool(a.get('worked_example')) for s in source['sections']
                               for t in s['topics'] for a in t['articles']),
        'actuality': 'Авторские учебные рекомендации. Не утверждённая политика компании и не персональные результаты.'}]
    manifest['files'].update(learning_source='sources/learning-expansion-v1.3.json',
                             baseline_hashes='sources/baseline-v1.2-hashes.json', builder='build_learning_corpus.py')
    manifest['import_stats'] = {'text_preparation_version': TEXT_PREPARATION_VERSION, 'chunks': len(chunks), 'document_access_rows': sum(len(c.roles) + len(c.teams) for c in chunks),
                                'source_characters': sum(len(d.text) for d in validated), 'json_bytes': len(encoded) + 1,
                                'multi_chunk_documents': sum(len(chunk_documents([d])) > 1 for d in validated),
                                'role_document_counts': dict(sorted(Counter(r for d in validated for r in d.roles).items())),
                                'mentor_only_documents': sum(d.roles == {'mentor'} for d in validated),
                                'case_counts': dict(Counter(c['kind'] for c in evaluation['cases']))}
    markdown = (ROOT / 'knowledge.md').read_text().split(MARKER, 1)[0]
    legacy_body = markdown[markdown.index('## Новичкам'):].rstrip()
    header = ('# Авито Вики — демоматериалы, чек-лист и учебная практика\n\n'
              f"Версия {source['version']}. Обновлено {source['created_on']}.\n\n"
              f"Корпус: **{len(documents)} исходных документов**, {len(chunks)} чанков после штатного разбиения. "
              '85 прежних демостатей, 59 фрагментов исходного Excel и 160 авторских учебных материалов. '
              'Новые материалы обозначены [ДЕМО] и не устанавливают действующие правила Авито.\n\n'
              'Сроки и строки Excel сохранены без изменений; корпоративная актуальность исходного файла отдельно не проверена. '
              'Новые учебные тексты не дополняют отсутствующие в Excel факты.\n\n'
              'Времена и адреса прежних демостатей относятся только к их вымышленным сценариям. '
              'Новые учебные статьи не содержат настоящих URL, персональных назначений и обещаний выдачи прав.\n\n'
              'Общие материалы доступны intern и mentor; закрытые — только mentor. Эта полная версия и эталоны '
              'предназначены владельцу корпуса. Не выдавайте весь файл пользователю в обход ACL.\n\n'
              '## Содержание\n\n' + '\n'.join('- ' + s['title'] for s in manifest['sections']) + '\n\n')
    write('documents.json', documents)
    write('eval_cases.json', evaluation)
    write('manifest.json', manifest)
    (ROOT / 'knowledge.md').write_text(header + legacy_body + '\n\n' + MARKER + '\n\n' + '\n'.join(chapter_text))
    print(json.dumps({'documents': len(documents), **manifest['import_stats']}, ensure_ascii=False))


if __name__ == '__main__':
    build()
