ALTER TABLE kb_quality_standard RENAME TO kb_quality_standard_legacy;
ALTER INDEX idx_kb_quality_standard_enabled_created_at RENAME TO idx_kb_quality_standard_legacy_enabled_created_at;

CREATE TABLE kb_quality_standard (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    source_document_version_id BIGINT REFERENCES kb_document_version(id),
    version VARCHAR(50) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'published', 'deprecated')),
    effective_from TIMESTAMP,
    effective_until TIMESTAMP,
    created_by BIGINT REFERENCES app_user(id),
    approved_by BIGINT REFERENCES app_user(id),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    UNIQUE(name, version),
    CHECK (effective_until IS NULL OR effective_from IS NULL OR effective_until > effective_from)
);

CREATE TABLE kb_quality_profile (
    id BIGSERIAL PRIMARY KEY,
    standard_id BIGINT NOT NULL REFERENCES kb_quality_standard(id) ON DELETE CASCADE,
    profile_key VARCHAR(120) NOT NULL,
    name VARCHAR(255) NOT NULL,
    applicable_doc_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    applicable_systems JSONB,
    applicable_environments JSONB,
    total_score NUMERIC(8,2) NOT NULL DEFAULT 100 CHECK (total_score > 0),
    pass_score NUMERIC(8,2) NOT NULL DEFAULT 80 CHECK (pass_score >= 0 AND pass_score <= total_score),
    warning_score NUMERIC(8,2) NOT NULL DEFAULT 70 CHECK (warning_score >= 0 AND warning_score <= pass_score),
    gate_policy JSONB,
    status VARCHAR(30) NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'published', 'deprecated')),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    UNIQUE(standard_id, profile_key)
);

CREATE TABLE kb_quality_criterion (
    id BIGSERIAL PRIMARY KEY,
    profile_id BIGINT NOT NULL REFERENCES kb_quality_profile(id) ON DELETE CASCADE,
    criterion_key VARCHAR(120) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    weight NUMERIC(8,4) NOT NULL CHECK (weight > 0 AND weight <= 1),
    max_score NUMERIC(8,2) NOT NULL CHECK (max_score > 0),
    scoring_method VARCHAR(30) NOT NULL CHECK (scoring_method IN ('rule', 'llm', 'hybrid', 'manual')),
    evidence_scope JSONB,
    order_no INT NOT NULL CHECK (order_no > 0),
    UNIQUE(profile_id, criterion_key),
    UNIQUE(profile_id, order_no)
);

CREATE TABLE kb_quality_rule (
    id BIGSERIAL PRIMARY KEY,
    criterion_id BIGINT NOT NULL REFERENCES kb_quality_criterion(id) ON DELETE CASCADE,
    rule_key VARCHAR(160) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    rule_type VARCHAR(50) NOT NULL CHECK (rule_type IN ('field_presence', 'section_presence', 'pattern', 'metadata', 'consistency', 'freshness', 'semantic', 'safety', 'cross_reference', 'manual')),
    severity VARCHAR(30) NOT NULL DEFAULT 'medium' CHECK (severity IN ('info', 'low', 'medium', 'high', 'critical')),
    max_score NUMERIC(8,2) NOT NULL CHECK (max_score >= 0),
    deduction NUMERIC(8,2) CHECK (deduction IS NULL OR deduction >= 0),
    required BOOLEAN NOT NULL DEFAULT false,
    hard_gate BOOLEAN NOT NULL DEFAULT false,
    evidence_requirement JSONB,
    detector_config JSONB,
    llm_instruction TEXT,
    examples JSONB,
    order_no INT NOT NULL CHECK (order_no > 0),
    UNIQUE(criterion_id, rule_key),
    UNIQUE(criterion_id, order_no),
    CHECK (NOT hard_gate OR (description IS NOT NULL AND length(btrim(description)) > 0))
);

INSERT INTO kb_quality_standard (name, description, version, status, effective_from)
VALUES ('builtin-semantic-ops-quality', 'Knowledge Center 2.0 内置语义运维文档质量标准', 'v1.0', 'published', now());

INSERT INTO kb_quality_profile (
    standard_id, profile_key, name, applicable_doc_types, applicable_systems,
    applicable_environments, total_score, pass_score, warning_score, gate_policy, status
)
SELECT id, 'semantic_ops_default', '语义运维文档默认评分',
       '["runbook","alert_handbook","troubleshooting","change_plan","architecture"]'::jsonb,
       '[]'::jsonb, '["production","staging","test"]'::jsonb,
       100, 80, 70, '{"violationResult":"blocked"}'::jsonb, 'published'
FROM kb_quality_standard WHERE name = 'builtin-semantic-ops-quality' AND version = 'v1.0';

