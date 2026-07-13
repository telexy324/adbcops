package model

import "time"

const (
	TopologyNodeKindK8sDeployment = "k8s_deployment"
	TopologyNodeKindK8sPod        = "k8s_pod"
	TopologyNodeKindK8sService    = "k8s_service"
	TopologyNodeKindK8sIngress    = "k8s_ingress"
	TopologyNodeKindK8sEndpoint   = "k8s_endpoint"
	TopologyNodeKindK8sNode       = "k8s_node"
	TopologyNodeKindK8sPVC        = "k8s_pvc"
	TopologyNodeKindManual        = "manual"

	TopologyEdgeTypeOwns         = "owns"
	TopologyEdgeTypeSelects      = "selects"
	TopologyEdgeTypeRoutesTo     = "routes_to"
	TopologyEdgeTypeDependsOn    = "depends_on"
	TopologyEdgeTypeRunsOn       = "runs_on"
	TopologyEdgeTypeStoresIn     = "stores_in"
	TopologyEdgeTypeCalls        = "calls"
	TopologyEdgeTypeMemberOf     = "member_of"
	TopologyEdgeTypeReplicatesTo = "replicates_to"
	TopologyEdgeTypeConnectsTo   = "connects_to"
	TopologyEdgeTypeRegisteredIn = "registered_in"
	TopologyEdgeTypeObservedWith = "observed_with"
	TopologyEdgeTypeExposes      = "exposes"

	TopologySourceManual = "manual"
	TopologySourceK8s    = "kubernetes"
)

const (
	TopologySourceTypeManual            = "manual"
	TopologySourceTypeKubernetes        = "kubernetes"
	TopologySourceTypeTraceServiceGraph = "trace_service_graph"
	TopologySourceTypeCMDB              = "cmdb"
	TopologySourceTypeEdgeAgent         = "edge_agent"
	TopologySourceTypeNacos             = "nacos"
	TopologySourceTypeRedis             = "redis"
	TopologySourceTypeTiDB              = "tidb"
	TopologySourceTypeNginx             = "nginx"
	TopologySourceTypeGenericHTTP       = "generic_http"
)

const (
	TopologyRelationSemanticsHardDep       = "hard_dep"
	TopologyRelationSemanticsRuntimeDep    = "runtime_dep"
	TopologyRelationSemanticsTraffic       = "traffic"
	TopologyRelationSemanticsOwnership     = "ownership"
	TopologyRelationSemanticsContainment   = "containment"
	TopologyRelationSemanticsConfiguration = "configuration"
	TopologyRelationSemanticsAnnotation    = "annotation"
	TopologyRelationSemanticsObservation   = "observation"

	TopologyFailurePropagationNone     = "none"
	TopologyFailurePropagationSrcToDst = "src_to_dst"
	TopologyFailurePropagationDstToSrc = "dst_to_src"
	TopologyFailurePropagationBoth     = "both"
)

