package linuxhost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
	linuxserver "aiops-platform/backend/internal/tool/linuxserver"
)

func TestSkillCollectorResolvesSavedCredentialOnlyInsideCollectorBoundary(t *testing.T) {
	store := &batchResolverStore{fakeRepository: newFakeRepository()}
	service := NewService(store, testCredentialManager(t), "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	username, password := "ops", "skill-secret"
	host, err := service.CreateHost(context.Background(), admin, HostInput{
		Name: "skill-host", Host: "10.0.0.8", Username: &username,
		AuthType: model.LinuxAuthTypePassword, Password: &password, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := &capturingLinuxSkillTool{}
	collector, err := NewSkillCollector(service, tool)
	if err != nil {
		t.Fatal(err)
	}
	result, err := collector.Collect(context.Background(), admin, host.ID, linuxserver.CollectorCPU, json.RawMessage(`{"topN":5}`))
	if err != nil {
		t.Fatal(err)
	}
	if tool.connection.Password != password || tool.request.Collector != linuxserver.CollectorCPU {
		t.Fatalf("collector did not receive resolved connection/request: %+v %+v", tool.connection, tool.request)
	}
	payload, _ := json.Marshal(result)
	if strings.Contains(string(payload), password) || strings.Contains(string(payload), "privateKey") {
		t.Fatalf("collector output leaked credential: %s", payload)
	}
}

func TestSkillCollectorRejectsDisabledHostForNormalUser(t *testing.T) {
	store := &batchResolverStore{fakeRepository: newFakeRepository()}
	service := NewService(store, testCredentialManager(t), "v1")
	admin := &model.AppUser{ID: 1, Role: model.RoleAdmin}
	username, password := "ops", "secret"
	host, err := service.CreateHost(context.Background(), admin, HostInput{
		Name: "disabled", Host: "10.0.0.9", Username: &username,
		AuthType: model.LinuxAuthTypePassword, Password: &password, Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	collector, _ := NewSkillCollector(service, &capturingLinuxSkillTool{})
	_, err = collector.Collect(context.Background(), &model.AppUser{ID: 2, Role: model.RoleUser}, host.ID, linuxserver.CollectorCPU, nil)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled host collect error = %v, want forbidden", err)
	}
}

type capturingLinuxSkillTool struct {
	connection linuxserver.LinuxServerConnection
	request    linuxserver.LinuxCollectRequest
}

func (t *capturingLinuxSkillTool) Test(_ context.Context, connection linuxserver.LinuxServerConnection) (*linuxserver.LinuxConnectionTestResult, error) {
	t.connection = connection
	return &linuxserver.LinuxConnectionTestResult{Status: linuxserver.CommandStatusSuccess}, nil
}

func (t *capturingLinuxSkillTool) DetectPlatform(context.Context, linuxserver.LinuxServerConnection) (*linuxserver.LinuxPlatformInfo, error) {
	return &linuxserver.LinuxPlatformInfo{}, nil
}

func (t *capturingLinuxSkillTool) Collect(_ context.Context, connection linuxserver.LinuxServerConnection, request linuxserver.LinuxCollectRequest) (*linuxserver.LinuxCollectResult, error) {
	t.connection = connection
	t.request = request
	return &linuxserver.LinuxCollectResult{
		Collector: request.Collector, Status: linuxserver.CommandStatusSuccess,
		Data: json.RawMessage(`{"cpu_count":4}`),
	}, nil
}
