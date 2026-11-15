-- name: document_access

    EXISTS (
        SELECT 1 FROM document_access
        WHERE document_id = d.id AND principal_type = 'role' AND principal_id = %(role)s
    )
    AND (
        NOT EXISTS (
            SELECT 1 FROM document_access
            WHERE document_id = d.id AND principal_type = 'team'
        )
        OR EXISTS (
            SELECT 1 FROM document_access
            WHERE document_id = d.id AND principal_type = 'team' AND principal_id = %(team)s
        )
    )
-- name: accessible_sources

    SELECT c.source_id AS id
    FROM document_chunks AS c
    JOIN documents AS d ON d.id = c.document_id
    WHERE c.embedding_profile = %(profile)s
      AND c.text_preparation_version = %(preparation)s
      AND c.source_id = ANY(%(source_ids)s::text[])
      AND {document_access}
