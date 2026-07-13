package topology

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	k8ssvc "aiops-platform/backend/internal/k8s"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var (
	ErrInvalidInput         = errors.New("invalid input")
	ErrForbidden            = errors.New("topology access forbidden")
	ErrNodeLimitExceeded    = errors.New("topology node limit exceeded")
	ErrTopologyNodeAbsent   = errors.New("topology node not found")
	ErrTopologyTypeDisabled = errors.New("topology type disabled")
	ErrTopologyTypeBuiltIn  = errors.New("built-in topology type is protected")
	ErrUnsupportedSource    = errors.New("unsupported topology source")
	ErrSensitiveConfig      = errors.New("topology config contains sensitive fields")
)

type Repository interface {
	UpsertNode(ctx context.Context, node *model.TopologyNode) error
	UpsertEdge(ctx context.Context, edge *model.TopologyEdge) error
	FindNodeByKey(ctx context.Context, nodeKey string) (*model.TopologyNode, error)
	FindNodeByID(ctx context.Context, id int64) (*model.TopologyNode, error)
	FindTopologyNodes(ctx context.Context, filters repository.TopologyNodeLookupFilters) ([]model.TopologyNode, error)
	CreateTopologyNodeAlias(ctx context.Context, alias *model.TopologyNodeAlias) error
	DeleteTopologyNodeAlias(ctx context.Context, id int64) error
	ListTopologyNodeAliases(ctx context.Context, nodeID int64) ([]model.TopologyNodeAlias, error)
	CreateTopologyConflict(ctx context.Context, conflict *model.TopologyConflict) error
	ListNodes(ctx context.Context, filters repository.TopologyFilters) ([]model.TopologyNode, error)
	ListEdges(ctx context.Context, filters repository.TopologyFilters) ([]model.TopologyEdge, error)
	ListTopologyNodeTypes(ctx context.Context) ([]model.TopologyNodeType, error)
	FindTopologyNodeTypeByKey(ctx context.Context, typeKey string) (*model.TopologyNodeType, error)
	FindTopologyNodeTypeByID(ctx context.Context, id int64) (*model.TopologyNodeType, error)
	CreateTopologyNodeType(ctx context.Context, nodeType *model.TopologyNodeType) error
	UpdateTopologyNodeType(ctx context.Context, nodeType *model.TopologyNodeType) error
	ListTopologyRelationTypes(ctx context.Context) ([]model.TopologyRelationType, error)
	FindTopologyRelationTypeByKey(ctx context.Context, typeKey string) (*model.TopologyRelationType, error)
	FindTopologyRelationTypeByID(ctx context.Context, id int64) (*model.TopologyRelationType, error)
	CreateTopologyRelationType(ctx context.Context, relationType *model.TopologyRelationType) error
	UpdateTopologyRelationType(ctx context.Context, relationType *model.TopologyRelationType) error
	CreateTopologyTypeAudit(ctx context.Context, audit *model.TopologyTypeAudit) error
	ListTopologySourceConfigs(ctx context.Context) ([]model.TopologySourceConfig, error)
	FindTopologySourceConfigByID(ctx context.Context, id int64) (*model.TopologySourceConfig, error)
	CreateTopologySourceConfig(ctx context.Context, source *model.TopologySourceConfig) error
	UpdateTopologySourceConfig(ctx context.Context, id int64, updates repository.TopologySourceConfigUpdates) (*model.TopologySourceConfig, error)
	DeleteTopologySourceConfig(ctx context.Context, id int64) error
	FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error)
}

type K8sReader interface {
	Resources(ctx context.Context, actor *model.AppUser, input k8ssvc.ResourceInput) (*k8ssvc.ResourceResult, error)
}

type Service struct {
	repository Repository
	k8sReader  K8sReader
}

type NodeInput struct {
	NodeKey            string          `json:"nodeKey"`
	Kind               string          `json:"kind"`
	Name               string          `json:"name"`
	DisplayName        *string         `json:"displayName"`
	Environment        string          `json:"environment"`
	Cluster            string          `json:"cluster"`
	Namespace          string          `json:"namespace"`
	Labels             json.RawMessage `json:"labels"`
	Properties         json.RawMessage `json:"properties"`
	SourceType         string          `json:"sourceType"`
	SourcePriority     int             `json:"sourcePriority"`
	LockedFields       json.RawMessage `json:"lockedFields"`
	ResolvedAttributes json.RawMessage `json:"resolvedAttributes"`
	SourceRef          json.RawMessage `json:"sourceRef"`
}

type EdgeInput struct {
	EdgeKey     string          `json:"edgeKey"`
	FromNodeKey string          `json:"fromNodeKey"`
	ToNodeKey   string          `json:"toNodeKey"`
	EdgeType    string          `json:"edgeType"`
	Confidence  *float64        `json:"confidence"`
	Properties  json.RawMessage `json:"properties"`
	SourceType  string          `json:"sourceType"`
	SourceRef   json.RawMessage `json:"sourceRef"`
}

type Query struct {
	Environment string
	Cluster     string
	Namespace   string
	Kind        string
	Limit       int
}

type Graph struct {
	Nodes []model.TopologyNode `json:"nodes"`
	Edges []model.TopologyEdge `json:"edges"`
}

type TraversalQuery struct {
	NodeKey     string
	Hops        int
	MaxNodes    int
	Environment string
	Cluster     string
	Namespace   string
}

type TraversalResult struct {
	RootKey       string               `json:"rootKey"`
	Direction     string               `json:"direction"`
	Hops          int                  `json:"hops"`
	Nodes         []model.TopologyNode `json:"nodes"`
	Edges         []model.TopologyEdge `json:"edges"`
	CycleDetected bool                 `json:"cycleDetected"`
}

type CommonDependencyQuery struct {
	NodeKeys    []string
	Hops        int
	MaxNodes    int
	Environment string
	Cluster     string
	Namespace   string
}

type CommonDependencyResult struct {
	NodeKeys        []string             `json:"nodeKeys"`
	Hops            int                  `json:"hops"`
	CommonNodes     []model.TopologyNode `json:"commonNodes"`
	SupportingEdges []model.TopologyEdge `json:"supportingEdges"`
	CycleDetected   bool                 `json:"cycleDetected"`
}

type SyncK8sInput struct {
	DataSourceID int64  `json:"dataSourceId"`
	Environment  string `json:"environment"`
	Cluster      string `json:"cluster"`
	Namespace    string `json:"namespace"`
	Limit        int    `json:"limit"`
}

type SyncResult struct {
	Nodes int `json:"nodes"`
	Edges int `json:"edges"`
}

type NodeTypeInput struct {
	TypeKey              string          `json:"typeKey"`
	DisplayName          string          `json:"displayName"`
	Category             *string         `json:"category"`
	Icon                 *string         `json:"icon"`
	DefaultColor         *string         `json:"defaultColor"`
	IdentityFields       json.RawMessage `json:"identityFields"`
	SearchableFields     json.RawMessage `json:"searchableFields"`
	DefaultLabelTemplate *string         `json:"defaultLabelTemplate"`
	DetailFields         json.RawMessage `json:"detailFields"`
	Enabled              *bool           `json:"enabled"`
}

type RelationTypeInput struct {
	TypeKey            string          `json:"typeKey"`
	DisplayName        string          `json:"displayName"`
	Semantics          string          `json:"semantics"`
	FailurePropagation string          `json:"failurePropagation"`
	DefaultDirection   string          `json:"defaultDirection"`
	PropagatesFailure  *bool           `json:"propagatesFailure"`
	AllowedSourceTypes json.RawMessage `json:"allowedSourceTypes"`
	AllowedTargetTypes json.RawMessage `json:"allowedTargetTypes"`
	Style              json.RawMessage `json:"style"`
	Enabled            *bool           `json:"enabled"`
}

type SourceConfigInput struct {
	Name               string          `json:"name"`
	SourceType         string          `json:"sourceType"`
	DataSourceID       *int64          `json:"dataSourceId"`
	Enabled            *bool           `json:"enabled"`
	Priority           *int            `json:"priority"`
	Schedule           *string         `json:"schedule"`
	Scope              json.RawMessage `json:"scope"`
	MappingRules       json.RawMessage `json:"mappingRules"`
	StaleAfterSeconds  *int            `json:"staleAfterSeconds"`
	DeleteAfterSeconds *int            `json:"deleteAfterSeconds"`
	CreatedBy          *int64          `json:"-"`
}

type SourceConfigTestResult struct {
	OK           bool   `json:"ok"`
	SourceType   string `json:"sourceType"`
	DataSourceID *int64 `json:"dataSourceId,omitempty"`
	Message      string `json:"message"`
}

