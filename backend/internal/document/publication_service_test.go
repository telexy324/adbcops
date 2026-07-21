package document

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

type publicationTestRepository struct {
	*fakeRepository
	quality         *model.KBQualityEvaluation
	index           *model.KBEmbeddingIndex
	run             *model.KBRetrievalEvaluationRun
	citationVersion *model.KBDocumentVersion
	citationChunk   *model.KBChunk
}

func (f *publicationTestRepository) ListDocumentVersions(_ context.Context, documentID int64) ([]model.KBDocumentVersion, error) {
	items := []model.KBDocumentVersion{}
	for _, item := range f.versions {
		if item.DocumentID == documentID {
			items = append(items, *item)
		}
	}
	return items, nil
}
func (f *publicationTestRepository) CreateDocumentVersion(_ context.Context, version *model.KBDocumentVersion) error {
	version.ID = f.nextVersionID
	f.nextVersionID++
	version.RevisionNo = 1
	f.versions[version.ID] = version
	return nil
}
func (f *publicationTestRepository) FindPublicationQualityEvaluation(context.Context, int64) (*model.KBQualityEvaluation, error) {
	if f.quality == nil {
		return nil, repository.ErrNotFound
	}
	copy := *f.quality
	return &copy, nil
}
func (f *publicationTestRepository) FindPublicationEmbeddingIndex(context.Context, int64) (*model.KBEmbeddingIndex, error) {
	if f.index == nil {
		return nil, repository.ErrNotFound
	}
	copy := *f.index
	return &copy, nil
}
func (f *publicationTestRepository) FindPublicationSmokeRun(context.Context, int64) (*model.KBRetrievalEvaluationRun, error) {
	if f.run == nil {
		return nil, repository.ErrNotFound
	}
	copy := *f.run
	return &copy, nil
}
func (f *publicationTestRepository) PublishDocumentVersion(_ context.Context, versionID, actorID int64, gate []byte, _ *string) (*model.KBDocument, error) {
	version := f.versions[versionID]
	document := f.documents[version.DocumentID]
	if document.CurrentPublishedVersionID != nil && *document.CurrentPublishedVersionID != versionID {
		f.versions[*document.CurrentPublishedVersionID].Status = model.DocumentVersionStatusSuperseded
	}
	version.Status, version.PublicationGate = model.DocumentVersionStatusPublished, json.RawMessage(gate)
	document.CurrentPublishedVersionID, document.Status, document.Version = &versionID, model.DocumentStatusPublished, version.Version
	return document, nil
}
func (f *publicationTestRepository) DeprecateDocumentVersion(context.Context, int64, int64, *string) (*model.KBDocumentVersion, error) {
	return nil, repository.ErrNotFound
}
func (f *publicationTestRepository) FindHistoricalCitation(context.Context, int64, int64) (*model.KBDocumentVersion, *model.KBChunk, error) {
	if f.citationVersion == nil || f.citationChunk == nil {
		return nil, nil, repository.ErrNotFound
	}
	version, chunk := *f.citationVersion, *f.citationChunk
	return &version, &chunk, nil
}

func TestResolveHistoricalCitationAllowsSupersededVersion(t *testing.T) {
	base := newFakeRepository()
	store := &publicationTestRepository{fakeRepository: base,
		citationVersion: &model.KBDocumentVersion{ID: 8, DocumentID: 3, Status: model.DocumentVersionStatusSuperseded},
		citationChunk:   &model.KBChunk{ID: 21, DocumentID: 3, DocumentVersionID: 8, Content: "historical evidence"},
	}
	service := &Service{documents: store, publication: store}
	citation, err := service.ResolveHistoricalCitation(context.Background(), &model.AppUser{ID: 99, Role: model.RoleUser}, 8, 21)
	if err != nil {
		t.Fatalf("ResolveHistoricalCitation() error = %v", err)
	}
	if citation.CitationID != "KC-8-21" || citation.Chunk.Content != "historical evidence" {
		t.Fatalf("citation = %+v", citation)
	}
}

