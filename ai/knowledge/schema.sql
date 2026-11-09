CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS ai_knowledge_chunks (
    id text PRIMARY KEY,
    title text NOT NULL,
    url text,
    body text NOT NULL,
    roles text[] NOT NULL CHECK (cardinality(roles) > 0),
    teams text[] NOT NULL,
    embedding_model text NOT NULL,
    embedding vector NOT NULL CHECK (vector_norm(embedding) > 0)
);
