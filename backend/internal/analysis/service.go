package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	llmsvc "aiops-platform/backend/internal/llm"
	logssvc "aiops-platform/backend/internal/logs"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"aiops-platform/backend/internal/resourcelimit"
)

const (
	defaultKnowledgeLimit = 5
	maxQuestionBytes      = 8192
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrForbidden    = errors.New("analysis access forbidden")
	ErrRateLimited  = errors.New("analysis concurrency limit exceeded")
)

type Repository interface {
	CreateAnalysisTask(ctx context.Context, task *model.AnalysisTask) error
	UpdateAnalysisTask(ctx context.Context, id int64, updates repository.AnalysisTaskUpdates) (*model.AnalysisTask, error)
	ListAnalysisTasks(ctx context.Context, userID *int64) ([]model.AnalysisTask, error)
	FindAnalysisTaskByID(ctx context.Context, id int64) (*model.AnalysisTask, error)
	SearchChunks(ctx context.Context, query string, limit int) ([]model.KBChunk, error)
	FindDefaultEnabledLLMConfig(ctx context.Context) (*model.LLMConfig, error)
}

type LogQuerier interface {
	Query(ctx context.Context, actor *model.AppUser, input logssvc.QueryInput) (*logssvc.QueryResult, error)
}

type SecretManager interface {
	Decrypt(value string) (string, error)
}

type Service struct {
	repository Repository
	logs       LogQuerier
	secrets    SecretManager
	client     llmsvc.Client
	limiter    *resourcelimit.KeyedLimiter
}

type Scope struct {
	Environment   string    `json:"environment"`
	SystemName    string    `json:"systemName"`
	ComponentName string    `json:"componentName"`
	TimeStart     time.Time `json:"timeStart"`
	TimeEnd       time.Time `json:"timeEnd"`
}

type RunInput struct {
	ConversationID *int64
	Question       string
	Scope          Scope
	DataSourceIDs  []int64
}

type Evidence struct {
	Type      string `json:"type"`
	Source    string `json:"source"`
	Summary   string `json:"summary"`
	Reference string `json:"reference,omitempty"`
}

type Citation struct {
	DocumentID    int64   `json:"documentId"`
	ChunkID       int64   `json:"chunkId"`
	ChunkIndex    int     `json:"chunkIndex"`
	SourceTitle   *string `json:"sourceTitle,omitempty"`
	SourceSection *string `json:"sourceSection,omitempty"`
	Snippet       string  `json:"snippet"`
}

type Confidence struct {
	Level   string   `json:"level"`
	Score   float64  `json:"score"`
	Reasons []string `json:"reasons"`
}

type Result struct {
	TaskID              int64                    `json:"taskId"`
	Status              string                   `json:"status"`
	Summary             string                   `json:"summary"`
	Impact              map[string]any           `json:"impact"`
	Timeline            []string                 `json:"timeline"`
	Facts               []string                 `json:"facts"`
	RootCauseCandidates []string                 `json:"rootCauseCandidates"`
	Suggestions         []string                 `json:"suggestions"`
	RiskTips            []string                 `json:"riskTips"`
	Evidence            []Evidence               `json:"evidence"`
	Citations           []Citation               `json:"citations"`
	Confidence          Confidence               `json:"confidence"`
	MissingEvidence     []string                 `json:"missingEvidence"`
	Preprocess          logssvc.PreprocessResult `json:"preprocess"`
	ReportSource        string                   `json:"reportSource"`
}

type RunOutput struct {
	Task   *model.AnalysisTask `json:"task"`
	Result Result              `json:"result"`
}

func NewService(repository Repository, logs LogQuerier, secrets SecretManager, client llmsvc.Client) *Service {
	return &Service{repository: repository, logs: logs, secrets: secrets, client: client, limiter: resourcelimit.NewKeyedLimiter(2)}
}

func (s *Service) SetUserLimiter(limiter *resourcelimit.KeyedLimiter) {
	s.limiter = limiter
}

