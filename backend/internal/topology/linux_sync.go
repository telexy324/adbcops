package topology

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

const defaultLinuxRuntimeTTL = 30 * time.Minute

type SyncLinuxHostsInput struct {
	HostIDs      []int64                `json:"hostIds"`
	Observations []LinuxHostObservation `json:"observations"`
	RuntimeFacts []LinuxRuntimeFact     `json:"runtimeFacts"`
}

type LinuxHostObservation struct {
	HostID           int64          `json:"hostId"`
	MachineID        string         `json:"machineId"`
	StaticAttributes map[string]any `json:"staticAttributes"`
	ObservedAt       *time.Time     `json:"observedAt"`
}

type LinuxRuntimeFact struct {
	HostID     int64          `json:"hostId"`
	Kind       string         `json:"kind"`
	Identity   string         `json:"identity"`
	Name       string         `json:"name"`
	Confidence float64        `json:"confidence"`
	Properties map[string]any `json:"properties"`
	ObservedAt *time.Time     `json:"observedAt"`
	TTLSeconds int            `json:"ttlSeconds"`
}

type LinuxSyncResult struct {
	Hosts     int `json:"hosts"`
	Groups    int `json:"groups"`
	Runtime   int `json:"runtime"`
	Edges     int `json:"edges"`
	Conflicts int `json:"conflicts"`
}

func (s *Service) SyncLinuxHosts(ctx context.Context, actor *model.AppUser, input SyncLinuxHostsInput) (*LinuxSyncResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if s.linuxTopologyReader == nil {
		return nil, ErrInvalidInput
	}
	hosts, err := s.linuxTopologyReader.ListLinuxHosts(ctx, false)
	if err != nil {
		return nil, err
	}
	selected := int64Set(input.HostIDs)
	observations, err := normalizeLinuxObservations(input.Observations)
	if err != nil {
		return nil, err
	}
	result := &LinuxSyncResult{}
	hostNodes := map[int64]*model.TopologyNode{}
	for index := range hosts {
		host := &hosts[index]
		if !host.Enabled || (len(selected) > 0 && !selected[host.ID]) {
			continue
		}
		node, conflicts, syncErr := s.syncLinuxHostNode(ctx, host, observations[host.ID])
		if syncErr != nil {
			return nil, syncErr
		}
		hostNodes[host.ID] = node
		result.Hosts++
		result.Conflicts += conflicts
	}
	if err := s.syncLinuxHostGroups(ctx, hostNodes, result); err != nil {
		return nil, err
	}
	if err := s.syncLinuxRuntimeFacts(ctx, hostNodes, input.RuntimeFacts, result); err != nil {
		return nil, err
	}
	return result, nil
}

