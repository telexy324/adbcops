ALTER TABLE llm_config
ADD COLUMN IF NOT EXISTS purpose VARCHAR(30) NOT NULL DEFAULT 'chat';

ALTER TABLE llm_config
DROP CONSTRAINT IF EXISTS chk_llm_config_purpose;

ALTER TABLE llm_config
ADD CONSTRAINT chk_llm_config_purpose
CHECK (purpose IN ('chat', 'embedding', 'rerank'));

DROP INDEX IF EXISTS idx_llm_config_default_unique;

CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_config_default_unique
ON llm_config(purpose, is_default)
WHERE is_default = true;

CREATE INDEX IF NOT EXISTS idx_llm_config_purpose
ON llm_config(purpose);
