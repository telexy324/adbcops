package topology

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
)

func TestSyncLinuxHostsHashesMachineIDFiltersMetricsAndBuildsRelations(t *testing.T) {
	repo := newMemoryTopologyRepository()
	seedLinuxTopologyTypes(repo)
	environment := "prod"
	reader := &fakeLinuxTopologyReader{
		hosts: []model.LinuxHost{
			{ID: 1, Name: "app-01", Host: "10.0.0.1", Port: 22, Environment: &environment, Enabled: true},
			{ID: 2, Name: "app-02", Host: "10.0.0.2", Port: 2222, Environment: &environment, Enabled: true},
		},
		groups:  []model.LinuxHostGroup{{ID: 5, Name: "payments", Environment: &environment}},
		members: map[int64][]int64{5: {1}}, updatedHashes: map[int64]string{},
	}
	service := NewService(repo, nil).WithLinuxTopologyReader(reader)
	observedAt := time.Date(2026, 7, 18, 1, 30, 0, 0, time.UTC)
	result, err := service.SyncLinuxHosts(context.Background(), &model.AppUser{ID: 1, Role: model.RoleAdmin}, SyncLinuxHostsInput{
		Observations: []LinuxHostObservation{{
			HostID: 1, MachineID: "RAW-MACHINE-ID-123", ObservedAt: &observedAt,
			StaticAttributes: map[string]any{
				"hostname": "app-01.internal", "cpu_count": 8, "memory_total": 16_000,
				"cpu_usage_percent": 99.9, "memory_used_percent": 87.5, "load_1m": 12.0,
			},
		}},
		RuntimeFacts: []LinuxRuntimeFact{
			{HostID: 1, Kind: "service", Identity: "payment-api", Name: "payment-api", Confidence: .93, ObservedAt: &observedAt, Properties: map[string]any{"version": "1.2.3", "cpuPercent": 88.0}},
			{HostID: 1, Kind: "process", Identity: "payment-worker", Name: "payment-worker", Confidence: .88, ObservedAt: &observedAt, Properties: map[string]any{"commandName": "payment-worker", "memoryPercent": 72.0}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Hosts != 2 || result.Groups != 1 || result.Runtime != 2 || result.Edges != 3 {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	hash := reader.updatedHashes[1]
	if !strings.HasPrefix(hash, "sha256:") || strings.Contains(hash, "RAW-MACHINE-ID-123") {
		t.Fatalf("machine id was not hashed before persistence: %q", hash)
	}
	machineNode := findLinuxHostNode(t, repo, 1)
	if strings.Contains(string(machineNode.Properties), "RAW-MACHINE-ID-123") {
		t.Fatalf("raw machine id leaked into topology: %s", machineNode.Properties)
	}
	properties := decodeObject(t, machineNode.Properties)
	if properties["machineIdentityHash"] != hash || properties["cpuCount"] != float64(8) {
		t.Fatalf("static host properties missing: %+v", properties)
	}
	for _, forbidden := range []string{"cpuUsagePercent", "cpu_usage_percent", "memoryUsedPercent", "memory_used_percent", "load1m", "load_1m"} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("dynamic metric %q entered topology: %+v", forbidden, properties)
		}
	}
	fallbackNode := findLinuxHostNode(t, repo, 2)
	fallback := decodeObject(t, fallbackNode.Properties)
	if fallback["identityType"] != "host_port" || fallback["identity"] != "10.0.0.2:2222" {
		t.Fatalf("host:port fallback identity missing: %+v", fallback)
	}
	assertLinuxEdges(t, repo, observedAt)
	for _, node := range repo.nodes {
		if node.Kind == "service" || node.Kind == model.TopologyNodeKindProcess {
			if strings.Contains(string(node.Properties), "cpuPercent") || strings.Contains(string(node.Properties), "memoryPercent") {
				t.Fatalf("dynamic runtime metric entered topology: %s", node.Properties)
			}
		}
	}
}

func TestSyncLinuxHostsRecordsCMDBMachineIdentityConflictWithoutMerge(t *testing.T) {
	repo := newMemoryTopologyRepository()
	seedLinuxTopologyTypes(repo)
	environment := "prod"
	cmdbHash := "sha256:" + strings.Repeat("a", 64)
	cmdbProperties, _ := json.Marshal(map[string]any{
		"hostname": "db-01", "managementIp": "10.0.0.9", "machineIdentityHash": cmdbHash,
	})
	repo.nodes["prod:host:cmdb:asset-9"] = model.TopologyNode{
		ID: 99, NodeKey: "prod:host:cmdb:asset-9", Kind: model.TopologyNodeKindHost,
		Name: "db-01", Environment: &environment, Properties: cmdbProperties,
		SourceType: model.TopologySourceTypeCMDB, SourcePriority: 90,
	}
	reader := &fakeLinuxTopologyReader{
		hosts:   []model.LinuxHost{{ID: 9, Name: "db-01", Host: "10.0.0.9", Port: 22, Environment: &environment, Enabled: true}},
		members: map[int64][]int64{}, updatedHashes: map[int64]string{},
	}
	service := NewService(repo, nil).WithLinuxTopologyReader(reader)
	result, err := service.SyncLinuxHosts(context.Background(), &model.AppUser{ID: 1, Role: model.RoleAdmin}, SyncLinuxHostsInput{
		Observations: []LinuxHostObservation{{HostID: 9, MachineID: "different-machine-id"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Conflicts != 1 || len(repo.conflicts) != 1 || repo.conflicts[0].ConflictType != "cmdb_identity_conflict" {
		t.Fatalf("CMDB conflict was not recorded: result=%+v conflicts=%+v", result, repo.conflicts)
	}
	if len(repo.nodes) != 2 {
		t.Fatalf("conflicting hosts were automatically merged: %+v", repo.nodes)
	}
	conflictPayload := string(repo.conflicts[0].Candidates)
	if strings.Contains(conflictPayload, "different-machine-id") || !strings.Contains(conflictPayload, "sha256:") {
		t.Fatalf("conflict payload leaks raw identity or omits hashes: %s", conflictPayload)
	}
}

func TestLinuxHostTopologyMigrationSeedsRequiredTypes(t *testing.T) {
	raw, err := migrationFile("../../migrations/000049_linux_host_topology.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"'host_group'", "'process'"} {
		if !strings.Contains(raw, expected) {
			t.Fatalf("migration does not seed %s", expected)
		}
	}
}

func seedLinuxTopologyTypes(repo *memoryTopologyRepository) {
	for _, kind := range []string{model.TopologyNodeKindHost, model.TopologyNodeKindHostGroup, model.TopologyNodeKindProcess} {
		repo.seedNodeType(kind)
	}
}

func findLinuxHostNode(t *testing.T, repo *memoryTopologyRepository, hostID int64) model.TopologyNode {
	t.Helper()
	for _, node := range repo.nodes {
		if node.Kind != model.TopologyNodeKindHost {
			continue
		}
		properties := decodeObject(t, node.Properties)
		if properties["hostId"] == float64(hostID) {
			return node
		}
	}
	t.Fatalf("host node %d not found: %+v", hostID, repo.nodes)
	return model.TopologyNode{}
}

func assertLinuxEdges(t *testing.T, repo *memoryTopologyRepository, observedAt time.Time) {
	t.Helper()
	memberOf, runsOn := 0, 0
	for _, edge := range repo.edges {
		if edge.SourceType != model.TopologySourceTypeLinuxServer || edge.Confidence == nil || *edge.Confidence <= 0 {
			t.Fatalf("edge lacks Linux source or confidence: %+v", edge)
		}
		switch edge.EdgeType {
		case model.TopologyEdgeTypeMemberOf:
			memberOf++
		case model.TopologyEdgeTypeRunsOn:
			runsOn++
			if edge.StaleAt == nil || !edge.StaleAt.After(observedAt) {
				t.Fatalf("runtime edge lacks TTL: %+v", edge)
			}
		}
	}
	if memberOf != 1 || runsOn != 2 {
		t.Fatalf("unexpected Linux relations: member_of=%d runs_on=%d", memberOf, runsOn)
	}
}

func decodeObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func migrationFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	return string(raw), err
}

type fakeLinuxTopologyReader struct {
	hosts         []model.LinuxHost
	groups        []model.LinuxHostGroup
	members       map[int64][]int64
	updatedHashes map[int64]string
}

func (r *fakeLinuxTopologyReader) ListLinuxHosts(_ context.Context, _ bool) ([]model.LinuxHost, error) {
	return append([]model.LinuxHost(nil), r.hosts...), nil
}

func (r *fakeLinuxTopologyReader) ListLinuxHostGroups(_ context.Context) ([]model.LinuxHostGroup, error) {
	return append([]model.LinuxHostGroup(nil), r.groups...), nil
}

func (r *fakeLinuxTopologyReader) ListLinuxHostIDsByGroupIDs(_ context.Context, groupIDs []int64) ([]int64, error) {
	result := []int64{}
	for _, groupID := range groupIDs {
		result = append(result, r.members[groupID]...)
	}
	return result, nil
}

func (r *fakeLinuxTopologyReader) UpdateLinuxHostMachineIdentityHash(_ context.Context, hostID int64, identityHash string) error {
	r.updatedHashes[hostID] = identityHash
	return nil
}