func HashLinuxMachineID(machineID string) (string, error) {
	machineID = strings.ToLower(strings.TrimSpace(machineID))
	if machineID == "" || len(machineID) > 512 || strings.ContainsAny(machineID, "\r\n\t ") {
		return "", ErrInvalidInput
	}
	sum := sha256.Sum256([]byte(machineID))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (s *Service) syncLinuxHostNode(ctx context.Context, host *model.LinuxHost, observation LinuxHostObservation) (*model.TopologyNode, int, error) {
	environment := linuxEnvironment(host.Environment)
	machineHash := cleanMachineHash(host.MachineIdentityHash)
	if strings.TrimSpace(observation.MachineID) != "" {
		var err error
		machineHash, err = HashLinuxMachineID(observation.MachineID)
		if err != nil {
			return nil, 0, err
		}
		if err := s.linuxTopologyReader.UpdateLinuxHostMachineIdentityHash(ctx, host.ID, machineHash); err != nil {
			return nil, 0, err
		}
		host.MachineIdentityHash = &machineHash
	}
	identityType, identity := "host_port", linuxEndpoint(host.Host, host.Port)
	if machineHash != "" {
		identityType, identity = "machine_id_hash", machineHash
	}
	nodeKey := linuxHostNodeKey(environment, identityType, identity)
	cmdbNode, conflict, err := s.resolveCMDBHost(ctx, host, machineHash)
	if err != nil {
		return nil, 0, err
	}
	if cmdbNode != nil && !conflict {
		nodeKey = cmdbNode.NodeKey
	}
	properties := linuxHostProperties(host, observation.StaticAttributes, identityType, identity, machineHash, observation.ObservedAt)
	sourceRef, _ := json.Marshal(map[string]any{"hostId": host.ID, "identityType": identityType})
	node, err := s.UpsertNode(ctx, NodeInput{
		NodeKey: nodeKey, Kind: model.TopologyNodeKindHost, Name: host.Name,
		Environment: environment, Properties: properties,
		SourceType: model.TopologySourceTypeLinuxServer, SourceRef: sourceRef,
	})
	if err != nil {
		return nil, 0, err
	}
	if cmdbNode != nil && !conflict {
		confidence := 0.95
		_ = s.repository.CreateTopologyNodeAlias(ctx, &model.TopologyNodeAlias{
			NodeID: node.ID, Alias: linuxEndpoint(host.Host, host.Port),
			AliasType: stringPointer("linux_endpoint"), Environment: stringPointer(environment),
			SourceType: stringPointer(model.TopologySourceTypeLinuxServer), Confidence: &confidence,
		})
	}
	if conflict {
		return node, 1, nil
	}
	return node, 0, nil
}

func (s *Service) resolveCMDBHost(ctx context.Context, host *model.LinuxHost, machineHash string) (*model.TopologyNode, bool, error) {
	environment := linuxEnvironment(host.Environment)
	candidatesByKey := map[string]model.TopologyNode{}
	for _, query := range []string{host.Name, host.Host} {
		if strings.TrimSpace(query) == "" {
			continue
		}
		candidates, err := s.repository.FindTopologyNodes(ctx, repository.TopologyNodeLookupFilters{
			Query: query, Environment: environment, Kinds: []string{model.TopologyNodeKindHost}, Limit: 20,
		})
		if err != nil {
			return nil, false, err
		}
		for _, candidate := range candidates {
			if candidate.SourceType == model.TopologySourceTypeCMDB {
				candidatesByKey[candidate.NodeKey] = candidate
			}
		}
	}
	candidates := make([]model.TopologyNode, 0, len(candidatesByKey))
	for _, candidate := range candidatesByKey {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].NodeKey < candidates[j].NodeKey })
	if len(candidates) == 0 {
		return nil, false, nil
	}
	if machineHash != "" {
		for index := range candidates {
			if propertyString(candidates[index].Properties, "machineIdentityHash", "machine_identity_hash") == machineHash {
				return &candidates[index], false, nil
			}
		}
		for index := range candidates {
			candidateHash := propertyString(candidates[index].Properties, "machineIdentityHash", "machine_identity_hash")
			if candidateHash != "" && candidateHash != machineHash {
				if err := s.recordLinuxCMDBConflict(ctx, host, candidates, "machine identity hash differs from CMDB host"); err != nil {
					return nil, false, err
				}
				return &candidates[index], true, nil
			}
		}
	}
	matching := []model.TopologyNode{}
	for _, candidate := range candidates {
		managementIP := propertyString(candidate.Properties, "managementIp", "management_ip", "host", "ip")
		hostname := propertyString(candidate.Properties, "hostname")
		if strings.EqualFold(managementIP, host.Host) || strings.EqualFold(hostname, host.Name) || strings.EqualFold(candidate.Name, host.Name) {
			matching = append(matching, candidate)
		}
	}
	if len(matching) == 1 {
		return &matching[0], false, nil
	}
	if len(matching) > 1 || len(candidates) > 1 {
		if err := s.recordLinuxCMDBConflict(ctx, host, candidates, "multiple CMDB hosts match Linux host identity"); err != nil {
			return nil, false, err
		}
		return &candidates[0], true, nil
	}
	return nil, false, nil
}

func (s *Service) recordLinuxCMDBConflict(ctx context.Context, host *model.LinuxHost, candidates []model.TopologyNode, description string) error {
	items := make([]map[string]any, 0, len(candidates)+1)
	items = append(items, map[string]any{
		"source": model.TopologySourceTypeLinuxServer, "hostId": host.ID,
		"machineIdentityHash": cleanMachineHash(host.MachineIdentityHash), "host": host.Host,
	})
	for _, candidate := range candidates {
		items = append(items, map[string]any{
			"source": model.TopologySourceTypeCMDB, "nodeKey": candidate.NodeKey,
			"machineIdentityHash": propertyString(candidate.Properties, "machineIdentityHash", "machine_identity_hash"),
		})
	}
	raw, _ := json.Marshal(items)
	var nodeID *int64
	if len(candidates) > 0 {
		id := candidates[0].ID
		nodeID = &id
	}
	return s.repository.CreateTopologyConflict(ctx, &model.TopologyConflict{
		ConflictType: "cmdb_identity_conflict", Status: "open", NodeID: nodeID,
		Description: description + "; automatic merge was blocked", Candidates: raw,
	})
}

