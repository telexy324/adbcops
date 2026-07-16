package document

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"os"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

type PublicationRepository interface {
	ListDocumentVersions(context.Context, int64) ([]model.KBDocumentVersion, error)
	CreateDocumentVersion(context.Context, *model.KBDocumentVersion) error
	FindPublicationQualityEvaluation(context.Context, int64) (*model.KBQualityEvaluation, error)
	FindPublicationEmbeddingIndex(context.Context, int64) (*model.KBEmbeddingIndex, error)
	FindPublicationSmokeRun(context.Context, int64) (*model.KBRetrievalEvaluationRun, error)
	PublishDocumentVersion(context.Context, int64, int64, []byte, *string) (*model.KBDocument, error)
	DeprecateDocumentVersion(context.Context, int64, int64, *string) (*model.KBDocumentVersion, error)
	FindHistoricalCitation(context.Context, int64, int64) (*model.KBDocumentVersion, *model.KBChunk, error)
}

type PublicationGateCheck struct {
	Name       string `json:"name"`
	Passed     bool   `json:"passed"`
	Message    string `json:"message"`
	ResourceID *int64 `json:"resourceId,omitempty"`
}

type PublicationGate struct {
	DocumentID        int64                  `json:"documentId"`
	DocumentVersionID int64                  `json:"documentVersionId"`
	CanPublish        bool                   `json:"canPublish"`
	Checks            []PublicationGateCheck `json:"checks"`
	EvaluatedAt       time.Time              `json:"evaluatedAt"`
}

type VersionDiff struct {
	FromVersion model.KBDocumentVersion `json:"fromVersion"`
	ToVersion   model.KBDocumentVersion `json:"toVersion"`
	Added       []string                `json:"addedBlockKeys"`
	Removed     []string                `json:"removedBlockKeys"`
	Changed     []string                `json:"changedBlockKeys"`
}

type HistoricalCitation struct {
	CitationID string                  `json:"citationId"`
	Version    model.KBDocumentVersion `json:"version"`
	Chunk      model.KBChunk           `json:"chunk"`
}

func (s *Service) UploadVersion(ctx context.Context, actor *model.AppUser, documentID int64, fileHeader *multipart.FileHeader, versionLabel string) (*model.KBDocumentVersion, error) {
	if actor == nil || documentID <= 0 || fileHeader == nil || fileHeader.Size <= 0 {
		return nil, ErrInvalidInput
	}
	document, err := s.Get(ctx, actor, documentID)
	if err != nil {
		return nil, err
	}
	if actor.Role != model.RoleAdmin && (document.CreatedBy == nil || *document.CreatedBy != actor.ID) {
		return nil, ErrForbidden
	}
	if s.publication == nil || fileHeader.Size > s.maxUploadBytes {
		if fileHeader.Size > s.maxUploadBytes {
			return nil, ErrFileTooLarge
		}
		return nil, fmt.Errorf("publication repository is unavailable")
	}
	name, err := normalizeFileName(fileHeader.Filename)
	if err != nil {
		return nil, err
	}
	fileType, ext, err := detectFileType(name)
	if err != nil {
		return nil, err
	}
	label := strings.TrimSpace(versionLabel)
	if label == "" {
		return nil, ErrInvalidInput
	}
	label, err = normalizeRequired(label, maxEnvironmentBytes)
	if err != nil {
		return nil, err
	}
	baseDir, err := ensureBaseDir(s.localFileDir)
	if err != nil {
		return nil, err
	}
	path, err := s.newStoragePath(baseDir, ext)
	if err != nil {
		return nil, err
	}
	if err := saveMultipartFile(fileHeader, path, s.maxUploadBytes); err != nil {
		return nil, err
	}
	hash, err := sha256File(path)
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	actorID := actor.ID
	row := &model.KBDocumentVersion{DocumentID: document.ID, Version: label, FileName: name, FilePath: path, FileType: fileType, FileHash: hash, Status: model.DocumentVersionStatusDraft, CreatedBy: &actorID}
	if err := s.publication.CreateDocumentVersion(ctx, row); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return row, nil
}

