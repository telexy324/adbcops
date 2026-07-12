package skillframework

import (
	"context"
	"encoding/json"
	"time"

	changesvc "aiops-platform/backend/internal/change"
	"aiops-platform/backend/internal/model"
)

type ChangeQuerier interface {
	QueryRecent(ctx context.Context, actor *model.AppUser, input changesvc.QueryInput) (*changesvc.Result, error)
}

func ChangeSkills(changes ChangeQuerier) []Skill {
	return []Skill{
		QueryRecentChangesSkill{changes: changes},
	}
}

type QueryRecentChangesSkill struct {
	changes ChangeQuerier
}

func (s QueryRecentChangesSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "query_recent_changes",
		Version:       "v1",
		Description:   "Query recent release, config and Git changes from a read-only generic HTTP data source.",
		InputSchema:   json.RawMessage(`{"type":"object","required":["dataSourceId"],"properties":{"dataSourceId":{"type":"integer"},"from":{"type":"string"},"to":{"type":"string"},"environment":{"type":"string"},"systemName":{"type":"string"},"component":{"type":"string"},"limit":{"type":"integer"}}}`),
		OutputSchema:  partialOutputSchema(),
		RiskLevel:     model.SkillRiskSensitiveRead,
		ReadOnly:      true,
		TimeoutSecond: 20,
		RequiredTools: []string{"generic_http"},
	}
}

func (s QueryRecentChangesSkill) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var request struct {
		DataSourceID int64  `json:"dataSourceId"`
		From         string `json:"from"`
		To           string `json:"to"`
		Environment  string `json:"environment"`
		SystemName   string `json:"systemName"`
		Component    string `json:"component"`
		Limit        int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, ErrInvalidInput
	}
	if request.DataSourceID <= 0 {
		return nil, ErrInvalidInput
	}
	var from *time.Time
	if request.From != "" {
		parsed, err := parseSkillTime(request.From)
		if err != nil {
			return nil, ErrInvalidInput
		}
		from = &parsed
	}
	var to *time.Time
	if request.To != "" {
		parsed, err := parseSkillTime(request.To)
		if err != nil {
			return nil, ErrInvalidInput
		}
		to = &parsed
	}
	if s.changes == nil {
		return partialError("generic_http", "change service is not configured"), nil
	}
	result, err := s.changes.QueryRecent(ctx, ActorFromContext(ctx), changesvc.QueryInput{
		DataSourceID: request.DataSourceID,
		From:         from,
		To:           to,
		Environment:  request.Environment,
		SystemName:   request.SystemName,
		Component:    request.Component,
		Limit:        request.Limit,
	})
	if err != nil {
		return partialError("generic_http", err.Error()), nil
	}
	return json.Marshal(map[string]any{"partial": result.Partial, "changes": result})
}
