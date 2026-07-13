package skillframework

import (
	"context"
	"encoding/json"
	"testing"

	"aiops-platform/backend/internal/model"
	nacossvc "aiops-platform/backend/internal/nacos"
)

func TestNacosConfigMetadataSkillDoesNotReturnContent(t *testing.T) {
	fake := &fakeNacosQuerier{
		metadata: &nacossvc.ConfigMetadataResult{
			DataSourceID: 1,
			Namespace:    "prod",
			Group:        "DEFAULT_GROUP",
			DataID:       "orders.yaml",
			Metadata: map[string]string{
				"dataId": "orders.yaml",
				"md5":    "abc",
			},
		},
	}
	skill := NacosSkills(fake)[2]
	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{"dataSourceId":1,"namespace":"prod","group":"DEFAULT_GROUP","dataId":"orders.yaml"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(output) == "" || jsonContainsKey(output, "content") {
		t.Fatalf("output leaked config content: %s", string(output))
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if decoded["partial"] != false {
		t.Fatalf("expected non-partial output: %s", string(output))
	}
}

func TestNacosRegistrationDiagnosisReturnsFactsAndEvidenceRef(t *testing.T) {
	fake := &fakeNacosQuerier{
		services: &nacossvc.ServiceListResult{
			DataSourceID: 1,
			Namespace:    "prod",
			Group:        "DEFAULT_GROUP",
			Services:     []nacossvc.NacosEntry{{Name: "orders"}},
		},
		instances: &nacossvc.InstanceListResult{
			DataSourceID: 1,
			Namespace:    "prod",
			Group:        "DEFAULT_GROUP",
			ServiceName:  "orders",
			Instances: []nacossvc.NacosInstance{
				{IP: "10.0.0.1", Port: 8848, Healthy: false, Enabled: true},
			},
		},
	}
	var skill Skill
	for _, candidate := range NacosSkills(fake) {
		if candidate.Definition().Name == "diagnose_nacos_registration" {
			skill = candidate
			break
		}
	}
	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{"dataSourceId":1,"namespace":"prod","group":"DEFAULT_GROUP","serviceName":"orders"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var decoded struct {
		Partial bool `json:"partial"`
		Facts   []struct {
			Type        string `json:"type"`
			EvidenceRef string `json:"evidenceRef"`
		} `json:"facts"`
	}
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if decoded.Partial || len(decoded.Facts) < 2 {
		t.Fatalf("unexpected diagnosis output: %s", string(output))
	}
	if decoded.Facts[0].Type != "FACT" || decoded.Facts[0].EvidenceRef == "" || decoded.Facts[1].Type != "RULE" {
		t.Fatalf("facts do not contain FACT/RULE/EvidenceRef: %+v", decoded.Facts)
	}
}

func TestNacosSkillWithoutServiceReturnsPartial(t *testing.T) {
	skill := NacosSkills(nil)[0]
	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{"dataSourceId":1}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if decoded["partial"] != true {
		t.Fatalf("expected partial output: %s", string(output))
	}
}

type fakeNacosQuerier struct {
	services    *nacossvc.ServiceListResult
	instances   *nacossvc.InstanceListResult
	metadata    *nacossvc.ConfigMetadataResult
	changes     *nacossvc.ConfigHistoryResult
	connections *nacossvc.ClientConnectionsResult
	listeners   *nacossvc.ListenersResult
}

func (f *fakeNacosQuerier) ListServices(context.Context, *model.AppUser, nacossvc.ListServicesInput) (*nacossvc.ServiceListResult, error) {
	if f.services != nil {
		return f.services, nil
	}
	return &nacossvc.ServiceListResult{}, nil
}

func (f *fakeNacosQuerier) ListInstances(context.Context, *model.AppUser, nacossvc.ListInstancesInput) (*nacossvc.InstanceListResult, error) {
	if f.instances != nil {
		return f.instances, nil
	}
	return &nacossvc.InstanceListResult{}, nil
}

func (f *fakeNacosQuerier) GetConfigMetadata(context.Context, *model.AppUser, nacossvc.ConfigMetadataInput) (*nacossvc.ConfigMetadataResult, error) {
	if f.metadata != nil {
		return f.metadata, nil
	}
	return &nacossvc.ConfigMetadataResult{}, nil
}

func (f *fakeNacosQuerier) ListConfigChanges(context.Context, *model.AppUser, nacossvc.ConfigHistoryInput) (*nacossvc.ConfigHistoryResult, error) {
	if f.changes != nil {
		return f.changes, nil
	}
	return &nacossvc.ConfigHistoryResult{}, nil
}

func (f *fakeNacosQuerier) ListClientConnections(context.Context, *model.AppUser, nacossvc.ClientConnectionsInput) (*nacossvc.ClientConnectionsResult, error) {
	if f.connections != nil {
		return f.connections, nil
	}
	return &nacossvc.ClientConnectionsResult{}, nil
}

func (f *fakeNacosQuerier) ListListeners(context.Context, *model.AppUser, nacossvc.ListenersInput) (*nacossvc.ListenersResult, error) {
	if f.listeners != nil {
		return f.listeners, nil
	}
	return &nacossvc.ListenersResult{}, nil
}

func jsonContainsKey(raw json.RawMessage, key string) bool {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	return containsKey(value, key)
}

func containsKey(value any, key string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if _, ok := typed[key]; ok {
			return true
		}
		for _, child := range typed {
			if containsKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsKey(child, key) {
				return true
			}
		}
	}
	return false
}
