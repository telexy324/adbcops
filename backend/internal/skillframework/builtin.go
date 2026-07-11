package skillframework

import (
	"context"
	"encoding/json"

	"aiops-platform/backend/internal/model"
)

func BuiltinSkills() []Skill {
	return []Skill{
		EchoSkill{},
	}
}

type EchoSkill struct{}

func (EchoSkill) Definition() SkillDefinition {
	return SkillDefinition{
		Name:          "echo_safe",
		Version:       "v1",
		Description:   "Framework smoke-test skill that echoes validated safe_read input.",
		InputSchema:   json.RawMessage(`{"type":"object","required":["message"],"properties":{"message":{"type":"string"}}}`),
		OutputSchema:  json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}}}`),
		RiskLevel:     model.SkillRiskSafeRead,
		ReadOnly:      true,
		TimeoutSecond: 5,
		RequiredTools: nil,
	}
}

func (EchoSkill) Execute(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	return input, nil
}
