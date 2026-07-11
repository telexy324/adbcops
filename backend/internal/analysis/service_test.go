package analysis

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	logssvc "aiops-platform/backend/internal/logs"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestRunGeneralProducesEvidenceCitationsAndSeparatesSpeculation(t *testing.T) {
	store := newFakeRepository()
	logs := &fakeLogQuerier{}
	service := NewService(store, logs, nil, nil)
	start := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	output, err := service.RunGeneral(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, RunInput{
		Question:      "支付接口超时增多，可能是什么原因？",
		Scope:         Scope{Environment: "prod", SystemName: "payment", ComponentName: "api", TimeStart: start, TimeEnd: start.Add(time.Hour)},
		DataSourceIDs: []int64{2, 1, 1},
	})
	if err != nil {
		t.Fatalf("RunGeneral() error = %v", err)
	}
	if output.Task.ID == 0 || output.Task.Status != model.AnalysisTaskStatusSuccess {
		t.Fatalf("task = %+v", output.Task)
	}
	result := output.Result
	if len(result.Evidence) == 0 || len(result.Citations) != 1 {
		t.Fatalf("evidence=%+v citations=%+v", result.Evidence, result.Citations)
	}
	if len(result.Facts) == 0 || !strings.Contains(result.Facts[0], "采集") {
		t.Fatalf("facts = %+v", result.Facts)
	}
	if len(result.RootCauseCandidates) == 0 || !strings.Contains(result.RootCauseCandidates[0], "推测") {
		t.Fatalf("rootCauseCandidates = %+v", result.RootCauseCandidates)
	}
	if strings.Contains(result.Summary, "确定根因") {
		t.Fatalf("summary should not overclaim certainty: %q", result.Summary)
	}
	if len(logs.queries) != 2 || logs.queries[0].DataSourceID != 1 || logs.queries[1].DataSourceID != 2 {
		t.Fatalf("queries = %+v", logs.queries)
	}
	var stored Result
	if err := json.Unmarshal(store.tasks[output.Task.ID].Result, &stored); err != nil {
		t.Fatalf("stored result invalid: %v", err)
	}
	if stored.TaskID != output.Task.ID || len(stored.Citations) != 1 {
		t.Fatalf("stored result = %+v", stored)
	}
}

func TestGetRejectsForeignTask(t *testing.T) {
	store := newFakeRepository()
	service := NewService(store, &fakeLogQuerier{}, nil, nil)
	task := &model.AnalysisTask{UserID: 99, TaskType: model.AnalysisTaskTypeGeneral, Question: "q", Status: model.AnalysisTaskStatusSuccess}
	_ = store.CreateAnalysisTask(context.Background(), task)
	_, err := service.Get(context.Background(), &model.AppUser{ID: 7, Role: model.RoleUser}, task.ID)
	if err != ErrForbidden {
		t.Fatalf("Get() error = %v, want ErrForbidden", err)
	}
}

type fakeLogQuerier struct {
	queries []logssvc.QueryInput
}

func (f *fakeLogQuerier) Query(_ context.Context, _ *model.AppUser, input logssvc.QueryInput) (*logssvc.QueryResult, error) {
	f.queries = append(f.queries, input)
	base := input.From
	return &logssvc.QueryResult{Items: []model.LogItem{
		{Timestamp: base.Add(1 * time.Minute), Level: "ERROR", Message: "request 123 failed for user 42", Pod: "payment-0"},
		{Timestamp: base.Add(1 * time.Minute), Level: "ERROR", Message: "request 123 failed for user 42", Pod: "payment-0"},
		{Timestamp: base.Add(2 * time.Minute), Level: "WARN", Message: "database pool active connections 99", Pod: "payment-1"},
	}}, nil
}

type fakeRepository struct {
	nextID int64
	tasks  map[int64]*model.AnalysisTask
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{nextID: 1, tasks: make(map[int64]*model.AnalysisTask)}
}

func (f *fakeRepository) CreateAnalysisTask(_ context.Context, task *model.AnalysisTask) error {
	task.ID = f.nextID
	f.nextID++
	copied := *task
	f.tasks[task.ID] = &copied
	return nil
}

func (f *fakeRepository) UpdateAnalysisTask(_ context.Context, id int64, updates repository.AnalysisTaskUpdates) (*model.AnalysisTask, error) {
	task, ok := f.tasks[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	task.Status = updates.Status
	task.Summary = updates.Summary
	task.Result = updates.Result
	task.ErrorMessage = updates.ErrorMessage
	task.FinishedAt = updates.FinishedAt
	return task, nil
}

func (f *fakeRepository) ListAnalysisTasks(_ context.Context, userID *int64) ([]model.AnalysisTask, error) {
	var tasks []model.AnalysisTask
	for _, task := range f.tasks {
		if userID == nil || task.UserID == *userID {
			tasks = append(tasks, *task)
		}
	}
	return tasks, nil
}

func (f *fakeRepository) FindAnalysisTaskByID(_ context.Context, id int64) (*model.AnalysisTask, error) {
	task, ok := f.tasks[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return task, nil
}

func (f *fakeRepository) SearchChunks(_ context.Context, query string, limit int) ([]model.KBChunk, error) {
	title := "支付排障手册"
	searchText := "数据库连接池 支付接口 超时"
	return []model.KBChunk{{
		ID:          10,
		DocumentID:  20,
		ChunkIndex:  1,
		Content:     "当支付接口超时增多时，优先检查数据库连接池和慢查询。",
		SourceTitle: &title,
		SearchText:  &searchText,
	}}, nil
}

func (f *fakeRepository) FindDefaultEnabledLLMConfig(_ context.Context) (*model.LLMConfig, error) {
	return nil, repository.ErrNotFound
}
