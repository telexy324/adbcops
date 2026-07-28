package skillframework

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	logssvc "aiops-platform/backend/internal/logs"
	"aiops-platform/backend/internal/model"
	ragsvc "aiops-platform/backend/internal/rag"
)

type KnowledgeSearcher interface {
	SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error)
}

type LogQuerier interface {
	Query(ctx context.Context, actor *model.AppUser, input logssvc.QueryInput) (*logssvc.QueryResult, error)
}

type HybridKnowledgeSearcher interface {
	SearchForRCA(ctx context.Context, actor *model.AppUser, input ragsvc.RCAKnowledgeSearchInput) (*ragsvc.RCAKnowledgeSearchResult, error)
}

func LogAndKnowledgeSkills(knowledge KnowledgeSearcher, logs LogQuerier) []Skill {
	return []Skill{
		SearchKnowledgeSkill{repository: knowledge},
		QueryLogsSkill{logs: logs},
		AggregateLogTemplatesSkill{},
		ExtractLogEntitiesSkill{},
	}
}

func HybridKnowledgeSkills(searcher HybridKnowledgeSearcher) []Skill {
	return []Skill{HybridSearchKnowledgeSkill{searcher: searcher}}
}

type HybridSearchKnowledgeSkill struct {
	searcher HybridKnowledgeSearcher
}

func (s HybridSearchKnowledgeSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:        "hybrid_search_knowledge",
		Version:     "v1",
		Description: "Search current published knowledge for RCA with query understanding, hybrid retrieval, RRF, rerank, context building, citations and degradation trace.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"required":["originalQuestion"],
			"properties":{
				"originalQuestion":{"type":"string","minLength":1,"maxLength":8192},
				"confirmedEntities":{"type":"array","maxItems":12,"items":{"type":"string"}},
				"logTemplates":{"type":"array","maxItems":12,"items":{"type":"string"}},
				"metricAnomalySummaries":{"type":"array","maxItems":12,"items":{"type":"string"}},
				"limit":{"type":"integer","minimum":1,"maximum":10}
			}
		}`),
		OutputSchema: json.RawMessage(`{
			"type":"object",
			"required":["retrievalInput","rewrittenQuery","citations","context","recallCount","retrievalTrace","degraded","degradedChannels"],
			"properties":{
				"retrievalInput":{"type":"object"},
				"rewrittenQuery":{"type":"string"},
				"citations":{"type":"array"},
				"context":{"type":"array"},
				"recallCount":{"type":"integer"},
				"retrievalTrace":{"type":"object"},
				"degraded":{"type":"boolean"},
				"degradedChannels":{"type":"array","items":{"type":"string"}}
			}
		}`),
		RiskLevel:     model.SkillRiskSafeRead,
		ReadOnly:      true,
		TimeoutSecond: 30,
	}
}

func (s HybridSearchKnowledgeSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if s.searcher == nil {
		return nil, ErrInvalidInput
	}
	var request ragsvc.RCAKnowledgeSearchInput
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	result, err := s.searcher.SearchForRCA(ctx, ActorFromContext(ctx), request)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

type SearchKnowledgeSkill struct {
	repository KnowledgeSearcher
}

func (s SearchKnowledgeSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "search_knowledge",
		Version:       "v1",
		Description:   "Search published knowledge chunks and return citation-ready snippets.",
		InputSchema:   json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"limit":{"type":"integer"}}}`),
		OutputSchema:  json.RawMessage(`{"type":"object","properties":{"chunks":{"type":"array"},"count":{"type":"integer"}}}`),
		RiskLevel:     model.SkillRiskSafeRead,
		ReadOnly:      true,
		TimeoutSecond: 10,
		RequiredTools: nil,
	}
}

func (s SearchKnowledgeSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	query := strings.TrimSpace(request.Query)
	if query == "" || s.repository == nil {
		return nil, ErrInvalidInput
	}
	limit := request.Limit
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	chunks, err := s.repository.SearchChunks(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	type chunkView struct {
		ID            int64   `json:"id"`
		DocumentID    int64   `json:"documentId"`
		ChunkIndex    int     `json:"chunkIndex"`
		Content       string  `json:"content"`
		SourceTitle   *string `json:"sourceTitle,omitempty"`
		SourceSection *string `json:"sourceSection,omitempty"`
	}
	views := make([]chunkView, 0, len(chunks))
	for _, chunk := range chunks {
		views = append(views, chunkView{
			ID:            chunk.ID,
			DocumentID:    chunk.DocumentID,
			ChunkIndex:    chunk.ChunkIndex,
			Content:       chunk.Content,
			SourceTitle:   chunk.SourceTitle,
			SourceSection: chunk.SourceSection,
		})
	}
	return json.Marshal(map[string]any{"count": len(views), "chunks": views})
}

type QueryLogsSkill struct {
	logs LogQuerier
}

func (s QueryLogsSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "query_logs",
		Version:       "v1",
		Description:   "Query logs from an Elasticsearch/OpenSearch data source with existing service limits.",
		InputSchema:   json.RawMessage(`{"type":"object","required":["dataSourceId","from","to"],"properties":{"dataSourceId":{"type":"integer"},"index":{"type":"string"},"from":{"type":"string"},"to":{"type":"string"},"keyword":{"type":"string"},"queryString":{"type":"string"},"level":{"type":"string"},"size":{"type":"integer"},"allowLargeRange":{"type":"boolean"}}}`),
		OutputSchema:  json.RawMessage(`{"type":"object","properties":{"items":{"type":"array"},"total":{"type":"integer"}}}`),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 20,
		RequiredTools: []string{"elasticsearch"},
	}
}

