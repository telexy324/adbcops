package repository

import (
	"context"
	"fmt"

	"aiops-platform/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TopologyRepository interface {
	UpsertNode(ctx context.Context, node *model.TopologyNode) error
	UpsertEdge(ctx context.Context, edge *model.TopologyEdge) error
	ListNodes(ctx context.Context, filters TopologyFilters) ([]model.TopologyNode, error)
	ListEdges(ctx context.Context, filters TopologyFilters) ([]model.TopologyEdge, error)
}

type TopologyFilters struct {
	Environment string
	Cluster     string
	Namespace   string
	Kind        string
	Limit       int
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
			"kind", "name", "display_name", "environment", "cluster", "namespace", "labels", "properties", "source_type", "source_ref", "updated_at",
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
			"from_node_key", "to_node_key", "edge_type", "confidence", "properties", "source_type", "source_ref", "updated_at",
		}),
	}).Create(edge).Error; err != nil {
		return fmt.Errorf("upsert topology edge: %w", err)
	}
	return nil
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
