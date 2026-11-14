-- +goose Up

-- Native PostgreSQL BM25 postings, not ts_rank. ts_debug supplies Russian
-- stemming without tsvector's 256 positions/term frequency truncation.
-- +goose StatementBegin
CREATE FUNCTION knowledge_terms(input_text text)
RETURNS TABLE(term text, tf integer)
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT term, count(*)::integer AS tf
    FROM (
        SELECT unnest(CASE WHEN token ~ '[A-Za-z0-9]'
                           THEN ARRAY[lower(token)] ELSE lexemes END) AS term
        FROM ts_debug('pg_catalog.russian'::regconfig, input_text)
        WHERE lexemes IS NOT NULL AND cardinality(lexemes) > 0
    ) AS tokens
    GROUP BY term
$$;
-- +goose StatementEnd

ALTER TABLE documents
    ADD COLUMN keywords text[] NOT NULL DEFAULT '{}',
    ADD COLUMN original_body text NOT NULL DEFAULT '';
ALTER TABLE document_chunks
    ADD COLUMN generated_context text NOT NULL DEFAULT '' CHECK (char_length(generated_context) <= 600),
    ADD COLUMN search_text text NOT NULL DEFAULT '',
    ADD COLUMN text_preparation_version text,
    ADD COLUMN token_count integer NOT NULL DEFAULT 0 CHECK (token_count >= 0);

CREATE TABLE chunk_terms (
    chunk_id bigint NOT NULL REFERENCES document_chunks(id) ON DELETE CASCADE,
    term text NOT NULL,
    tf integer NOT NULL CHECK (tf > 0),
    PRIMARY KEY (chunk_id, term)
);
CREATE INDEX chunk_terms_term_idx ON chunk_terms (term, chunk_id) INCLUDE (tf);
CREATE INDEX document_chunks_profile_idx ON document_chunks (embedding_profile, text_preparation_version);

-- Deliberately no backfill: old embeddings did not include generated context.
-- Readers refuse old/mixed preparations until a complete successful reimport.

-- +goose Down

-- Refuse a downgrade that could expose new embeddings to an old reader.
ALTER TABLE document_chunks ADD CONSTRAINT no_hybrid_downgrade CHECK (text_preparation_version IS NULL);
DROP TABLE chunk_terms;
DROP FUNCTION knowledge_terms(text);
ALTER TABLE document_chunks
    DROP CONSTRAINT no_hybrid_downgrade,
    DROP COLUMN generated_context,
    DROP COLUMN search_text,
    DROP COLUMN text_preparation_version,
    DROP COLUMN token_count;
ALTER TABLE documents DROP COLUMN keywords, DROP COLUMN original_body;
