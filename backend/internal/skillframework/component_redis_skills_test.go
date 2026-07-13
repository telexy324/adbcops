package skillframework

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"aiops-platform/backend/internal/model"
	redissvc "aiops-platform/backend/internal/redis"
)

func TestRedisKeyspaceSkillDoesNotReturnValues(t *testing.T) {
	fake := &fakeRedisQuerier{
		scan: &redissvc.ScanSummaryResult{
			DataSourceID:    1,
			Nodes:           []redissvc.NodeInfo{{Endpoint: "redis-a:6379", OK: true}},
			ScannedKeys:     2,
			PrefixHistogram: map[string]int{"order:*": 2},
		},
	}
	skill := redisSkillByName(t, fake, "query_redis_keyspace")
	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{"dataSourceId":1,"match":"order:*","maxKeys":2}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if jsonContainsKey(output, "value") {
		t.Fatalf("redis keyspace output leaked value: %s", string(output))
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if decoded["partial"] != false {
		t.Fatalf("expected non-partial output: %s", string(output))
	}
}

func TestRedisHealthDiagnosisIncludesSourceNodeEvidence(t *testing.T) {
	fake := &fakeRedisQuerier{
		info:    &redissvc.InfoResult{DataSourceID: 1, Nodes: []redissvc.NodeInfo{{Endpoint: "redis-a:6379", OK: true}}},
		clients: &redissvc.ClientSummaryResult{DataSourceID: 1, Nodes: []redissvc.NodeInfo{{Endpoint: "redis-a:6379", OK: true}}},
		slowlog: &redissvc.SlowLogResult{DataSourceID: 1, Items: []redissvc.SlowLogItem{{Source: "redis-a:6379", Command: "SET [args redacted]"}}},
	}
	skill := redisSkillByName(t, fake, "diagnose_redis_health")
	output, err := skill.Execute(ContextWithActor(context.Background(), adminActor()), json.RawMessage(`{"dataSourceId":1}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !jsonContainsString(output, "redis-a:6379") {
		t.Fatalf("diagnosis did not include source node evidence: %s", string(output))
	}
	if !jsonContainsString(output, "high") {
		t.Fatalf("diagnosis did not mark high-risk recommendations: %s", string(output))
	}
}

func TestRedisSkillWithoutServiceReturnsPartial(t *testing.T) {
	skill := redisSkillByName(t, nil, "query_redis_info")
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

func redisSkillByName(t *testing.T, redis RedisQuerier, name string) Skill {
	t.Helper()
	for _, skill := range RedisSkills(redis) {
		if skill.Definition().Name == name {
			return skill
		}
	}
	t.Fatalf("redis skill %s not found", name)
	return nil
}

type fakeRedisQuerier struct {
	info        *redissvc.InfoResult
	memory      *redissvc.MemoryStatsResult
	clients     *redissvc.ClientSummaryResult
	slowlog     *redissvc.SlowLogResult
	replication *redissvc.ReplicationResult
	cluster     *redissvc.ClusterResult
	sentinel    *redissvc.SentinelResult
	latency     *redissvc.LatencyResult
	scan        *redissvc.ScanSummaryResult
}

func (f *fakeRedisQuerier) Info(context.Context, *model.AppUser, redissvc.InfoInput) (*redissvc.InfoResult, error) {
	if f.info != nil {
		return f.info, nil
	}
	return &redissvc.InfoResult{}, nil
}

func (f *fakeRedisQuerier) MemoryStats(context.Context, *model.AppUser, redissvc.QueryInput) (*redissvc.MemoryStatsResult, error) {
	if f.memory != nil {
		return f.memory, nil
	}
	return &redissvc.MemoryStatsResult{}, nil
}

func (f *fakeRedisQuerier) ClientListSummary(context.Context, *model.AppUser, redissvc.QueryInput) (*redissvc.ClientSummaryResult, error) {
	if f.clients != nil {
		return f.clients, nil
	}
	return &redissvc.ClientSummaryResult{}, nil
}

func (f *fakeRedisQuerier) SlowLog(context.Context, *model.AppUser, redissvc.SlowLogInput) (*redissvc.SlowLogResult, error) {
	if f.slowlog != nil {
		return f.slowlog, nil
	}
	return &redissvc.SlowLogResult{}, nil
}

func (f *fakeRedisQuerier) Replication(context.Context, *model.AppUser, redissvc.QueryInput) (*redissvc.ReplicationResult, error) {
	if f.replication != nil {
		return f.replication, nil
	}
	return &redissvc.ReplicationResult{}, nil
}

func (f *fakeRedisQuerier) ClusterState(context.Context, *model.AppUser, redissvc.QueryInput) (*redissvc.ClusterResult, error) {
	if f.cluster != nil {
		return f.cluster, nil
	}
	return &redissvc.ClusterResult{}, nil
}

func (f *fakeRedisQuerier) SentinelState(context.Context, *model.AppUser, redissvc.QueryInput) (*redissvc.SentinelResult, error) {
	if f.sentinel != nil {
		return f.sentinel, nil
	}
	return &redissvc.SentinelResult{}, nil
}

func (f *fakeRedisQuerier) LatencyLatest(context.Context, *model.AppUser, redissvc.QueryInput) (*redissvc.LatencyResult, error) {
	if f.latency != nil {
		return f.latency, nil
	}
	return &redissvc.LatencyResult{}, nil
}

func (f *fakeRedisQuerier) ScanSummary(context.Context, *model.AppUser, redissvc.ScanInput) (*redissvc.ScanSummaryResult, error) {
	if f.scan != nil {
		return f.scan, nil
	}
	return &redissvc.ScanSummaryResult{}, nil
}

func jsonContainsString(raw json.RawMessage, value string) bool {
	return strings.Contains(string(raw), value)
}
