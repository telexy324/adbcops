ALTER TABLE kb_document_version
ADD COLUMN published_by BIGINT REFERENCES app_user(id),
ADD COLUMN published_at TIMESTAMP,
ADD COLUMN superseded_at TIMESTAMP,
ADD COLUMN deprecated_at TIMESTAMP,
ADD COLUMN publication_gate JSONB;

ALTER TABLE kb_document
ADD COLUMN current_published_version_id BIGINT REFERENCES kb_document_version(id);

WITH latest AS (
    SELECT DISTINCT ON (v.document_id) v.id, v.document_id
    FROM kb_document_version v
    JOIN kb_document d ON d.id = v.document_id
    WHERE d.status = 'published'
    ORDER BY v.document_id, v.created_at DESC, v.id DESC
)
UPDATE kb_document_version v
SET status = 'published',
    published_at = COALESCE(v.reviewed_at, v.updated_at, now()),
    published_by = v.reviewed_by
FROM latest
WHERE v.id = latest.id;

WITH latest AS (
    SELECT DISTINCT ON (v.document_id) v.id, v.document_id
    FROM kb_document_version v
    WHERE v.status = 'published'
    ORDER BY v.document_id, v.created_at DESC, v.id DESC
)
UPDATE kb_document d
SET current_published_version_id = latest.id
FROM latest
WHERE d.id = latest.document_id;

CREATE UNIQUE INDEX idx_kb_document_version_one_published
ON kb_document_version(document_id)
WHERE status = 'published';

CREATE INDEX idx_kb_document_current_published
ON kb_document(current_published_version_id)
WHERE current_published_version_id IS NOT NULL;

CREATE TABLE kb_document_version_publication (
    id BIGSERIAL PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES kb_document(id) ON DELETE CASCADE,
    document_version_id BIGINT NOT NULL REFERENCES kb_document_version(id) ON DELETE CASCADE,
    action VARCHAR(30) NOT NULL CHECK (action IN ('publish', 'supersede', 'deprecate')),
    gate_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    actor_id BIGINT NOT NULL REFERENCES app_user(id),
    comment TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE INDEX idx_kb_document_version_publication_history
ON kb_document_version_publication(document_id, created_at DESC, id DESC);
