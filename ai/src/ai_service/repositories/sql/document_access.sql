
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
