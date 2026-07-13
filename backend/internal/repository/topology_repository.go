package repository

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TopologyRepository interface {
	UpsertNode(ctx context.Context, node *model.TopologyNode) error
	UpsertEdge(ctx context.Context, edge *model.TopologyEdge) error
	FindNodeByKey(ctx context.Context, nodeKey string) (*model.TopologyNode, error)
	ListNodes(ctx context.Context, filters TopologyFilters) ([]model.TopologyNode, error)
	ListEdges(ctx context.Context, filters TopologyFilters) ([]model.TopologyEdge, error)
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
	UpdateTopologySourceConfig(ctx context.Context, id int64, updates TopologySourceConfigUpdates) (*model.TopologySourceConfig, error)
	DeleteTopologySourceConfig(ctx context.Context, id int64) error
	FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error)
}

type TopologyFilters struct {
	Environment string
	Cluster     string
	Namespace   string
	Kind        string
	Limit       int
}

type TopologySourceConfigUpdates struct {
	Name               *string
	SourceType         *string
	DataSourceID       *int64
	DataSourceIDSet    bool
	Enabled            *bool
	Priority           *int
	Schedule           *string
	ScheduleSet        bool
	Scope              []byte
	ScopeSet           bool
	MappingRules       []byte
	MappingRulesSet    bool
	StaleAfterSeconds  *int
	DeleteAfterSeconds *int
}

type GORMTopologyRepository struct {
	db *gorm.DB
}

func NewTopologyRepository(db *gorm.DB) *GORMTopologyRepository {
	return &GORMTopologyRepository{db: db}
}

func (r *GORMTopologyRepository) UpsertNode(ctx context.Context, node *model.TopologyNode) error {
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "node_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"kind", "node_type_id", "name", "display_name", "environment", "cluster", "namespace", "labels", "properties", "source_type", "source_ref", "updated_at",
		}),
	}).Create(node).Error; err != nil {
		return fmt.Errorf("upsert topology node: %w", err)
	}
	return nil
}

func (r *GORMTopologyRepository) UpsertEdge(ctx context.Context, edge *model.TopologyEdge) error {
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "edge_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"from_node_key", "to_node_key", "edge_type", "relation_type_id", "confidence", "properties", "source_type", "source_ref", "updated_at",
		}),
	}).Create(edge).Error; err != nil {
		return fmt.Errorf("upsert topology edge: %w", err)
	}
	return nil
}

func (r *GORMTopologyRepository) FindNodeByKey(ctx context.Context, nodeKey string) (*model.TopologyNode, error) {
	var node model.TopologyNode
	if err := r.db.WithContext(ctx).Where("node_key = ?", nodeKey).First(&node).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find topology node by key: %w", err)
	}
	return &node, nil
}

func (r *GORMTopologyRepository) ListNodes(ctx context.Context, filters TopologyFilters) ([]model.TopologyNode, error) {
	limit := filters.Limit
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	query := r.db.WithContext(ctx).Order("kind ASC, name ASC").Limit(limit)
	if filters.Environment != "" {
		query = query.Where("environment = ?", filters.Environment)
	}
	if filters.Cluster != "" {
		query = query.Where("cluster = ?", filters.Cluster)
	}
	if filters.Namespace != "" {
		query = query.Where("namespace = ?", filters.Namespace)
	}
	if filters.Kind != "" {
		query = query.Where("kind = ?", filters.Kind)
	}
	var nodes []model.TopologyNode
	if err := query.Find(&nodes).Error; err != nil {
		return nil, fmt.Errorf("list topology nodes: %w", err)
	}
	return nodes, nil
}

func (r *GORMTopologyRepository) ListEdges(ctx context.Context, filters TopologyFilters) ([]model.TopologyEdge, error) {
	limit := filters.Limit
	if limit <= 0 || limit > 3000 {
		limit = 1500
	}
	query := r.db.WithContext(ctx).
		Joins("JOIN topology_node from_node ON from_node.node_key = topology_edge.from_node_key").
		Order("topology_edge.edge_type ASC, topology_edge.from_node_key ASC, topology_edge.to_node_key ASC").
		Limit(limit)
	if filters.Environment != "" {
		query = query.Where("from_node.environment = ?", filters.Environment)
	}
	if filters.Cluster != "" {
		query = query.Where("from_node.cluster = ?", filters.Cluster)
	}
	if filters.Namespace != "" {
		query = query.Where("from_node.namespace = ?", filters.Namespace)
	}
	var edges []model.TopologyEdge
	if err := query.Find(&edges).Error; err != nil {
		return nil, fmt.Errorf("list topology edges: %w", err)
	}
	return edges, nil
}

func (r *GORMTopologyRepository) ListTopologyNodeTypes(ctx context.Context) ([]model.TopologyNodeType, error) {
	var nodeTypes []model.TopologyNodeType
	if err := r.db.WithContext(ctx).Order("built_in DESC, type_key ASC").Find(&nodeTypes).Error; err != nil {
		return nil, fmt.Errorf("list topology node types: %w", err)
	}
	return nodeTypes, nil
}

func (r *GORMTopologyRepository) FindTopologyNodeTypeByKey(ctx context.Context, typeKey string) (*model.TopologyNodeType, error) {
	var nodeType model.TopologyNodeType
	if err := r.db.WithContext(ctx).Where("type_key = ?", typeKey).First(&nodeType).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find topology node type by key: %w", err)
	}
	return &nodeType, nil
}

func (r *GORMTopologyRepository) FindTopologyNodeTypeByID(ctx context.Context, id int64) (*model.TopologyNodeType, error) {
	var nodeType model.TopologyNodeType
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&nodeType).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find topology node type by id: %w", err)
	}
	return &nodeType, nil
}

