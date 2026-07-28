package rca

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/auditutil"
	"aiops-platform/backend/internal/model"
	tidbsvc "aiops-platform/backend/internal/tidb"
)

const RCAReportVersion = "rca-report-v1"

type ReportAgentRunLister interface {
	ListAgentRunsByWorkflowRunID(context.Context, int64) ([]model.AgentRun, error)
}

type ReportSkillRunLister interface {
	ListSkillRunsByWorkflowRunID(context.Context, int64) ([]model.SkillRun, error)
}

type RCAReport struct {
	Version             string                   `json:"version"`
	RunID               int64                    `json:"runId"`
	Status              string                   `json:"status"`
	Query               string                   `json:"query"`
	Scope               json.RawMessage          `json:"scope"`
	ImpactScope         RCAReportImpactScope     `json:"impactScope"`
	Timeline            []RCAReportTimelineEntry `json:"timeline"`
	Evidence            RCAReportEvidenceGroups  `json:"evidence"`
	RootCauseCandidates []RCAReportRootCause     `json:"rootCauseCandidates"`
	RejectedHypotheses  []RCAReportHypothesis    `json:"rejectedHypotheses"`
	Investigation       []RCAReportInvestigation `json:"investigation"`
	MissingEvidence     []string                 `json:"missingEvidence"`
	Incomplete          bool                     `json:"incomplete"`
	Conclusion          string                   `json:"conclusion"`
	Suggestions         []RCAReportSuggestion    `json:"suggestions"`
	RiskNotices         []string                 `json:"riskNotices"`
	Traceability        RCAReportTraceability    `json:"traceability"`
	StopReason          string                   `json:"stopReason"`
	GeneratedAt         time.Time                `json:"generatedAt"`
}

type RCAReportImpactScope struct {
	ServiceName string   `json:"serviceName,omitempty"`
	Environment string   `json:"environment,omitempty"`
	Namespace   string   `json:"namespace,omitempty"`
	WindowStart string   `json:"windowStart,omitempty"`
	WindowEnd   string   `json:"windowEnd,omitempty"`
	Entities    []string `json:"entities"`
}

type RCAReportTimelineEntry struct {
	Type        string     `json:"type"`
	RoundNumber int        `json:"roundNumber,omitempty"`
	ReferenceID int64      `json:"referenceId"`
	Status      string     `json:"status,omitempty"`
	Summary     string     `json:"summary"`
	OccurredAt  *time.Time `json:"occurredAt,omitempty"`
}

type RCAReportEvidenceGroups struct {
	Facts      []RCAReportEvidenceRef `json:"facts"`
	Rules      []RCAReportEvidenceRef `json:"rules"`
	Knowledge  []RCAReportEvidenceRef `json:"knowledge"`
	Hypotheses []RCAReportEvidenceRef `json:"hypotheses"`
}

type RCAReportEvidenceRef struct {
	ID           int64    `json:"id"`
	EvidenceKey  string   `json:"evidenceKey"`
	Kind         string   `json:"kind"`
	SourceType   string   `json:"sourceType"`
	SourceSkill  string   `json:"sourceSkill,omitempty"`
	Summary      string   `json:"summary"`
	Confidence   *float64 `json:"confidence,omitempty"`
	RoundID      *int64   `json:"roundId,omitempty"`
	ActionID     *int64   `json:"actionId,omitempty"`
	DataSourceID *int64   `json:"dataSourceId,omitempty"`
	URL          string   `json:"url"`
}

type RCAReportRootCause struct {
	ID                    int64                  `json:"id"`
	Summary               string                 `json:"summary"`
	Status                string                 `json:"status"`
	Confidence            float64                `json:"confidence"`
	EvidenceStrength      string                 `json:"evidenceStrength"`
	EvidenceStrengthScore float64                `json:"evidenceStrengthScore"`
	SupportingEvidence    []RCAReportEvidenceRef `json:"supportingEvidence"`
	ContradictingEvidence []RCAReportEvidenceRef `json:"contradictingEvidence"`
}

type RCAReportHypothesis struct {
	ID         string                 `json:"id,omitempty"`
	Summary    string                 `json:"summary"`
	Confidence float64                `json:"confidence"`
	Evidence   []RCAReportEvidenceRef `json:"evidence"`
	Status     string                 `json:"status"`
}

