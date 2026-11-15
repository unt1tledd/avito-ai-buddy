
                        SELECT vector_dims(c.embedding), c.source_id, c.body, d.title, d.url, d.access_policies, a.principal_id
                        FROM document_chunks AS c
                        JOIN documents AS d ON d.id = c.document_id
                        JOIN document_access AS a ON a.document_id = d.id
                        WHERE a.principal_type = 'role' LIMIT 0
