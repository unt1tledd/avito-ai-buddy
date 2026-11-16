-- +goose Up

ALTER TABLE document_chunks
    ALTER COLUMN embedding TYPE VECTOR,
    ADD COLUMN embedding_profile TEXT,
    ADD CONSTRAINT document_chunks_embedding_profile_check CHECK (
        (embedding_profile IS NULL AND vector_dims(embedding) = 1536)
        OR (
            embedding_profile IS NOT NULL
            AND embedding_profile ~ '^[a-f0-9]{64}$'
            AND vector_dims(embedding) IN (1024, 1536)
        )
    );

-- +goose Down

-- Legacy readers use the model name alone. Refuse to expose a newly indexed
-- space to them, even if its dimensions happen to match.
ALTER TABLE document_chunks
    ADD CONSTRAINT document_chunks_legacy_only CHECK (embedding_profile IS NULL);
ALTER TABLE document_chunks
    ALTER COLUMN embedding TYPE VECTOR(1536),
    DROP CONSTRAINT document_chunks_legacy_only,
    DROP CONSTRAINT document_chunks_embedding_profile_check,
    DROP COLUMN embedding_profile;
