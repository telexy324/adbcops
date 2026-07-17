package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type QualityEvaluationReviewUpdate struct {
	Evaluation *model.KBQualityEvaluation
	RuleResult *model.KBQualityRuleResult
	Audit      *model.KBQualityEvaluationOverride
}

func (r *GORMUserRepository) FindLatestQualityEvaluation(ctx context.Context, versionID, profileID int64, fingerprint string) (*model.KBQualityEvaluation, error) {
	var evaluation model.KBQualityEvaluation
	err := r.db.WithContext(ctx).Where("document_version_id = ? AND quality_profile_id = ? AND request_fingerprint = ? AND status = 'completed'", versionID, profileID, fingerprint).Order("created_at DESC, id DESC").First(&evaluation).Error
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

func (r *GORMUserRepository) ApplyQualityEvaluationOverride(ctx context.Context, update QualityEvaluationReviewUpdate) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var evaluation model.KBQualityEvaluation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&evaluation, update.Evaluation.ID).Error; err != nil {
			return mapRepositoryError(err)
		}
		if evaluation.ReviewStatus != "draft" {
			return ErrImmutable
		}
		var result model.KBQualityRuleResult
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND evaluation_id = ?", update.RuleResult.ID, evaluation.ID).First(&result).Error; err != nil {
			return mapRepositoryError(err)
		}
		if err := tx.Model(&result).Updates(map[string]any{
			"score":               update.RuleResult.Score,
			"finding_status":      update.RuleResult.FindingStatus,
			"source":              "manual",
			"manually_overridden": true,
			"overridden_by":       update.RuleResult.OverriddenBy,
			"override_comment":    update.RuleResult.OverrideComment,
		}).Error; err != nil {
			return fmt.Errorf("override quality rule result: %w", err)
		}
		if err := tx.Model(&evaluation).Updates(map[string]any{
			"content_score": update.Evaluation.ContentScore,
			"total_score":   update.Evaluation.TotalScore,
			"gate_status":   update.Evaluation.GateStatus,
			"level":         update.Evaluation.Level,
			"summary":       update.Evaluation.Summary,
			"result":        update.Evaluation.Result,
		}).Error; err != nil {
			return fmt.Errorf("update quality evaluation aggregate: %w", err)
		}
		if err := tx.Create(update.Audit).Error; err != nil {
			return fmt.Errorf("record quality evaluation override: %w", err)
		}
		return nil
	})
}

func (r *GORMUserRepository) PublishQualityEvaluation(ctx context.Context, evaluationID, actorID int64, publishedAt time.Time) (*model.KBQualityEvaluation, error) {
	var evaluation model.KBQualityEvaluation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&evaluation, evaluationID).Error; err != nil {
			return mapRepositoryError(err)
		}
		if evaluation.ReviewStatus != "draft" {
			return ErrImmutable
		}
		var previous model.KBQualityEvaluation
		previousErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("document_version_id = ? AND review_status = 'published' AND id <> ?", evaluation.DocumentVersionID, evaluation.ID).
			Order("published_at DESC NULLS LAST, id DESC").First(&previous).Error
		if previousErr != nil && previousErr != gorm.ErrRecordNotFound {
			return fmt.Errorf("find published quality evaluation: %w", previousErr)
		}
		if previousErr == nil {
			if err := tx.Model(&model.KBQualityEvaluation{}).
				Where("document_version_id = ? AND review_status = 'published' AND id <> ?", evaluation.DocumentVersionID, evaluation.ID).
				Update("review_status", "superseded").Error; err != nil {
				return fmt.Errorf("supersede published quality evaluation: %w", err)
			}
			if evaluation.SupersedesEvaluationID == nil {
				evaluation.SupersedesEvaluationID = &previous.ID
			}
		}
		if err := tx.Model(&evaluation).Updates(map[string]any{"review_status": "published", "published_by": actorID, "published_at": publishedAt, "supersedes_evaluation_id": evaluation.SupersedesEvaluationID}).Error; err != nil {
			return fmt.Errorf("publish quality evaluation: %w", err)
		}
		evaluation.ReviewStatus, evaluation.PublishedBy, evaluation.PublishedAt = "published", &actorID, &publishedAt
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &evaluation, nil
}

func (r *GORMUserRepository) ListQualityEvaluationOverrides(ctx context.Context, evaluationID int64) ([]model.KBQualityEvaluationOverride, error) {
	var audits []model.KBQualityEvaluationOverride
	if err := r.db.WithContext(ctx).Where("evaluation_id = ?", evaluationID).Order("created_at DESC, id DESC").Find(&audits).Error; err != nil {
		return nil, fmt.Errorf("list quality evaluation overrides: %w", err)
	}
	return audits, nil
}