type RCAReportInvestigation struct {
	RoundNumber    int                      `json:"roundNumber"`
	Status         string                   `json:"status"`
	Checked        []RCAReportCheckedAction `json:"checked"`
	Findings       []RCAReportEvidenceRef   `json:"findings"`
	ContinueReason string                   `json:"continueReason,omitempty"`
	StopReason     string                   `json:"stopReason,omitempty"`
}

type RCAReportCheckedAction struct {
	ActionID    int64   `json:"actionId"`
	ActionKey   string  `json:"actionKey"`
	SkillName   string  `json:"skillName"`
	Status      string  `json:"status"`
	EvidenceIDs []int64 `json:"evidenceIds"`
	ErrorCode   string  `json:"errorCode,omitempty"`
}

type RCAReportSuggestion struct {
	Summary      string  `json:"summary"`
	EvidenceIDs  []int64 `json:"evidenceIds"`
	AdvisoryOnly bool    `json:"advisoryOnly"`
	AutoExecute  bool    `json:"autoExecute"`
}

type RCAReportTraceability struct {
	RCARunID      int64   `json:"rcaRunId"`
	WorkflowRunID *int64  `json:"workflowRunId,omitempty"`
	AgentRunIDs   []int64 `json:"agentRunIds"`
	SkillRunIDs   []int64 `json:"skillRunIds"`
	RoundIDs      []int64 `json:"roundIds"`
	ActionIDs     []int64 `json:"actionIds"`
	EvidenceIDs   []int64 `json:"evidenceIds"`
}

type RCAReportDrafts struct {
	Incident    RCAIncidentDraft `json:"incident"`
	RCADocument RCADocumentDraft `json:"rcaDocument"`
}

type RCAIncidentDraft struct {
	Draft          bool                        `json:"draft"`
	SourceRCARunID int64                       `json:"sourceRcaRunId"`
	Title          string                      `json:"title"`
	Severity       string                      `json:"severity"`
	Status         string                      `json:"status"`
	Environment    string                      `json:"environment,omitempty"`
	ComponentName  string                      `json:"componentName,omitempty"`
	Summary        string                      `json:"summary"`
	EvidenceKeys   []string                    `json:"evidenceKeys"`
	RootCauses     []RCAIncidentRootCauseDraft `json:"rootCauses"`
}

type RCAIncidentRootCauseDraft struct {
	Summary string          `json:"summary"`
	Score   float64         `json:"score"`
	Details json.RawMessage `json:"details"`
}

type RCADocumentDraft struct {
	Draft          bool     `json:"draft"`
	SourceRCARunID int64    `json:"sourceRcaRunId"`
	Title          string   `json:"title"`
	FileName       string   `json:"fileName"`
	FileType       string   `json:"fileType"`
	DocType        string   `json:"docType"`
	Environment    string   `json:"environment,omitempty"`
	ComponentName  string   `json:"componentName,omitempty"`
	Tags           []string `json:"tags"`
	Content        string   `json:"content"`
}

func (s *Service) WithReportTraceRepositories(agentRuns ReportAgentRunLister, skillRuns ReportSkillRunLister) *Service {
	s.reportAgentRuns = agentRuns
	s.reportSkillRuns = skillRuns
	return s
}

