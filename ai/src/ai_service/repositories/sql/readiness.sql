-- name: readiness_schema

                        SELECT vector_dims(c.embedding), c.source_id, c.body, d.title, d.url, d.access_policies, a.principal_id
                        FROM document_chunks AS c
                        JOIN documents AS d ON d.id = c.document_id
                        JOIN document_access AS a ON a.document_id = d.id
                        WHERE a.principal_type = 'role' LIMIT 0
-- name: readiness_state

                        SELECT count(*) AS chunks,
                               coalesce(bool_or(char_length(body) > %s), false) AS oversized,
                               coalesce(bool_and(text_preparation_version IS NOT DISTINCT FROM %s
                                                 AND search_text <> ''), false) AS prepared
                        FROM document_chunks WHERE embedding_profile = %s
