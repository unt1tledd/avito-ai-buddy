
                UPDATE document_chunks c SET token_count = stats.length
                FROM (SELECT chunk_id, sum(tf)::integer AS length FROM chunk_terms GROUP BY chunk_id) stats
                WHERE c.id = stats.chunk_id