func (s *Service) BuildReport(ctx context.Context, actor *model.AppUser, runID int64) (*RCAReport, error) {
	detail, err := s.GetDetail(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	report := &RCAReport{
		Version: RCAReportVersion, RunID: runID, Status: detail.Run.Status,
		Query: safeReportText(detail.Run.Query, 4096), Scope: safeReportJSON(detail.Run.Scope, 16<<10),
		Timeline: []RCAReportTimelineEntry{}, RootCauseCandidates: []RCAReportRootCause{},
		RejectedHypotheses: []RCAReportHypothesis{}, Investigation: []RCAReportInvestigation{},
		MissingEvidence: []string{}, Suggestions: []RCAReportSuggestion{}, RiskNotices: []string{},
		Evidence: RCAReportEvidenceGroups{
			Facts: []RCAReportEvidenceRef{}, Rules: []RCAReportEvidenceRef{},
			Knowledge: []RCAReportEvidenceRef{}, Hypotheses: []RCAReportEvidenceRef{},
		},
		Traceability: RCAReportTraceability{
			RCARunID: runID, WorkflowRunID: detail.Run.WorkflowRunID,
			AgentRunIDs: []int64{}, SkillRunIDs: []int64{}, RoundIDs: []int64{},
			ActionIDs: []int64{}, EvidenceIDs: []int64{},
		},
		GeneratedAt: s.now(),
	}
	for _, round := range detail.Rounds {
		report.Traceability.RoundIDs = append(report.Traceability.RoundIDs, round.ID)
	}
	for _, action := range detail.Actions {
		report.Traceability.ActionIDs = append(report.Traceability.ActionIDs, action.ID)
		report.Traceability.SkillRunIDs = append(
			report.Traceability.SkillRunIDs, int64ValuesFromJSON(action.Output, "skillRunId")...,
		)
	}
	if detail.Run.StopReason != nil {
		report.StopReason = safeReportText(*detail.Run.StopReason, 160)
	}
	report.ImpactScope = buildReportImpactScope(detail)
	evidenceByID := map[int64]RCAReportEvidenceRef{}
	for _, record := range detail.Evidence {
		ref := reportEvidenceRef(record)
		evidenceByID[record.ID] = ref
		report.Traceability.EvidenceIDs = append(report.Traceability.EvidenceIDs, record.ID)
		switch ref.Kind {
		case model.EvidenceKindRule:
			report.Evidence.Rules = append(report.Evidence.Rules, ref)
		case model.EvidenceKindKnowledge:
			report.Evidence.Knowledge = append(report.Evidence.Knowledge, ref)
		case model.EvidenceKindModelHypothesis:
			report.Evidence.Hypotheses = append(report.Evidence.Hypotheses, ref)
		default:
			report.Evidence.Facts = append(report.Evidence.Facts, ref)
		}
		report.Traceability.SkillRunIDs = append(report.Traceability.SkillRunIDs, int64ValuesFromJSON(record.SourceRef, "skillRunId")...)
		report.Traceability.AgentRunIDs = append(report.Traceability.AgentRunIDs, int64ValuesFromJSON(record.SourceRef, "agentRunId")...)
	}
	rejected := rejectedReportHypotheses(detail.Rounds, evidenceByID)
	report.RejectedHypotheses = rejected
	report.RootCauseCandidates = reportRootCauses(detail.Candidates, rejected, evidenceByID, detail.Evidence)
	report.Investigation, report.Timeline = buildReportInvestigation(detail, evidenceByID, report.StopReason)
	report.MissingEvidence = reportMissingEvidence(detail, report)
	report.Incomplete = reportIsIncomplete(detail.Run.Status, detail.Rounds, report.MissingEvidence)
	report.Conclusion = reportConclusion(report)
	report.Suggestions = reportSuggestions(report)
	report.RiskNotices = reportRisks(report)
	report.Traceability = s.completeReportTraceability(ctx, report.Traceability)
	return report, nil
}

func (s *Service) BuildReportDrafts(ctx context.Context, actor *model.AppUser, runID int64) (*RCAReportDrafts, error) {
	report, err := s.BuildReport(ctx, actor, runID)
	if err != nil {
		return nil, err
	}
	evidenceKeys := []string{}
	for _, ref := range allReportEvidence(report.Evidence) {
		evidenceKeys = append(evidenceKeys, ref.EvidenceKey)
	}
	rootCauses := []RCAIncidentRootCauseDraft{}
	for _, candidate := range report.RootCauseCandidates {
		if candidate.Status == "rejected" {
			continue
		}
		details, _ := json.Marshal(map[string]any{
			"status": candidate.Status, "evidenceStrength": candidate.EvidenceStrength,
			"supportingEvidenceIds":    reportEvidenceIDs(candidate.SupportingEvidence),
			"contradictingEvidenceIds": reportEvidenceIDs(candidate.ContradictingEvidence),
			"sourceRcaRunId":           report.RunID,
		})
		rootCauses = append(rootCauses, RCAIncidentRootCauseDraft{
			Summary: candidate.Summary, Score: candidate.Confidence, Details: details,
		})
	}
	title := truncateText("RCA: "+report.Query, 255)
	return &RCAReportDrafts{
		Incident: RCAIncidentDraft{
			Draft: true, SourceRCARunID: report.RunID, Title: title,
			Severity: model.IncidentSeverityWarning, Status: model.IncidentStatusOpen,
			Environment: report.ImpactScope.Environment, ComponentName: report.ImpactScope.ServiceName,
			Summary: report.Conclusion, EvidenceKeys: uniqueStrings(evidenceKeys), RootCauses: rootCauses,
		},
		RCADocument: RCADocumentDraft{
			Draft: true, SourceRCARunID: report.RunID, Title: title,
			FileName: fmt.Sprintf("rca-run-%d.md", report.RunID), FileType: "text/markdown",
			DocType: "rca", Environment: report.ImpactScope.Environment,
			ComponentName: report.ImpactScope.ServiceName, Tags: []string{"rca", "draft"},
			Content: renderReportMarkdown(report),
		},
	}, nil
}

func reportEvidenceRef(record model.EvidenceRecord) RCAReportEvidenceRef {
	kind := model.EvidenceKindFact
	if record.EvidenceKind != nil && strings.TrimSpace(*record.EvidenceKind) != "" {
		kind = strings.TrimSpace(*record.EvidenceKind)
	}
	skill := ""
	if record.SourceSkill != nil {
		skill = safeReportText(*record.SourceSkill, 120)
	}
	return RCAReportEvidenceRef{
		ID: record.ID, EvidenceKey: safeReportText(record.EvidenceKey, 100), Kind: kind,
		SourceType: safeReportText(record.SourceType, 80), SourceSkill: skill,
		Summary: safeReportText(record.Summary, 1024), Confidence: record.Confidence,
		RoundID: record.RCARoundID, ActionID: record.RCAActionID, DataSourceID: record.DataSourceID,
		URL: fmt.Sprintf("/api/evidence/%d", record.ID),
	}
}

func buildReportImpactScope(detail *Detail) RCAReportImpactScope {
	scope := decodePlannerScope(detail.Run.Scope)
	result := RCAReportImpactScope{
		ServiceName: scope.string("serviceName", "component"),
		Environment: scope.string("environment"), Namespace: scope.string("namespace"),
		WindowStart: scope.string("from", "startTime", "windowStart"),
		WindowEnd:   scope.string("to", "endTime", "windowEnd"), Entities: []string{},
	}
	for _, record := range detail.Evidence {
		var entity map[string]any
		if json.Unmarshal(record.Entity, &entity) == nil {
			for _, key := range []string{"serviceName", "component", "nodeKey", "namespace", "host"} {
				if value := strings.TrimSpace(fmt.Sprint(entity[key])); value != "" && value != "<nil>" {
					result.Entities = append(result.Entities, safeReportText(value, 240))
				}
			}
		}
		if result.WindowStart == "" && record.WindowStart != nil {
			result.WindowStart = record.WindowStart.UTC().Format(time.RFC3339)
		}
		if result.WindowEnd == "" && record.WindowEnd != nil {
			result.WindowEnd = record.WindowEnd.UTC().Format(time.RFC3339)
		}
	}
	if result.ServiceName != "" {
		result.Entities = append(result.Entities, result.ServiceName)
	}
	result.Entities = uniqueStrings(result.Entities)
	return result
}

func rejectedReportHypotheses(rounds []model.RCARound, evidence map[int64]RCAReportEvidenceRef) []RCAReportHypothesis {
	result := []RCAReportHypothesis{}
	seen := map[string]struct{}{}
	for _, round := range rounds {
		var rejected []Hypothesis
		if json.Unmarshal(round.RejectedHypotheses, &rejected) != nil {
			continue
		}
		for _, item := range rejected {
			key := item.ID + "\x00" + strings.TrimSpace(item.Summary)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, RCAReportHypothesis{
				ID: safeReportText(item.ID, 160), Summary: safeReportText(item.Summary, 2048),
				Confidence: item.Confidence, Evidence: evidenceRefs(item.EvidenceIDs, evidence), Status: "rejected",
			})
		}
	}
	return result
}

