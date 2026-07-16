package repository

import (
	"context"
	"fmt"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *GORMUserRepository) ListStructuredQualityStandards(ctx context.Context) ([]model.KBStructuredQualityStandard, error) {
	var standards []model.KBStructuredQualityStandard
	if err := r.db.WithContext(ctx).Order("updated_at DESC, id DESC").Find(&standards).Error; err != nil {
		return nil, fmt.Errorf("list structured quality standards: %w", err)
	}
	return standards, nil
}

func (r *GORMUserRepository) FindStructuredQualityStandard(ctx context.Context, id int64) (*model.KBStructuredQualityStandard, error) {
	var standard model.KBStructuredQualityStandard
	err := r.db.WithContext(ctx).
		Preload("Profiles", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).
		Preload("Profiles.Criteria", func(db *gorm.DB) *gorm.DB { return db.Order("order_no ASC, id ASC") }).
		Preload("Profiles.Criteria.Rules", func(db *gorm.DB) *gorm.DB { return db.Order("order_no ASC, id ASC") }).
		First(&standard, id).Error
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	return &standard, nil
}

func (r *GORMUserRepository) CreateStructuredQualityStandard(ctx context.Context, standard *model.KBStructuredQualityStandard) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return createStructuredStandard(tx, standard)
	})
}

func createStructuredStandard(tx *gorm.DB, standard *model.KBStructuredQualityStandard) error {
	profiles := standard.Profiles
	standard.Profiles = nil
	if err := tx.Create(standard).Error; err != nil {
		return fmt.Errorf("create structured quality standard: %w", err)
	}
	standard.Profiles = profiles
	for i := range standard.Profiles {
		standard.Profiles[i].StandardID = standard.ID
		if err := createProfile(tx, &standard.Profiles[i]); err != nil {
			return err
		}
	}
	return nil
}

func createProfile(tx *gorm.DB, profile *model.KBQualityProfile) error {
	criteria := profile.Criteria
	profile.Criteria = nil
	if err := tx.Create(profile).Error; err != nil {
		return fmt.Errorf("create quality profile: %w", err)
	}
	profile.Criteria = criteria
	for i := range profile.Criteria {
		profile.Criteria[i].ProfileID = profile.ID
		rules := profile.Criteria[i].Rules
		profile.Criteria[i].Rules = nil
		if err := tx.Create(&profile.Criteria[i]).Error; err != nil {
			return fmt.Errorf("create quality criterion: %w", err)
		}
		profile.Criteria[i].Rules = rules
		for j := range profile.Criteria[i].Rules {
			profile.Criteria[i].Rules[j].CriterionID = profile.Criteria[i].ID
		}
		if len(profile.Criteria[i].Rules) > 0 {
			if err := tx.Create(&profile.Criteria[i].Rules).Error; err != nil {
				return fmt.Errorf("create quality rules: %w", err)
			}
		}
	}
	return nil
}