type TopologyNode struct {
	ID                 int64     `gorm:"column:id;primaryKey" json:"id"`
	NodeKey            string    `gorm:"column:node_key;size:255;not null;unique" json:"nodeKey"`
	Kind               string    `gorm:"column:kind;size:60;not null" json:"kind"`
	NodeTypeID         *int64    `gorm:"column:node_type_id" json:"nodeTypeId,omitempty"`
	Name               string    `gorm:"column:name;size:255;not null" json:"name"`
	DisplayName        *string   `gorm:"column:display_name;size:255" json:"displayName,omitempty"`
	Environment        *string   `gorm:"column:environment;size:80" json:"environment,omitempty"`
	Cluster            *string   `gorm:"column:cluster;size:120" json:"cluster,omitempty"`
	Namespace          *string   `gorm:"column:namespace;size:120" json:"namespace,omitempty"`
	Labels             []byte    `gorm:"column:labels;type:jsonb" json:"labels,omitempty"`
	Properties         []byte    `gorm:"column:properties;type:jsonb" json:"properties,omitempty"`
	SourceType         string    `gorm:"column:source_type;size:50;not null" json:"sourceType"`
	SourcePriority     int       `gorm:"column:source_priority;not null" json:"sourcePriority"`
	LockedFields       []byte    `gorm:"column:locked_fields;type:jsonb" json:"lockedFields,omitempty"`
	ResolvedAttributes []byte    `gorm:"column:resolved_attributes;type:jsonb" json:"resolvedAttributes,omitempty"`
	SourceRef          []byte    `gorm:"column:source_ref;type:jsonb" json:"sourceRef,omitempty"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (TopologyNode) TableName() string {
	return "topology_node"
}

type TopologyEdge struct {
	ID                 int64      `gorm:"column:id;primaryKey" json:"id"`
	EdgeKey            string     `gorm:"column:edge_key;size:255;not null;unique" json:"edgeKey"`
	FromNodeKey        string     `gorm:"column:from_node_key;size:255;not null" json:"fromNodeKey"`
	ToNodeKey          string     `gorm:"column:to_node_key;size:255;not null" json:"toNodeKey"`
	EdgeType           string     `gorm:"column:edge_type;size:80;not null" json:"edgeType"`
	RelationTypeID     *int64     `gorm:"column:relation_type_id" json:"relationTypeId,omitempty"`
	Confidence         *float64   `gorm:"column:confidence" json:"confidence,omitempty"`
	Status             string     `gorm:"column:status;size:30;not null" json:"status"`
	SourcePriority     int        `gorm:"column:source_priority;not null" json:"sourcePriority"`
	ResolvedConfidence *float64   `gorm:"column:resolved_confidence" json:"resolvedConfidence,omitempty"`
	FirstObservedAt    *time.Time `gorm:"column:first_observed_at" json:"firstObservedAt,omitempty"`
	LastObservedAt     *time.Time `gorm:"column:last_observed_at" json:"lastObservedAt,omitempty"`
	StaleAt            *time.Time `gorm:"column:stale_at" json:"staleAt,omitempty"`
	DeletedAt          *time.Time `gorm:"column:deleted_at" json:"deletedAt,omitempty"`
	Properties         []byte     `gorm:"column:properties;type:jsonb" json:"properties,omitempty"`
	SourceType         string     `gorm:"column:source_type;size:50;not null" json:"sourceType"`
	SourceRef          []byte     `gorm:"column:source_ref;type:jsonb" json:"sourceRef,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (TopologyEdge) TableName() string {
	return "topology_edge"
}

