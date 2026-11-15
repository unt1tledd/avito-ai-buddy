
                        SELECT count(*) AS chunks,
                               coalesce(bool_or(char_length(body) > %s), false) AS oversized,
                               coalesce(bool_and(text_preparation_version IS NOT DISTINCT FROM %s
                                                 AND search_text <> ''), false) AS prepared
                        FROM document_chunks WHERE embedding_profile = %s
