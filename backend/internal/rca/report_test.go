package rca

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
)

func TestBuildReportAggregatesEvidenceRanksCausesAndPreservesTraceability(t *testing.T) {
	service, actor, runID := reportFixture(t)
	report, err := service.BuildReport(context.Background(), actor, runID)
	if err != nil {
		t.Fatalf("BuildReport() error = %v", err)
	}
	if report.Version != RCAReportVersion || len(report.Investigation) != 3 {
		t.Fatalf("report did not aggregate all rounds: %+v", report)
	}
	if len(report.Evidence.Facts) != 2 || len(report.Evidence.Rules) != 1 ||
		len(report.Evidence.Knowledge) != 1 {
		t.Fatalf("evidence kinds were not preserved: %+v", report.Evidence)
	}
	if len(report.RootCauseCandidates) != 2 ||
		report.RootCauseCandidates[0].Summary != "数据库锁竞争导致订单请求变慢" ||
		report.RootCauseCandidates[0].EvidenceStrength != "strong" {
		t.Fatalf("root causes were not ranked by evidence strength: %+v", report.RootCauseCandidates)
	}
	if len(report.RootCauseCandidates[0].SupportingEvidence) != 2 ||
		len(report.RootCauseCandidates[0].ContradictingEvidence) != 1 ||
		report.RootCauseCandidates[0].SupportingEvidence[0].URL != "/api/evidence/1" {
		t.Fatalf("supporting and contradicting evidence are not clickable: %+v", report.RootCauseCandidates[0])
	}
	if report.RootCauseCandidates[0].Status == "confirmed" ||
		!strings.Contains(report.Conclusion, "仍是候选") {
		t.Fatalf("hypothesis was presented as confirmed fact: %+v", report.RootCauseCandidates[0])
	}
	if !report.Incomplete || len(report.MissingEvidence) == 0 ||
		!strings.Contains(strings.Join(report.MissingEvidence, " "), "upstream_unavailable") {
		t.Fatalf("partial success and missing data source are not prominent: %+v", report)
	}
	trace := report.Traceability
	if trace.WorkflowRunID == nil || *trace.WorkflowRunID != 77 ||
		!containsInt64(trace.AgentRunIDs, 701) ||
		!containsInt64(trace.SkillRunIDs, 501) ||
		!containsInt64(trace.SkillRunIDs, 702) ||
		len(trace.RoundIDs) != 3 || len(trace.ActionIDs) != 3 {
		t.Fatalf("execution trace is incomplete: %+v", trace)
	}
	encoded, _ := json.Marshal(report)
	for _, secret := range []string{"top-secret", "alice@example.com", "customer_id=42"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("report leaked %q: %s", secret, encoded)
		}
	}
	for _, suggestion := range report.Suggestions {
		if !suggestion.AdvisoryOnly || suggestion.AutoExecute {
			t.Fatalf("report suggestion became executable: %+v", suggestion)
		}
	}
}

func TestBuildReportAllowsUnableToLocateConclusionWithoutEvidence(t *testing.T) {
	repository := newMemoryRCARepository()
	service := NewService(repository, &memoryEvidenceCreator{repository: repository}, fakeRCADataSources{})
	service.now = fixedRCATime
	actor := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	run, err := service.CreateRun(context.Background(), actor, CreateRunInput{
		Query: "服务偶发变慢", Scope: json.RawMessage(`{"serviceName":"order-service"}`),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	report, err := service.BuildReport(context.Background(), actor, run.ID)
	if err != nil {
		t.Fatalf("BuildReport() error = %v", err)
	}
	if report.Conclusion != "暂无法定位：当前没有具备充分 Evidence 支撑的根因候选。" ||
		!report.Incomplete {
		t.Fatalf("insufficient evidence was overstated: %+v", report)
	}
}

func TestBuildReportDraftsAreNonExecutingAndSanitized(t *testing.T) {
	service, actor, runID := reportFixture(t)
	drafts, err := service.BuildReportDrafts(context.Background(), actor, runID)
	if err != nil {
		t.Fatalf("BuildReportDrafts() error = %v", err)
	}
	if !drafts.Incident.Draft || !drafts.RCADocument.Draft ||
		drafts.Incident.SourceRCARunID != runID || drafts.RCADocument.SourceRCARunID != runID {
		t.Fatalf("draft provenance is missing: %+v", drafts)
	}
	if drafts.RCADocument.FileType != "text/markdown" ||
		!strings.Contains(drafts.RCADocument.Content, "仅供建议，不自动执行") ||
		!strings.Contains(drafts.RCADocument.Content, "/api/evidence/1") {
		t.Fatalf("RCA document draft is incomplete: %+v", drafts.RCADocument)
	}
	encoded, _ := json.Marshal(drafts)
	if strings.Contains(string(encoded), "top-secret") || strings.Contains(string(encoded), "alice@example.com") {
		t.Fatalf("draft leaked sensitive source text: %s", encoded)
	}
}

func TestSafeReportJSONNeverReturnsUnredactedTruncatedPreview(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"padding":     strings.Repeat("x", 4096),
		"readonlySQL": "SELECT * FROM customer WHERE email='alice@example.com'",
		"password":    "top-secret",
	})
	safe := string(safeReportJSON(raw, 256))
	if strings.Contains(safe, "alice@example.com") || strings.Contains(safe, "top-secret") ||
		strings.Contains(safe, "preview") {
		t.Fatalf("truncated report scope leaked a raw preview: %s", safe)
	}
}