func (s *Service) RunGeneral(ctx context.Context, actor *model.AppUser, input RunInput) (*RunOutput, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	release, err := s.limiter.Acquire(ctx, fmt.Sprintf("user:%d", actor.ID))
	if err != nil {
		if errors.Is(err, resourcelimit.ErrLimitExceeded) {
			return nil, ErrRateLimited
		}
		return nil, err
	}
	defer release()
	normalized, err := normalizeRunInput(input)
	if err != nil {
		return nil, err
	}
	scopeJSON, _ := json.Marshal(normalized.Scope)
	sourceJSON, _ := json.Marshal(normalized.DataSourceIDs)
	now := time.Now().UTC()
	task := &model.AnalysisTask{
		UserID:         actor.ID,
		ConversationID: normalized.ConversationID,
		TaskType:       model.AnalysisTaskTypeGeneral,
		Question:       normalized.Question,
		Scope:          scopeJSON,
		DataSourceIDs:  sourceJSON,
		Status:         model.AnalysisTaskStatusRunning,
		StartedAt:      &now,
	}
	if err := s.repository.CreateAnalysisTask(ctx, task); err != nil {
		return nil, err
	}
	result, runErr := s.run(ctx, actor, task.ID, normalized)
	finished := time.Now().UTC()
	if runErr != nil {
		message := runErr.Error()
		updated, err := s.repository.UpdateAnalysisTask(ctx, task.ID, repository.AnalysisTaskUpdates{
			Status:       model.AnalysisTaskStatusFailed,
			ErrorMessage: &message,
			FinishedAt:   &finished,
		})
		if err == nil {
			task = updated
		}
		return nil, runErr
	}
	result.TaskID = task.ID
	result.Status = model.AnalysisTaskStatusSuccess
	resultJSON, _ := json.Marshal(result)
	updated, err := s.repository.UpdateAnalysisTask(ctx, task.ID, repository.AnalysisTaskUpdates{
		Status:     model.AnalysisTaskStatusSuccess,
		Summary:    &result.Summary,
		Result:     resultJSON,
		FinishedAt: &finished,
	})
	if err != nil {
		return nil, err
	}
	return &RunOutput{Task: updated, Result: result}, nil
}

func (s *Service) List(ctx context.Context, actor *model.AppUser) ([]model.AnalysisTask, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	var userID *int64
	if actor.Role != model.RoleAdmin {
		id := actor.ID
		userID = &id
	}
	return s.repository.ListAnalysisTasks(ctx, userID)
}

func (s *Service) Get(ctx context.Context, actor *model.AppUser, id int64) (*model.AnalysisTask, error) {
	if actor == nil || id <= 0 {
		return nil, ErrInvalidInput
	}
	task, err := s.repository.FindAnalysisTaskByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if actor.Role != model.RoleAdmin && task.UserID != actor.ID {
		return nil, ErrForbidden
	}
	return task, nil
}

func (s *Service) run(ctx context.Context, actor *model.AppUser, taskID int64, input RunInput) (Result, error) {
	var allLogs []model.LogItem
	for _, dataSourceID := range input.DataSourceIDs {
		query, err := s.logs.Query(ctx, actor, logssvc.QueryInput{
			DataSourceID: dataSourceID,
			From:         input.Scope.TimeStart,
			To:           input.Scope.TimeEnd,
			Keyword:      input.Question,
			Size:         200,
		})
		if err != nil {
			return Result{}, err
		}
		allLogs = append(allLogs, query.Items...)
	}
	preprocessed := logssvc.Preprocess(logssvc.PreprocessInput{Items: allLogs})
	citations, err := s.retrieveKnowledge(ctx, input.Question, preprocessed)
	if err != nil {
		return Result{}, err
	}
	result := buildLocalResult(taskID, input, preprocessed, citations)
	if llmSummary, ok := s.generateLLMSummary(ctx, input, preprocessed, citations); ok {
		result.Summary = llmSummary
		result.ReportSource = "llm"
	} else {
		result.ReportSource = "local-fallback"
	}
	return result, nil
}

