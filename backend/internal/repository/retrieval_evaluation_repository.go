package repository

import (
	"context"
	"fmt"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

type RetrievalEvaluationRepository interface {
	CreateRetrievalTestCase(context.Context, *model.KBRetrievalTestCase) error
	ListRetrievalTestCases(context.Context, *int64, []int64, bool) ([]model.KBRetrievalTestCase, error)
	FindDocumentVersionByID(context.Context, int64) (*model.KBDocumentVersion, error)
	CreateRetrievalEvaluationRun(context.Context, *model.KBRetrievalEvaluationRun) error
	CompleteRetrievalEvaluationRun(context.Context, *model.KBRetrievalEvaluationRun, []model.KBRetrievalEvaluationResult) error
	FindRetrievalEvaluationRun(context.Context, int64) (*model.KBRetrievalEvaluationRun, error)
	ListRetrievalEvaluationRuns(context.Context, int) ([]model.KBRetrievalEvaluationRun, error)
}

func (r *GORMUserRepository) CreateRetrievalTestCase(ctx context.Context, testCase *model.KBRetrievalTestCase) error {
	if err := r.db.WithContext(ctx).Create(testCase).Error; err != nil {
		return fmt.Errorf("create retrieval test case: %w", err)
	}
	return nil
}

func (r *GORMUserRepository) ListRetrievalTestCases(ctx context.Context, versionID *int64, ids []int64, enabledOnly bool) ([]model.KBRetrievalTestCase, error) {
	var testCases []model.KBRetrievalTestCase
	query := r.db.WithContext(ctx)
	if versionID != nil {
		query = query.Where("document_version_id = ?", *versionID)
	}
	if len(ids) > 0 {
		query = query.Where("id IN ?", ids)
	}
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Order("category ASC, id ASC").Find(&testCases).Error; err != nil {
		return nil, fmt.Errorf("list retrieval test cases: %w", err)
	}
	return testCases, nil
}

func (r *GORMUserRepository) CreateRetrievalEvaluationRun(ctx context.Context, run *model.KBRetrievalEvaluationRun) error {
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return fmt.Errorf("create retrieval evaluation run: %w", err)
	}
	return nil
}

func (r *GORMUserRepository) CompleteRetrievalEvaluationRun(ctx context.Context, run *model.KBRetrievalEvaluationRun, results []model.KBRetrievalEvaluationResult) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for index := range results {
			results[index].RunID = run.ID
		}
		if len(results) > 0 {
			if err := tx.Create(&results).Error; err != nil {
				return fmt.Errorf("create retrieval evaluation results: %w", err)
			}
		}
		updates := map[string]any{
			"status": run.Status, "metrics": run.Metrics, "case_count": run.CaseCount,
			"passed": run.Passed, "error_message": run.ErrorMessage, "completed_at": run.CompletedAt,
			"embedding_model": run.EmbeddingModel, "embedding_model_revision": run.EmbeddingModelRevision,
			"embedding_config_id": run.EmbeddingConfigID, "rerank_model": run.RerankModel,
			"rerank_config_id": run.RerankConfigID, "chunk_strategy_id": run.ChunkStrategyID,
		}
		if err := tx.Model(&model.KBRetrievalEvaluationRun{}).Where("id = ?", run.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("complete retrieval evaluation run: %w", err)
		}
		return nil
	})
}

func (r *GORMUserRepository) FindRetrievalEvaluationRun(ctx context.Context, id int64) (*model.KBRetrievalEvaluationRun, error) {
	var run model.KBRetrievalEvaluationRun
	if err := r.db.WithContext(ctx).Preload("Results").First(&run, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &run, nil
}

func (r *GORMUserRepository) ListRetrievalEvaluationRuns(ctx context.Context, limit int) ([]model.KBRetrievalEvaluationRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var runs []model.KBRetrievalEvaluationRun
	if err := r.db.WithContext(ctx).Order("created_at DESC, id DESC").Limit(limit).Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("list retrieval evaluation runs: %w", err)
	}
	return runs, nil
}
