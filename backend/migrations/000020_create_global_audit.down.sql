DROP INDEX IF EXISTS idx_workflow_run_request_id;
DROP INDEX IF EXISTS idx_agent_run_request_id;
DROP INDEX IF EXISTS idx_skill_run_request_id;

ALTER TABLE workflow_run DROP COLUMN IF EXISTS request_id;
ALTER TABLE agent_run DROP COLUMN IF EXISTS request_id;
ALTER TABLE skill_run DROP COLUMN IF EXISTS request_id;

DROP TABLE IF EXISTS audit_log;