func (s *Service) retrieveKnowledge(ctx context.Context, question string, preprocessed logssvc.PreprocessResult) ([]Citation, error) {
	query := question
	if len(preprocessed.Clusters) > 0 {
		query += " " + preprocessed.Clusters[0].Template
	}
	chunks, err := s.repository.SearchChunks(ctx, query, defaultKnowledgeLimit)
	if err != nil {
		return nil, err
	}
	citations := make([]Citation, 0, len(chunks))
	for _, chunk := range chunks {
		citations = append(citations, Citation{
			DocumentID:    chunk.DocumentID,
			ChunkID:       chunk.ID,
			ChunkIndex:    chunk.ChunkIndex,
			SourceTitle:   chunk.SourceTitle,
			SourceSection: chunk.SourceSection,
			Snippet:       snippet(chunk.Content),
		})
	}
	return citations, nil
}

func (s *Service) generateLLMSummary(ctx context.Context, input RunInput, preprocessed logssvc.PreprocessResult, citations []Citation) (string, bool) {
	if s.client == nil {
		return "", false
	}
	config, err := s.repository.FindDefaultEnabledLLMConfig(ctx)
	if err != nil || config == nil {
		return "", false
	}
	apiKey := ""
	if config.APIKeyRef != nil && *config.APIKeyRef != "" && s.secrets != nil {
		apiKey, err = s.secrets.Decrypt(*config.APIKeyRef)
		if err != nil {
			return "", false
		}
	}
	apiSecret := ""
	if config.APISecretRef != nil && *config.APISecretRef != "" && s.secrets != nil {
		apiSecret, err = s.secrets.Decrypt(*config.APISecretRef)
		if err != nil {
			return "", false
		}
	}
	prompt := buildReportPrompt(input, preprocessed, citations)
	response, err := s.client.Chat(ctx, llmsvc.ChatRequest{
		BaseURL:     config.BaseURL,
		APIKey:      apiKey,
		APISecret:   apiSecret,
		Model:       config.Model,
		Temperature: config.Temperature,
		Messages: []llmsvc.ChatMessage{
			{Role: model.MessageRoleSystem, Content: "You are an AIOps analyst. Produce a concise Chinese summary grounded in facts and citations. Mark speculation clearly."},
			{Role: model.MessageRoleUser, Content: prompt},
		},
	})
	if err != nil || strings.TrimSpace(response.Content) == "" {
		return "", false
	}
	return strings.TrimSpace(response.Content), true
}

func buildLocalResult(taskID int64, input RunInput, preprocessed logssvc.PreprocessResult, citations []Citation) Result {
	facts := []string{
		fmt.Sprintf("分析时间窗内共采集 %d 条日志，预处理后保留 %d 条。", preprocessed.TotalInput, preprocessed.TotalOutput),
		fmt.Sprintf("错误级别日志数量为 %d 条。", preprocessed.ErrorCount),
	}
	evidence := []Evidence{
		{Type: "logs", Source: "preprocess", Summary: facts[0]},
		{Type: "logs", Source: "preprocess", Summary: facts[1]},
	}
	rootCauses := []string{}
	if len(preprocessed.Clusters) > 0 {
		top := preprocessed.Clusters[0]
		facts = append(facts, fmt.Sprintf("最高频日志模板出现 %d 次：%s", top.Count, top.Template))
		evidence = append(evidence, Evidence{Type: "log_template", Source: "cluster", Summary: top.Template, Reference: top.Example})
		rootCauses = append(rootCauses, fmt.Sprintf("推测：高频模板“%s”可能与问题相关，需要结合指标和发布记录确认。", top.Template))
	}
	if len(citations) > 0 {
		evidence = append(evidence, Evidence{Type: "knowledge", Source: "rag", Summary: "检索到已发布知识库引用。"})
	} else {
		rootCauses = append(rootCauses, "推测：当前未检索到相关知识库依据，根因候选仅基于日志模式。")
	}
	missing := []string{}
	if len(citations) == 0 {
		missing = append(missing, "缺少可引用的已发布知识库依据。")
	}
	if preprocessed.TotalInput == 0 {
		missing = append(missing, "缺少日志证据，无法形成可靠事实。")
	}
	confidence := Confidence{Level: "medium", Score: 0.68, Reasons: []string{"包含日志事实和预处理统计。"}}
	if len(citations) == 0 || preprocessed.TotalInput == 0 {
		confidence = Confidence{Level: "low", Score: 0.42, Reasons: []string{"证据不完整，存在缺失证据。"}}
	}
	return Result{
		TaskID:              taskID,
		Status:              model.AnalysisTaskStatusSuccess,
		Summary:             buildSummary(input, preprocessed, citations),
		Impact:              map[string]any{"environment": input.Scope.Environment, "systemName": input.Scope.SystemName, "componentName": input.Scope.ComponentName},
		Timeline:            buildTimeline(preprocessed),
		Facts:               facts,
		RootCauseCandidates: rootCauses,
		Suggestions:         buildSuggestions(preprocessed, citations),
		RiskTips:            []string{"所有根因候选均需结合指标、发布记录或人工确认后再执行生产变更。"},
		Evidence:            evidence,
		Citations:           citations,
		Confidence:          confidence,
		MissingEvidence:     missing,
		Preprocess:          preprocessed,
		ReportSource:        "local-fallback",
	}
}