func (s *Service) ListVersions(ctx context.Context, actor *model.AppUser, documentID int64) ([]model.KBDocumentVersion, error) {
	if _, err := s.Get(ctx, actor, documentID); err != nil {
		return nil, err
	}
	if s.publication == nil {
		return nil, fmt.Errorf("publication repository is unavailable")
	}
	return s.publication.ListDocumentVersions(ctx, documentID)
}

func (s *Service) EvaluatePublicationGate(ctx context.Context, actor *model.AppUser, versionID int64) (*PublicationGate, error) {
	version, err := s.GetDocumentVersion(ctx, actor, versionID)
	if err != nil {
		return nil, err
	}
	if s.publication == nil {
		return nil, fmt.Errorf("publication repository is unavailable")
	}
	gate := &PublicationGate{DocumentID: version.DocumentID, DocumentVersionID: version.ID, EvaluatedAt: time.Now().UTC()}
	var parseQuality ParseQuality
	parsePassed := len(version.ParseQuality) > 0 && json.Unmarshal(version.ParseQuality, &parseQuality) == nil && parseQuality.ParseSuccess && parseQuality.BlockCount > 0
	gate.Checks = append(gate.Checks, PublicationGateCheck{Name: "parse", Passed: parsePassed, Message: gateMessage(parsePassed, "parse succeeded", "parse result is missing or failed")})

	evaluation, qualityErr := s.publication.FindPublicationQualityEvaluation(ctx, version.ID)
	qualityPassed := qualityErr == nil && evaluation.GateStatus == "pass"
	reviewPassed := qualityErr == nil && evaluation.ReviewStatus == "published"
	qualityCheck := PublicationGateCheck{Name: "quality", Passed: qualityPassed, Message: gateMessage(qualityPassed, "quality gate passed", "completed passing quality evaluation is required")}
	reviewCheck := PublicationGateCheck{Name: "review", Passed: reviewPassed, Message: gateMessage(reviewPassed, "quality evaluation reviewed and published", "published quality review is required")}
	if qualityErr == nil {
		qualityCheck.ResourceID, reviewCheck.ResourceID = &evaluation.ID, &evaluation.ID
	} else if !errors.Is(qualityErr, repository.ErrNotFound) {
		return nil, qualityErr
	}
	gate.Checks = append(gate.Checks, qualityCheck)

	index, indexErr := s.publication.FindPublicationEmbeddingIndex(ctx, version.ID)
	indexPassed := indexErr == nil && index.Status == model.EmbeddingIndexReady && index.ChunkCount > 0 && index.EmbeddedCount == index.ChunkCount
	indexCheck := PublicationGateCheck{Name: "embedding", Passed: indexPassed, Message: gateMessage(indexPassed, "embedding index is ready and complete", "complete ready embedding index is required")}
	if indexErr == nil {
		indexCheck.ResourceID = &index.ID
	} else if !errors.Is(indexErr, repository.ErrNotFound) {
		return nil, indexErr
	}
	gate.Checks = append(gate.Checks, indexCheck)

	run, runErr := s.publication.FindPublicationSmokeRun(ctx, version.ID)
	retrievalPassed := runErr == nil && run.Status == model.RetrievalEvaluationCompleted && run.Passed
	retrievalCheck := PublicationGateCheck{Name: "retrieval", Passed: retrievalPassed, Message: gateMessage(retrievalPassed, "retrieval smoke test passed", "passing retrieval smoke test is required")}
	if runErr == nil {
		retrievalCheck.ResourceID = &run.ID
	} else if !errors.Is(runErr, repository.ErrNotFound) {
		return nil, runErr
	}
	gate.Checks = append(gate.Checks, retrievalCheck, reviewCheck)
	gate.CanPublish = version.Status != model.DocumentVersionStatusFailed && version.Status != model.DocumentVersionStatusDeprecated
	for _, check := range gate.Checks {
		gate.CanPublish = gate.CanPublish && check.Passed
	}
	return gate, nil
}

