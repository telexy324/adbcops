package document

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"aiops-platform/backend/internal/model"
)

const QualityPrompt = `Review the operations knowledge document. Return strict JSON with score, summary, findings, and suggestions. Score must be 0-100.`

var ErrInvalidQualityJSON = errors.New("invalid quality result JSON")

type QualityResult struct {
	Score       int      `json:"score"`
	Summary     string   `json:"summary"`
	Findings    []string `json:"findings"`
	Suggestions []string `json:"suggestions"`
}

func ParseQualityResult(raw json.RawMessage) (QualityResult, []byte, error) {
	if len(raw) == 0 {
		return QualityResult{}, nil, fmt.Errorf("%w: result is required", ErrInvalidQualityJSON)
	}
	var result QualityResult
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return QualityResult{}, nil, fmt.Errorf("%w: %v", ErrInvalidQualityJSON, err)
	}
	if result.Score < 0 || result.Score > 100 {
		return QualityResult{}, nil, fmt.Errorf("%w: score must be from 0 to 100", ErrInvalidQualityJSON)
	}
	if strings.TrimSpace(result.Summary) == "" {
		return QualityResult{}, nil, fmt.Errorf("%w: summary is required", ErrInvalidQualityJSON)
	}
	if len(result.Findings) == 0 {
		return QualityResult{}, nil, fmt.Errorf("%w: findings must not be empty", ErrInvalidQualityJSON)
	}
	if len(result.Suggestions) == 0 {
		return QualityResult{}, nil, fmt.Errorf("%w: suggestions must not be empty", ErrInvalidQualityJSON)
	}
	normalized, err := json.Marshal(result)
	if err != nil {
		return QualityResult{}, nil, fmt.Errorf("normalize quality result: %w", err)
	}
	return result, normalized, nil
}

func StatusAfterQualityScore(score int) string {
	if score >= 70 {
		return model.DocumentStatusReviewing
	}
	return model.DocumentStatusRejected
}

func CanPublish(document *model.KBDocument) bool {
	return document != nil && document.QualityScore >= 70 && document.Status == model.DocumentStatusReviewing
}