func reportRootCauses(
	candidates []model.RCARootCauseCandidate,
	rejected []RCAReportHypothesis,
	evidence map[int64]RCAReportEvidenceRef,
	records []model.EvidenceRecord,
) []RCAReportRootCause {
	sources := map[int64]string{}
	for _, record := range records {
		sources[record.ID] = record.SourceType
	}
	result := make([]RCAReportRootCause, 0, len(candidates))
	for _, candidate := range candidates {
		var supportingIDs []int64
		_ = json.Unmarshal(candidate.EvidenceIDs, &supportingIDs)
		supportingIDs = uniqueIDs(supportingIDs)
		contradicting := []RCAReportEvidenceRef{}
		for _, hypothesis := range rejected {
			if normalizedReportSummary(hypothesis.Summary) == normalizedReportSummary(candidate.Summary) {
				contradicting = append(contradicting, hypothesis.Evidence...)
			}
		}
		sourceSet := map[string]struct{}{}
		for _, id := range supportingIDs {
			if source := strings.TrimSpace(sources[id]); source != "" {
				sourceSet[source] = struct{}{}
			}
		}
		score := candidate.Confidence*.7 +
			float64(minIntRCA(len(sourceSet), 3))*.1 +
			float64(minIntRCA(len(supportingIDs), 5))*.02
		if score > 1 {
			score = 1
		}
		strength := "weak"
		if len(supportingIDs) > 0 {
			strength = "moderate"
		}
		if len(sourceSet) >= 2 && candidate.Confidence >= .75 {
			strength = "strong"
		}
		status := "candidate"
		if candidate.Rejected {
			status = "rejected"
		}
		result = append(result, RCAReportRootCause{
			ID: candidate.ID, Summary: safeReportText(candidate.Summary, 4096), Status: status,
			Confidence: candidate.Confidence, EvidenceStrength: strength, EvidenceStrengthScore: score,
			SupportingEvidence: evidenceRefs(supportingIDs, evidence), ContradictingEvidence: uniqueEvidenceRefs(contradicting),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Status != result[j].Status {
			return result[i].Status != "rejected"
		}
		if result[i].EvidenceStrengthScore == result[j].EvidenceStrengthScore {
			return result[i].ID < result[j].ID
		}
		return result[i].EvidenceStrengthScore > result[j].EvidenceStrengthScore
	})
	return result
}

func buildReportInvestigation(
	detail *Detail,
	evidence map[int64]RCAReportEvidenceRef,
	stopReason string,
) ([]RCAReportInvestigation, []RCAReportTimelineEntry) {
	roundNumberByID := map[int64]int{}
	investigations := make([]RCAReportInvestigation, 0, len(detail.Rounds))
	timeline := []RCAReportTimelineEntry{}
	for _, round := range detail.Rounds {
		roundNumberByID[round.ID] = round.RoundNumber
		var newEvidenceIDs []int64
		_ = json.Unmarshal(round.NewEvidenceIDs, &newEvidenceIDs)
		item := RCAReportInvestigation{
			RoundNumber: round.RoundNumber, Status: round.Status,
			Checked: []RCAReportCheckedAction{}, Findings: evidenceRefs(newEvidenceIDs, evidence),
		}
		var next []NextAction
		_ = json.Unmarshal(round.NextActions, &next)
		if len(next) > 0 && round.RoundNumber < detail.Run.CurrentRound {
			skills := []string{}
			for _, action := range next {
				skills = append(skills, action.SkillName)
			}
			item.ContinueReason = "前序证据需要继续通过只读 Skill 验证：" + strings.Join(uniqueStrings(skills), "、")
		} else if round.RoundNumber < detail.Run.CurrentRound {
			item.ContinueReason = "本轮结束后仍有未验证假设或证据缺口，因此进入下一轮调查。"
		}
		if round.RoundNumber == detail.Run.CurrentRound {
			item.StopReason = stopReason
		}
		investigations = append(investigations, item)
		timeline = append(timeline, RCAReportTimelineEntry{
			Type: "round", RoundNumber: round.RoundNumber, ReferenceID: round.ID,
			Status: round.Status, Summary: fmt.Sprintf("第 %d 轮调查：%s", round.RoundNumber, round.Status),
			OccurredAt: round.StartedAt,
		})
	}
	for _, action := range detail.Actions {
		number := roundNumberByID[action.RoundID]
		ids := []int64{}
		_ = json.Unmarshal(action.EvidenceIDs, &ids)
		errorCode := ""
		if action.ErrorCode != nil {
			errorCode = safeReportText(*action.ErrorCode, 80)
		}
		for index := range investigations {
			if investigations[index].RoundNumber == number {
				investigations[index].Checked = append(investigations[index].Checked, RCAReportCheckedAction{
					ActionID: action.ID, ActionKey: safeReportText(action.ActionKey, 160),
					SkillName: safeReportText(action.SkillName, 120), Status: action.Status,
					EvidenceIDs: uniqueIDs(ids), ErrorCode: errorCode,
				})
				break
			}
		}
		timeline = append(timeline, RCAReportTimelineEntry{
			Type: "action", RoundNumber: number, ReferenceID: action.ID, Status: action.Status,
			Summary: safeReportText(action.SkillName, 120) + "：" + action.Status, OccurredAt: action.StartedAt,
		})
	}
	for _, record := range detail.Evidence {
		number := 0
		if record.RCARoundID != nil {
			number = roundNumberByID[*record.RCARoundID]
		}
		occurredAt := record.ObservedAt
		if occurredAt == nil && !record.CreatedAt.IsZero() {
			value := record.CreatedAt
			occurredAt = &value
		}
		timeline = append(timeline, RCAReportTimelineEntry{
			Type: "evidence", RoundNumber: number, ReferenceID: record.ID,
			Summary: safeReportText(record.Summary, 1024), OccurredAt: occurredAt,
		})
	}
	sort.SliceStable(timeline, func(i, j int) bool {
		if timeline[i].OccurredAt == nil {
			return false
		}
		if timeline[j].OccurredAt == nil {
			return true
		}
		return timeline[i].OccurredAt.Before(*timeline[j].OccurredAt)
	})
	return investigations, timeline
}

func reportMissingEvidence(detail *Detail, report *RCAReport) []string {
	result := []string{}
	for _, round := range detail.Rounds {
		if round.Status == model.RCARoundStatusPartialSuccess || round.Status == model.RCARoundStatusFailed ||
			round.Status == model.RCARoundStatusTimedOut {
			result = append(result, fmt.Sprintf("第 %d 轮证据不完整（%s）", round.RoundNumber, round.Status))
		}
	}
	for _, action := range detail.Actions {
		if action.Status == model.RCAActionStatusSuccess {
			continue
		}
		code := "missing_evidence"
		if action.ErrorCode != nil && strings.TrimSpace(*action.ErrorCode) != "" {
			code = safeReportText(*action.ErrorCode, 80)
		}
		result = append(result, fmt.Sprintf("%s：%s", safeReportText(action.SkillName, 120), code))
	}
	if len(allReportEvidence(report.Evidence)) == 0 {
		result = append(result, "没有可访问的 RCA Evidence")
	}
	if len(report.RootCauseCandidates) == 0 {
		result = append(result, "没有具备 Evidence 引用的根因候选")
	}
	return uniqueStrings(result)
}

func reportIsIncomplete(status string, rounds []model.RCARound, missing []string) bool {
	if status != model.RCARunStatusSuccess || len(missing) > 0 {
		return true
	}
	for _, round := range rounds {
		if round.Status != model.RCARoundStatusSuccess {
			return true
		}
	}
	return false
}

func reportConclusion(report *RCAReport) string {
	for _, candidate := range report.RootCauseCandidates {
		if candidate.Status == "candidate" && len(candidate.SupportingEvidence) > 0 {
			return fmt.Sprintf("当前最高优先级根因候选：%s（置信度 %.0f%%，证据强度：%s）。该结论仍是候选，需人工确认。",
				candidate.Summary, candidate.Confidence*100, candidate.EvidenceStrength)
		}
	}
	return "暂无法定位：当前没有具备充分 Evidence 支撑的根因候选。"
}

func reportSuggestions(report *RCAReport) []RCAReportSuggestion {
	result := []RCAReportSuggestion{}
	if len(report.RootCauseCandidates) > 0 {
		candidate := report.RootCauseCandidates[0]
		result = append(result, RCAReportSuggestion{
			Summary:     "变更前请人工复核最高优先级候选及其支持、反证 Evidence。",
			EvidenceIDs: reportEvidenceIDs(candidate.SupportingEvidence), AdvisoryOnly: true, AutoExecute: false,
		})
	}
	if report.Incomplete {
		result = append(result, RCAReportSuggestion{
			Summary:      "补齐 missingEvidence 中的数据源或查询结果后重新运行 RCA。",
			AdvisoryOnly: true, AutoExecute: false,
		})
	}
	for _, ref := range allReportEvidence(report.Evidence) {
		if strings.EqualFold(ref.SourceType, model.DataSourceTypeTiDB) {
			result = append(result, RCAReportSuggestion{
				Summary:     "由数据库负责人结合慢 SQL 指纹、执行计划、锁和统计信息评估优化方案。",
				EvidenceIDs: []int64{ref.ID}, AdvisoryOnly: true, AutoExecute: false,
			})
			break
		}
	}
	if len(result) == 0 {
		result = append(result, RCAReportSuggestion{
			Summary:      "保持观察并补充与当前时间窗一致的日志、指标和拓扑 Evidence。",
			AdvisoryOnly: true, AutoExecute: false,
		})
	}
	return result
}

func reportRisks(report *RCAReport) []string {
	result := []string{"所有建议仅供人工评估，平台不会自动执行修复、SQL 或配置变更。"}
	if report.Incomplete {
		result = append(result, "报告包含 partial_success 或缺失证据，结论可能随补充数据发生变化。")
	}
	if len(report.RootCauseCandidates) > 0 {
		result = append(result, "根因条目均为候选或已驳回假设，不会作为已确认事实展示。")
	}
	return result
}

func (s *Service) completeReportTraceability(ctx context.Context, trace RCAReportTraceability) RCAReportTraceability {
	if trace.WorkflowRunID != nil {
		if s.reportAgentRuns != nil {
			if runs, err := s.reportAgentRuns.ListAgentRunsByWorkflowRunID(ctx, *trace.WorkflowRunID); err == nil {
				for _, run := range runs {
					trace.AgentRunIDs = append(trace.AgentRunIDs, run.ID)
				}
			}
		}
		if s.reportSkillRuns != nil {
			if runs, err := s.reportSkillRuns.ListSkillRunsByWorkflowRunID(ctx, *trace.WorkflowRunID); err == nil {
				for _, run := range runs {
					trace.SkillRunIDs = append(trace.SkillRunIDs, run.ID)
				}
			}
		}
	}
	trace.AgentRunIDs = uniqueIDs(trace.AgentRunIDs)
	trace.SkillRunIDs = uniqueIDs(trace.SkillRunIDs)
	trace.RoundIDs = uniqueIDs(trace.RoundIDs)
	trace.ActionIDs = uniqueIDs(trace.ActionIDs)
	trace.EvidenceIDs = uniqueIDs(trace.EvidenceIDs)
	return trace
}

func renderReportMarkdown(report *RCAReport) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# %s\n\n", safeReportText("RCA 报告："+report.Query, 4096))
	fmt.Fprintf(&builder, "- RCA Run：%d\n- 状态：%s\n- 停止原因：%s\n- 结论：%s\n\n",
		report.RunID, report.Status, report.StopReason, report.Conclusion)
	builder.WriteString("## 影响范围\n\n")
	fmt.Fprintf(&builder, "- 服务：%s\n- 环境：%s\n- 时间窗：%s ～ %s\n\n",
		report.ImpactScope.ServiceName, report.ImpactScope.Environment,
		report.ImpactScope.WindowStart, report.ImpactScope.WindowEnd)
	builder.WriteString("## 根因候选\n\n")
	if len(report.RootCauseCandidates) == 0 {
		builder.WriteString("- 暂无法定位。\n")
	}
	for _, candidate := range report.RootCauseCandidates {
		fmt.Fprintf(&builder, "- [%s] %s（%.0f%%，%s）\n", candidate.Status, candidate.Summary,
			candidate.Confidence*100, candidate.EvidenceStrength)
		for _, evidence := range candidate.SupportingEvidence {
			fmt.Fprintf(&builder, "  - 支持 Evidence [%d](%s)：%s\n", evidence.ID, evidence.URL, evidence.Summary)
		}
		for _, evidence := range candidate.ContradictingEvidence {
			fmt.Fprintf(&builder, "  - 反证 Evidence [%d](%s)：%s\n", evidence.ID, evidence.URL, evidence.Summary)
		}
	}
	builder.WriteString("\n## 排查过程\n\n")
	for _, item := range report.Investigation {
		fmt.Fprintf(&builder, "### 第 %d 轮（%s）\n\n", item.RoundNumber, item.Status)
		for _, checked := range item.Checked {
			fmt.Fprintf(&builder, "- 查询：%s（%s）\n", checked.SkillName, checked.Status)
		}
		if item.ContinueReason != "" {
			fmt.Fprintf(&builder, "- 继续原因：%s\n", item.ContinueReason)
		}
		if item.StopReason != "" {
			fmt.Fprintf(&builder, "- 停止原因：%s\n", item.StopReason)
		}
	}
	builder.WriteString("\n## 缺失证据\n\n")
	if len(report.MissingEvidence) == 0 {
		builder.WriteString("- 无。\n")
	}
	for _, missing := range report.MissingEvidence {
		fmt.Fprintf(&builder, "- %s\n", missing)
	}
	builder.WriteString("\n## 建议与风险\n\n")
	for _, suggestion := range report.Suggestions {
		fmt.Fprintf(&builder, "- %s（仅供建议，不自动执行）\n", suggestion.Summary)
	}
	for _, risk := range report.RiskNotices {
		fmt.Fprintf(&builder, "- 风险：%s\n", risk)
	}
	return safeReportText(builder.String(), 256<<10)
}