INSERT INTO kb_quality_criterion (profile_id, criterion_key, name, weight, max_score, scoring_method, order_no)
SELECT p.id, v.criterion_key, v.name, v.weight, v.max_score, v.scoring_method, v.order_no
FROM kb_quality_profile p
CROSS JOIN (VALUES
    ('completeness', '完整性', 0.2000, 20.00, 'hybrid', 1),
    ('accuracy', '准确性与一致性', 0.2000, 20.00, 'hybrid', 2),
    ('operability', '可操作性', 0.1500, 15.00, 'hybrid', 3),
    ('verifiability', '可验证性', 0.1000, 10.00, 'hybrid', 4),
    ('safety', '安全与风险', 0.1500, 15.00, 'hybrid', 5),
    ('traceability', '可追溯性', 0.1000, 10.00, 'rule', 6),
    ('retrievability', '可检索性', 0.0500, 5.00, 'hybrid', 7),
    ('freshness', '时效性', 0.0500, 5.00, 'rule', 8)
) AS v(criterion_key, name, weight, max_score, scoring_method, order_no)
WHERE p.profile_key = 'semantic_ops_default';

INSERT INTO kb_quality_rule (
    criterion_id, rule_key, name, description, rule_type, severity, max_score,
    required, hard_gate, evidence_requirement, order_no
)
SELECT c.id, v.rule_key, v.name, v.description, v.rule_type, v.severity,
       v.max_score, true, v.hard_gate, '{"required":"block_reference"}'::jsonb, v.order_no
FROM kb_quality_criterion c
JOIN (VALUES
    ('completeness', 'document_structure_complete', '文档结构完整', '检查适用范围、前置条件、步骤、验证和回滚等必要内容。', 'section_presence', 'high', 20.00, false, 1),
    ('accuracy', 'content_consistent', '内容准确且一致', '检查命令、路径、环境、术语及引用对象是否明确一致。', 'consistency', 'high', 20.00, false, 1),
    ('operability', 'steps_actionable', '步骤可执行', '检查步骤顺序、对象、动作、权限、工具和异常升级路径。', 'semantic', 'high', 15.00, false, 1),
    ('verifiability', 'verification_complete', '验证闭环完整', '检查验证方式、预期结果、失败判断和下一步。', 'section_presence', 'high', 10.00, false, 1),
    ('safety', 'risk_controls_complete', '风险控制完整', '检查风险警示、审批、敏感信息保护和回滚影响。', 'safety', 'critical', 15.00, false, 1),
    ('traceability', 'metadata_traceable', '版本与责任可追溯', '检查版本、更新时间、责任人、审核人和变更记录。', 'metadata', 'medium', 10.00, false, 1),
    ('retrievability', 'content_retrievable', '内容可检索', '检查标题、章节、关键术语、标签和可切片结构。', 'semantic', 'medium', 5.00, false, 1),
    ('freshness', 'document_fresh', '文档在复审周期内', '检查复审周期以及组件适用版本。', 'freshness', 'high', 5.00, false, 1)
) AS v(criterion_key, rule_key, name, description, rule_type, severity, max_score, hard_gate, order_no)
ON c.criterion_key = v.criterion_key;

INSERT INTO kb_quality_rule (
    criterion_id, rule_key, name, description, rule_type, severity, max_score,
    required, hard_gate, evidence_requirement, order_no
)
SELECT c.id, v.rule_key, v.name, v.description, v.rule_type, v.severity, 0,
       true, true, '{"required":"block_reference_and_reason"}'::jsonb, v.order_no
FROM kb_quality_criterion c
JOIN (VALUES
    ('completeness', 'parse_failed', '解析失败', '解析失败时无法进行正式评分，必须阻断。', 'manual', 'critical', 10),
    ('completeness', 'empty_document', '空文档', '文档无有效内容时必须阻断。', 'field_presence', 'critical', 11),
    ('safety', 'sensitive_credential_exposed', '敏感凭据暴露', '发现明文密码、令牌或密钥时必须阻断。', 'safety', 'critical', 10),
    ('safety', 'destructive_command_without_warning', '危险命令缺少警示', '破坏性命令没有相邻风险警示时必须阻断。', 'safety', 'critical', 11),
    ('safety', 'high_risk_action_without_approval', '高风险操作缺少审批', '生产高风险操作没有审批要求时必须阻断。', 'safety', 'critical', 12),
    ('accuracy', 'production_test_environment_confusion', '生产测试环境混淆', '生产与测试环境描述冲突可能导致误操作，必须阻断。', 'consistency', 'critical', 10),
    ('completeness', 'missing_rollback_for_change_plan', '变更缺少回滚', '变更方案没有可执行回滚路径时必须阻断。', 'section_presence', 'critical', 12),
    ('verifiability', 'missing_verification_for_runbook', 'Runbook 缺少验证', 'Runbook 没有验证步骤时必须阻断。', 'section_presence', 'critical', 10),
    ('freshness', 'expired_document', '文档已过期', '超过强制复审周期的文档按默认策略阻断。', 'freshness', 'high', 10),
    ('accuracy', 'contradictory_critical_steps', '关键步骤矛盾', '相互矛盾的关键步骤会造成误操作，必须阻断。', 'consistency', 'critical', 11)
) AS v(criterion_key, rule_key, name, description, rule_type, severity, order_no)
ON c.criterion_key = v.criterion_key;

CREATE INDEX idx_kb_quality_standard_status ON kb_quality_standard(status, updated_at DESC);
CREATE INDEX idx_kb_quality_profile_standard ON kb_quality_profile(standard_id, profile_key);
CREATE INDEX idx_kb_quality_criterion_profile ON kb_quality_criterion(profile_id, order_no);
CREATE INDEX idx_kb_quality_rule_criterion ON kb_quality_rule(criterion_id, order_no);
