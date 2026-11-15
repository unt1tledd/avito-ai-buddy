
    SELECT c.source_id AS id
    FROM document_chunks AS c
    JOIN documents AS d ON d.id = c.document_id
    WHERE c.embedding_profile = %(profile)s
      AND c.text_preparation_version = %(preparation)s
      AND c.source_id = ANY(%(source_ids)s::text[])
      AND {document_access}