func (s *Service) syncLinuxHostGroups(ctx context.Context, hostNodes map[int64]*model.TopologyNode, result *LinuxSyncResult) error {
	groups, err := s.linuxTopologyReader.ListLinuxHostGroups(ctx)
	if err != nil {
		return err
	}
	for index := range groups {
		group := &groups[index]
		hostIDs, err := s.linuxTopologyReader.ListLinuxHostIDsByGroupIDs(ctx, []int64{group.ID})
		if err != nil {
			return err
		}
		members := make([]int64, 0, len(hostIDs))
		for _, hostID := range hostIDs {
			if hostNodes[hostID] != nil {
				members = append(members, hostID)
			}
		}
		if len(members) == 0 {
			continue
		}
		environment := linuxEnvironment(group.Environment)
		properties, _ := json.Marshal(map[string]any{"groupId": group.ID, "systemName": pointerValue(group.SystemName)})
		sourceRef, _ := json.Marshal(map[string]any{"groupId": group.ID})
		groupNode, err := s.UpsertNode(ctx, NodeInput{
			NodeKey: fmt.Sprintf("%s:host_group:linux:%d", environment, group.ID),
			Kind:    model.TopologyNodeKindHostGroup, Name: group.Name, Environment: environment,
			Properties: properties, SourceType: model.TopologySourceTypeLinuxServer, SourceRef: sourceRef,
		})
		if err != nil {
			return err
		}
		result.Groups++
		for _, hostID := range members {
			confidence := 1.0
			recordKey := fmt.Sprintf("group:%d:host:%d", group.ID, hostID)
			if _, err := s.UpsertEdge(ctx, EdgeInput{
				FromNodeKey: hostNodes[hostID].NodeKey, ToNodeKey: groupNode.NodeKey,
				EdgeType: model.TopologyEdgeTypeMemberOf, Confidence: &confidence,
				SourceType: model.TopologySourceTypeLinuxServer, SourceRecordKey: &recordKey,
				SourceRef: sourceRef,
			}); err != nil {
				return err
			}
			result.Edges++
		}
	}
	return nil
}

func (s *Service) syncLinuxRuntimeFacts(ctx context.Context, hostNodes map[int64]*model.TopologyNode, facts []LinuxRuntimeFact, result *LinuxSyncResult) error {
	for _, fact := range facts {
		hostNode := hostNodes[fact.HostID]
		kind := strings.ToLower(strings.TrimSpace(fact.Kind))
		identity := strings.TrimSpace(fact.Identity)
		name := strings.TrimSpace(fact.Name)
		if hostNode == nil || !validLinuxRuntimeKind(kind) || identity == "" {
			return ErrInvalidInput
		}
		if name == "" {
			name = identity
		}
		environment := linuxEnvironment(hostNode.Environment)
		runtimeNode, err := s.resolveLinuxRuntimeNode(ctx, environment, kind, identity, name, fact.Properties)
		if err != nil {
			return err
		}
		confidence := fact.Confidence
		if confidence <= 0 {
			confidence = .9
		}
		if confidence > 1 {
			return ErrInvalidInput
		}
		observedAt := time.Now().UTC()
		if fact.ObservedAt != nil {
			observedAt = fact.ObservedAt.UTC()
		}
		ttl := defaultLinuxRuntimeTTL
		if fact.TTLSeconds > 0 && fact.TTLSeconds <= 24*60*60 {
			ttl = time.Duration(fact.TTLSeconds) * time.Second
		}
		expiresAt := observedAt.Add(ttl)
		recordKey := fmt.Sprintf("host:%d:%s:%s", fact.HostID, kind, identity)
		sourceRef, _ := json.Marshal(map[string]any{"hostId": fact.HostID, "kind": kind, "identity": identity})
		if _, err := s.UpsertEdge(ctx, EdgeInput{
			FromNodeKey: runtimeNode.NodeKey, ToNodeKey: hostNode.NodeKey,
			EdgeType: model.TopologyEdgeTypeRunsOn, Confidence: &confidence,
			SourceType: model.TopologySourceTypeLinuxServer, SourceRecordKey: &recordKey,
			ObservedAt: &observedAt, ExpiresAt: &expiresAt, SourceRef: sourceRef,
		}); err != nil {
			return err
		}
		result.Runtime++
		result.Edges++
	}
	return nil
}

