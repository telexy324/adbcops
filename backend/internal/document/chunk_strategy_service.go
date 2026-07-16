package document

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

var ErrChunkSetExists = errors.New("chunk set already exists for this document version and strategy")

type CreateChunkStrategyInput struct {
	Name               string          `json:"name"`
	Version            string          `json:"version"`
	ApplicableDocTypes json.RawMessage `json:"applicableDocTypes"`
	Config             json.RawMessage `json:"config"`
	Enabled            *bool           `json:"enabled"`
}

func (s *Service) CreateChunkStrategy(ctx context.Context, actor *model.AppUser, input CreateChunkStrategyInput) (*model.KBChunkStrategy, error) {
	if actor == nil || actor.Role != model.RoleAdmin {
		return nil, ErrAdminRequired
	}
	name, version := strings.TrimSpace(input.Name), strings.TrimSpace(input.Version)
	if name == "" || version == "" || len(name) > 120 || len(version) > 50 {
		return nil, ErrInvalidInput
	}
	config, err := ParseChunkStrategyConfig(input.Config)
	if err != nil {
		return nil, err
	}
	configJSON, _ := json.Marshal(config)
	docTypes := input.ApplicableDocTypes
	if len(docTypes) == 0 {
		docTypes = json.RawMessage("[]")
	}
	var validatedDocTypes []string
	if json.Unmarshal(docTypes, &validatedDocTypes) != nil {
		return nil, ErrInvalidInput
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	actorID := actor.ID
	strategy := &model.KBChunkStrategy{Name: name, Version: version, ApplicableDocTypes: docTypes, Config: configJSON, Enabled: enabled, CreatedBy: &actorID}
	if err := s.documents.CreateChunkStrategy(ctx, strategy); err != nil {
		return nil, err
	}
	return strategy, nil
}

func (s *Service) ListChunkStrategies(ctx context.Context, actor *model.AppUser) ([]model.KBChunkStrategy, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	return s.documents.ListChunkStrategies(ctx, actor.Role != model.RoleAdmin)
}

func (s *Service) GetChunkStrategy(ctx context.Context, actor *model.AppUser, id int64) (*model.KBChunkStrategy, error) {
	if actor == nil || id <= 0 {
		return nil, ErrInvalidInput
	}
	strategy, err := s.documents.FindChunkStrategy(ctx, id)
	if err != nil {
		return nil, err
	}
	if actor.Role != model.RoleAdmin && !strategy.Enabled {
		return nil, repository.ErrNotFound
	}
	return strategy, nil
}

func (s *Service) ChunkDocumentVersion(ctx context.Context, actor *model.AppUser, versionID, strategyID int64) ([]model.KBChunk, error) {
	version, err := s.GetDocumentVersion(ctx, actor, versionID)
	if err != nil {
		return nil, err
	}
	document, err := s.Get(ctx, actor, version.DocumentID)
	if err != nil {
		return nil, err
	}
	strategy, err := s.GetChunkStrategy(ctx, actor, strategyID)
	if err != nil {
		return nil, err
	}
	if !strategy.Enabled {
		return nil, ErrInvalidInput
	}
	if !strategyApplies(strategy, document.DocType) {
		return nil, fmt.Errorf("%w: strategy does not apply to document type", ErrInvalidInput)
	}
	var quality ParseQuality
	if len(version.ParseQuality) == 0 || json.Unmarshal(version.ParseQuality, &quality) != nil || !quality.ParseSuccess {
		return nil, ErrParseQualityFailed
	}
	blocks, err := s.documents.ListDocumentVersionBlocks(ctx, version.ID)
	if err != nil {
		return nil, err
	}
	chunks, err := BuildSemanticChunks(document, version, strategy, blocks)
	if err != nil {
		return nil, err
	}
	if err := s.documents.CreateDocumentVersionChunks(ctx, version.ID, strategy.ID, chunks); err != nil {
		if errors.Is(err, repository.ErrImmutable) {
			return nil, ErrChunkSetExists
		}
		return nil, err
	}
	strategyIDValue := strategy.ID
	return s.documents.ListDocumentVersionChunks(ctx, version.ID, &strategyIDValue)
}

func (s *Service) ListDocumentVersionChunks(ctx context.Context, actor *model.AppUser, versionID int64, strategyID *int64) ([]model.KBChunk, error) {
	if _, err := s.GetDocumentVersion(ctx, actor, versionID); err != nil {
		return nil, err
	}
	if strategyID != nil && *strategyID <= 0 {
		return nil, ErrInvalidInput
	}
	return s.documents.ListDocumentVersionChunks(ctx, versionID, strategyID)
}

func strategyApplies(strategy *model.KBChunkStrategy, docType *string) bool {
	var values []string
	if len(strategy.ApplicableDocTypes) == 0 || json.Unmarshal(strategy.ApplicableDocTypes, &values) != nil || len(values) == 0 {
		return true
	}
	if docType == nil {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(*docType)) {
			return true
		}
	}
	return false
}
