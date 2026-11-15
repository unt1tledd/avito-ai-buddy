
    WITH index_state AS (
        SELECT count(*) > 0 AS present,
               coalesce(bool_and(text_preparation_version IS NOT DISTINCT FROM %(preparation)s
                                 AND search_text <> ''), false) AS prepared
        FROM document_chunks WHERE embedding_profile = %(profile)s
    ), eligible AS MATERIALIZED (
        SELECT c.id AS chunk_id, c.source_id AS id, d.title, d.url, c.body AS text,
               d.access_policies, {embedding_column}c.token_count
        FROM document_chunks AS c JOIN documents AS d ON d.id = c.document_id
        WHERE c.embedding_profile = %(profile)s
          AND c.text_preparation_version = %(preparation)s
          AND {document_access}
    ), corpus AS (
        SELECT count(*)::float8 AS n, avg(token_count)::float8 AS avgdl FROM eligible
    ), query_terms AS (
        SELECT term FROM knowledge_terms(%(query)s)
    ), matches AS (
        SELECT e.chunk_id, e.id, e.token_count, p.tf, corpus.n, corpus.avgdl,
               count(*) OVER (PARTITION BY p.term)::float8 AS df
        FROM query_terms q JOIN chunk_terms p ON p.term = q.term
        JOIN eligible e ON e.chunk_id = p.chunk_id CROSS JOIN corpus
    ), bm25_scores AS (
        SELECT id, sum(
            ln(1 + (n - df + 0.5) / (df + 0.5)) *
            (tf * (%(bm25_k1)s + 1)) /
            (tf + %(bm25_k1)s * (1 - %(bm25_b)s + %(bm25_b)s * token_count / nullif(avgdl, 0)))
        ) AS score
        FROM matches GROUP BY id
    ), lexical AS (
        SELECT id, score FROM bm25_scores WHERE score > 0
        ORDER BY score DESC, id COLLATE "C" LIMIT %(candidates)s
    )
