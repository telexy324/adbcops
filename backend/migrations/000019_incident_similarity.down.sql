DROP INDEX IF EXISTS idx_incident_tags_gin;
DROP INDEX IF EXISTS idx_incident_error_template_trgm;
DROP INDEX IF EXISTS idx_incident_summary_trgm;
DROP INDEX IF EXISTS idx_incident_title_trgm;
ALTER TABLE incident
    DROP COLUMN IF EXISTS error_template,
    DROP COLUMN IF EXISTS tags;
