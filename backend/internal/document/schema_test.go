package document

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"aiops-platform/backend/internal/model"
)

func TestRunbookSchemaRecognizesStepsVerificationRollbackRiskAndExactCommand(t *testing.T) {
	content := `# Payment Recovery Runbook

## Prerequisites

Confirm the incident commander approved the operation.

## Risk Warning

Deleting a production pod may interrupt requests.

## Steps

1. Inspect the workload.

` + "```sh\nkubectl delete pod payment-api-0 --namespace prod --grace-period=30\n```" + `

## Verification

Confirm HTTP 200 and error rate below 1%.

## Rollback

Run helm rollback payment-api 41 --namespace prod.
`
	ast := parseSchemaFixture(t, "runbook.md", "md", content)
	beforeExtraction, _ := json.Marshal(ast)
	docType := "runbook"
	system, environment, component := "payment", "prod", "payment-api"
	document := &model.KBDocument{Title: "Payment Runbook", Version: "v3.0", DocType: &docType, SystemName: &system, Environment: &environment, ComponentName: &component}
	extraction := ExtractDocumentSchema(document, ast)
	afterExtraction, _ := json.Marshal(ast)
	if !bytes.Equal(beforeExtraction, afterExtraction) {
		t.Fatal("schema extraction modified the source AST")
	}
	if extraction.DocumentType != "runbook" || extraction.TypeInferred {
		t.Fatalf("document type = %q inferred=%v", extraction.DocumentType, extraction.TypeInferred)
	}
	for _, field := range []string{"steps", "verification", "rollback", "risk_level", "prerequisites"} {
		if len(extraction.Fields[field].Values) == 0 {
			t.Fatalf("field %q was not extracted: %+v", field, extraction.Fields[field])
		}
		if !extraction.Fields[field].Inferred {
			t.Fatalf("field %q must be marked inferred", field)
		}
	}
	if extraction.Fields["applicable_system"].Inferred || extraction.Fields["applicable_system"].Values[0] != system {
		t.Fatalf("explicit metadata field = %+v", extraction.Fields["applicable_system"])
	}
	exactCommand := "kubectl delete pod payment-api-0 --namespace prod --grace-period=30"
	if !hasEntity(extraction.Entities, "command", exactCommand) {
		t.Fatalf("exact command was not preserved: %+v", extraction.Entities)
	}
	if !hasDiagnostic(extraction.Diagnostics, "command_risk") {
		t.Fatalf("dangerous command diagnostic missing: %+v", extraction.Diagnostics)
	}
}

func TestAlertHandbookSchemaRecognizesMeaningCausesEvidenceAndRecovery(t *testing.T) {
	content := `# HighErrorRate Alert

## Alert Meaning

The API error ratio exceeded its threshold.

## Trigger Condition

HTTP 5xx is above 5% for five minutes.

## Common Causes

Database pool exhaustion or upstream HTTP 503.

## Evidence to Collect

| Evidence | Source |
| --- | --- |
| error rate | Prometheus |
| SQLSTATE 08006 | application log |

## Diagnostic Steps

Inspect metrics and logs.

## Recovery Criteria

HTTP 5xx remains below 1% for ten minutes.
`
	ast := parseSchemaFixture(t, "alert.md", "md", content)
	docType := "alert_handbook"
	extraction := ExtractDocumentSchema(&model.KBDocument{DocType: &docType}, ast)
	for _, field := range []string{"alert_name", "alert_meaning", "trigger_condition", "common_causes", "evidence_to_collect", "diagnostic_steps", "recovery_criteria"} {
		if len(extraction.Fields[field].Values) == 0 {
			t.Fatalf("alert field %q missing: %+v", field, extraction.Fields[field])
		}
	}
	if !hasEntity(extraction.Entities, "error_code", "SQLSTATE 08006") {
		t.Fatalf("error code entity missing: %+v", extraction.Entities)
	}
}

func TestEmergencyPlanTypeAndFieldsAreInferredWithoutChangingEvidence(t *testing.T) {
	content := `# Production Emergency Plan 应急预案

## Incident Level 事件等级

P1

## Trigger Condition 启动条件

Payment success rate is below 90%.

## Roles 职责

Incident commander coordinates response.

## Communication 通报

Notify the service owner.

## Containment 止损

Disable new traffic.

## Recovery 恢复

Restore the last healthy deployment.

## Verification 验证

Payment success rate returns above 99%.

## Exit Criteria 退出条件

All critical metrics remain healthy for 30 minutes.
`
	ast := parseSchemaFixture(t, "emergency.md", "md", content)
	extraction := ExtractDocumentSchema(&model.KBDocument{}, ast)
	if extraction.DocumentType != "emergency_plan" || !extraction.TypeInferred || extraction.Confidence <= 0 {
		t.Fatalf("inferred type = %+v", extraction)
	}
	for _, field := range []string{"incident_level", "trigger_condition", "roles", "communication", "containment", "recovery", "verification", "exit_criteria"} {
		extracted := extraction.Fields[field]
		if len(extracted.Values) == 0 || !extracted.Inferred || extracted.Evidence[0].Text != extracted.Values[0] {
			t.Fatalf("emergency field %q = %+v", field, extracted)
		}
	}
}

func TestSchemaExtractionIsPersistedWithDocumentVersion(t *testing.T) {
	store := newFakeRepository()
	service := newTestService(t, store, t.TempDir(), 4096)
	owner := &model.AppUser{ID: 7, Role: model.RoleUser}
	documentType := "runbook"
	document, err := service.Upload(context.Background(), owner, newFileHeader(t, "guide.md", "# Guide\n\n## Steps\n\nInspect pods.\n\n## Verification\n\nConfirm healthy.\n\n## Rollback\n\nRestore the prior release."), UploadMetadata{Title: "Guide", DocType: documentType})
	if err != nil {
		t.Fatal(err)
	}
	version, _ := service.GetLatestDocumentVersion(context.Background(), owner, document.ID)
	parsed, err := service.ParseDocumentVersion(context.Background(), owner, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := service.GetParsedStructure(context.Background(), owner, parsed.Version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.DocumentSchema.DocumentType != "runbook" || len(stored.DocumentSchema.Fields["steps"].Values) == 0 || len(stored.Version.DocumentSchema) == 0 {
		t.Fatalf("stored schema = %+v version=%+v", stored.DocumentSchema, stored.Version)
	}
}

func parseSchemaFixture(t *testing.T, name, fileType, content string) *DocumentAST {
	t.Helper()
	path := writeFixture(t, name, []byte(content))
	ast, err := newFixtureRegistry(t, DefaultParseLimits()).Parse(context.Background(), ParseRequest{Path: path, FileName: name, FileType: fileType})
	if err != nil {
		t.Fatalf("parse schema fixture: %v", err)
	}
	return ast
}

func hasEntity(entities []DocumentEntity, entityType, value string) bool {
	for _, entity := range entities {
		if entity.Type == entityType && entity.Value == value {
			return true
		}
	}
	return false
}

func hasDiagnostic(diagnostics []SchemaDiagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
