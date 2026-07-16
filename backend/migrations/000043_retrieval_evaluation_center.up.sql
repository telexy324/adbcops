CREATE TABLE kb_retrieval_test_case (
    id BIGSERIAL PRIMARY KEY,
    document_id BIGINT REFERENCES kb_document(id) ON DELETE CASCADE,
    document_version_id BIGINT REFERENCES kb_document_version(id) ON DELETE CASCADE,
    question TEXT NOT NULL,
    category VARCHAR(30) NOT NULL CHECK (category IN ('title', 'core_step', 'error_code', 'scenario', 'irrelevant', 'custom')),
    expected_document_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    expected_chunk_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    expected_sections JSONB NOT NULL DEFAULT '[]'::jsonb,
    must_include_facts JSONB NOT NULL DEFAULT '[]'::jsonb,
    must_not_include JSONB NOT NULL DEFAULT '[]'::jsonb,
    expect_no_answer BOOLEAN NOT NULL DEFAULT false,
    source VARCHAR(30) NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'author', 'llm_reviewed', 'qa_feedback')),
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by BIGINT NOT NULL REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    CHECK (document_version_id IS NULL OR document_id IS NOT NULL)
);

CREATE INDEX idx_kb_retrieval_test_case_scope
ON kb_retrieval_test_case(document_version_id, enabled, category);

CREATE TABLE kb_retrieval_evaluation_run (
    id BIGSERIAL PRIMARY KEY,
    mode VARCHAR(20) NOT NULL CHECK (mode IN ('smoke', 'lab')),
    name VARCHAR(120) NOT NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    document_version_id BIGINT REFERENCES kb_document_version(id),
    embedding_config_id BIGINT REFERENCES llm_config(id),
    embedding_model VARCHAR(120),
    embedding_model_revision VARCHAR(120),
    rerank_config_id BIGINT REFERENCES llm_config(id),
    rerank_model VARCHAR(120),
    chunk_strategy_id BIGINT REFERENCES kb_chunk_strategy(id),
    retrieval_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    thresholds JSONB NOT NULL DEFAULT '{}'::jsonb,
    metrics JSONB NOT NULL DEFAULT '{}'::jsonb,
    case_count INT NOT NULL DEFAULT 0,
    passed BOOLEAN NOT NULL DEFAULT false,
    error_message TEXT,
    created_by BIGINT NOT NULL REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    completed_at TIMESTAMP
);

CREATE INDEX idx_kb_retrieval_evaluation_run_scope
ON kb_retrieval_evaluation_run(document_version_id, mode, created_at DESC);

CREATE TABLE kb_retrieval_evaluation_result (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES kb_retrieval_evaluation_run(id) ON DELETE CASCADE,
    test_case_id BIGINT NOT NULL REFERENCES kb_retrieval_test_case(id),
    retrieved_document_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    retrieved_chunk_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    citation_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    context_text TEXT NOT NULL DEFAULT '',
    metrics JSONB NOT NULL DEFAULT '{}'::jsonb,
    retrieval_trace JSONB NOT NULL DEFAULT '{}'::jsonb,
    passed BOOLEAN NOT NULL DEFAULT false,
    error_message TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    UNIQUE(run_id, test_case_id)
);

CREATE INDEX idx_kb_retrieval_evaluation_result_run
ON kb_retrieval_evaluation_result(run_id, test_case_id);