func reportFixture(t *testing.T) (*Service, *model.AppUser, int64) {
	t.Helper()
	repository := newMemoryRCARepository()
	workflowRunID := int64(77)
	now := fixedRCATime()
	stopReason := StopReasonMaxRounds
	repository.runs[1] = model.RCARun{
		ID: 1, UserID: 1, WorkflowRunID: &workflowRunID, Status: model.RCARunStatusPartialSuccess,
		Query:        "订单服务变慢 password=top-secret",
		Scope:        json.RawMessage(`{"serviceName":"order-service","environment":"prod","from":"2026-07-28T05:00:00Z","to":"2026-07-28T06:00:00Z","readonlySQL":"SELECT * FROM customer WHERE customer_id=42 AND email='alice@example.com'"}`),
		CurrentRound: 3, MaxRounds: 3, StopReason: &stopReason, CreatedAt: now,
	}
	repository.nextRunID = 2
	repository.rounds[1] = reportRound(1, 1, model.RCARoundStatusSuccess, []int64{1}, nil)
	repository.rounds[2] = reportRound(2, 2, model.RCARoundStatusPartialSuccess, []int64{2, 3}, []NextAction{{
		ActionKey: "database", SkillName: "query_tidb_lock_waits", Input: json.RawMessage(`{"dataSourceId":7}`),
	}})
	rejected, _ := json.Marshal([]Hypothesis{{
		ID: "db-lock", Summary: "数据库锁竞争导致订单请求变慢", Confidence: .25, EvidenceIDs: []int64{3},
	}})
	third := reportRound(3, 3, model.RCARoundStatusSuccess, []int64{4}, nil)
	third.RejectedHypotheses = rejected
	repository.rounds[3] = third
	repository.nextRoundID = 4
	upstream := "upstream_unavailable"
	repository.actions[1] = reportAction(1, 1, 1, "query_logs", model.RCAActionStatusSuccess, []int64{1}, nil, 501)
	repository.actions[2] = reportAction(2, 1, 2, "query_metrics", model.RCAActionStatusPartialSuccess, []int64{2}, &upstream, 502)
	repository.actions[3] = reportAction(3, 1, 3, "query_tidb_lock_waits", model.RCAActionStatusSuccess, []int64{4}, nil, 503)
	repository.nextActionID = 4
	owner := int64(1)
	repository.evidence[1] = reportEvidence(1, 1, 1, model.EvidenceKindFact, "elasticsearch", "数据库调用耗时升高 password=top-secret", 501, &owner)
	repository.evidence[2] = reportEvidence(2, 2, 2, model.EvidenceKindRule, "prometheus", "调用量基线同步变化", 502, &owner)
	repository.evidence[3] = reportEvidence(3, 2, 2, model.EvidenceKindKnowledge, "knowledge", "历史案例不支持锁竞争", 502, &owner)
	repository.evidence[4] = reportEvidence(4, 3, 3, model.EvidenceKindFact, model.DataSourceTypeTiDB, "锁等待与慢 SQL SELECT * FROM orders WHERE email='alice@example.com'", 503, &owner)
	repository.nextEvidenceID = 5
	strongEvidence, _ := json.Marshal([]int64{1, 4})
	weakEvidence, _ := json.Marshal([]int64{2})
	repository.candidates[1] = model.RCARootCauseCandidate{
		ID: 1, RunID: 1, RoundID: 3, Summary: "数据库锁竞争导致订单请求变慢",
		Confidence: .8, EvidenceIDs: strongEvidence,
	}
	repository.candidates[2] = model.RCARootCauseCandidate{
		ID: 2, RunID: 1, RoundID: 2, Summary: "流量增加导致服务变慢",
		Confidence: .9, EvidenceIDs: weakEvidence,
	}
	repository.nextCandidateID = 3
	agentRuns := fakeReportAgentRuns{runs: []model.AgentRun{
		{ID: 701, WorkflowRunID: &workflowRunID}, {ID: 799, WorkflowRunID: int64TestPointer(88)},
	}}
	skillRuns := fakeReportSkillRuns{runs: []model.SkillRun{
		{ID: 702, WorkflowRunID: &workflowRunID}, {ID: 798, WorkflowRunID: int64TestPointer(88)},
	}}
	service := NewService(repository, &memoryEvidenceCreator{repository: repository}, fakeRCADataSources{}).
		WithReportTraceRepositories(agentRuns, skillRuns)
	service.now = fixedRCATime
	return service, &model.AppUser{ID: 1, Role: model.RoleAdmin}, 1
}

