
    SELECT index_state.present, index_state.prepared, e.id, e.title, e.url, e.text, e.access_policies
    FROM index_state LEFT JOIN (fused JOIN eligible e USING (id)) ON true
    ORDER BY fused.score DESC, e.id COLLATE "C"
