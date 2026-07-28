package skillframework

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	logssvc "aiops-platform/backend/internal/logs"
	"aiops-platform/backend/internal/model"
	ragsvc "aiops-platform/backend/internal/rag"
	"aiops-platform/backend/internal/repository"
)

func TestSearchKnowledgeSkillOutputMatchesSchema(t *testing.T) {
	title := "支付排障手册"
	skill := SearchKnowledgeSkill{repository: fakeKnowledgeSearcher{chunks: []model.KBChunk{
		{ID: 1, DocumentID: 10, ChunkIndex: 0, Content: "检查连接池", SourceTitle: &title},
	}}}

	output, err := skill.Execute(context.Background(), json.RawMessage(`{"query":"连接池","limit":3}`))
	if err != nil {
		t.Fatalf("execute search_knowledge: %v", err)
	}
	if err := ValidateJSONSchema(skill.Definition().OutputSchema, output); err != nil {
		t.Fatalf("output schema mismatch: %v, output=%s", err, string(output))
	}
}

func TestHybridSearchKnowledgeSkillIsReadOnlyActorScopedAndAudited(t *testing.T) {
	searcher := &fakeHybridKnowledgeSearcher{}
	audit := newMemoryAudit()
	registry, err := NewRegistry(nil, audit, HybridKnowledgeSkills(searcher)...)
	if err != nil {
		t.Fatalf("register hybrid skill: %v", err)
	}
	definition, err := registry.Get("hybrid_search_knowledge")
	if err != nil || !definition.ReadOnly || definition.RiskLevel != model.SkillRiskSafeRead {
		t.Fatalf("definition=%+v err=%v", definition, err)
	}
	actor := &model.AppUser{ID: 17, Role: model.RoleUser}
	executed, err := registry.Execute(context.Background(), ExecuteInput{
		Actor: actor, Name: "hybrid_search_knowledge",
		Payload: json.RawMessage(`{
			"originalQuestion":"订单服务变慢，请查询可能原因",
			"confirmedEntities":["order-service"],
			"logTemplates":["trace_id=abcdef0123456789 db call timeout"],
			"metricAnomalySummaries":["db latency p99 high"],
			"limit":5
		}`),
	})
	if err != nil {
		t.Fatalf("execute hybrid skill: %v", err)
	}
	if searcher.actor == nil || searcher.actor.ID != actor.ID || searcher.input.OriginalQuestion == "" {
		t.Fatalf("actor/input not forwarded: actor=%+v input=%+v", searcher.actor, searcher.input)
	}
	if err := ValidateJSONSchema(definition.OutputSchema, executed.Output); err != nil {
		t.Fatalf("output schema: %v output=%s", err, executed.Output)
	}
	runs, err := audit.ListSkillRuns(context.Background(), 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("audit runs=%+v err=%v", runs, err)
	}
	output := string(runs[0].OutputSummary)
	for _, expected := range []string{`"recallCount":1`, `"permissionScope":"actor_published"`, `"selectedCount":1`} {
		if !strings.Contains(output, expected) {
			t.Fatalf("audit output missing %s: %s", expected, output)
		}
	}
}

func TestQueryLogsSkillUsesLogServiceAndOutputMatchesSchema(t *testing.T) {
	fakeLogs := &fakeLogQuerier{result: &logssvc.QueryResult{
		Items: []model.LogItem{{Timestamp: time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC), Level: "ERROR", Message: "boom"}},
		Total: 1,
	}}
	skill := QueryLogsSkill{logs: fakeLogs}
	ctx := ContextWithActor(context.Background(), adminActor())

	output, err := skill.Execute(ctx, json.RawMessage(`{
		"dataSourceId": 1,
		"from": "2026-07-12T10:00:00Z",
		"to": "2026-07-12T10:05:00Z",
		"keyword": "boom",
		"size": 10
	}`))
	if err != nil {
		t.Fatalf("execute query_logs: %v", err)
	}
	if fakeLogs.lastInput.DataSourceID != 1 || fakeLogs.lastInput.Keyword != "boom" {
		t.Fatalf("unexpected log query input: %+v", fakeLogs.lastInput)
	}
	if err := ValidateJSONSchema(skill.Definition().OutputSchema, output); err != nil {
		t.Fatalf("output schema mismatch: %v, output=%s", err, string(output))
	}
}

