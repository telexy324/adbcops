package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RCARepository interface {
	CreateRCARun(ctx context.Context, run *model.RCARun) error
	FindRCARunByID(ctx context.Context, id int64) (*model.RCARun, error)
	ListRCARuns(ctx context.Context, filters RCARunFilters) ([]model.RCARun, error)
	UpdateRCARun(ctx context.Context, id int64, updates RCARunUpdates) (*model.RCARun, error)
	MarkTimedOutRCARuns(ctx context.Context, now time.Time) error

	CreateRCARound(ctx context.Context, round *model.RCARound) error
	FindRCARoundByID(ctx context.Context, id int64) (*model.RCARound, error)
	ListRCARounds(ctx context.Context, runID int64) ([]model.RCARound, error)
	UpdateRCARound(ctx context.Context, id int64, updates RCARoundUpdates) (*model.RCARound, error)

	CreateOrGetRCAAction(ctx context.Context, action *model.RCAAction) (*model.RCAAction, error)
	FindRCAActionByID(ctx context.Context, id int64) (*model.RCAAction, error)
	ListRCAActions(ctx context.Context, runID int64) ([]model.RCAAction, error)
	UpdateRCAAction(ctx context.Context, id int64, updates RCAActionUpdates) (*model.RCAAction, error)

	CreateRCARootCauseCandidate(ctx context.Context, candidate *model.RCARootCauseCandidate) error
	ListRCARootCauseCandidates(ctx context.Context, runID int64) ([]model.RCARootCauseCandidate, error)
	EvidenceIDsBelongToRCARun(ctx context.Context, runID int64, evidenceIDs []int64) (bool, error)
	ListRCAEvidence(ctx context.Context, runID int64) ([]model.EvidenceRecord, error)
}

type RCARunFilters struct {
	UserID int64
	Limit  int
	Status string
}

type RCARunUpdates struct {
	Status            string
	CurrentRound      *int
	CancelRequestedAt *time.Time
	ErrorCode         *string
	ErrorMessage      *string
	StopReason        *string
	ClearError        bool
	ClearStopReason   bool
	StartedAt         *time.Time
	FinishedAt        *time.Time
	ClearFinishedAt   bool
}

type RCARoundUpdates struct {
	Status             string
	NewEvidenceIDs     []byte
	RejectedHypotheses []byte
	NextActions        []byte
	ErrorCode          *string
	ClearError         bool
	StartedAt          *time.Time
	FinishedAt         *time.Time
	ClearFinishedAt    bool
}

type RCAActionUpdates struct {
	Status          string
	Output          []byte
	EvidenceIDs     []byte
	ErrorCode       *string
	ErrorMessage    *string
	ClearError      bool
	Attempt         *int
	StartedAt       *time.Time
	FinishedAt      *time.Time
	ClearFinishedAt bool
}

type GORMRCARepository struct {
	db *gorm.DB
}

func NewRCARepository(db *gorm.DB) *GORMRCARepository {
	return &GORMRCARepository{db: db}
}

func (r *GORMRCARepository) CreateRCARun(ctx context.Context, run *model.RCARun) error {
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return fmt.Errorf("create rca run: %w", err)
	}
	return nil
}

func (r *GORMRCARepository) FindRCARunByID(ctx context.Context, id int64) (*model.RCARun, error) {
	var run model.RCARun
	if err := r.db.WithContext(ctx).First(&run, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &run, nil
}

func (r *GORMRCARepository) ListRCARuns(ctx context.Context, filters RCARunFilters) ([]model.RCARun, error) {
	limit := filters.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := r.db.WithContext(ctx).Order("created_at DESC, id DESC").Limit(limit)
	if filters.UserID > 0 {
		query = query.Where("user_id = ?", filters.UserID)
	}
	if filters.Status != "" {
		query = query.Where("status = ?", filters.Status)
	}
	var runs []model.RCARun
	if err := query.Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("list rca runs: %w", err)
	}
	return runs, nil
}

