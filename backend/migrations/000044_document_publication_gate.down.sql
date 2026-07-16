DROP TABLE IF EXISTS kb_document_version_publication;
DROP INDEX IF EXISTS idx_kb_document_current_published;
DROP INDEX IF EXISTS idx_kb_document_version_one_published;
ALTER TABLE kb_document DROP COLUMN IF EXISTS current_published_version_id;
ALTER TABLE kb_document_version
DROP COLUMN IF EXISTS publication_gate,
DROP COLUMN IF EXISTS deprecated_at,
DROP COLUMN IF EXISTS superseded_at,
DROP COLUMN IF EXISTS published_at,
DROP COLUMN IF EXISTS published_by;