type TopologyEdgeObservation struct {
	ID                 int64      `gorm:"column:id;primaryKey" json:"id"`
	EdgeID             int64      `gorm:"column:edge_id;not null" json:"edgeId"`
	SourceConfigID     *int64     `gorm:"column:source_config_id" json:"sourceConfigId,omitempty"`
	SourceType         string     `gorm:"column:source_type;size:50;not null" json:"sourceType"`
	SourceRecordKey    *string    `gorm:"column:source_record_key;size:255" json:"sourceRecordKey,omitempty"`
	SourcePriority     int        `gorm:"column:source_priority;not null" json:"sourcePriority"`
	ObservedAttributes []byte     `gorm:"column:observed_attributes;type:jsonb" json:"observedAttributes,omitempty"`
	Confidence         *float64   `gorm:"column:confidence" json:"confidence,omitempty"`
	ObservedAt         time.Time  `gorm:"column:observed_at;not null" json:"observedAt"`
	ExpiresAt          *time.Time `gorm:"column:expires_at" json:"expiresAt,omitempty"`
	RawRef             []byte     `gorm:"column:raw_ref;type:jsonb" json:"rawRef,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (TopologyEdgeObservation) TableName() string {
	return "topology_edge_observation"
}

type TopologyNodeType struct {
	ID                   int64     `gorm:"column:id;primaryKey" json:"id"`
	TypeKey              string    `gorm:"column:type_key;size:80;not null;unique" json:"typeKey"`
	DisplayName          string    `gorm:"column:display_name;size:120;not null" json:"displayName"`
	Category             *string   `gorm:"column:category;size:80" json:"category,omitempty"`
	Icon                 *string   `gorm:"column:icon;size:120" json:"icon,omitempty"`
	DefaultColor         *string   `gorm:"column:default_color;size:50" json:"defaultColor,omitempty"`
	IdentityFields       []byte    `gorm:"column:identity_fields;type:jsonb" json:"identityFields,omitempty"`
	SearchableFields     []byte    `gorm:"column:searchable_fields;type:jsonb" json:"searchableFields,omitempty"`
	DefaultLabelTemplate *string   `gorm:"column:label_template" json:"defaultLabelTemplate,omitempty"`
	DetailFields         []byte    `gorm:"column:detail_fields;type:jsonb" json:"detailFields,omitempty"`
	Enabled              bool      `gorm:"column:enabled;not null" json:"enabled"`
	BuiltIn              bool      `gorm:"column:built_in;not null" json:"builtIn"`
	CreatedAt            time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt            time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (TopologyNodeType) TableName() string {
	return "topology_node_type"
}

type TopologyRelationType struct {
	ID                 int64     `gorm:"column:id;primaryKey" json:"id"`
	TypeKey            string    `gorm:"column:type_key;size:80;not null;unique" json:"typeKey"`
	DisplayName        string    `gorm:"column:display_name;size:120;not null" json:"displayName"`
	Semantics          string    `gorm:"column:semantics;size:50;not null" json:"semantics"`
	FailurePropagation string    `gorm:"column:failure_propagation;size:30;not null" json:"failurePropagation"`
	DefaultDirection   string    `gorm:"column:default_direction;size:30;not null" json:"defaultDirection"`
	PropagatesFailure  bool      `gorm:"column:propagates_failure;not null" json:"propagatesFailure"`
	AllowedSourceTypes []byte    `gorm:"column:allowed_source_types;type:jsonb" json:"allowedSourceTypes,omitempty"`
	AllowedTargetTypes []byte    `gorm:"column:allowed_target_types;type:jsonb" json:"allowedTargetTypes,omitempty"`
	Style              []byte    `gorm:"column:style;type:jsonb" json:"style,omitempty"`
	Enabled            bool      `gorm:"column:enabled;not null" json:"enabled"`
	BuiltIn            bool      `gorm:"column:built_in;not null" json:"builtIn"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (TopologyRelationType) TableName() string {
	return "topology_relation_type"
}

type TopologyTypeAudit struct {
	ID        int64     `gorm:"column:id;primaryKey" json:"id"`
	TypeKind  string    `gorm:"column:type_kind;size:30;not null" json:"typeKind"`
	TypeID    int64     `gorm:"column:type_id;not null" json:"typeId"`
	Action    string    `gorm:"column:action;size:80;not null" json:"action"`
	Before    []byte    `gorm:"column:before_value;type:jsonb" json:"before,omitempty"`
	After     []byte    `gorm:"column:after_value;type:jsonb" json:"after,omitempty"`
	ActorID   *int64    `gorm:"column:actor_id" json:"actorId,omitempty"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (TopologyTypeAudit) TableName() string {
	return "topology_type_audit"
}

type TopologySourceConfig struct {
	ID                 int64      `gorm:"column:id;primaryKey" json:"id"`
	Name               string     `gorm:"column:name;size:120;not null;unique" json:"name"`
	SourceType         string     `gorm:"column:source_type;size:50;not null" json:"sourceType"`
	DataSourceID       *int64     `gorm:"column:data_source_id" json:"dataSourceId,omitempty"`
	Enabled            bool       `gorm:"column:enabled;not null" json:"enabled"`
	Priority           int        `gorm:"column:priority;not null" json:"priority"`
	Schedule           *string    `gorm:"column:schedule;size:120" json:"schedule,omitempty"`
	Scope              []byte     `gorm:"column:scope;type:jsonb" json:"scope,omitempty"`
	MappingRules       []byte     `gorm:"column:mapping_rules;type:jsonb" json:"mappingRules,omitempty"`
	StaleAfterSeconds  int        `gorm:"column:stale_after_seconds;not null" json:"staleAfterSeconds"`
	DeleteAfterSeconds int        `gorm:"column:delete_after_seconds;not null" json:"deleteAfterSeconds"`
	LastSyncAt         *time.Time `gorm:"column:last_sync_at" json:"lastSyncAt,omitempty"`
	NextSyncAt         *time.Time `gorm:"column:next_sync_at" json:"nextSyncAt,omitempty"`
	CreatedBy          *int64     `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (TopologySourceConfig) TableName() string {
	return "topology_source_config"
}

type TopologySyncRun struct {
	ID              int64      `gorm:"column:id;primaryKey" json:"id"`
	SourceConfigID  int64      `gorm:"column:source_config_id;not null" json:"sourceConfigId"`
	TriggerType     string     `gorm:"column:trigger_type;size:30;not null" json:"triggerType"`
	Status          string     `gorm:"column:status;size:30;not null" json:"status"`
	DiscoveredNodes int        `gorm:"column:discovered_nodes;not null" json:"discoveredNodes"`
	DiscoveredEdges int        `gorm:"column:discovered_edges;not null" json:"discoveredEdges"`
	CreatedNodes    int        `gorm:"column:created_nodes;not null" json:"createdNodes"`
	UpdatedNodes    int        `gorm:"column:updated_nodes;not null" json:"updatedNodes"`
	StaleNodes      int        `gorm:"column:stale_nodes;not null" json:"staleNodes"`
	CreatedEdges    int        `gorm:"column:created_edges;not null" json:"createdEdges"`
	UpdatedEdges    int        `gorm:"column:updated_edges;not null" json:"updatedEdges"`
	StaleEdges      int        `gorm:"column:stale_edges;not null" json:"staleEdges"`
	ConflictCount   int        `gorm:"column:conflict_count;not null" json:"conflictCount"`
	WarningCount    int        `gorm:"column:warning_count;not null" json:"warningCount"`
	ErrorMessage    *string    `gorm:"column:error_message" json:"errorMessage,omitempty"`
	Detail          []byte     `gorm:"column:detail;type:jsonb" json:"detail,omitempty"`
	StartedAt       *time.Time `gorm:"column:started_at" json:"startedAt,omitempty"`
	FinishedAt      *time.Time `gorm:"column:finished_at" json:"finishedAt,omitempty"`
	CreatedAt       time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (TopologySyncRun) TableName() string {
	return "topology_sync_run"
}

type TopologyNodeAlias struct {
	ID          int64     `gorm:"column:id;primaryKey" json:"id"`
	NodeID      int64     `gorm:"column:node_id;not null" json:"nodeId"`
	Alias       string    `gorm:"column:alias;size:255;not null" json:"alias"`
	AliasType   *string   `gorm:"column:alias_type;size:50" json:"aliasType,omitempty"`
	Environment *string   `gorm:"column:environment;size:50" json:"environment,omitempty"`
	SourceType  *string   `gorm:"column:source_type;size:50" json:"sourceType,omitempty"`
	Confidence  *float64  `gorm:"column:confidence" json:"confidence,omitempty"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (TopologyNodeAlias) TableName() string {
	return "topology_node_alias"
}

type TopologyConflict struct {
	ID             int64      `gorm:"column:id;primaryKey" json:"id"`
	ConflictType   string     `gorm:"column:conflict_type;size:50;not null" json:"conflictType"`
	Status         string     `gorm:"column:status;size:30;not null" json:"status"`
	NodeID         *int64     `gorm:"column:node_id" json:"nodeId,omitempty"`
	EdgeID         *int64     `gorm:"column:edge_id" json:"edgeId,omitempty"`
	SourceConfigID *int64     `gorm:"column:source_config_id" json:"sourceConfigId,omitempty"`
	Description    string     `gorm:"column:description;not null" json:"description"`
	Candidates     []byte     `gorm:"column:candidates;type:jsonb" json:"candidates,omitempty"`
	Resolution     []byte     `gorm:"column:resolution;type:jsonb" json:"resolution,omitempty"`
	ResolvedBy     *int64     `gorm:"column:resolved_by" json:"resolvedBy,omitempty"`
	ResolvedAt     *time.Time `gorm:"column:resolved_at" json:"resolvedAt,omitempty"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

func (TopologyConflict) TableName() string {
	return "topology_conflict"
}