func (r *GORMRCARepository) UpdateRCARun(ctx context.Context, id int64, updates RCARunUpdates) (*model.RCARun, error) {
	values := map[string]any{"updated_at": time.Now().UTC()}
	if updates.Status != "" {
		values["status"] = updates.Status
	}
	if updates.CurrentRound != nil {
		values["current_round"] = *updates.CurrentRound
	}
	if updates.CancelRequestedAt != nil {
		values["cancel_requested_at"] = *updates.CancelRequestedAt
	}
	if updates.ClearError {
		values["error_code"] = nil
		values["error_message"] = nil
	} else {
		if updates.ErrorCode != nil {
			values["error_code"] = *updates.ErrorCode
		}
		if updates.ErrorMessage != nil {
			values["error_message"] = *updates.ErrorMessage
		}
	}
	if updates.ClearStopReason {
		values["stop_reason"] = nil
	} else if updates.StopReason != nil {
		values["stop_reason"] = *updates.StopReason
	}
	if updates.StartedAt != nil {
		values["started_at"] = *updates.StartedAt
	}
	if updates.ClearFinishedAt {
		values["finished_at"] = nil
	} else if updates.FinishedAt != nil {
		values["finished_at"] = *updates.FinishedAt
	}
	result := r.db.WithContext(ctx).Model(&model.RCARun{}).Where("id = ?", id).Updates(values)
	if result.Error != nil {
		return nil, fmt.Errorf("update rca run: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	return r.FindRCARunByID(ctx, id)
}

func (r *GORMRCARepository) MarkTimedOutRCARuns(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var runIDs []int64
		if err := tx.Model(&model.RCARun{}).
			Where("status IN ? AND timeout_at IS NOT NULL AND timeout_at <= ?", []string{
				model.RCARunStatusPending, model.RCARunStatusRunning,
			}, now).
			Pluck("id", &runIDs).Error; err != nil {
			return fmt.Errorf("find timed out rca runs: %w", err)
		}
		if len(runIDs) == 0 {
			return nil
		}
		if err := tx.Model(&model.RCARun{}).Where("id IN ?", runIDs).Updates(map[string]any{
			"status": model.RCARunStatusTimedOut, "error_code": "run_timeout",
			"error_message": "RCA run exceeded its configured deadline", "stop_reason": "wall_time_budget_exhausted",
			"finished_at": now, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("mark timed out rca runs: %w", err)
		}
		if err := tx.Model(&model.RCARound{}).
			Where("run_id IN ? AND status IN ?", runIDs, []string{model.RCARoundStatusPending, model.RCARoundStatusRunning}).
			Updates(map[string]any{"status": model.RCARoundStatusTimedOut, "error_code": "run_timeout", "finished_at": now, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("mark timed out rca rounds: %w", err)
		}
		if err := tx.Model(&model.RCAAction{}).
			Where("run_id IN ? AND status IN ?", runIDs, []string{model.RCAActionStatusPending, model.RCAActionStatusRunning}).
			Updates(map[string]any{
				"status": model.RCAActionStatusTimedOut, "error_code": "run_timeout",
				"error_message": "RCA run exceeded its configured deadline", "finished_at": now, "updated_at": now,
			}).Error; err != nil {
			return fmt.Errorf("mark timed out rca actions: %w", err)
		}
		return nil
	})
}

func (r *GORMRCARepository) CreateRCARound(ctx context.Context, round *model.RCARound) error {
	if err := r.db.WithContext(ctx).Create(round).Error; err != nil {
		return fmt.Errorf("create rca round: %w", err)
	}
	return nil
}

func (r *GORMRCARepository) FindRCARoundByID(ctx context.Context, id int64) (*model.RCARound, error) {
	var round model.RCARound
	if err := r.db.WithContext(ctx).First(&round, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &round, nil
}

func (r *GORMRCARepository) ListRCARounds(ctx context.Context, runID int64) ([]model.RCARound, error) {
	var rounds []model.RCARound
	if err := r.db.WithContext(ctx).Where("run_id = ?", runID).Order("round_number ASC").Find(&rounds).Error; err != nil {
		return nil, fmt.Errorf("list rca rounds: %w", err)
	}
	return rounds, nil
}

func (r *GORMRCARepository) UpdateRCARound(ctx context.Context, id int64, updates RCARoundUpdates) (*model.RCARound, error) {
	values := map[string]any{"updated_at": time.Now().UTC()}
	if updates.Status != "" {
		values["status"] = updates.Status
	}
	if updates.NewEvidenceIDs != nil {
		values["new_evidence_ids"] = updates.NewEvidenceIDs
	}
	if updates.RejectedHypotheses != nil {
		values["rejected_hypotheses"] = updates.RejectedHypotheses
	}
	if updates.NextActions != nil {
		values["next_actions"] = updates.NextActions
	}
	if updates.ClearError {
		values["error_code"] = nil
	} else if updates.ErrorCode != nil {
		values["error_code"] = *updates.ErrorCode
	}
	if updates.StartedAt != nil {
		values["started_at"] = *updates.StartedAt
	}
	if updates.ClearFinishedAt {
		values["finished_at"] = nil
	} else if updates.FinishedAt != nil {
		values["finished_at"] = *updates.FinishedAt
	}
	result := r.db.WithContext(ctx).Model(&model.RCARound{}).Where("id = ?", id).Updates(values)
	if result.Error != nil {
		return nil, fmt.Errorf("update rca round: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	return r.FindRCARoundByID(ctx, id)
}

func (r *GORMRCARepository) CreateOrGetRCAAction(ctx context.Context, action *model.RCAAction) (*model.RCAAction, error) {
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "run_id"}, {Name: "action_key"}},
		DoNothing: true,
	}).Create(action)
	if result.Error != nil {
		return nil, fmt.Errorf("create rca action: %w", result.Error)
	}
	var stored model.RCAAction
	if err := r.db.WithContext(ctx).Where("run_id = ? AND action_key = ?", action.RunID, action.ActionKey).First(&stored).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &stored, nil
}

func (r *GORMRCARepository) FindRCAActionByID(ctx context.Context, id int64) (*model.RCAAction, error) {
	var action model.RCAAction
	if err := r.db.WithContext(ctx).First(&action, id).Error; err != nil {
		return nil, mapRepositoryError(err)
	}
	return &action, nil
}

func (r *GORMRCARepository) ListRCAActions(ctx context.Context, runID int64) ([]model.RCAAction, error) {
	var actions []model.RCAAction
	if err := r.db.WithContext(ctx).Where("run_id = ?", runID).Order("round_id ASC, id ASC").Find(&actions).Error; err != nil {
		return nil, fmt.Errorf("list rca actions: %w", err)
	}
	return actions, nil
}

func (r *GORMRCARepository) UpdateRCAAction(ctx context.Context, id int64, updates RCAActionUpdates) (*model.RCAAction, error) {
	values := map[string]any{"updated_at": time.Now().UTC()}
	if updates.Status != "" {
		values["status"] = updates.Status
	}
	if updates.Output != nil {
		values["output"] = updates.Output
	}
	if updates.EvidenceIDs != nil {
		values["evidence_ids"] = updates.EvidenceIDs
	}
	if updates.ClearError {
		values["error_code"] = nil
		values["error_message"] = nil
	} else {
		if updates.ErrorCode != nil {
			values["error_code"] = *updates.ErrorCode
		}
		if updates.ErrorMessage != nil {
			values["error_message"] = *updates.ErrorMessage
		}
	}
	if updates.Attempt != nil {
		values["attempt"] = *updates.Attempt
	}
	if updates.StartedAt != nil {
		values["started_at"] = *updates.StartedAt
	}
	if updates.ClearFinishedAt {
		values["finished_at"] = nil
	} else if updates.FinishedAt != nil {
		values["finished_at"] = *updates.FinishedAt
	}
	result := r.db.WithContext(ctx).Model(&model.RCAAction{}).Where("id = ?", id).Updates(values)
	if result.Error != nil {
		return nil, fmt.Errorf("update rca action: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	return r.FindRCAActionByID(ctx, id)
}

func (r *GORMRCARepository) CreateRCARootCauseCandidate(ctx context.Context, candidate *model.RCARootCauseCandidate) error {
	if err := r.db.WithContext(ctx).Create(candidate).Error; err != nil {
		return fmt.Errorf("create rca root cause candidate: %w", err)
	}
	return nil
}

func (r *GORMRCARepository) ListRCARootCauseCandidates(ctx context.Context, runID int64) ([]model.RCARootCauseCandidate, error) {
	var candidates []model.RCARootCauseCandidate
	if err := r.db.WithContext(ctx).Where("run_id = ?", runID).Order("confidence DESC, id ASC").Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("list rca root cause candidates: %w", err)
	}
	return candidates, nil
}

func (r *GORMRCARepository) EvidenceIDsBelongToRCARun(ctx context.Context, runID int64, evidenceIDs []int64) (bool, error) {
	if len(evidenceIDs) == 0 {
		return false, nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.EvidenceRecord{}).
		Where("rca_run_id = ? AND id IN ?", runID, evidenceIDs).
		Distinct("id").Count(&count).Error; err != nil {
		return false, fmt.Errorf("validate rca evidence ids: %w", err)
	}
	return count == int64(len(uniqueInt64Values(evidenceIDs))), nil
}

func (r *GORMRCARepository) ListRCAEvidence(ctx context.Context, runID int64) ([]model.EvidenceRecord, error) {
	var evidence []model.EvidenceRecord
	if err := r.db.WithContext(ctx).Where("rca_run_id = ?", runID).Order("rca_round_id ASC, id ASC").Find(&evidence).Error; err != nil {
		return nil, fmt.Errorf("list rca evidence: %w", err)
	}
	return evidence, nil
}

func uniqueInt64Values(values []int64) []int64 {
	seen := map[int64]struct{}{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
