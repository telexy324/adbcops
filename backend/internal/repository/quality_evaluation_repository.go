package repository

import (
	"context"
	"fmt"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
)

func (r *GORMUserRepository) FindLatestQualityEvaluation(ctx context.Context, versionID, profileID int64, source string) (*model.KBQualityEvaluation, error) {
	var evaluation model.KBQualityEvaluation
	err := r.db.WithContext(ctx).Where("document_version_id = ? AND quality_profile_id = ? AND source = ? AND status = 'completed'", versionID, profileID, source).Order("created_at DESC, id DESC").First(&evaluation).Error
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	return &evaluation, nil
}

func (r *GORMUserRepository) CreateQualityEvaluation(ctx context.Context, evaluation *model.KBQualityEvaluation) error {
	results := evaluation.RuleResults
	evaluation.RuleResults = nil
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(evaluation).Error; err != nil {
			return fmt.Errorf("create quality evaluation: %w", err)
		}
		for index := range results {
			results[index].EvaluationID = evaluation.ID
		}
		if len(results) > 0 {
			if err := tx.Create(&results).Error; err != nil {
				return fmt.Errorf("create quality rule results: %w", err)
			}
		}
		return nil
	})
	evaluation.RuleResults = results
	return err
}

func (r *GORMUserRepository) FindQualityEvaluation(ctx context.Context, id int64) (*model.KBQualityEvaluation, error) {
	var evaluation model.KBQualityEvaluation
	if err := r.db.WithContext(ctx).First(&evaluation, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &evaluation, nil
}

func (r *GORMUserRepository) ListQualityRuleResults(ctx context.Context, evaluationID int64) ([]model.KBQualityRuleResult, error) {
	var results []model.KBQualityRuleResult
	if err := r.db.WithContext(ctx).Where("evaluation_id = ?", evaluationID).Order("criterion_key ASC, id ASC").Find(&results).Error; err != nil {
		return nil, fmt.Errorf("list quality rule results: %w", err)
	}
	return results, nil
}
