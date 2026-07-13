CREATE TABLE llm_config (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    provider VARCHAR(50) NOT NULL,
    base_url TEXT NOT NULL,
    model VARCHAR(120) NOT NULL,
    purpose VARCHAR(30) NOT NULL DEFAULT 'chat',
    api_key_ref TEXT,
    api_secret_ref TEXT,
    temperature NUMERIC(4,3) DEFAULT 0.2,
    enabled BOOLEAN NOT NULL DEFAULT true,
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now(),
    CONSTRAINT chk_llm_config_provider CHECK (provider IN ('deepseek', 'qwen', 'openai-compatible')),
    CONSTRAINT chk_llm_config_purpose CHECK (purpose IN ('chat', 'embedding', 'rerank')),
    CONSTRAINT chk_llm_config_temperature CHECK (temperature >= 0 AND temperature <= 2)
);

CREATE UNIQUE INDEX idx_llm_config_default_unique
ON llm_config(purpose, is_default)
WHERE is_default = true;

CREATE INDEX idx_llm_config_enabled
ON llm_config(enabled);

CREATE INDEX idx_llm_config_purpose
ON llm_config(purpose);
