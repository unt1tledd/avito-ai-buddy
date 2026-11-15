{search_ctes}
    , fused AS (
        SELECT id, score FROM lexical
        ORDER BY score DESC, id COLLATE "C" LIMIT %(limit)s
    )
{search_result}