func (r *GORMUserRepository) ReplaceStructuredQualityStandard(ctx context.Context, standard *model.KBStructuredQualityStandard) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.KBStructuredQualityStandard
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, standard.ID).Error; err != nil {
			return mapRepositoryError(err)
		}
		if current.Status == model.QualityStandardPublished {
			return ErrImmutable
		}
		updates := map[string]any{"name": standard.Name, "description": standard.Description, "source_document_version_id": standard.SourceDocumentVersionID, "version": standard.Version, "effective_from": standard.EffectiveFrom, "effective_until": standard.EffectiveUntil}
		if err := tx.Model(&current).Updates(updates).Error; err != nil {
			return fmt.Errorf("update structured quality standard: %w", err)
		}
		if err := tx.Where("standard_id = ?", standard.ID).Delete(&model.KBQualityProfile{}).Error; err != nil {
			return fmt.Errorf("replace quality profiles: %w", err)
		}
		for i := range standard.Profiles {
			standard.Profiles[i].ID = 0
			standard.Profiles[i].StandardID = standard.ID
			if err := createProfile(tx, &standard.Profiles[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *GORMUserRepository) UpdateStructuredQualityStandardStatus(ctx context.Context, id int64, status string, approvedBy *int64) (*model.KBStructuredQualityStandard, error) {
	result := r.db.WithContext(ctx).Model(&model.KBStructuredQualityStandard{}).Where("id = ?", id).Updates(map[string]any{"status": status, "approved_by": approvedBy})
	if result.Error != nil {
		return nil, fmt.Errorf("update quality standard status: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	if err := r.db.WithContext(ctx).Model(&model.KBQualityProfile{}).Where("standard_id = ?", id).Update("status", status).Error; err != nil {
		return nil, fmt.Errorf("update quality profile status: %w", err)
	}
	return r.FindStructuredQualityStandard(ctx, id)
}

func (r *GORMUserRepository) FindQualityProfile(ctx context.Context, id int64) (*model.KBQualityProfile, error) {
	var profile model.KBQualityProfile
	err := r.db.WithContext(ctx).Preload("Criteria", func(db *gorm.DB) *gorm.DB { return db.Order("order_no ASC, id ASC") }).Preload("Criteria.Rules", func(db *gorm.DB) *gorm.DB { return db.Order("order_no ASC, id ASC") }).First(&profile, id).Error
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	return &profile, nil
}

func (r *GORMUserRepository) CreateQualityProfile(ctx context.Context, profile *model.KBQualityProfile) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var standard model.KBStructuredQualityStandard
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&standard, profile.StandardID).Error; err != nil {
			return mapRepositoryError(err)
		}
		if standard.Status == model.QualityStandardPublished {
			return ErrImmutable
		}
		return createProfile(tx, profile)
	})
}

func (r *GORMUserRepository) ReplaceQualityProfile(ctx context.Context, profile *model.KBQualityProfile) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.KBQualityProfile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, profile.ID).Error; err != nil {
			return mapRepositoryError(err)
		}
		var standard model.KBStructuredQualityStandard
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&standard, current.StandardID).Error; err != nil {
			return mapRepositoryError(err)
		}
		if standard.Status == model.QualityStandardPublished {
			return ErrImmutable
		}
		updates := map[string]any{"profile_key": profile.ProfileKey, "name": profile.Name, "applicable_doc_types": profile.ApplicableDocTypes, "applicable_systems": profile.ApplicableSystems, "applicable_environments": profile.ApplicableEnvironments, "total_score": profile.TotalScore, "pass_score": profile.PassScore, "warning_score": profile.WarningScore, "gate_policy": profile.GatePolicy}
		if err := tx.Model(&current).Updates(updates).Error; err != nil {
			return fmt.Errorf("update quality profile: %w", err)
		}
		if err := tx.Where("profile_id = ?", profile.ID).Delete(&model.KBQualityCriterion{}).Error; err != nil {
			return fmt.Errorf("replace quality criteria: %w", err)
		}
		for i := range profile.Criteria {
			profile.Criteria[i].ID = 0
			profile.Criteria[i].ProfileID = profile.ID
			rules := profile.Criteria[i].Rules
			profile.Criteria[i].Rules = nil
			if err := tx.Create(&profile.Criteria[i]).Error; err != nil {
				return fmt.Errorf("create quality criterion: %w", err)
			}
			profile.Criteria[i].Rules = rules
			for j := range profile.Criteria[i].Rules {
				profile.Criteria[i].Rules[j].ID = 0
				profile.Criteria[i].Rules[j].CriterionID = profile.Criteria[i].ID
			}
			if len(profile.Criteria[i].Rules) > 0 {
				if err := tx.Create(&profile.Criteria[i].Rules).Error; err != nil {
					return fmt.Errorf("create quality rules: %w", err)
				}
			}
		}
		return nil
	})
}
