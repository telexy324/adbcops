CREATE TABLE kb_document_review (
    id BIGSERIAL PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES kb_document(id) ON DELETE CASCADE,
    reviewer_id BIGINT NOT NULL REFERENCES app_user(id),
    action VARCHAR(50) NOT NULL,
    from_status VARCHAR(50) NOT NULL,
    to_status VARCHAR(50) NOT NULL,
    comment TEXT,
    created_at TIMESTAMP DEFAULT now()
);

CREATE INDEX idx_kb_document_review_document_id_created_at
ON kb_document_review(document_id, created_at DESC);

CREATE INDEX idx_kb_document_review_reviewer_id_created_at
ON kb_document_review(reviewer_id, created_at DESC);
