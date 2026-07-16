DROP TABLE IF EXISTS kb_quality_rule;
DROP TABLE IF EXISTS kb_quality_criterion;
DROP TABLE IF EXISTS kb_quality_profile;
DROP TABLE IF EXISTS kb_quality_standard;
ALTER TABLE kb_quality_standard_legacy RENAME TO kb_quality_standard;
ALTER INDEX idx_kb_quality_standard_legacy_enabled_created_at RENAME TO idx_kb_quality_standard_enabled_created_at;
