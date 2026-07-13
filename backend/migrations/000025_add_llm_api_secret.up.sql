ALTER TABLE llm_config
ADD COLUMN IF NOT EXISTS api_secret_ref TEXT;