type MappingPreviewInput struct {
	MappingRules json.RawMessage `json:"mappingRules"`
	SampleData   json.RawMessage `json:"sampleData"`
	Limit        int             `json:"limit"`
}

type MappingPreviewResult struct {
	Nodes      []PreviewNode `json:"nodes"`
	Edges      []PreviewEdge `json:"edges"`
	Unresolved []string      `json:"unresolved"`
	Warnings   []string      `json:"warnings"`
	Truncated  bool          `json:"truncated"`
}

type PreviewNode struct {
	NodeKey    string         `json:"nodeKey"`
	NodeType   string         `json:"nodeType"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Aliases    []string       `json:"aliases,omitempty"`
}

type PreviewEdge struct {
	FromNodeKey  string   `json:"fromNodeKey"`
	ToNodeKey    string   `json:"toNodeKey"`
	RelationType string   `json:"relationType"`
	Confidence   *float64 `json:"confidence,omitempty"`
}

type mappingRulesSpec struct {
	NodeMappings []nodeMappingSpec `json:"nodeMappings"`
	EdgeMappings []edgeMappingSpec `json:"edgeMappings"`
}

type nodeMappingSpec struct {
	Name                string            `json:"name"`
	EntityPath          string            `json:"entityPath"`
	TargetNodeType      string            `json:"targetNodeType"`
	ExternalKeyTemplate string            `json:"externalKeyTemplate"`
	NameTemplate        string            `json:"nameTemplate"`
	Attributes          map[string]string `json:"attributes"`
	Aliases             []string          `json:"aliases"`
}

type edgeMappingSpec struct {
	Name         string            `json:"name"`
	EntityPath   string            `json:"entityPath"`
	SourceLookup lookupMappingSpec `json:"sourceLookup"`
	TargetLookup lookupMappingSpec `json:"targetLookup"`
	RelationType string            `json:"relationType"`
	Confidence   *float64          `json:"confidence"`
}

type lookupMappingSpec struct {
	NodeType            string `json:"nodeType"`
	ExternalKeyTemplate string `json:"externalKeyTemplate"`
}

type AliasInput struct {
	Alias       string   `json:"alias"`
	AliasType   *string  `json:"aliasType"`
	Environment string   `json:"environment"`
	SourceType  string   `json:"sourceType"`
	Confidence  *float64 `json:"confidence"`
}

type FindNodeInput struct {
	Query       string   `json:"query"`
	Environment string   `json:"environment"`
	NodeTypes   []string `json:"nodeTypes"`
	Limit       int      `json:"limit"`
}

type FindNodeResult struct {
	Matched    bool                 `json:"matched"`
	Ambiguous  bool                 `json:"ambiguous"`
	Node       *model.TopologyNode  `json:"node,omitempty"`
	Candidates []model.TopologyNode `json:"candidates"`
}

func NewService(repository Repository, k8sReader K8sReader) *Service {
	return &Service{repository: repository, k8sReader: k8sReader}
}

func (s *Service) ListNodeTypes(ctx context.Context) ([]model.TopologyNodeType, error) {
	return s.repository.ListTopologyNodeTypes(ctx)
}

func (s *Service) CreateNodeType(ctx context.Context, input NodeTypeInput) (*model.TopologyNodeType, error) {
	nodeType, err := normalizeNodeType(input)
	if err != nil {
		return nil, err
	}
	if err := s.repository.CreateTopologyNodeType(ctx, nodeType); err != nil {
		return nil, err
	}
	return nodeType, nil
}

func (s *Service) UpdateNodeType(ctx context.Context, id int64, input NodeTypeInput) (*model.TopologyNodeType, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	existing, err := s.repository.FindTopologyNodeTypeByID(ctx, id)
	if err != nil {
		return nil, err
	}
	updated, err := normalizeNodeType(input)
	if err != nil {
		return nil, err
	}
	if existing.BuiltIn && updated.TypeKey != existing.TypeKey {
		return nil, ErrTopologyTypeBuiltIn
	}
	updated.ID = existing.ID
	updated.BuiltIn = existing.BuiltIn
	if err := s.repository.UpdateTopologyNodeType(ctx, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Service) SetNodeTypeEnabled(ctx context.Context, id int64, enabled bool) (*model.TopologyNodeType, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	nodeType, err := s.repository.FindTopologyNodeTypeByID(ctx, id)
	if err != nil {
		return nil, err
	}
	nodeType.Enabled = enabled
	if err := s.repository.UpdateTopologyNodeType(ctx, nodeType); err != nil {
		return nil, err
	}
	return nodeType, nil
}

func (s *Service) ListRelationTypes(ctx context.Context) ([]model.TopologyRelationType, error) {
	return s.repository.ListTopologyRelationTypes(ctx)
}

func (s *Service) CreateRelationType(ctx context.Context, input RelationTypeInput) (*model.TopologyRelationType, error) {
	relationType, err := normalizeRelationType(input)
	if err != nil {
		return nil, err
	}
	if err := s.validateAllowedNodeTypes(ctx, relationType); err != nil {
		return nil, err
	}
	if err := s.repository.CreateTopologyRelationType(ctx, relationType); err != nil {
		return nil, err
	}
	return relationType, nil
}

func (s *Service) UpdateRelationType(ctx context.Context, id int64, actorID *int64, input RelationTypeInput) (*model.TopologyRelationType, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	existing, err := s.repository.FindTopologyRelationTypeByID(ctx, id)
	if err != nil {
		return nil, err
	}
	updated, err := normalizeRelationType(input)
	if err != nil {
		return nil, err
	}
	if existing.BuiltIn && updated.TypeKey != existing.TypeKey {
		return nil, ErrTopologyTypeBuiltIn
	}
	if err := s.validateAllowedNodeTypes(ctx, updated); err != nil {
		return nil, err
	}
	updated.ID = existing.ID
	updated.BuiltIn = existing.BuiltIn
	if err := s.repository.UpdateTopologyRelationType(ctx, updated); err != nil {
		return nil, err
	}
	if existing.Semantics != updated.Semantics || existing.FailurePropagation != updated.FailurePropagation {
		before, _ := json.Marshal(map[string]string{
			"semantics":          existing.Semantics,
			"failurePropagation": existing.FailurePropagation,
		})
		after, _ := json.Marshal(map[string]string{
			"semantics":          updated.Semantics,
			"failurePropagation": updated.FailurePropagation,
		})
		_ = s.repository.CreateTopologyTypeAudit(ctx, &model.TopologyTypeAudit{
			TypeKind: "relation",
			TypeID:   updated.ID,
			Action:   "update_semantics_or_propagation",
			Before:   before,
			After:    after,
			ActorID:  actorID,
		})
	}
	return updated, nil
}

func (s *Service) SetRelationTypeEnabled(ctx context.Context, id int64, enabled bool) (*model.TopologyRelationType, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	relationType, err := s.repository.FindTopologyRelationTypeByID(ctx, id)
	if err != nil {
		return nil, err
	}
	relationType.Enabled = enabled
	if err := s.repository.UpdateTopologyRelationType(ctx, relationType); err != nil {
		return nil, err
	}
	return relationType, nil
}

func (s *Service) ListSourceConfigs(ctx context.Context) ([]model.TopologySourceConfig, error) {
	return s.repository.ListTopologySourceConfigs(ctx)
}

func (s *Service) GetSourceConfig(ctx context.Context, id int64) (*model.TopologySourceConfig, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	return s.repository.FindTopologySourceConfigByID(ctx, id)
}

func (s *Service) CreateSourceConfig(ctx context.Context, input SourceConfigInput) (*model.TopologySourceConfig, error) {
	source, err := s.normalizeSourceConfig(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := s.repository.CreateTopologySourceConfig(ctx, source); err != nil {
		return nil, err
	}
	return source, nil
}

func (s *Service) UpdateSourceConfig(ctx context.Context, id int64, input SourceConfigInput) (*model.TopologySourceConfig, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	source, err := s.normalizeSourceConfig(ctx, input)
	if err != nil {
		return nil, err
	}
	return s.repository.UpdateTopologySourceConfig(ctx, id, repository.TopologySourceConfigUpdates{
		Name:               &source.Name,
		SourceType:         &source.SourceType,
		DataSourceID:       source.DataSourceID,
		DataSourceIDSet:    true,
		Enabled:            &source.Enabled,
		Priority:           &source.Priority,
		Schedule:           source.Schedule,
		ScheduleSet:        true,
		Scope:              source.Scope,
		ScopeSet:           true,
		MappingRules:       source.MappingRules,
		MappingRulesSet:    true,
		StaleAfterSeconds:  &source.StaleAfterSeconds,
		DeleteAfterSeconds: &source.DeleteAfterSeconds,
	})
}

func (s *Service) DeleteSourceConfig(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalidInput
	}
	return s.repository.DeleteTopologySourceConfig(ctx, id)
}

func (s *Service) TestSourceConfig(ctx context.Context, id int64) (*SourceConfigTestResult, error) {
	source, err := s.GetSourceConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.validateSourceDataSource(ctx, source.SourceType, source.DataSourceID); err != nil {
		return nil, err
	}
	return &SourceConfigTestResult{
		OK:           true,
		SourceType:   source.SourceType,
		DataSourceID: source.DataSourceID,
		Message:      "topology source config validated",
	}, nil
}

func (s *Service) PreviewSourceMapping(ctx context.Context, id int64, input MappingPreviewInput) (*MappingPreviewResult, error) {
	source, err := s.GetSourceConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	mappingRules := source.MappingRules
	if len(input.MappingRules) > 0 {
		mappingRules = input.MappingRules
	}
	return s.PreviewMapping(ctx, MappingPreviewInput{
		MappingRules: mappingRules,
		SampleData:   input.SampleData,
		Limit:        input.Limit,
	})
}

func (s *Service) PreviewMapping(_ context.Context, input MappingPreviewInput) (*MappingPreviewResult, error) {
	if len(input.MappingRules) == 0 || len(input.SampleData) == 0 {
		return nil, ErrInvalidInput
	}
	if len(input.MappingRules) > 262144 || len(input.SampleData) > 524288 {
		return nil, ErrInvalidInput
	}
	if containsSensitiveJSON(input.MappingRules) || containsSensitiveJSON(input.SampleData) {
		return nil, ErrSensitiveConfig
	}
	limit := input.Limit
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	var rules mappingRulesSpec
	if err := json.Unmarshal(input.MappingRules, &rules); err != nil {
		return nil, ErrInvalidInput
	}
	if len(rules.NodeMappings) == 0 && len(rules.EdgeMappings) == 0 {
		return nil, ErrInvalidInput
	}
	var sample any
	if err := json.Unmarshal(input.SampleData, &sample); err != nil {
		return nil, ErrInvalidInput
	}
	result := &MappingPreviewResult{
		Nodes:      []PreviewNode{},
		Edges:      []PreviewEdge{},
		Unresolved: []string{},
		Warnings:   []string{},
	}
	for _, mapping := range rules.NodeMappings {
		if result.Truncated {
			break
		}
		if !validNodeMapping(mapping) {
			result.Unresolved = append(result.Unresolved, "invalid node mapping: "+mapping.Name)
			continue
		}
		entities, err := selectJSONPath(sample, mapping.EntityPath)
		if err != nil {
			result.Unresolved = append(result.Unresolved, mapping.Name+": "+err.Error())
			continue
		}
		for _, entity := range entities {
			if len(result.Nodes) >= limit {
				result.Truncated = true
				break
			}
			contextMap, ok := entity.(map[string]any)
			if !ok {
				result.Unresolved = append(result.Unresolved, mapping.Name+": entity is not object")
				continue
			}
			nodeKey, err := renderTemplate(mapping.ExternalKeyTemplate, contextMap)
			if err != nil || nodeKey == "" {
				result.Unresolved = append(result.Unresolved, mapping.Name+": node key unresolved")
				continue
			}
			name, err := renderTemplate(mapping.NameTemplate, contextMap)
			if err != nil || name == "" {
				result.Unresolved = append(result.Unresolved, mapping.Name+": node name unresolved")
				continue
			}
			attributes := map[string]any{}
			for key, template := range mapping.Attributes {
				if sensitiveTopologyKey(key) {
					return nil, ErrSensitiveConfig
				}
				value, err := renderTemplate(template, contextMap)
				if err != nil {
					result.Unresolved = append(result.Unresolved, mapping.Name+": attribute "+key+" unresolved")
					continue
				}
				attributes[key] = value
			}
			aliases := []string{}
			for _, template := range mapping.Aliases {
				value, err := renderTemplate(template, contextMap)
				if err == nil && value != "" {
					aliases = append(aliases, value)
				}
			}
			result.Nodes = append(result.Nodes, PreviewNode{
				NodeKey:    nodeKey,
				NodeType:   mapping.TargetNodeType,
				Name:       name,
				Attributes: attributes,
				Aliases:    aliases,
			})
		}
	}
	for _, mapping := range rules.EdgeMappings {
		if result.Truncated {
			break
		}
		if !validEdgeMapping(mapping) {
			result.Unresolved = append(result.Unresolved, "invalid edge mapping: "+mapping.Name)
			continue
		}
		entities, err := selectJSONPath(sample, mapping.EntityPath)
		if err != nil {
			result.Unresolved = append(result.Unresolved, mapping.Name+": "+err.Error())
			continue
		}
		for _, entity := range entities {
			if len(result.Edges) >= limit {
				result.Truncated = true
				break
			}
			contextMap, ok := entity.(map[string]any)
			if !ok {
				result.Unresolved = append(result.Unresolved, mapping.Name+": entity is not object")
				continue
			}
			from, err := renderTemplate(mapping.SourceLookup.ExternalKeyTemplate, contextMap)
			if err != nil || from == "" {
				result.Unresolved = append(result.Unresolved, mapping.Name+": source unresolved")
				continue
			}
			to, err := renderTemplate(mapping.TargetLookup.ExternalKeyTemplate, contextMap)
			if err != nil || to == "" {
				result.Unresolved = append(result.Unresolved, mapping.Name+": target unresolved")
				continue
			}
			result.Edges = append(result.Edges, PreviewEdge{
				FromNodeKey:  from,
				ToNodeKey:    to,
				RelationType: mapping.RelationType,
				Confidence:   mapping.Confidence,
			})
		}
	}
	if len(result.Nodes) == 0 && len(result.Edges) == 0 {
		result.Warnings = append(result.Warnings, "mapping preview produced no nodes or edges")
	}
	return result, nil
}

func (s *Service) AddNodeAlias(ctx context.Context, nodeID int64, input AliasInput) (*model.TopologyNodeAlias, error) {
	if nodeID <= 0 {
		return nil, ErrInvalidInput
	}
	node, err := s.repository.FindNodeByID(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	aliasValue := strings.TrimSpace(input.Alias)
	if aliasValue == "" || len(aliasValue) > 255 {
		return nil, ErrInvalidInput
	}
	if input.Confidence != nil && (*input.Confidence < 0 || *input.Confidence > 1) {
		return nil, ErrInvalidInput
	}
	environment := cleanString(input.Environment)
	if environment == nil {
		environment = node.Environment
	}
	sourceType := cleanString(input.SourceType)
	if sourceType == nil {
		defaultSource := model.TopologySourceManual
		sourceType = &defaultSource
	}
	alias := &model.TopologyNodeAlias{
		NodeID:      nodeID,
		Alias:       aliasValue,
		AliasType:   cleanStringPtr(input.AliasType),
		Environment: environment,
		SourceType:  sourceType,
		Confidence:  input.Confidence,
	}
	if err := s.repository.CreateTopologyNodeAlias(ctx, alias); err != nil {
		return nil, err
	}
	return alias, nil
}

func (s *Service) ListNodeAliases(ctx context.Context, nodeID int64) ([]model.TopologyNodeAlias, error) {
	if nodeID <= 0 {
		return nil, ErrInvalidInput
	}
	return s.repository.ListTopologyNodeAliases(ctx, nodeID)
}

func (s *Service) DeleteNodeAlias(ctx context.Context, aliasID int64) error {
	if aliasID <= 0 {
		return ErrInvalidInput
	}
	return s.repository.DeleteTopologyNodeAlias(ctx, aliasID)
}

func (s *Service) FindNode(ctx context.Context, input FindNodeInput) (*FindNodeResult, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return nil, ErrInvalidInput
	}
	nodeTypes := make([]string, 0, len(input.NodeTypes))
	for _, nodeType := range input.NodeTypes {
		nodeType = strings.TrimSpace(nodeType)
		if nodeType == "" {
			continue
		}
		if !validTypeKey(nodeType) {
			return nil, ErrInvalidInput
		}
		nodeTypes = append(nodeTypes, nodeType)
	}
	nodes, err := s.repository.FindTopologyNodes(ctx, repository.TopologyNodeLookupFilters{
		Query:       query,
		Environment: strings.TrimSpace(input.Environment),
		Kinds:       nodeTypes,
		Limit:       input.Limit,
	})
	if err != nil {
		return nil, err
	}
	result := &FindNodeResult{Candidates: nodes}
	if len(nodes) == 1 {
		result.Matched = true
		result.Node = &nodes[0]
		return result, nil
	}
	if len(nodes) > 1 {
		result.Ambiguous = true
	}
	return result, nil
}

func (s *Service) UpsertNode(ctx context.Context, input NodeInput) (*model.TopologyNode, error) {
	node, err := normalizeNode(input)
	if err != nil {
		return nil, err
	}
	nodeType, err := s.repository.FindTopologyNodeTypeByKey(ctx, node.Kind)
	if err != nil {
		return nil, err
	}
	if !nodeType.Enabled {
		return nil, ErrTopologyTypeDisabled
	}
	node.NodeTypeID = &nodeType.ID
	if existing, err := s.repository.FindNodeByKey(ctx, node.NodeKey); err == nil {
		node = s.mergeNode(ctx, existing, node)
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if err := s.repository.UpsertNode(ctx, node); err != nil {
		return nil, err
	}
	return node, nil
}

func (s *Service) UpsertEdge(ctx context.Context, input EdgeInput) (*model.TopologyEdge, error) {
	edge, err := normalizeEdge(input)
	if err != nil {
		return nil, err
	}
	relationType, err := s.repository.FindTopologyRelationTypeByKey(ctx, edge.EdgeType)
	if err != nil {
		return nil, err
	}
	if !relationType.Enabled {
		return nil, ErrTopologyTypeDisabled
	}
	if err := s.validateEdgeTypeCompatibility(ctx, edge, relationType); err != nil {
		return nil, err
	}
	edge.RelationTypeID = &relationType.ID
	if err := s.repository.UpsertEdge(ctx, edge); err != nil {
		return nil, err
	}
	return edge, nil
}

func (s *Service) Graph(ctx context.Context, query Query) (*Graph, error) {
	filters := repository.TopologyFilters{
		Environment: strings.TrimSpace(query.Environment),
		Cluster:     strings.TrimSpace(query.Cluster),
		Namespace:   strings.TrimSpace(query.Namespace),
		Kind:        strings.TrimSpace(query.Kind),
		Limit:       query.Limit,
	}
	nodes, err := s.repository.ListNodes(ctx, filters)
	if err != nil {
		return nil, err
	}
	edges, err := s.repository.ListEdges(ctx, filters)
	if err != nil {
		return nil, err
	}
	return &Graph{Nodes: nodes, Edges: edges}, nil
}

func (s *Service) Upstream(ctx context.Context, query TraversalQuery) (*TraversalResult, error) {
	return s.traverse(ctx, query, "upstream")
}

func (s *Service) Downstream(ctx context.Context, query TraversalQuery) (*TraversalResult, error) {
	return s.traverse(ctx, query, "downstream")
}

func (s *Service) BlastRadius(ctx context.Context, query TraversalQuery) (*TraversalResult, error) {
	result, err := s.traverse(ctx, query, "downstream")
	if result != nil {
		result.Direction = "blast_radius"
	}
	return result, err
}

func (s *Service) CommonDependencies(ctx context.Context, query CommonDependencyQuery) (*CommonDependencyResult, error) {
	nodeKeys := normalizeNodeKeys(query.NodeKeys)
	if len(nodeKeys) < 2 {
		return nil, ErrInvalidInput
	}
	maxNodes := normalizeMaxNodes(query.MaxNodes)
	hops := normalizeHops(query.Hops)
	graph, err := s.Graph(ctx, Query{
		Environment: query.Environment,
		Cluster:     query.Cluster,
		Namespace:   query.Namespace,
		Limit:       maxNodes,
	})
	if err != nil {
		return nil, err
	}
	index := newGraphIndex(graph)
	for _, key := range nodeKeys {
		if _, ok := index.nodes[key]; !ok {
			return nil, ErrTopologyNodeAbsent
		}
	}

	var intersection map[string]struct{}
	supportingEdges := map[string]model.TopologyEdge{}
	cycleDetected := false
	for _, key := range nodeKeys {
		reachable, edges, cycle, err := traverseIndex(index, key, "downstream", hops, maxNodes)
		if err != nil {
			return nil, err
		}
		delete(reachable, key)
		if intersection == nil {
			intersection = reachable
		} else {
			for candidate := range intersection {
				if _, ok := reachable[candidate]; !ok {
					delete(intersection, candidate)
				}
			}
		}
		for _, edge := range edges {
			supportingEdges[edge.EdgeKey] = edge
		}
		cycleDetected = cycleDetected || cycle
	}
	nodes := make([]model.TopologyNode, 0, len(intersection))
	for key := range intersection {
		nodes = append(nodes, index.nodes[key])
	}
	sortTopologyNodes(nodes)
	edges := make([]model.TopologyEdge, 0, len(supportingEdges))
	for _, edge := range supportingEdges {
		if _, fromOK := intersection[edge.FromNodeKey]; fromOK {
			edges = append(edges, edge)
			continue
		}
		if _, toOK := intersection[edge.ToNodeKey]; toOK {
			edges = append(edges, edge)
		}
	}
	sortTopologyEdges(edges)
	return &CommonDependencyResult{NodeKeys: nodeKeys, Hops: hops, CommonNodes: nodes, SupportingEdges: edges, CycleDetected: cycleDetected}, nil
}

func (s *Service) traverse(ctx context.Context, query TraversalQuery, direction string) (*TraversalResult, error) {
	root := strings.TrimSpace(query.NodeKey)
	if root == "" {
		return nil, ErrInvalidInput
	}
	maxNodes := normalizeMaxNodes(query.MaxNodes)
	hops := normalizeHops(query.Hops)
	graph, err := s.Graph(ctx, Query{
		Environment: query.Environment,
		Cluster:     query.Cluster,
		Namespace:   query.Namespace,
		Limit:       maxNodes,
	})
	if err != nil {
		return nil, err
	}
	index := newGraphIndex(graph)
	if _, ok := index.nodes[root]; !ok {
		return nil, ErrTopologyNodeAbsent
	}
	reachable, edges, cycleDetected, err := traverseIndex(index, root, direction, hops, maxNodes)
	if err != nil {
		return nil, err
	}
	nodes := make([]model.TopologyNode, 0, len(reachable))
	for key := range reachable {
		nodes = append(nodes, index.nodes[key])
	}
	sortTopologyNodes(nodes)
	sortTopologyEdges(edges)
	return &TraversalResult{RootKey: root, Direction: direction, Hops: hops, Nodes: nodes, Edges: edges, CycleDetected: cycleDetected}, nil
}

func (s *Service) SyncK8s(ctx context.Context, actor *model.AppUser, input SyncK8sInput) (*SyncResult, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	if s.k8sReader == nil {
		return nil, ErrInvalidInput
	}
	namespace := strings.TrimSpace(input.Namespace)
	cluster := strings.TrimSpace(input.Cluster)
	if input.DataSourceID <= 0 || namespace == "" {
		return nil, ErrInvalidInput
	}
	limit := input.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	deployments, err := s.readK8sResource(ctx, actor, input.DataSourceID, namespace, "deployments", limit)
	if err != nil {
		return nil, err
	}
	pods, err := s.readK8sResource(ctx, actor, input.DataSourceID, namespace, "pods", limit)
	if err != nil {
		return nil, err
	}
	services, err := s.readK8sResource(ctx, actor, input.DataSourceID, namespace, "services", limit)
	if err != nil {
		return nil, err
	}
	ingresses, err := s.readK8sResource(ctx, actor, input.DataSourceID, namespace, "ingresses", limit)
	if err != nil {
		return nil, err
	}

	builder := newK8sGraphBuilder(input.Environment, cluster, namespace)
	for _, item := range deployments.Items {
		var deployment appsv1.Deployment
		if decodeK8s(item.Raw, &deployment) == nil {
			builder.addDeployment(&deployment)
		}
	}
	for _, item := range pods.Items {
		var pod corev1.Pod
		if decodeK8s(item.Raw, &pod) == nil {
			builder.addPod(&pod)
		}
	}
	for _, item := range services.Items {
		var service corev1.Service
		if decodeK8s(item.Raw, &service) == nil {
			builder.addService(&service)
		}
	}
	for _, item := range ingresses.Items {
		var ingress networkingv1.Ingress
		if decodeK8s(item.Raw, &ingress) == nil {
			builder.addIngress(&ingress)
		}
	}
	builder.linkK8sResources()

	for index := range builder.nodes {
		if err := s.repository.UpsertNode(ctx, &builder.nodes[index]); err != nil {
			return nil, err
		}
	}
	for index := range builder.edges {
		if err := s.repository.UpsertEdge(ctx, &builder.edges[index]); err != nil {
			return nil, err
		}
	}
	return &SyncResult{Nodes: len(builder.nodes), Edges: len(builder.edges)}, nil
}

func (s *Service) readK8sResource(ctx context.Context, actor *model.AppUser, dataSourceID int64, namespace, resource string, limit int) (*k8ssvc.ResourceResult, error) {
	return s.k8sReader.Resources(ctx, actor, k8ssvc.ResourceInput{
		DataSourceID: dataSourceID,
		Namespace:    namespace,
		Resource:     resource,
		Limit:        limit,
	})
}

type graphIndex struct {
	nodes    map[string]model.TopologyNode
	outgoing map[string][]model.TopologyEdge
	incoming map[string][]model.TopologyEdge
}

func newGraphIndex(graph *Graph) graphIndex {
	index := graphIndex{
		nodes:    map[string]model.TopologyNode{},
		outgoing: map[string][]model.TopologyEdge{},
		incoming: map[string][]model.TopologyEdge{},
	}
	if graph == nil {
		return index
	}
	for _, node := range graph.Nodes {
		index.nodes[node.NodeKey] = node
	}
	for _, edge := range graph.Edges {
		if _, ok := index.nodes[edge.FromNodeKey]; !ok {
			continue
		}
		if _, ok := index.nodes[edge.ToNodeKey]; !ok {
			continue
		}
		index.outgoing[edge.FromNodeKey] = append(index.outgoing[edge.FromNodeKey], edge)
		index.incoming[edge.ToNodeKey] = append(index.incoming[edge.ToNodeKey], edge)
	}
	return index
}

func traverseIndex(index graphIndex, root, direction string, hops, maxNodes int) (map[string]struct{}, []model.TopologyEdge, bool, error) {
	if direction != "upstream" && direction != "downstream" {
		return nil, nil, false, ErrInvalidInput
	}
	type queueItem struct {
		key   string
		depth int
	}
	visited := map[string]struct{}{root: {}}
	edgeSeen := map[string]model.TopologyEdge{}
	queue := []queueItem{{key: root, depth: 0}}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if item.depth >= hops {
			continue
		}
		for _, edge := range directionalEdges(index, item.key, direction) {
			next := edge.ToNodeKey
			if direction == "upstream" {
				next = edge.FromNodeKey
			}
			if _, ok := index.nodes[next]; !ok {
				continue
			}
			edgeSeen[edge.EdgeKey] = edge
			if _, seen := visited[next]; seen {
				continue
			}
			if len(visited)+1 > maxNodes {
				return nil, nil, false, ErrNodeLimitExceeded
			}
			visited[next] = struct{}{}
			queue = append(queue, queueItem{key: next, depth: item.depth + 1})
		}
	}

	edges := make([]model.TopologyEdge, 0, len(edgeSeen))
	for _, edge := range edgeSeen {
		edges = append(edges, edge)
	}
	cycleDetected := hasDirectedCycle(index, visited, direction)
	return visited, edges, cycleDetected, nil
}

func directionalEdges(index graphIndex, key, direction string) []model.TopologyEdge {
	if direction == "upstream" {
		return index.incoming[key]
	}
	return index.outgoing[key]
}

func hasDirectedCycle(index graphIndex, nodeKeys map[string]struct{}, direction string) bool {
	color := map[string]int{}
	var visit func(string) bool
	visit = func(key string) bool {
		color[key] = 1
		for _, edge := range directionalEdges(index, key, direction) {
			next := edge.ToNodeKey
			if direction == "upstream" {
				next = edge.FromNodeKey
			}
			if _, ok := nodeKeys[next]; !ok {
				continue
			}
			if color[next] == 1 {
				return true
			}
			if color[next] == 0 && visit(next) {
				return true
			}
		}
		color[key] = 2
		return false
	}
	for key := range nodeKeys {
		if color[key] == 0 && visit(key) {
			return true
		}
	}
	return false
}

func normalizeHops(hops int) int {
	if hops <= 0 {
		return 1
	}
	if hops > 10 {
		return 10
	}
	return hops
}

func normalizeMaxNodes(maxNodes int) int {
	if maxNodes <= 0 {
		return 200
	}
	if maxNodes > 1000 {
		return 1000
	}
	return maxNodes
}

func normalizeNodeKeys(keys []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}

func sortTopologyNodes(nodes []model.TopologyNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Kind == nodes[j].Kind {
			return nodes[i].NodeKey < nodes[j].NodeKey
		}
		return nodes[i].Kind < nodes[j].Kind
	})
}

func sortTopologyEdges(edges []model.TopologyEdge) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].EdgeType == edges[j].EdgeType {
			return edges[i].EdgeKey < edges[j].EdgeKey
		}
		return edges[i].EdgeType < edges[j].EdgeType
	})
}

func normalizeNodeType(input NodeTypeInput) (*model.TopologyNodeType, error) {
	typeKey := strings.TrimSpace(input.TypeKey)
	displayName := strings.TrimSpace(input.DisplayName)
	if !validTypeKey(typeKey) || displayName == "" {
		return nil, ErrInvalidInput
	}
	identityFields := validJSONOrDefault(input.IdentityFields, []byte(`[]`))
	searchableFields := validJSONOrDefault(input.SearchableFields, []byte(`[]`))
	detailFields := validJSONOrDefault(input.DetailFields, []byte(`[]`))
	if identityFields == nil || searchableFields == nil || detailFields == nil {
		return nil, ErrInvalidInput
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	return &model.TopologyNodeType{
		TypeKey:              typeKey,
		DisplayName:          displayName,
		Category:             cleanStringPtr(input.Category),
		Icon:                 cleanStringPtr(input.Icon),
		DefaultColor:         cleanStringPtr(input.DefaultColor),
		IdentityFields:       identityFields,
		SearchableFields:     searchableFields,
		DefaultLabelTemplate: cleanStringPtr(input.DefaultLabelTemplate),
		DetailFields:         detailFields,
		Enabled:              enabled,
	}, nil
}

func normalizeRelationType(input RelationTypeInput) (*model.TopologyRelationType, error) {
	typeKey := strings.TrimSpace(input.TypeKey)
	displayName := strings.TrimSpace(input.DisplayName)
	semantics := strings.TrimSpace(input.Semantics)
	failurePropagation := strings.TrimSpace(input.FailurePropagation)
	defaultDirection := strings.TrimSpace(input.DefaultDirection)
	if !validTypeKey(typeKey) || displayName == "" || !validSemantics(semantics) || !validFailurePropagation(failurePropagation) {
		return nil, ErrInvalidInput
	}
	if defaultDirection == "" {
		defaultDirection = "both"
	}
	if defaultDirection != "upstream" && defaultDirection != "downstream" && defaultDirection != "both" {
		return nil, ErrInvalidInput
	}
	allowedSourceTypes := validJSONOrDefault(input.AllowedSourceTypes, []byte(`[]`))
	allowedTargetTypes := validJSONOrDefault(input.AllowedTargetTypes, []byte(`[]`))
	style := validJSONOrDefault(input.Style, []byte(`{}`))
	if allowedSourceTypes == nil || allowedTargetTypes == nil || style == nil {
		return nil, ErrInvalidInput
	}
	propagatesFailure := semantics == model.TopologyRelationSemanticsHardDep ||
		semantics == model.TopologyRelationSemanticsRuntimeDep ||
		semantics == model.TopologyRelationSemanticsTraffic
	if input.PropagatesFailure != nil {
		propagatesFailure = *input.PropagatesFailure
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	return &model.TopologyRelationType{
		TypeKey:            typeKey,
		DisplayName:        displayName,
		Semantics:          semantics,
		FailurePropagation: failurePropagation,
		DefaultDirection:   defaultDirection,
		PropagatesFailure:  propagatesFailure,
		AllowedSourceTypes: allowedSourceTypes,
		AllowedTargetTypes: allowedTargetTypes,
		Style:              style,
		Enabled:            enabled,
	}, nil
}

func (s *Service) normalizeSourceConfig(ctx context.Context, input SourceConfigInput) (*model.TopologySourceConfig, error) {
	name := strings.TrimSpace(input.Name)
	sourceType := strings.TrimSpace(input.SourceType)
	if name == "" || len(name) > 120 || !validSourceType(sourceType) {
		return nil, ErrInvalidInput
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	priority := defaultSourcePriority(sourceType)
	if input.Priority != nil {
		priority = *input.Priority
	}
	if priority < 0 || priority > 100 {
		return nil, ErrInvalidInput
	}
	schedule := cleanStringPtr(input.Schedule)
	if schedule != nil && !validSchedule(*schedule) {
		return nil, ErrInvalidInput
	}
	scope := validJSONOrDefault(input.Scope, []byte(`{}`))
	mappingRules := validJSONOrDefault(input.MappingRules, []byte(`{}`))
	if scope == nil || mappingRules == nil {
		return nil, ErrInvalidInput
	}
	if containsSensitiveJSON(scope) || containsSensitiveJSON(mappingRules) {
		return nil, ErrSensitiveConfig
	}
	staleAfter := 900
	if input.StaleAfterSeconds != nil {
		staleAfter = *input.StaleAfterSeconds
	}
	deleteAfter := 604800
	if input.DeleteAfterSeconds != nil {
		deleteAfter = *input.DeleteAfterSeconds
	}
	if staleAfter <= 0 || deleteAfter < staleAfter {
		return nil, ErrInvalidInput
	}
	if err := s.validateSourceDataSource(ctx, sourceType, input.DataSourceID); err != nil {
		return nil, err
	}
	return &model.TopologySourceConfig{
		Name:               name,
		SourceType:         sourceType,
		DataSourceID:       input.DataSourceID,
		Enabled:            enabled,
		Priority:           priority,
		Schedule:           schedule,
		Scope:              scope,
		MappingRules:       mappingRules,
		StaleAfterSeconds:  staleAfter,
		DeleteAfterSeconds: deleteAfter,
		CreatedBy:          input.CreatedBy,
	}, nil
}

func (s *Service) validateSourceDataSource(ctx context.Context, sourceType string, dataSourceID *int64) error {
	if sourceType == model.TopologySourceTypeManual || sourceType == model.TopologySourceTypeEdgeAgent {
		return nil
	}
	if dataSourceID == nil || *dataSourceID <= 0 {
		return ErrInvalidInput
	}
	dataSource, err := s.repository.FindDataSourceByID(ctx, *dataSourceID)
	if err != nil {
		return err
	}
	if !dataSource.Enabled {
		return ErrInvalidInput
	}
	if !compatibleTopologyDataSource(sourceType, dataSource.SourceType) {
		return ErrInvalidInput
	}
	return nil
}

func validSourceType(value string) bool {
	switch value {
	case model.TopologySourceTypeManual,
		model.TopologySourceTypeKubernetes,
		model.TopologySourceTypeTraceServiceGraph,
		model.TopologySourceTypeCMDB,
		model.TopologySourceTypeEdgeAgent,
		model.TopologySourceTypeNacos,
		model.TopologySourceTypeRedis,
		model.TopologySourceTypeTiDB,
		model.TopologySourceTypeNginx,
		model.TopologySourceTypeGenericHTTP:
		return true
	default:
		return false
	}
}

func defaultSourcePriority(sourceType string) int {
	switch sourceType {
	case model.TopologySourceTypeManual:
		return 100
	case model.TopologySourceTypeCMDB:
		return 90
	case model.TopologySourceTypeKubernetes:
		return 80
	case model.TopologySourceTypeTraceServiceGraph:
		return 70
	case model.TopologySourceTypeNacos:
		return 68
	case model.TopologySourceTypeNginx:
		return 66
	case model.TopologySourceTypeTiDB:
		return 64
	case model.TopologySourceTypeRedis:
		return 62
	case model.TopologySourceTypeEdgeAgent:
		return 60
	default:
		return 50
	}
}

func compatibleTopologyDataSource(sourceType, dataSourceType string) bool {
	switch sourceType {
	case model.TopologySourceTypeKubernetes:
		return dataSourceType == model.DataSourceTypeKubernetes
	case model.TopologySourceTypeNacos:
		return dataSourceType == model.DataSourceTypeNacos
	case model.TopologySourceTypeRedis:
		return dataSourceType == model.DataSourceTypeRedis
	case model.TopologySourceTypeTiDB:
		return dataSourceType == model.DataSourceTypeTiDB
	case model.TopologySourceTypeNginx:
		return dataSourceType == model.DataSourceTypeNginx
	case model.TopologySourceTypeGenericHTTP, model.TopologySourceTypeCMDB, model.TopologySourceTypeTraceServiceGraph:
		return dataSourceType == model.DataSourceTypeHTTP || dataSourceType == model.DataSourceTypePrometheus
	default:
		return true
	}
}

func validSchedule(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	switch value {
	case "@hourly", "@daily", "@weekly":
		return true
	}
	parts := strings.Fields(value)
	if len(parts) != 5 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 32 {
			return false
		}
		for _, r := range part {
			if (r >= '0' && r <= '9') || r == '*' || r == '/' || r == ',' || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func containsSensitiveJSON(raw []byte) bool {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return true
	}
	return containsSensitiveValue(value)
}

func containsSensitiveValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveTopologyKey(key) || containsSensitiveValue(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitiveValue(child) {
				return true
			}
		}
	}
	return false
}

func sensitiveTopologyKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, token := range []string{"password", "token", "secret", "private_key", "authorization", "cookie", "connection_password"} {
		if strings.Contains(key, token) {
			return true
		}
	}
	return false
}

func validNodeMapping(mapping nodeMappingSpec) bool {
	return strings.TrimSpace(mapping.Name) != "" &&
		validJSONPath(mapping.EntityPath) &&
		validTypeKey(mapping.TargetNodeType) &&
		validTemplate(mapping.ExternalKeyTemplate) &&
		validTemplate(mapping.NameTemplate)
}

func validEdgeMapping(mapping edgeMappingSpec) bool {
	if mapping.Confidence != nil && (*mapping.Confidence < 0 || *mapping.Confidence > 1) {
		return false
	}
	return strings.TrimSpace(mapping.Name) != "" &&
		validJSONPath(mapping.EntityPath) &&
		validTypeKey(mapping.SourceLookup.NodeType) &&
		validTypeKey(mapping.TargetLookup.NodeType) &&
		validTypeKey(mapping.RelationType) &&
		validTemplate(mapping.SourceLookup.ExternalKeyTemplate) &&
		validTemplate(mapping.TargetLookup.ExternalKeyTemplate)
}

func validJSONPath(path string) bool {
	if path == "$" {
		return true
	}
	if !strings.HasPrefix(path, "$.") || len(path) > 200 {
		return false
	}
	for _, segment := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		if segment == "" {
			return false
		}
		if strings.HasSuffix(segment, "[*]") {
			segment = strings.TrimSuffix(segment, "[*]")
		}
		if !validPathSegment(segment) {
			return false
		}
	}
	return true
}

func validPathSegment(segment string) bool {
	if segment == "" || len(segment) > 80 {
		return false
	}
	for _, r := range segment {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validTemplate(template string) bool {
	if strings.TrimSpace(template) == "" || len(template) > 500 {
		return false
	}
	for index := 0; index < len(template); {
		start := strings.Index(template[index:], "{{")
		if start < 0 {
			return true
		}
		start += index
		end := strings.Index(template[start+2:], "}}")
		if end < 0 {
			return false
		}
		end += start + 2
		token := strings.TrimSpace(template[start+2 : end])
		if !validTemplateToken(token) {
			return false
		}
		index = end + 2
	}
	return true
}

func validTemplateToken(token string) bool {
	if token == "" || len(token) > 120 {
		return false
	}
	for _, segment := range strings.Split(token, ".") {
		if !validPathSegment(segment) {
			return false
		}
	}
	return true
}

func selectJSONPath(root any, path string) ([]any, error) {
	if !validJSONPath(path) {
		return nil, ErrInvalidInput
	}
	if path == "$" {
		return []any{root}, nil
	}
	current := []any{root}
	for _, rawSegment := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		expand := strings.HasSuffix(rawSegment, "[*]")
		segment := strings.TrimSuffix(rawSegment, "[*]")
		next := []any{}
		for _, item := range current {
			object, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("path %s expects object", path)
			}
			value, ok := object[segment]
			if !ok {
				return nil, fmt.Errorf("path %s missing %s", path, segment)
			}
			if expand {
				values, ok := value.([]any)
				if !ok {
					return nil, fmt.Errorf("path %s expects array", path)
				}
				next = append(next, values...)
				continue
			}
			next = append(next, value)
		}
		current = next
		if len(current) > 500 {
			return current[:500], nil
		}
	}
	return current, nil
}

func renderTemplate(template string, contextMap map[string]any) (string, error) {
	if !validTemplate(template) {
		return "", ErrInvalidInput
	}
	var builder strings.Builder
	for index := 0; index < len(template); {
		start := strings.Index(template[index:], "{{")
		if start < 0 {
			builder.WriteString(template[index:])
			break
		}
		start += index
		builder.WriteString(template[index:start])
		end := strings.Index(template[start+2:], "}}")
		if end < 0 {
			return "", ErrInvalidInput
		}
		end += start + 2
		token := strings.TrimSpace(template[start+2 : end])
		value, ok := lookupTemplateValue(contextMap, token)
		if !ok {
			return "", ErrInvalidInput
		}
		builder.WriteString(fmt.Sprint(value))
		index = end + 2
	}
	result := strings.TrimSpace(builder.String())
	if len(result) > 500 {
		return "", ErrInvalidInput
	}
	return result, nil
}

func lookupTemplateValue(contextMap map[string]any, token string) (any, bool) {
	var current any = contextMap
	for _, segment := range strings.Split(token, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok || current == nil {
			return nil, false
		}
	}
	switch current.(type) {
	case map[string]any, []any:
		return nil, false
	default:
		return current, true
	}
}

func (s *Service) validateAllowedNodeTypes(ctx context.Context, relationType *model.TopologyRelationType) error {
	sourceTypes, err := decodeStringArray(relationType.AllowedSourceTypes)
	if err != nil {
		return ErrInvalidInput
	}
	targetTypes, err := decodeStringArray(relationType.AllowedTargetTypes)
	if err != nil {
		return ErrInvalidInput
	}
	for _, typeKey := range append(sourceTypes, targetTypes...) {
		nodeType, err := s.repository.FindTopologyNodeTypeByKey(ctx, typeKey)
		if err != nil {
			return err
		}
		if !nodeType.Enabled {
			return ErrTopologyTypeDisabled
		}
	}
	return nil
}

func (s *Service) validateEdgeTypeCompatibility(ctx context.Context, edge *model.TopologyEdge, relationType *model.TopologyRelationType) error {
	sourceTypes, err := decodeStringArray(relationType.AllowedSourceTypes)
	if err != nil {
		return ErrInvalidInput
	}
	targetTypes, err := decodeStringArray(relationType.AllowedTargetTypes)
	if err != nil {
		return ErrInvalidInput
	}
	if len(sourceTypes) == 0 && len(targetTypes) == 0 {
		return nil
	}
	from, err := s.repository.FindNodeByKey(ctx, edge.FromNodeKey)
	if err != nil {
		return err
	}
	to, err := s.repository.FindNodeByKey(ctx, edge.ToNodeKey)
	if err != nil {
		return err
	}
	if len(sourceTypes) > 0 && !stringInSlice(from.Kind, sourceTypes) {
		return ErrInvalidInput
	}
	if len(targetTypes) > 0 && !stringInSlice(to.Kind, targetTypes) {
		return ErrInvalidInput
	}
	return nil
}

func normalizeNode(input NodeInput) (*model.TopologyNode, error) {
	kind := strings.TrimSpace(input.Kind)
	name := strings.TrimSpace(input.Name)
	nodeKey := strings.TrimSpace(input.NodeKey)
	if kind == "" || name == "" {
		return nil, ErrInvalidInput
	}
	if nodeKey == "" {
		nodeKey = externalKey(input.Environment, kind, input.SourceType, name)
	}
	labelsJSON := validJSONOrEmpty(input.Labels)
	propertiesJSON := validJSONOrEmpty(input.Properties)
	lockedFieldsJSON := validJSONOrDefault(input.LockedFields, []byte(`[]`))
	resolvedAttributesJSON := validJSONOrDefault(input.ResolvedAttributes, []byte(`{}`))
	sourceRef := validJSONOrEmpty(input.SourceRef)
	if labelsJSON == nil || propertiesJSON == nil || lockedFieldsJSON == nil || resolvedAttributesJSON == nil || sourceRef == nil {
		return nil, ErrInvalidInput
	}
	sourceType := strings.TrimSpace(input.SourceType)
	if sourceType == "" {
		sourceType = model.TopologySourceManual
	}
	sourcePriority := input.SourcePriority
	if sourcePriority < 0 || sourcePriority > 100 {
		return nil, ErrInvalidInput
	}
	if sourcePriority == 0 {
		sourcePriority = defaultSourcePriority(sourceType)
	}
	return &model.TopologyNode{
		NodeKey:            nodeKey,
		Kind:               kind,
		Name:               name,
		DisplayName:        cleanStringPtr(input.DisplayName),
		Environment:        cleanString(input.Environment),
		Cluster:            cleanString(input.Cluster),
		Namespace:          cleanString(input.Namespace),
		Labels:             labelsJSON,
		Properties:         propertiesJSON,
		SourceType:         sourceType,
		SourcePriority:     sourcePriority,
		LockedFields:       lockedFieldsJSON,
		ResolvedAttributes: resolvedAttributesJSON,
		SourceRef:          sourceRef,
	}, nil
}

func (s *Service) mergeNode(ctx context.Context, existing, incoming *model.TopologyNode) *model.TopologyNode {
	lockedFields := lockedFieldSet(existing.LockedFields)
	if incoming.SourcePriority < existing.SourcePriority {
		if !lockedFields["name"] {
			_ = s.repository.CreateTopologyConflict(ctx, &model.TopologyConflict{
				ConflictType: "attribute_conflict",
				Status:       "open",
				NodeID:       &existing.ID,
				Description:  "lower priority source attempted to update topology node name",
				Candidates:   conflictCandidates(existing.Name, incoming.Name),
			})
		}
		return existing
	}
	if lockedFields["name"] {
		incoming.Name = existing.Name
	}
	if lockedFields["display_name"] {
		incoming.DisplayName = existing.DisplayName
	}
	if lockedFields["properties"] {
		incoming.Properties = existing.Properties
	}
	if len(existing.LockedFields) > 0 && string(existing.LockedFields) != "[]" {
		incoming.LockedFields = existing.LockedFields
	}
	return incoming
}

func normalizeEdge(input EdgeInput) (*model.TopologyEdge, error) {
	from := strings.TrimSpace(input.FromNodeKey)
	to := strings.TrimSpace(input.ToNodeKey)
	edgeType := strings.TrimSpace(input.EdgeType)
	if from == "" || to == "" || edgeType == "" {
		return nil, ErrInvalidInput
	}
	if input.Confidence != nil && (*input.Confidence < 0 || *input.Confidence > 1) {
		return nil, ErrInvalidInput
	}
	edgeKey := strings.TrimSpace(input.EdgeKey)
	if edgeKey == "" {
		edgeKey = edgeKeyFor(from, to, edgeType)
	}
	propertiesJSON := validJSONOrEmpty(input.Properties)
	sourceRef := validJSONOrEmpty(input.SourceRef)
	if propertiesJSON == nil || sourceRef == nil {
		return nil, ErrInvalidInput
	}
	sourceType := strings.TrimSpace(input.SourceType)
	if sourceType == "" {
		sourceType = model.TopologySourceManual
	}
	return &model.TopologyEdge{
		EdgeKey:     edgeKey,
		FromNodeKey: from,
		ToNodeKey:   to,
		EdgeType:    edgeType,
		Confidence:  input.Confidence,
		Properties:  propertiesJSON,
		SourceType:  sourceType,
		SourceRef:   sourceRef,
	}, nil
}

func validJSONOrEmpty(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	if !json.Valid(raw) {
		return nil
	}
	return raw
}

func externalKey(environment, nodeType, sourceType, identity string) string {
	environment = strings.TrimSpace(environment)
	if environment == "" {
		environment = "default"
	}
	sourceType = strings.TrimSpace(sourceType)
	if sourceType == "" {
		sourceType = model.TopologySourceManual
	}
	return strings.ToLower(environment + ":" + strings.TrimSpace(nodeType) + ":" + sourceType + ":" + strings.TrimSpace(identity))
}

func lockedFieldSet(raw []byte) map[string]bool {
	result := map[string]bool{}
	if len(raw) == 0 {
		return result
	}
	var fields []string
	if err := json.Unmarshal(raw, &fields); err != nil {
		return result
	}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			result[field] = true
		}
	}
	return result
}

func conflictCandidates(existing, incoming string) []byte {
	payload, _ := json.Marshal([]map[string]string{
		{"source": "existing", "value": existing},
		{"source": "incoming", "value": incoming},
	})
	return payload
}

func validJSONOrDefault(raw json.RawMessage, fallback []byte) []byte {
	if len(raw) == 0 {
		return fallback
	}
	if !json.Valid(raw) {
		return nil
	}
	return raw
}

func decodeStringArray(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || !validTypeKey(value) {
			return nil, ErrInvalidInput
		}
		normalized = append(normalized, value)
	}
	return normalized, nil
}

func validTypeKey(value string) bool {
	if value == "" || len(value) > 80 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func validSemantics(value string) bool {
	switch value {
	case model.TopologyRelationSemanticsHardDep,
		model.TopologyRelationSemanticsRuntimeDep,
		model.TopologyRelationSemanticsTraffic,
		model.TopologyRelationSemanticsOwnership,
		model.TopologyRelationSemanticsContainment,
		model.TopologyRelationSemanticsConfiguration,
		model.TopologyRelationSemanticsAnnotation,
		model.TopologyRelationSemanticsObservation:
		return true
	default:
		return false
	}
}

func validFailurePropagation(value string) bool {
	switch value {
	case model.TopologyFailurePropagationNone,
		model.TopologyFailurePropagationSrcToDst,
		model.TopologyFailurePropagationDstToSrc,
		model.TopologyFailurePropagationBoth:
		return true
	default:
		return false
	}
}

func stringInSlice(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if candidate == value {
			return true
		}
	}
	return false
}

func cleanString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return cleanString(*value)
}

func edgeKeyFor(from, to, edgeType string) string {
	return strings.ToLower(from + "->" + to + ":" + edgeType)
}

func decodeK8s(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return ErrInvalidInput
	}
	return json.Unmarshal(raw, target)
}

type k8sGraphBuilder struct {
	environment string
	cluster     string
	namespace   string
	nodes       []model.TopologyNode
	edges       []model.TopologyEdge
	deployments []appsv1.Deployment
	pods        []corev1.Pod
	services    []corev1.Service
}

func newK8sGraphBuilder(environment, cluster, namespace string) *k8sGraphBuilder {
	return &k8sGraphBuilder{
		environment: strings.TrimSpace(environment),
		cluster:     strings.TrimSpace(cluster),
		namespace:   strings.TrimSpace(namespace),
	}
}

func (b *k8sGraphBuilder) addDeployment(deployment *appsv1.Deployment) {
	b.deployments = append(b.deployments, *deployment)
	b.nodes = append(b.nodes, b.node(model.TopologyNodeKindK8sDeployment, deployment.Name, deployment.Labels, map[string]any{
		"replicas":      deployment.Status.Replicas,
		"readyReplicas": deployment.Status.ReadyReplicas,
	}, deployment))
}

func (b *k8sGraphBuilder) addPod(pod *corev1.Pod) {
	b.pods = append(b.pods, *pod)
	b.nodes = append(b.nodes, b.node(model.TopologyNodeKindK8sPod, pod.Name, pod.Labels, map[string]any{
		"phase":    string(pod.Status.Phase),
		"podIp":    pod.Status.PodIP,
		"nodeName": pod.Spec.NodeName,
	}, pod))
}

func (b *k8sGraphBuilder) addService(service *corev1.Service) {
	b.services = append(b.services, *service)
	b.nodes = append(b.nodes, b.node(model.TopologyNodeKindK8sService, service.Name, service.Labels, map[string]any{
		"type":      string(service.Spec.Type),
		"clusterIp": service.Spec.ClusterIP,
	}, service))
}

func (b *k8sGraphBuilder) addIngress(ingress *networkingv1.Ingress) {
	b.nodes = append(b.nodes, b.node(model.TopologyNodeKindK8sIngress, ingress.Name, ingress.Labels, map[string]any{
		"class": ingressClass(ingress),
		"hosts": ingressHosts(ingress),
	}, ingress))
	for _, serviceName := range ingressServiceBackends(ingress) {
		b.edges = append(b.edges, b.edge(
			k8sNodeKey(b.cluster, ingress.Namespace, model.TopologyNodeKindK8sIngress, ingress.Name),
			k8sNodeKey(b.cluster, ingress.Namespace, model.TopologyNodeKindK8sService, serviceName),
			model.TopologyEdgeTypeRoutesTo,
			ingress,
		))
	}
}

func (b *k8sGraphBuilder) linkK8sResources() {
	for dIndex := range b.deployments {
		deployment := &b.deployments[dIndex]
		selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		if err != nil || selector.Empty() {
			continue
		}
		for pIndex := range b.pods {
			pod := &b.pods[pIndex]
			if pod.Namespace == deployment.Namespace && selector.Matches(labels.Set(pod.Labels)) {
				b.edges = append(b.edges, b.edge(
					k8sNodeKey(b.cluster, deployment.Namespace, model.TopologyNodeKindK8sDeployment, deployment.Name),
					k8sNodeKey(b.cluster, pod.Namespace, model.TopologyNodeKindK8sPod, pod.Name),
					model.TopologyEdgeTypeOwns,
					deployment,
				))
			}
		}
	}
	for sIndex := range b.services {
		service := &b.services[sIndex]
		if len(service.Spec.Selector) == 0 {
			continue
		}
		selector := labels.SelectorFromSet(labels.Set(service.Spec.Selector))
		for pIndex := range b.pods {
			pod := &b.pods[pIndex]
			if pod.Namespace == service.Namespace && selector.Matches(labels.Set(pod.Labels)) {
				b.edges = append(b.edges, b.edge(
					k8sNodeKey(b.cluster, service.Namespace, model.TopologyNodeKindK8sService, service.Name),
					k8sNodeKey(b.cluster, pod.Namespace, model.TopologyNodeKindK8sPod, pod.Name),
					model.TopologyEdgeTypeSelects,
					service,
				))
			}
		}
		for dIndex := range b.deployments {
			deployment := &b.deployments[dIndex]
			deploymentSelector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
			if err != nil || deploymentSelector.Empty() {
				continue
			}
			if deployment.Namespace == service.Namespace && selectorsOverlap(selector, deploymentSelector) {
				b.edges = append(b.edges, b.edge(
					k8sNodeKey(b.cluster, service.Namespace, model.TopologyNodeKindK8sService, service.Name),
					k8sNodeKey(b.cluster, deployment.Namespace, model.TopologyNodeKindK8sDeployment, deployment.Name),
					model.TopologyEdgeTypeDependsOn,
					service,
				))
			}
		}
	}
}

func (b *k8sGraphBuilder) node(kind, name string, labelMap map[string]string, properties map[string]any, object metav1.Object) model.TopologyNode {
	labelsJSON, _ := json.Marshal(labelMap)
	propertiesJSON, _ := json.Marshal(properties)
	sourceRef, _ := json.Marshal(map[string]any{
		"uid":             string(object.GetUID()),
		"resourceVersion": object.GetResourceVersion(),
	})
	return model.TopologyNode{
		NodeKey:     k8sNodeKey(b.cluster, object.GetNamespace(), kind, name),
		Kind:        kind,
		Name:        name,
		Environment: cleanString(b.environment),
		Cluster:     cleanString(b.cluster),
		Namespace:   cleanString(object.GetNamespace()),
		Labels:      labelsJSON,
		Properties:  propertiesJSON,
		SourceType:  model.TopologySourceK8s,
		SourceRef:   sourceRef,
	}
}

func (b *k8sGraphBuilder) edge(from, to, edgeType string, object metav1.Object) model.TopologyEdge {
	confidence := 1.0
	sourceRef, _ := json.Marshal(map[string]any{
		"kind":      k8sObjectKind(object),
		"namespace": object.GetNamespace(),
		"name":      object.GetName(),
		"uid":       string(object.GetUID()),
	})
	return model.TopologyEdge{
		EdgeKey:     edgeKeyFor(from, to, edgeType),
		FromNodeKey: from,
		ToNodeKey:   to,
		EdgeType:    edgeType,
		Confidence:  &confidence,
		Properties:  []byte(`{}`),
		SourceType:  model.TopologySourceK8s,
		SourceRef:   sourceRef,
	}
}

func k8sObjectKind(object metav1.Object) string {
	switch object.(type) {
	case *appsv1.Deployment:
		return "Deployment"
	case *corev1.Pod:
		return "Pod"
	case *corev1.Service:
		return "Service"
	case *networkingv1.Ingress:
		return "Ingress"
	default:
		return ""
	}
}

func k8sNodeKey(cluster, namespace, kind, name string) string {
	return fmt.Sprintf("k8s:%s:%s:%s:%s", strings.TrimSpace(cluster), namespace, kind, name)
}

func ingressClass(ingress *networkingv1.Ingress) string {
	if ingress.Spec.IngressClassName == nil {
		return ""
	}
	return *ingress.Spec.IngressClassName
}

func ingressHosts(ingress *networkingv1.Ingress) []string {
	hosts := []string{}
	for _, rule := range ingress.Spec.Rules {
		if strings.TrimSpace(rule.Host) != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	return hosts
}

func ingressServiceBackends(ingress *networkingv1.Ingress) []string {
	seen := map[string]struct{}{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		seen[name] = struct{}{}
	}
	if ingress.Spec.DefaultBackend != nil && ingress.Spec.DefaultBackend.Service != nil {
		add(ingress.Spec.DefaultBackend.Service.Name)
	}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				add(path.Backend.Service.Name)
			}
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	return result
}

func selectorsOverlap(left, right labels.Selector) bool {
	requirements, selectable := left.Requirements()
	if !selectable {
		return false
	}
	candidate := labels.Set{}
	for _, requirement := range requirements {
		values := requirement.Values().List()
		if len(values) > 0 {
			candidate[requirement.Key()] = values[0]
		}
	}
	return right.Matches(candidate)
}
