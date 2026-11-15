{search_ctes}
    , semantic AS (
        SELECT id, embedding <=> %(vector)s::vector AS distance
        FROM eligible
        WHERE embedding <=> %(vector)s::vector <= %(max_distance)s
        ORDER BY distance, id COLLATE "C" LIMIT %(candidates)s
    ), votes AS (
        SELECT id, %(bm25_weight)s::float8 / (%(rrf_k)s + row_number() OVER (ORDER BY score DESC, id COLLATE "C")) AS score
        FROM lexical
        UNION ALL
        SELECT id, %(vector_weight)s::float8 / (%(rrf_k)s + row_number() OVER (ORDER BY distance, id COLLATE "C")) AS score
        FROM semantic
    ), fused AS (
        SELECT id, sum(score) AS score FROM votes GROUP BY id
        ORDER BY score DESC, id COLLATE "C" LIMIT %(limit)s
    )
{search_result}
