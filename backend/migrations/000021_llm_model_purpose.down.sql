DROP INDEX IF EXISTS idx_llm_config_purpose;

DROP INDEX IF EXISTS idx_llm_config_default_unique;

CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_config_default_unique
ON llm_config(is_default)
WHERE is_default = true;

ALTER TABLE llm_config
DROP CONSTRAINT IF EXISTS chk_llm_config_purpose;

ALTER TABLE llm_config
DROP COLUMN IF EXISTS purpose;
