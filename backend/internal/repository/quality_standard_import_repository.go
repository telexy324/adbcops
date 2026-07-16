package repository

import (
	"context"
	"fmt"

	"aiops-platform/backend/internal/model"
)

func (r *GORMUserRepository) CreateQualityStandardImport(ctx context.Context, record *model.KBQualityStandardImport) error {
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return fmt.Errorf("create quality standard import: %w", err)
	}
	return nil
}

func (r *GORMUserRepository) UpdateQualityStandardImport(ctx context.Context, record *model.KBQualityStandardImport) error {
	updates := map[string]any{
		"standard_id": record.StandardID, "parser_name": record.ParserName, "parser_version": record.ParserVersion,
		"status": record.Status, "warnings": record.Warnings, "validation_errors": record.ValidationErrors, "preview": record.Preview,
	}
	result := r.db.WithContext(ctx).Model(&model.KBQualityStandardImport{}).Where("id = ?", record.ID).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update quality standard import: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}
