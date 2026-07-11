package evidence

import (
	"context"
	"errors"
	"sort"
	"testing"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

func TestCreateEvidenceDefaultsAndRetrieval(t *testing.T) {
	repo := newMemoryEvidenceRepository()
	service := NewService(repo)
	confidence := 0.92

	record, err := service.Create(context.Background(), CreateInput{
		SourceType: "log_anomaly",
		SourceRef:  []byte(`{"index":"app-logs","query":"error"}`),
		Summary:    "payment api error logs spiked",
		Content:    []byte(`{"count":42}`),
		Confidence: &confidence,
	})
	if err != nil {
		t.Fatalf("create evidence: %v", err)
	}
	if record.ID == 0 {
		t.Fatalf("expected repository assigned id")
	}
	if len(record.EvidenceKey) < 4 || record.EvidenceKey[:3] != "ev_" {
		t.Fatalf("expected generated evidence key, got %q", record.EvidenceKey)
	}
	if record.Sensitivity == nil || *record.Sensitivity != model.EvidenceSensitivityInternal {
		t.Fatalf("expected default internal sensitivity, got %v", record.Sensitivity)
	}

	found, err := service.GetByKey(context.Background(), record.EvidenceKey)
	if err != nil {
		t.Fatalf("get evidence by key: %v", err)
	}
	if found.ID != record.ID || found.Summary != record.Summary {
		t.Fatalf("unexpected found evidence: %+v", found)
	}
}

func TestCreateEvidenceRejectsInvalidSensitivityAndConfidence(t *testing.T) {
	service := NewService(newMemoryEvidenceRepository())
	badConfidence := 1.2

	_, err := service.Create(context.Background(), CreateInput{
		SourceType:  "metric_anomaly",
		Summary:     "cpu saturated",
		Sensitivity: "secret",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid sensitivity error, got %v", err)
	}

	_, err = service.Create(context.Background(), CreateInput{
		SourceType: "metric_anomaly",
		Summary:    "cpu saturated",
		Confidence: &badConfidence,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid confidence error, got %v", err)
	}
}

func TestValidateReferencesFailsForMissingEvidence(t *testing.T) {
	repo := newMemoryEvidenceRepository()
	service := NewService(repo)

	_, err := service.Create(context.Background(), CreateInput{
		EvidenceKey: "ev_existing",
		SourceType:  "manual_note",
		Summary:     "operator noted the deploy window",
	})
	if err != nil {
		t.Fatalf("seed evidence: %v", err)
	}

	err = service.ValidateReferences(context.Background(), []string{"ev_existing", "ev_missing", "ev_missing"})
	if !errors.Is(err, ErrEvidenceRefMissing) {
		t.Fatalf("expected missing evidence reference error, got %v", err)
	}
}

type memoryEvidenceRepository struct {
	nextID int64
	byID   map[int64]*model.EvidenceRecord
	byKey  map[string]*model.EvidenceRecord
}

func newMemoryEvidenceRepository() *memoryEvidenceRepository {
	return &memoryEvidenceRepository{
		nextID: 1,
		byID:   map[int64]*model.EvidenceRecord{},
		byKey:  map[string]*model.EvidenceRecord{},
	}
}

func (r *memoryEvidenceRepository) CreateEvidence(_ context.Context, evidence *model.EvidenceRecord) error {
	if evidence.ID == 0 {
		evidence.ID = r.nextID
		r.nextID++
	}
	copied := *evidence
	r.byID[copied.ID] = &copied
	r.byKey[copied.EvidenceKey] = &copied
	return nil
}

func (r *memoryEvidenceRepository) FindEvidenceByID(_ context.Context, id int64) (*model.EvidenceRecord, error) {
	record, ok := r.byID[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copied := *record
	return &copied, nil
}

func (r *memoryEvidenceRepository) FindEvidenceByKey(_ context.Context, key string) (*model.EvidenceRecord, error) {
	record, ok := r.byKey[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copied := *record
	return &copied, nil
}

func (r *memoryEvidenceRepository) ListEvidence(_ context.Context, _ repository.EvidenceFilters) ([]model.EvidenceRecord, error) {
	ids := make([]int, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	result := make([]model.EvidenceRecord, 0, len(ids))
	for _, id := range ids {
		result = append(result, *r.byID[int64(id)])
	}
	return result, nil
}

func (r *memoryEvidenceRepository) MissingEvidenceKeys(_ context.Context, keys []string) ([]string, error) {
	missing := []string{}
	for _, key := range keys {
		if _, ok := r.byKey[key]; !ok {
			missing = append(missing, key)
		}
	}
	return missing, nil
}