func (s *Service) PublishVersion(ctx context.Context, actor *model.AppUser, versionID int64, comment string) (*model.KBDocument, *PublicationGate, error) {
	if actor == nil || actor.Role != model.RoleAdmin {
		return nil, nil, ErrAdminRequired
	}
	gate, err := s.EvaluatePublicationGate(ctx, actor, versionID)
	if err != nil {
		return nil, nil, err
	}
	if !gate.CanPublish {
		return nil, gate, ErrPublicationGate
	}
	snapshot, _ := json.Marshal(gate)
	document, err := s.publication.PublishDocumentVersion(ctx, versionID, actor.ID, snapshot, optionalReviewComment(comment))
	return document, gate, err
}

func (s *Service) DeprecateVersion(ctx context.Context, actor *model.AppUser, versionID int64, comment string) (*model.KBDocumentVersion, error) {
	if actor == nil || actor.Role != model.RoleAdmin {
		return nil, ErrAdminRequired
	}
	if _, err := s.GetDocumentVersion(ctx, actor, versionID); err != nil {
		return nil, err
	}
	return s.publication.DeprecateDocumentVersion(ctx, versionID, actor.ID, optionalReviewComment(comment))
}

func (s *Service) DiffVersions(ctx context.Context, actor *model.AppUser, fromID, toID int64) (*VersionDiff, error) {
	from, err := s.GetDocumentVersion(ctx, actor, fromID)
	if err != nil {
		return nil, err
	}
	to, err := s.GetDocumentVersion(ctx, actor, toID)
	if err != nil {
		return nil, err
	}
	if from.DocumentID != to.DocumentID {
		return nil, ErrInvalidInput
	}
	fromBlocks, err := s.documents.ListDocumentVersionBlocks(ctx, from.ID)
	if err != nil {
		return nil, err
	}
	toBlocks, err := s.documents.ListDocumentVersionBlocks(ctx, to.ID)
	if err != nil {
		return nil, err
	}
	a, b := map[string]string{}, map[string]string{}
	for _, block := range fromBlocks {
		if block.ContentHash != nil {
			a[block.BlockKey] = *block.ContentHash
		} else {
			a[block.BlockKey] = block.TextContent
		}
	}
	for _, block := range toBlocks {
		if block.ContentHash != nil {
			b[block.BlockKey] = *block.ContentHash
		} else {
			b[block.BlockKey] = block.TextContent
		}
	}
	diff := &VersionDiff{FromVersion: *from, ToVersion: *to, Added: []string{}, Removed: []string{}, Changed: []string{}}
	for key, hash := range a {
		if next, ok := b[key]; !ok {
			diff.Removed = append(diff.Removed, key)
		} else if next != hash {
			diff.Changed = append(diff.Changed, key)
		}
	}
	for key := range b {
		if _, ok := a[key]; !ok {
			diff.Added = append(diff.Added, key)
		}
	}
	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	sort.Strings(diff.Changed)
	return diff, nil
}

func (s *Service) ResolveHistoricalCitation(ctx context.Context, actor *model.AppUser, versionID, chunkID int64) (*HistoricalCitation, error) {
	if actor == nil || versionID <= 0 || chunkID <= 0 {
		return nil, ErrInvalidInput
	}
	if s.publication == nil {
		return nil, fmt.Errorf("publication repository is unavailable")
	}
	version, chunk, err := s.publication.FindHistoricalCitation(ctx, versionID, chunkID)
	if err != nil {
		return nil, err
	}
	if version.Status != model.DocumentVersionStatusPublished && version.Status != model.DocumentVersionStatusSuperseded && version.Status != model.DocumentVersionStatusDeprecated {
		return nil, ErrForbidden
	}
	return &HistoricalCitation{CitationID: fmt.Sprintf("KC-%d-%d", versionID, chunkID), Version: *version, Chunk: *chunk}, nil
}

func gateMessage(passed bool, success, failure string) string {
	if passed {
		return success
	}
	return failure
}