func reportRound(id int64, number int, status string, evidenceIDs []int64, next []NextAction) model.RCARound {
	rawEvidence, _ := json.Marshal(evidenceIDs)
	rawNext, _ := json.Marshal(next)
	at := fixedRCATime().Add(time.Duration(number) * time.Minute)
	return model.RCARound{
		ID: id, RunID: 1, RoundNumber: number, Status: status,
		InputHypotheses: json.RawMessage(`[]`), NewEvidenceIDs: rawEvidence,
		RejectedHypotheses: json.RawMessage(`[]`), NextActions: rawNext,
		StartedAt: &at, FinishedAt: &at,
	}
}

func reportAction(id int64, runID int64, roundID int64, skill string, status string, evidenceIDs []int64, errorCode *string, skillRunID int64) model.RCAAction {
	rawEvidence, _ := json.Marshal(evidenceIDs)
	output, _ := json.Marshal(map[string]any{"skillRunId": skillRunID})
	at := fixedRCATime().Add(time.Duration(id) * time.Minute)
	return model.RCAAction{
		ID: id, RunID: runID, RoundID: roundID, ActionKey: "action-" + skill,
		SkillName: skill, Status: status, Input: json.RawMessage(`{}`), Output: output,
		EvidenceIDs: rawEvidence, ErrorCode: errorCode, Attempt: 1, StartedAt: &at, FinishedAt: &at,
	}
}

func reportEvidence(id, roundID, actionID int64, kind, source, summary string, skillRunID int64, owner *int64) model.EvidenceRecord {
	skill := "query_" + source
	sensitivity := model.EvidenceSensitivityInternal
	at := fixedRCATime().Add(time.Duration(id) * time.Minute)
	return model.EvidenceRecord{
		ID: id, EvidenceKey: "ev-report-" + string(rune('0'+id)), SourceType: source,
		SourceRef:  json.RawMessage(`{"skillRunId":` + jsonNumber(skillRunID) + `}`),
		ObservedAt: &at, Summary: summary, Confidence: float64TestPointer(.8),
		RCARunID: int64TestPointer(1), RCARoundID: &roundID, RCAActionID: &actionID,
		EvidenceKind: &kind, SourceSkill: &skill, Sensitivity: &sensitivity, OwnerUserID: owner,
	}
}

func jsonNumber(value int64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func float64TestPointer(value float64) *float64 { return &value }

type fakeReportAgentRuns struct {
	runs []model.AgentRun
}

func (f fakeReportAgentRuns) ListAgentRunsByWorkflowRunID(_ context.Context, workflowRunID int64) ([]model.AgentRun, error) {
	result := []model.AgentRun{}
	for _, run := range f.runs {
		if run.WorkflowRunID != nil && *run.WorkflowRunID == workflowRunID {
			result = append(result, run)
		}
	}
	return result, nil
}

type fakeReportSkillRuns struct {
	runs []model.SkillRun
}

func (f fakeReportSkillRuns) ListSkillRunsByWorkflowRunID(_ context.Context, workflowRunID int64) ([]model.SkillRun, error) {
	result := []model.SkillRun{}
	for _, run := range f.runs {
		if run.WorkflowRunID != nil && *run.WorkflowRunID == workflowRunID {
			result = append(result, run)
		}
	}
	return result, nil
}