func TestPublicationGateAndPublishSupersedesCurrentVersion(t *testing.T) {
	base := newFakeRepository()
	owner := int64(7)
	oldID, candidateID := int64(1), int64(2)
	base.documents[1] = &model.KBDocument{ID: 1, CreatedBy: &owner, Status: model.DocumentStatusPublished, CurrentPublishedVersionID: &oldID}
	base.versions[oldID] = &model.KBDocumentVersion{ID: oldID, DocumentID: 1, Version: "v1", Status: model.DocumentVersionStatusPublished}
	base.versions[candidateID] = &model.KBDocumentVersion{ID: candidateID, DocumentID: 1, Version: "v2", Status: model.DocumentVersionStatusReviewing, ParseQuality: ParseQuality{ParseSuccess: true, BlockCount: 2}.JSON()}
	store := &publicationTestRepository{fakeRepository: base,
		quality: &model.KBQualityEvaluation{ID: 10, GateStatus: "pass", Status: "completed", ReviewStatus: "published"},
		index:   &model.KBEmbeddingIndex{ID: 20, Status: model.EmbeddingIndexReady, ChunkCount: 2, EmbeddedCount: 2},
		run:     &model.KBRetrievalEvaluationRun{ID: 30, Status: model.RetrievalEvaluationCompleted, Passed: true, CompletedAt: func() *time.Time { now := time.Now(); return &now }()},
	}
	service := &Service{documents: store, publication: store}
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	gate, err := service.EvaluatePublicationGate(context.Background(), admin, candidateID)
	if err != nil || !gate.CanPublish || len(gate.Checks) != 5 {
		t.Fatalf("gate = %+v, err = %v", gate, err)
	}
	document, _, err := service.PublishVersion(context.Background(), admin, candidateID, "release")
	if err != nil {
		t.Fatalf("PublishVersion() error = %v", err)
	}
	if document.CurrentPublishedVersionID == nil || *document.CurrentPublishedVersionID != candidateID {
		t.Fatalf("current version = %v", document.CurrentPublishedVersionID)
	}
	if base.versions[oldID].Status != model.DocumentVersionStatusSuperseded {
		t.Fatalf("old status = %q", base.versions[oldID].Status)
	}
	if base.versions[candidateID].Status != model.DocumentVersionStatusPublished {
		t.Fatalf("candidate status = %q", base.versions[candidateID].Status)
	}
}

func TestPublicationGateSeparatesQualityFromReview(t *testing.T) {
	base := newFakeRepository()
	owner := int64(7)
	versionID := int64(2)
	base.documents[1] = &model.KBDocument{ID: 1, CreatedBy: &owner}
	base.versions[versionID] = &model.KBDocumentVersion{ID: versionID, DocumentID: 1, Status: model.DocumentVersionStatusReviewing, ParseQuality: ParseQuality{ParseSuccess: true, BlockCount: 1}.JSON()}
	store := &publicationTestRepository{fakeRepository: base,
		quality: &model.KBQualityEvaluation{ID: 10, GateStatus: "pass", Status: "completed", ReviewStatus: "draft"},
		index:   &model.KBEmbeddingIndex{ID: 20, Status: model.EmbeddingIndexReady, ChunkCount: 1, EmbeddedCount: 1},
		run:     &model.KBRetrievalEvaluationRun{ID: 30, Status: model.RetrievalEvaluationCompleted, Passed: true},
	}
	gate, err := (&Service{documents: store, publication: store}).EvaluatePublicationGate(context.Background(), &model.AppUser{ID: 1, Role: model.RoleAdmin}, versionID)
	if err != nil {
		t.Fatalf("EvaluatePublicationGate() error = %v", err)
	}
	if gate.CanPublish || len(gate.Checks) != 5 || !gate.Checks[1].Passed || gate.Checks[4].Passed {
		t.Fatalf("gate = %+v", gate)
	}
}