func buildSummary(input RunInput, preprocessed logssvc.PreprocessResult, citations []Citation) string {
	return fmt.Sprintf("针对“%s”的日志分析已完成：采集 %d 条日志、错误 %d 条、知识引用 %d 条。结论中事实来自日志统计，根因候选均以“推测”标注。",
		input.Question, preprocessed.TotalInput, preprocessed.ErrorCount, len(citations))
}

func buildTimeline(preprocessed logssvc.PreprocessResult) []string {
	timeline := make([]string, 0, len(preprocessed.TimeStats))
	for _, bucket := range preprocessed.TimeStats {
		timeline = append(timeline, fmt.Sprintf("%s：日志 %d 条，错误 %d 条", bucket.Start.Format(time.RFC3339), bucket.Count, bucket.ErrorCount))
	}
	return timeline
}

func buildSuggestions(preprocessed logssvc.PreprocessResult, citations []Citation) []string {
	suggestions := []string{"优先查看最高频错误模板对应的服务实例、Pod 和 trace。"}
	if preprocessed.ErrorCount > 0 {
		suggestions = append(suggestions, "对错误时间段关联指标、慢查询、连接池和上游依赖状态。")
	}
	if len(citations) > 0 {
		suggestions = append(suggestions, "按引用的知识库 runbook 逐项核对。")
	}
	return suggestions
}

func normalizeRunInput(input RunInput) (RunInput, error) {
	input.Question = strings.TrimSpace(input.Question)
	if input.Question == "" || len(input.Question) > maxQuestionBytes || !utf8.ValidString(input.Question) {
		return RunInput{}, ErrInvalidInput
	}
	if input.Scope.TimeStart.IsZero() || input.Scope.TimeEnd.IsZero() || !input.Scope.TimeStart.Before(input.Scope.TimeEnd) {
		return RunInput{}, ErrInvalidInput
	}
	if len(input.DataSourceIDs) == 0 {
		return RunInput{}, ErrInvalidInput
	}
	seen := map[int64]struct{}{}
	ids := make([]int64, 0, len(input.DataSourceIDs))
	for _, id := range input.DataSourceIDs {
		if id <= 0 {
			return RunInput{}, ErrInvalidInput
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	input.DataSourceIDs = ids
	input.Scope.Environment = strings.TrimSpace(input.Scope.Environment)
	input.Scope.SystemName = strings.TrimSpace(input.Scope.SystemName)
	input.Scope.ComponentName = strings.TrimSpace(input.Scope.ComponentName)
	input.Scope.TimeStart = input.Scope.TimeStart.UTC()
	input.Scope.TimeEnd = input.Scope.TimeEnd.UTC()
	return input, nil
}

func buildReportPrompt(input RunInput, preprocessed logssvc.PreprocessResult, citations []Citation) string {
	payload, _ := json.Marshal(map[string]any{
		"question":   input.Question,
		"scope":      input.Scope,
		"clusters":   preprocessed.Clusters,
		"timeStats":  preprocessed.TimeStats,
		"errorCount": preprocessed.ErrorCount,
		"citations":  citations,
	})
	return string(payload)
}

func snippet(content string) string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) <= 160 {
		return string(runes)
	}
	return string(runes[:160]) + "..."
}
