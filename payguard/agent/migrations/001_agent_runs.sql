CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS policy_chunks (
    id BIGSERIAL PRIMARY KEY,
    doc_name TEXT NOT NULL,
    chunk_index INT NOT NULL,
    content TEXT NOT NULL,
    embedding vector(768) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (doc_name, chunk_index)
);

CREATE INDEX IF NOT EXISTS policy_chunks_embedding_idx ON policy_chunks
    USING hnsw (embedding vector_cosine_ops);

-- One row per investigation. A case can be investigated more than once (e.g. re-run after new
-- evidence arrives), so this is an append-only log, not a 1:1 with reconciliation_cases -- the
-- latest row per case_id is what the approval UI shows.
CREATE TABLE IF NOT EXISTS agent_runs (
    id BIGSERIAL PRIMARY KEY,
    case_id BIGINT NOT NULL,
    conclusion TEXT NOT NULL,
    confidence TEXT NOT NULL,
    recommended_action TEXT NOT NULL DEFAULT '',
    evidence_json TEXT NOT NULL DEFAULT '[]',
    missing_information_json TEXT NOT NULL DEFAULT '[]',
    policy_citations_json TEXT NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS agent_runs_case_id_idx ON agent_runs (case_id, created_at DESC);