func (s *Service) resolveLinuxRuntimeNode(ctx context.Context, environment, kind, identity, name string, properties map[string]any) (*model.TopologyNode, error) {
	candidates, err := s.repository.FindTopologyNodes(ctx, repository.TopologyNodeLookupFilters{
		Query: name, Environment: environment, Kinds: []string{kind}, Limit: 20,
	})
	if err != nil {
		return nil, err
	}
	if len(candidates) == 1 {
		return &candidates[0], nil
	}
	safeProperties, _ := json.Marshal(filterLinuxRuntimeProperties(properties))
	sourceRef, _ := json.Marshal(map[string]any{"identity": identity})
	return s.UpsertNode(ctx, NodeInput{
		NodeKey: fmt.Sprintf("%s:%s:linux:%s", environment, kind, normalizedTopologyIdentity(identity)),
		Kind:    kind, Name: name, Environment: environment, Properties: safeProperties,
		SourceType: model.TopologySourceTypeLinuxServer, SourceRef: sourceRef,
	})
}

func normalizeLinuxObservations(values []LinuxHostObservation) (map[int64]LinuxHostObservation, error) {
	result := map[int64]LinuxHostObservation{}
	for _, value := range values {
		if value.HostID <= 0 {
			return nil, ErrInvalidInput
		}
		if _, exists := result[value.HostID]; exists {
			return nil, ErrInvalidInput
		}
		result[value.HostID] = value
	}
	return result, nil
}

func linuxHostProperties(host *model.LinuxHost, attributes map[string]any, identityType, identity, machineHash string, observedAt *time.Time) json.RawMessage {
	properties := map[string]any{
		"hostId": host.ID, "managementIp": host.Host, "port": host.Port,
		"identityType": identityType, "identity": identity,
	}
	if machineHash != "" {
		properties["machineIdentityHash"] = machineHash
	}
	for inputKey, value := range attributes {
		if key := staticLinuxTopologyKey(inputKey); key != "" {
			properties[key] = value
		}
	}
	if _, ok := properties["hostname"]; !ok {
		properties["hostname"] = host.Name
	}
	if observedAt != nil {
		properties["lastCollectedAt"] = observedAt.UTC()
	}
	raw, _ := json.Marshal(properties)
	return raw
}

func staticLinuxTopologyKey(key string) string {
	normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	return map[string]string{
		"hostname": "hostname", "osname": "osName", "osversion": "osVersion",
		"kernel": "kernel", "architecture": "architecture", "cpucount": "cpuCount",
		"memorytotal": "memoryTotal", "primaryip": "primaryIp", "boottime": "bootTime",
		"lastcollectedat": "lastCollectedAt", "healthlevel": "healthLevel",
	}[normalized]
}

func filterLinuxRuntimeProperties(input map[string]any) map[string]any {
	allowed := map[string]bool{
		"serviceName": true, "unit": true, "version": true, "commandName": true,
		"executable": true, "listenAddress": true, "port": true,
	}
	result := map[string]any{}
	for key, value := range input {
		if allowed[key] {
			result[key] = value
		}
	}
	return result
}

func validLinuxRuntimeKind(kind string) bool {
	switch kind {
	case "service", model.TopologyNodeKindProcess, "nginx", "redis_instance", "tidb", "tikv", "pd", "nacos":
		return true
	default:
		return false
	}
}

func linuxHostNodeKey(environment, identityType, identity string) string {
	return strings.ToLower(fmt.Sprintf("%s:host:%s:%s", environment, identityType, normalizedTopologyIdentity(identity)))
}

func normalizedTopologyIdentity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer(" ", "-", "/", "_", "\\", "_", "#", "_").Replace(value)
}

func linuxEndpoint(host string, port int) string {
	host = strings.TrimSpace(host)
	if port <= 0 {
		port = 22
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func linuxEnvironment(value *string) string {
	if value != nil && strings.TrimSpace(*value) != "" {
		return strings.ToLower(strings.TrimSpace(*value))
	}
	return "default"
}

func cleanMachineHash(value *string) string {
	if value == nil {
		return ""
	}
	trimmed := strings.ToLower(strings.TrimSpace(*value))
	if strings.HasPrefix(trimmed, "sha256:") && len(strings.TrimPrefix(trimmed, "sha256:")) == 64 {
		return trimmed
	}
	if len(trimmed) == 64 {
		return "sha256:" + trimmed
	}
	return ""
}

func propertyString(raw []byte, keys ...string) string {
	var properties map[string]any
	if json.Unmarshal(raw, &properties) != nil {
		return ""
	}
	for _, key := range keys {
		if value, ok := properties[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func int64Set(values []int64) map[int64]bool {
	result := map[int64]bool{}
	for _, value := range values {
		if value > 0 {
			result[value] = true
		}
	}
	return result
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func stringPointer(value string) *string { return &value }
