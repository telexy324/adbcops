CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE incident
    ADD COLUMN IF NOT EXISTS tags JSONB,
    ADD COLUMN IF NOT EXISTS error_template TEXT;

CREATE INDEX IF NOT EXISTS idx_incident_title_trgm
ON incident USING gin (title gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_incident_summary_trgm
ON incident USING gin (summary gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_incident_error_template_trgm
ON incident USING gin (error_template gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_incident_tags_gin
ON incident USING gin (tags);
