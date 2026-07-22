package linuxhost

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"aiops-platform/backend/internal/auditutil"
	evidencesvc "aiops-platform/backend/internal/evidence"
	appmiddleware "aiops-platform/backend/internal/middleware"
	"aiops-platform/backend/internal/model"
	linuxserver "aiops-platform/backend/internal/tool/linuxserver"
)

// SkillCollector is the only bridge used by Linux Skills to reach the
// allowlisted collector tool. It never exposes decrypted connections.
type SkillCollector struct {
	hosts    *Service
	tool     linuxserver.LinuxServerTool
	evidence LinuxEvidenceRecorder
}

type LinuxEvidenceRecorder interface {
	Create(context.Context, evidencesvc.CreateInput) (*model.EvidenceRecord, error)
}

func NewSkillCollector(hosts *Service, tool linuxserver.LinuxServerTool) (*SkillCollector, error) {
	if hosts == nil || tool == nil {
		return nil, ErrInvalidInput
	}
	return &SkillCollector{hosts: hosts, tool: tool}, nil
}

func (c *SkillCollector) WithEvidenceRecorder(recorder LinuxEvidenceRecorder) *SkillCollector {
	c.evidence = recorder
	return c
}

func (c *SkillCollector) TestConnection(ctx context.Context, actor *model.AppUser, hostID int64) (*linuxserver.LinuxConnectionTestResult, error) {
	connection, err := c.connection(ctx, actor, hostID)
	if err != nil {
		return nil, err
	}
	result, err := c.tool.Test(ctx, connection)
	if err == nil && result != nil && result.Status == linuxserver.CommandStatusSuccess {
		_, err = c.hosts.VerifyObservedHostKey(ctx, hostID, HostKeyOperationTest, result.HostKeyAlgorithm, result.HostKeyFingerprint)
	}
	return result, err
}

func (c *SkillCollector) Collect(ctx context.Context, actor *model.AppUser, hostID int64, collector string, parameters json.RawMessage) (*linuxserver.LinuxCollectResult, error) {
	connection, err := c.connection(ctx, actor, hostID)
	if err != nil {
		return nil, err
	}
	connection, err = c.pinFirstObservedHostKey(ctx, hostID, connection)
	if err != nil {
		return nil, err
	}
	result, err := c.tool.Collect(ctx, connection, linuxserver.LinuxCollectRequest{
		HostID: hostID, Collector: collector, Parameters: parameters,
	})
	if err != nil || result == nil || c.evidence == nil {
		return result, err
	}
	if persistErr := c.persistEvidence(ctx, hostID, result); persistErr != nil {
		slog.WarnContext(ctx, "linux collector evidence persistence failed",
			"request_id", appmiddleware.GetRequestIDFromContext(ctx), "host_id", hostID,
			"collector", collector, "error", "evidence persistence failed")
	}
	return result, nil
}

func (c *SkillCollector) pinFirstObservedHostKey(ctx context.Context, hostID int64, connection linuxserver.LinuxServerConnection) (linuxserver.LinuxServerConnection, error) {
	if strings.TrimSpace(connection.HostKeyFingerprint) != "" {
		return connection, nil
	}
	result, err := c.tool.Test(ctx, connection)
	if err != nil {
		return linuxserver.LinuxServerConnection{}, err
	}
	if result == nil || result.Status != linuxserver.CommandStatusSuccess ||
		strings.TrimSpace(result.HostKeyAlgorithm) == "" || strings.TrimSpace(result.HostKeyFingerprint) == "" {
		return linuxserver.LinuxServerConnection{}, errors.New("SSH host key observation failed")
	}
	verification, err := c.hosts.VerifyObservedHostKey(ctx, hostID, HostKeyOperationTest, result.HostKeyAlgorithm, result.HostKeyFingerprint)
	if err != nil {
		return linuxserver.LinuxServerConnection{}, err
	}
	connection.HostKeyAlgorithm = verification.Algorithm
	connection.HostKeyFingerprint = verification.Fingerprint
	return connection, nil
}

func (c *SkillCollector) persistEvidence(ctx context.Context, hostID int64, result *linuxserver.LinuxCollectResult) error {
	sourceRef, _ := json.Marshal(map[string]any{
		"hostId": hostID, "collector": result.Collector,
		"requestId": appmiddleware.GetRequestIDFromContext(ctx), "collectedAt": result.CollectedAt,
	})
	content, _ := json.Marshal(result)
	content = auditutil.SanitizeJSON(content, 256<<10)
	title := "Linux " + result.Collector + " collector"
	confidence := 1.0
	if result.Status != linuxserver.CommandStatusSuccess {
		confidence = 0.6
	}
	_, err := c.evidence.Create(ctx, evidencesvc.CreateInput{
		SourceType: "linux_server", SourceRef: sourceRef, ObservedAt: &result.CollectedAt,
		Title: &title, Summary: "Linux " + result.Collector + " collector returned " + result.Status,
		Content: content, Confidence: &confidence, Sensitivity: model.EvidenceSensitivityInternal,
	})
	return err
}

func (c *SkillCollector) connection(ctx context.Context, actor *model.AppUser, hostID int64) (linuxserver.LinuxServerConnection, error) {
	if actor == nil || hostID <= 0 {
		return linuxserver.LinuxServerConnection{}, ErrForbidden
	}
	repository, ok := c.hosts.repository.(batchConnectionRepository)
	if !ok || c.hosts.secrets == nil {
		return linuxserver.LinuxServerConnection{}, ErrInvalidInput
	}
	host, err := repository.FindLinuxHostWithCredential(ctx, hostID)
	if err != nil {
		return linuxserver.LinuxServerConnection{}, err
	}
	if actor.Role != model.RoleAdmin && !host.Enabled {
		return linuxserver.LinuxServerConnection{}, ErrForbidden
	}
	return c.hosts.connectionForHost(host)
}
