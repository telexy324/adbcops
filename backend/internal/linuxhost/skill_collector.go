package linuxhost

import (
	"context"
	"encoding/json"

	"aiops-platform/backend/internal/model"
	linuxserver "aiops-platform/backend/internal/tool/linuxserver"
)

// SkillCollector is the only bridge used by Linux Skills to reach the
// allowlisted collector tool. It never exposes decrypted connections.
type SkillCollector struct {
	hosts *Service
	tool  linuxserver.LinuxServerTool
}

func NewSkillCollector(hosts *Service, tool linuxserver.LinuxServerTool) (*SkillCollector, error) {
	if hosts == nil || tool == nil {
		return nil, ErrInvalidInput
	}
	return &SkillCollector{hosts: hosts, tool: tool}, nil
}

func (c *SkillCollector) TestConnection(ctx context.Context, actor *model.AppUser, hostID int64) (*linuxserver.LinuxConnectionTestResult, error) {
	connection, err := c.connection(ctx, actor, hostID)
	if err != nil {
		return nil, err
	}
	return c.tool.Test(ctx, connection)
}

func (c *SkillCollector) Collect(ctx context.Context, actor *model.AppUser, hostID int64, collector string, parameters json.RawMessage) (*linuxserver.LinuxCollectResult, error) {
	connection, err := c.connection(ctx, actor, hostID)
	if err != nil {
		return nil, err
	}
	return c.tool.Collect(ctx, connection, linuxserver.LinuxCollectRequest{
		HostID: hostID, Collector: collector, Parameters: parameters,
	})
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
