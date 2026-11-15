-- name: lock_documents
LOCK TABLE documents IN SHARE ROW EXCLUSIVE MODE
-- name: delete_documents
DELETE FROM documents
-- name: insert_document

                WITH new_document AS (
                    INSERT INTO documents (title, url, access_policies, keywords, original_body) VALUES (%s, %s, %s::jsonb, %s, %s)
                    RETURNING id
                ), new_chunk AS (
                    INSERT INTO document_chunks (document_id, source_id, body, generated_context, search_text,
                                                 text_preparation_version, embedding_model, embedding_profile, embedding)
                    SELECT id, %s, %s, %s, %s, %s, %s, %s, %s::vector FROM new_document
                    RETURNING id, search_text
                ), postings AS (
                    INSERT INTO chunk_terms (chunk_id, term, tf)
                    SELECT id, terms.term, terms.tf FROM new_chunk
                    CROSS JOIN LATERAL knowledge_terms(search_text) AS terms
                )
                INSERT INTO document_access (document_id, principal_type, principal_id)
                SELECT d.id, p.principal_type, p.principal_id
                FROM new_document AS d
                CROSS JOIN (
                    SELECT 'role' AS principal_type, unnest(%s::text[]) AS principal_id
                    UNION ALL
                    SELECT 'team', unnest(%s::text[])
                ) AS p
-- name: update_token_counts

                UPDATE document_chunks c SET token_count = stats.length
                FROM (SELECT chunk_id, sum(tf)::integer AS length FROM chunk_terms GROUP BY chunk_id) stats
                WHERE c.id = stats.chunk_id