func (s QueryLogsSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		DataSourceID    int64  `json:"dataSourceId"`
		Index           string `json:"index"`
		From            string `json:"from"`
		To              string `json:"to"`
		Keyword         string `json:"keyword"`
		QueryString     string `json:"queryString"`
		Level           string `json:"level"`
		Size            int    `json:"size"`
		AllowLargeRange bool   `json:"allowLargeRange"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	from, err := parseSkillTime(request.From)
	if err != nil {
		return nil, ErrInvalidInput
	}
	to, err := parseSkillTime(request.To)
	if err != nil {
		return nil, ErrInvalidInput
	}
	if s.logs == nil {
		return nil, ErrInvalidInput
	}
	result, err := s.logs.Query(ctx, ActorFromContext(ctx), logssvc.QueryInput{
		DataSourceID:    request.DataSourceID,
		Index:           request.Index,
		From:            from,
		To:              to,
		Keyword:         request.Keyword,
		QueryString:     request.QueryString,
		Level:           request.Level,
		Size:            request.Size,
		AllowLargeRange: request.AllowLargeRange,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

type AggregateLogTemplatesSkill struct{}

func (AggregateLogTemplatesSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "aggregate_log_templates",
		Version:       "v1",
		Description:   "Preprocess log items and aggregate normalized message templates.",
		InputSchema:   json.RawMessage(`{"type":"object","required":["items"],"properties":{"items":{"type":"array"},"stackMaxLines":{"type":"integer"}}}`),
		OutputSchema:  json.RawMessage(`{"type":"object","properties":{"clusters":{"type":"array"},"timeStats":{"type":"array"},"errorCount":{"type":"integer"}}}`),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 10,
		RequiredTools: nil,
	}
}

func (AggregateLogTemplatesSkill) Execute(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		Items         []model.LogItem `json:"items"`
		StackMaxLines int             `json:"stackMaxLines"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if len(request.Items) == 0 || len(request.Items) > 2000 {
		return nil, ErrInvalidInput
	}
	result := logssvc.Preprocess(logssvc.PreprocessInput{Items: request.Items, StackMaxLines: request.StackMaxLines})
	return json.Marshal(map[string]any{
		"clusters":       result.Clusters,
		"timeStats":      result.TimeStats,
		"errorCount":     result.ErrorCount,
		"totalInput":     result.TotalInput,
		"totalOutput":    result.TotalOutput,
		"redactionCount": result.RedactionCount,
	})
}

type ExtractLogEntitiesSkill struct{}

func (ExtractLogEntitiesSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "extract_log_entities",
		Version:       "v1",
		Description:   "Extract common operational entities from log items after normalization/redaction.",
		InputSchema:   json.RawMessage(`{"type":"object","required":["items"],"properties":{"items":{"type":"array"},"stackMaxLines":{"type":"integer"}}}`),
		OutputSchema:  json.RawMessage(`{"type":"object","properties":{"entities":{"type":"object"}}}`),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 10,
		RequiredTools: nil,
	}
}

func (ExtractLogEntitiesSkill) Execute(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		Items         []model.LogItem `json:"items"`
		StackMaxLines int             `json:"stackMaxLines"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if len(request.Items) == 0 || len(request.Items) > 2000 {
		return nil, ErrInvalidInput
	}
	processed := logssvc.Preprocess(logssvc.PreprocessInput{Items: request.Items, StackMaxLines: request.StackMaxLines})
	entities := map[string][]string{
		"levels":     {},
		"hosts":      {},
		"clusters":   {},
		"namespaces": {},
		"pods":       {},
		"containers": {},
		"traceIds":   {},
		"requestIds": {},
		"errorCodes": {},
	}
	sets := map[string]map[string]struct{}{}
	for key := range entities {
		sets[key] = map[string]struct{}{}
	}
	for _, item := range processed.Items {
		addEntity(sets["levels"], item.Level)
		addEntity(sets["hosts"], item.Host)
		addEntity(sets["clusters"], item.Cluster)
		addEntity(sets["namespaces"], item.Namespace)
		addEntity(sets["pods"], item.Pod)
		addEntity(sets["containers"], item.Container)
		addEntity(sets["traceIds"], item.TraceID)
		addEntity(sets["requestIds"], item.RequestID)
		addEntity(sets["errorCodes"], item.ErrorCode)
	}
	for key, set := range sets {
		entities[key] = sortedSet(set)
	}
	return json.Marshal(map[string]any{"entities": entities, "redactionCount": processed.RedactionCount})
}

func parseSkillTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, ErrInvalidInput
}

func addEntity(set map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		set[value] = struct{}{}
	}
}

func sortedSet(set map[string]struct{}) []string {
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sortStrings(result)
	return result
}

func sortStrings(values []string) {
	if len(values) < 2 {
		return
	}
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
