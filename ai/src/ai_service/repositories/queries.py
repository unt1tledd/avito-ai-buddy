"""Load bundled SQL once; only trusted SQL fragments are composed here.

Runtime values remain separate psycopg parameters. Search modes share the same
ACL and BM25 definitions; lexical recovery never selects the embedding column.
"""

import re
from importlib.resources import files


def _parse_queries(text: str) -> dict[str, str]:
    """Split bundled SQL at named sections without changing statement contents."""
    parts = re.split(r"^-- name: ([a-z][a-z0-9_]*)\r?\n", text, flags=re.MULTILINE)
    if parts[0].strip() or len(parts) == 1:
        raise ValueError("SQL resources must start with a named query section.")
    names, statements = parts[1::2], parts[2::2]
    if len(set(names)) != len(names) or any(not statement.strip() for statement in statements):
        raise ValueError("SQL section names must be unique and statements must be nonempty.")
    return dict(zip(names, statements, strict=True))


def _load_queries() -> dict[str, str]:
    directory = files("ai_service.repositories").joinpath("sql")
    queries: dict[str, str] = {}
    for group in ("search", "access", "import", "readiness", "context_cache"):
        sections = _parse_queries(directory.joinpath(f"{group}.sql").read_text(encoding="utf-8"))
        if queries.keys() & sections.keys():
            raise ValueError("SQL section names must be unique across resource files.")
        queries.update(sections)
    return queries


_QUERIES = _load_queries()


def _load_sql(name: str, **fragments: str) -> str:
    text = _QUERIES[name]
    return text.format_map(fragments) if fragments else text


DOCUMENT_ACCESS_SQL = _load_sql("document_access")
ACCESSIBLE_SOURCES_SQL = _load_sql("accessible_sources", document_access=DOCUMENT_ACCESS_SQL)
SEARCH_RESULT_SQL = _load_sql("search_result")


def _search_sql(name: str, *, include_embedding: bool) -> str:
    ctes = _load_sql(
        "search_ctes", document_access=DOCUMENT_ACCESS_SQL,
        embedding_column=_load_sql("embedding_column").rstrip() + " " if include_embedding else "",
    )
    return _load_sql(name, search_ctes=ctes, search_result=SEARCH_RESULT_SQL)


SEARCH_SQL = _search_sql("search", include_embedding=True)
LEXICAL_SEARCH_SQL = _search_sql("lexical_search", include_embedding=False)
READINESS_SCHEMA_SQL = _load_sql("readiness_schema")
READINESS_STATE_SQL = _load_sql("readiness_state")
LOCK_DOCUMENTS_SQL = _load_sql("lock_documents")
DELETE_DOCUMENTS_SQL = _load_sql("delete_documents")
INSERT_DOCUMENT_SQL = _load_sql("insert_document")
UPDATE_TOKEN_COUNTS_SQL = _load_sql("update_token_counts")
CONTEXT_CACHE_CREATE_SQL = _load_sql("context_cache_create")
CONTEXT_CACHE_GET_SQL = _load_sql("context_cache_get")
CONTEXT_CACHE_PUT_SQL = _load_sql("context_cache_put")