func (r *GORMTopologyRepository) CreateTopologyNodeType(ctx context.Context, nodeType *model.TopologyNodeType) error {
	if err := r.db.WithContext(ctx).Create(nodeType).Error; err != nil {
		return fmt.Errorf("create topology node type: %w", err)
	}
	return nil
}

func (r *GORMTopologyRepository) UpdateTopologyNodeType(ctx context.Context, nodeType *model.TopologyNodeType) error {
	if err := r.db.WithContext(ctx).Save(nodeType).Error; err != nil {
		return fmt.Errorf("update topology node type: %w", err)
	}
	return nil
}

func (r *GORMTopologyRepository) ListTopologyRelationTypes(ctx context.Context) ([]model.TopologyRelationType, error) {
	var relationTypes []model.TopologyRelationType
	if err := r.db.WithContext(ctx).Order("built_in DESC, type_key ASC").Find(&relationTypes).Error; err != nil {
		return nil, fmt.Errorf("list topology relation types: %w", err)
	}
	return relationTypes, nil
}

func (r *GORMTopologyRepository) FindTopologyRelationTypeByKey(ctx context.Context, typeKey string) (*model.TopologyRelationType, error) {
	var relationType model.TopologyRelationType
	if err := r.db.WithContext(ctx).Where("type_key = ?", typeKey).First(&relationType).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find topology relation type by key: %w", err)
	}
	return &relationType, nil
}

func (r *GORMTopologyRepository) FindTopologyRelationTypeByID(ctx context.Context, id int64) (*model.TopologyRelationType, error) {
	var relationType model.TopologyRelationType
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&relationType).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find topology relation type by id: %w", err)
	}
	return &relationType, nil
}

func (r *GORMTopologyRepository) CreateTopologyRelationType(ctx context.Context, relationType *model.TopologyRelationType) error {
	if err := r.db.WithContext(ctx).Create(relationType).Error; err != nil {
		return fmt.Errorf("create topology relation type: %w", err)
	}
	return nil
}

func (r *GORMTopologyRepository) UpdateTopologyRelationType(ctx context.Context, relationType *model.TopologyRelationType) error {
	if err := r.db.WithContext(ctx).Save(relationType).Error; err != nil {
		return fmt.Errorf("update topology relation type: %w", err)
	}
	return nil
}

func (r *GORMTopologyRepository) CreateTopologyTypeAudit(ctx context.Context, audit *model.TopologyTypeAudit) error {
	if err := r.db.WithContext(ctx).Create(audit).Error; err != nil {
		return fmt.Errorf("create topology type audit: %w", err)
	}
	return nil
}

func (r *GORMTopologyRepository) ListTopologySourceConfigs(ctx context.Context) ([]model.TopologySourceConfig, error) {
	var sources []model.TopologySourceConfig
	if err := r.db.WithContext(ctx).Order("priority DESC, id ASC").Find(&sources).Error; err != nil {
		return nil, fmt.Errorf("list topology source configs: %w", err)
	}
	return sources, nil
}

func (r *GORMTopologyRepository) FindTopologySourceConfigByID(ctx context.Context, id int64) (*model.TopologySourceConfig, error) {
	var source model.TopologySourceConfig
	if err := r.db.WithContext(ctx).First(&source, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find topology source config: %w", err)
	}
	return &source, nil
}

func (r *GORMTopologyRepository) CreateTopologySourceConfig(ctx context.Context, source *model.TopologySourceConfig) error {
	if err := r.db.WithContext(ctx).Create(source).Error; err != nil {
		return fmt.Errorf("create topology source config: %w", err)
	}
	return nil
}

func (r *GORMTopologyRepository) UpdateTopologySourceConfig(ctx context.Context, id int64, updates TopologySourceConfigUpdates) (*model.TopologySourceConfig, error) {
	values := map[string]any{"updated_at": time.Now().UTC()}
	if updates.Name != nil {
		values["name"] = *updates.Name
	}
	if updates.SourceType != nil {
		values["source_type"] = *updates.SourceType
	}
	if updates.DataSourceIDSet {
		values["data_source_id"] = updates.DataSourceID
	}
	if updates.Enabled != nil {
		values["enabled"] = *updates.Enabled
	}
	if updates.Priority != nil {
		values["priority"] = *updates.Priority
	}
	if updates.ScheduleSet {
		values["schedule"] = updates.Schedule
	}
	if updates.ScopeSet {
		values["scope"] = updates.Scope
	}
	if updates.MappingRulesSet {
		values["mapping_rules"] = updates.MappingRules
	}
	if updates.StaleAfterSeconds != nil {
		values["stale_after_seconds"] = *updates.StaleAfterSeconds
	}
	if updates.DeleteAfterSeconds != nil {
		values["delete_after_seconds"] = *updates.DeleteAfterSeconds
	}
	result := r.db.WithContext(ctx).Model(&model.TopologySourceConfig{}).Where("id = ?", id).Updates(values)
	if result.Error != nil {
		return nil, fmt.Errorf("update topology source config: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrNotFound
	}
	return r.FindTopologySourceConfigByID(ctx, id)
}

func (r *GORMTopologyRepository) DeleteTopologySourceConfig(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Delete(&model.TopologySourceConfig{}, id)
	if result.Error != nil {
		return fmt.Errorf("delete topology source config: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *GORMTopologyRepository) FindDataSourceByID(ctx context.Context, id int64) (*model.DataSource, error) {
	var dataSource model.DataSource
	if err := r.db.WithContext(ctx).First(&dataSource, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find data source by id: %w", err)
	}
	return &dataSource, nil
}
