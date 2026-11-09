-- +goose Up

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE documents (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title TEXT NOT NULL,
    url TEXT
);

CREATE TABLE document_chunks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES documents(id)
        ON DELETE CASCADE,
    source_id TEXT NOT NULL UNIQUE,
    body TEXT NOT NULL,
    embedding_model TEXT NOT NULL,
    embedding VECTOR(1536) NOT NULL CHECK (vector_norm(embedding) > 0)
);

CREATE INDEX document_chunks_document_id_idx ON document_chunks (document_id);

CREATE TABLE document_access (
    document_id BIGINT NOT NULL REFERENCES documents(id)
        ON DELETE CASCADE,
    principal_type TEXT NOT NULL
        CHECK (principal_type IN ('role', 'team')),
    principal_id TEXT NOT NULL,

    PRIMARY KEY (document_id, principal_type, principal_id)
);

-- +goose Down

DROP TABLE IF EXISTS document_access;
DROP TABLE IF EXISTS document_chunks;
DROP TABLE IF EXISTS documents;
