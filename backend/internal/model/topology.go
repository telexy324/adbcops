package model

import "time"

const (
	TopologyNodeKindK8sDeployment = "k8s_deployment"
	TopologyNodeKindK8sPod        = "k8s_pod"
	TopologyNodeKindK8sService    = "k8s_service"
	TopologyNodeKindK8sIngress    = "k8s_ingress"
	TopologyNodeKindManual        = "manual"

	TopologyEdgeTypeOwns      = "owns"
	TopologyEdgeTypeSelects   = "selects"
	TopologyEdgeTypeRoutesTo  = "routes_to"
	TopologyEdgeTypeDependsOn = "depends_on"

	TopologySourceManual = "manual"
	TopologySourceK8s    = "kubernetes"
)

type TopologyNode struct {
	ID          int64     `gorm:"column:id;primaryKey" json:"id"`
	NodeKey     string    `gorm:"column:node_key;size:255;not null;unique" json:"nodeKey"`
	Kind        string    `gorm:"column:kind;size:60;not null" json:"kind"`
	Name        string    `gorm:"column:name;size:255;not null" json:"name"`
	DisplayName *string   `gorm:"column:display_name;size:255" json:"displayName,omitempty"`
	Environment *string   `gorm:"column:environment;size:80" json:"environment,omitempty"`
	Cluster     *string   `gorm:"column:cluster;size:120" json:"cluster,omitempty"`
	Namespace   *string   `gorm:"column:namespace;size:120" json:"namespace,omitempty"`
	Labels      []byte    `gorm:"column:labels;type:jsonb" json:"labels,omitempty"`
	Properties  []byte    `gorm:"column:properties;type:jsonb" json:"properties,omitempty"`
	SourceType  string    `gorm:"column:source_type;size:50;not null" json:"sourceType"`
	SourceRef   []byte    `gorm:"column:source_ref;type:jsonb" json:"sourceRef,omitempty"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (TopologyNode) TableName() string {
	return "topology_node"
}

type TopologyEdge struct {
	ID          int64     `gorm:"column:id;primaryKey" json:"id"`
	EdgeKey     string    `gorm:"column:edge_key;size:255;not null;unique" json:"edgeKey"`
	FromNodeKey string    `gorm:"column:from_node_key;size:255;not null" json:"fromNodeKey"`
	ToNodeKey   string    `gorm:"column:to_node_key;size:255;not null" json:"toNodeKey"`
	EdgeType    string    `gorm:"column:edge_type;size:80;not null" json:"edgeType"`
	Confidence  *float64  `gorm:"column:confidence" json:"confidence,omitempty"`
	Properties  []byte    `gorm:"column:properties;type:jsonb" json:"properties,omitempty"`
	SourceType  string    `gorm:"column:source_type;size:50;not null" json:"sourceType"`
	SourceRef   []byte    `gorm:"column:source_ref;type:jsonb" json:"sourceRef,omitempty"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (TopologyEdge) TableName() string {
	return "topology_edge"
}