func evidenceRefs(ids []int64, evidence map[int64]RCAReportEvidenceRef) []RCAReportEvidenceRef {
	result := []RCAReportEvidenceRef{}
	for _, id := range uniqueIDs(ids) {
		if ref, exists := evidence[id]; exists {
			result = append(result, ref)
		}
	}
	return result
}

func uniqueEvidenceRefs(values []RCAReportEvidenceRef) []RCAReportEvidenceRef {
	seen := map[int64]struct{}{}
	result := []RCAReportEvidenceRef{}
	for _, value := range values {
		if _, exists := seen[value.ID]; exists {
			continue
		}
		seen[value.ID] = struct{}{}
		result = append(result, value)
	}
	return result
}

func allReportEvidence(groups RCAReportEvidenceGroups) []RCAReportEvidenceRef {
	result := []RCAReportEvidenceRef{}
	result = append(result, groups.Facts...)
	result = append(result, groups.Rules...)
	result = append(result, groups.Knowledge...)
	result = append(result, groups.Hypotheses...)
	return result
}

func reportEvidenceIDs(values []RCAReportEvidenceRef) []int64 {
	result := make([]int64, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return uniqueIDs(result)
}

func int64ValuesFromJSON(raw json.RawMessage, key string) []int64 {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return findInt64JSONValues(value, key)
}

func findInt64JSONValues(value any, key string) []int64 {
	result := []int64{}
	switch typed := value.(type) {
	case map[string]any:
		for field, nested := range typed {
			if strings.EqualFold(field, key) {
				switch number := nested.(type) {
				case float64:
					result = append(result, int64(number))
				case []any:
					for _, item := range number {
						if parsed, ok := item.(float64); ok {
							result = append(result, int64(parsed))
						}
					}
				}
			}
			result = append(result, findInt64JSONValues(nested, key)...)
		}
	case []any:
		for _, nested := range typed {
			result = append(result, findInt64JSONValues(nested, key)...)
		}
	}
	return uniqueIDs(result)
}

func normalizedReportSummary(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func safeReportText(value string, maximum int) string {
	value = auditutil.SanitizeText(strings.TrimSpace(value))
	lower := strings.ToLower(value)
	sqlIndex := -1
	for _, keyword := range []string{"select ", "show ", "explain ", "update ", "insert ", "delete "} {
		if index := strings.Index(lower, keyword); index >= 0 && (sqlIndex < 0 || index < sqlIndex) {
			sqlIndex = index
		}
	}
	if sqlIndex >= 0 {
		value = value[:sqlIndex] + tidbsvc.SanitizeSQLForEvidence(value[sqlIndex:])
	}
	return truncateText(value, maximum)
}

func safeReportJSON(value json.RawMessage, maximum int) json.RawMessage {
	// Redact the complete object before enforcing the output size. Returning a
	// raw truncated preview could otherwise cut through an SQL or credential
	// field before the report-specific sanitizer sees it.
	sanitized := auditutil.SanitizeJSON(value, 0)
	if len(sanitized) == 0 {
		return json.RawMessage(`{}`)
	}
	var decoded any
	if json.Unmarshal(sanitized, &decoded) != nil {
		return json.RawMessage(`{}`)
	}
	decoded = sanitizeReportSQLFields(decoded)
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	if maximum > 0 && len(encoded) > maximum {
		return json.RawMessage(`{"truncated":true}`)
	}
	return encoded
}

func sanitizeReportSQLFields(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
			switch normalized {
			case "user", "username", "account", "accountname", "email", "phone", "mobile", "idcard", "ssn":
				result[key] = auditutil.RedactedValue
				continue
			}
			if (normalized == "sql" || normalized == "readonlysql" || normalized == "querysql") && nested != nil {
				result[key] = tidbsvc.SanitizeSQLForEvidence(fmt.Sprint(nested))
				continue
			}
			result[key] = sanitizeReportSQLFields(nested)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, nested := range typed {
			result = append(result, sanitizeReportSQLFields(nested))
		}
		return result
	default:
		return typed
	}
}