func TestAggregateLogTemplatesSkillOutputMatchesSchema(t *testing.T) {
	skill := AggregateLogTemplatesSkill{}
	output, err := skill.Execute(context.Background(), json.RawMessage(`{
		"items": [
			{"timestamp":"2026-07-12T10:00:00Z","level":"error","message":"request 123 failed password=secret","pod":"api-0"},
			{"timestamp":"2026-07-12T10:00:30Z","level":"error","message":"request 456 failed password=secret","pod":"api-0"}
		]
	}`))
	if err != nil {
		t.Fatalf("execute aggregate_log_templates: %v", err)
	}
	if err := ValidateJSONSchema(skill.Definition().OutputSchema, output); err != nil {
		t.Fatalf("output schema mismatch: %v, output=%s", err, string(output))
	}
	var result struct {
		Clusters []logssvc.TemplateCluster `json:"clusters"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(result.Clusters) != 1 {
		t.Fatalf("clusters = %+v, want one normalized template", result.Clusters)
	}
}

func TestExtractLogEntitiesSkillOutputMatchesSchemaAndRedacts(t *testing.T) {
	skill := ExtractLogEntitiesSkill{}
	output, err := skill.Execute(context.Background(), json.RawMessage(`{
		"items": [
			{"timestamp":"2026-07-12T10:00:00Z","level":"warn","message":"token=abc failed","host":"node-1","namespace":"prod","pod":"api-0","container":"app","traceId":"t1","requestId":"r1","errorCode":"E_CONN"}
		]
	}`))
	if err != nil {
		t.Fatalf("execute extract_log_entities: %v", err)
	}
	if err := ValidateJSONSchema(skill.Definition().OutputSchema, output); err != nil {
		t.Fatalf("output schema mismatch: %v, output=%s", err, string(output))
	}
	var result struct {
		Entities       map[string][]string `json:"entities"`
		RedactionCount int                 `json:"redactionCount"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if result.Entities["pods"][0] != "api-0" || result.RedactionCount == 0 {
		t.Fatalf("unexpected entities: %+v", result)
	}
}

type fakeKnowledgeSearcher struct {
	chunks []model.KBChunk
}

type fakeHybridKnowledgeSearcher struct {
	actor *model.AppUser
	input ragsvc.RCAKnowledgeSearchInput
}

func (f *fakeHybridKnowledgeSearcher) SearchForRCA(_ context.Context, actor *model.AppUser, input ragsvc.RCAKnowledgeSearchInput) (*ragsvc.RCAKnowledgeSearchResult, error) {
	f.actor, f.input = actor, input
	return &ragsvc.RCAKnowledgeSearchResult{
		Input: ragsvc.RCARetrievalInput{
			OriginalQuestion: input.OriginalQuestion, ComposedQuery: input.OriginalQuestion,
		},
		RewrittenQuery: "订单服务 数据库调用慢",
		Citations: []ragsvc.Citation{{
			CitationID: "KC-9-11", DocumentID: 3, DocumentVersionID: 9, ChunkID: 11, ChunkIDs: []int64{11},
		}},
		Context:     []ragsvc.ContextBlock{{CitationID: "KC-9-11", DocumentID: 3, DocumentVersionID: 9, ChunkIDs: []int64{11}}},
		RecallCount: 1,
		RetrievalTrace: ragsvc.RetrievalTrace{
			Filters: repository.KnowledgeRetrievalFilter{PermissionScope: "actor_published", ActorUserID: actor.ID, ActorRole: actor.Role},
			Context: ragsvc.ContextBuildTrace{Selected: 1},
		},
		DegradedChannels: []string{},
	}, nil
}

func (f fakeKnowledgeSearcher) SearchChunks(context.Context, string, int) ([]model.KBChunk, error) {
	return f.chunks, nil
}

type fakeLogQuerier struct {
	result    *logssvc.QueryResult
	lastInput logssvc.QueryInput
}

func (f *fakeLogQuerier) Query(_ context.Context, _ *model.AppUser, input logssvc.QueryInput) (*logssvc.QueryResult, error) {
	f.lastInput = input
	return f.result, nil
}
