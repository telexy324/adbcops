package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"aiops-platform/backend/internal/repository"
	topologysvc "aiops-platform/backend/internal/topology"
	"github.com/gin-gonic/gin"
)

type TopologyHandler struct {
	service *topologysvc.Service
}

func NewTopologyHandler(service *topologysvc.Service) *TopologyHandler {
	return &TopologyHandler{service: service}
}

func (h *TopologyHandler) Graph(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	graph, err := h.service.Graph(c.Request.Context(), topologysvc.Query{
		Environment: c.Query("environment"),
		Cluster:     c.Query("cluster"),
		Namespace:   c.Query("namespace"),
		Kind:        c.Query("kind"),
		Limit:       limit,
	})
	if handleTopologyError(c, err, "query topology failed") {
		return
	}
	success(c, graph)
}

func (h *TopologyHandler) FindNode(c *gin.Context) {
	var request topologysvc.FindNodeInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.FindNode(c.Request.Context(), request)
	if handleTopologyError(c, err, "find topology node failed") {
		return
	}
	success(c, result)
}

func (h *TopologyHandler) ListNodeTypes(c *gin.Context) {
	nodeTypes, err := h.service.ListNodeTypes(c.Request.Context())
	if handleTopologyError(c, err, "list topology node types failed") {
		return
	}
	success(c, nodeTypes)
}

func (h *TopologyHandler) CreateNodeType(c *gin.Context) {
	var request topologysvc.NodeTypeInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	nodeType, err := h.service.CreateNodeType(c.Request.Context(), request)
	if handleTopologyError(c, err, "create topology node type failed") {
		return
	}
	success(c, nodeType)
}

func (h *TopologyHandler) UpdateNodeType(c *gin.Context) {
	id, ok := idFromParam(c)
	if !ok {
		return
	}
	var request topologysvc.NodeTypeInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	nodeType, err := h.service.UpdateNodeType(c.Request.Context(), id, request)
	if handleTopologyError(c, err, "update topology node type failed") {
		return
	}
	success(c, nodeType)
}

func (h *TopologyHandler) EnableNodeType(c *gin.Context) {
	h.setNodeTypeEnabled(c, true)
}

func (h *TopologyHandler) DisableNodeType(c *gin.Context) {
	h.setNodeTypeEnabled(c, false)
}

func (h *TopologyHandler) setNodeTypeEnabled(c *gin.Context, enabled bool) {
	id, ok := idFromParam(c)
	if !ok {
		return
	}
	nodeType, err := h.service.SetNodeTypeEnabled(c.Request.Context(), id, enabled)
	if handleTopologyError(c, err, "update topology node type status failed") {
		return
	}
	success(c, nodeType)
}

func (h *TopologyHandler) ListRelationTypes(c *gin.Context) {
	relationTypes, err := h.service.ListRelationTypes(c.Request.Context())
	if handleTopologyError(c, err, "list topology relation types failed") {
		return
	}
	success(c, relationTypes)
}

func (h *TopologyHandler) CreateRelationType(c *gin.Context) {
	var request topologysvc.RelationTypeInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	relationType, err := h.service.CreateRelationType(c.Request.Context(), request)
	if handleTopologyError(c, err, "create topology relation type failed") {
		return
	}
	success(c, relationType)
}

func (h *TopologyHandler) UpdateRelationType(c *gin.Context) {
	id, ok := idFromParam(c)
	if !ok {
		return
	}
	var request topologysvc.RelationTypeInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	var actorID *int64
	if actor, exists := currentUser(c); exists {
		actorID = &actor.ID
	}
	relationType, err := h.service.UpdateRelationType(c.Request.Context(), id, actorID, request)
	if handleTopologyError(c, err, "update topology relation type failed") {
		return
	}
	success(c, relationType)
}

func (h *TopologyHandler) EnableRelationType(c *gin.Context) {
	h.setRelationTypeEnabled(c, true)
}

func (h *TopologyHandler) DisableRelationType(c *gin.Context) {
	h.setRelationTypeEnabled(c, false)
}

func (h *TopologyHandler) setRelationTypeEnabled(c *gin.Context, enabled bool) {
	id, ok := idFromParam(c)
	if !ok {
		return
	}
	relationType, err := h.service.SetRelationTypeEnabled(c.Request.Context(), id, enabled)
	if handleTopologyError(c, err, "update topology relation type status failed") {
		return
	}
	success(c, relationType)
}

func (h *TopologyHandler) ListSources(c *gin.Context) {
	sources, err := h.service.ListSourceConfigs(c.Request.Context())
	if handleTopologyError(c, err, "list topology sources failed") {
		return
	}
	success(c, sources)
}

func (h *TopologyHandler) GetSource(c *gin.Context) {
	id, ok := idFromParam(c)
	if !ok {
		return
	}
	source, err := h.service.GetSourceConfig(c.Request.Context(), id)
	if handleTopologyError(c, err, "get topology source failed") {
		return
	}
	success(c, source)
}

func (h *TopologyHandler) CreateSource(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request topologysvc.SourceConfigInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	request.CreatedBy = &actor.ID
	source, err := h.service.CreateSourceConfig(c.Request.Context(), request)
	if handleTopologyError(c, err, "create topology source failed") {
		return
	}
	success(c, source)
}

func (h *TopologyHandler) UpdateSource(c *gin.Context) {
	id, ok := idFromParam(c)
	if !ok {
		return
	}
	var request topologysvc.SourceConfigInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	source, err := h.service.UpdateSourceConfig(c.Request.Context(), id, request)
	if handleTopologyError(c, err, "update topology source failed") {
		return
	}
	success(c, source)
}

func (h *TopologyHandler) DeleteSource(c *gin.Context) {
	id, ok := idFromParam(c)
	if !ok {
		return
	}
	if err := h.service.DeleteSourceConfig(c.Request.Context(), id); handleTopologyError(c, err, "delete topology source failed") {
		return
	}
	success(c, gin.H{"deleted": true})
}

func (h *TopologyHandler) TestSource(c *gin.Context) {
	id, ok := idFromParam(c)
	if !ok {
		return
	}
	result, err := h.service.TestSourceConfig(c.Request.Context(), id)
	if handleTopologyError(c, err, "test topology source failed") {
		return
	}
	success(c, result)
}

func (h *TopologyHandler) PreviewSource(c *gin.Context) {
	id, ok := idFromParam(c)
	if !ok {
		return
	}
	var request topologysvc.MappingPreviewInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.PreviewSourceMapping(c.Request.Context(), id, request)
	if handleTopologyError(c, err, "preview topology mapping failed") {
		return
	}
	success(c, result)
}

func (h *TopologyHandler) Upstream(c *gin.Context) {
	result, err := h.service.Upstream(c.Request.Context(), traversalQueryFromRequest(c))
	if handleTopologyError(c, err, "query upstream topology failed") {
		return
	}
	success(c, result)
}

func (h *TopologyHandler) Downstream(c *gin.Context) {
	result, err := h.service.Downstream(c.Request.Context(), traversalQueryFromRequest(c))
	if handleTopologyError(c, err, "query downstream topology failed") {
		return
	}
	success(c, result)
}

func (h *TopologyHandler) BlastRadius(c *gin.Context) {
	result, err := h.service.BlastRadius(c.Request.Context(), traversalQueryFromRequest(c))
	if handleTopologyError(c, err, "query topology blast radius failed") {
		return
	}
	success(c, result)
}

func (h *TopologyHandler) CommonDependencies(c *gin.Context) {
	hops, _ := strconv.Atoi(c.Query("hops"))
	maxNodes, _ := strconv.Atoi(c.Query("maxNodes"))
	nodeKeys := strings.Split(c.Query("nodeKeys"), ",")
	result, err := h.service.CommonDependencies(c.Request.Context(), topologysvc.CommonDependencyQuery{
		NodeKeys:    nodeKeys,
		Hops:        hops,
		MaxNodes:    maxNodes,
		Environment: c.Query("environment"),
		Cluster:     c.Query("cluster"),
		Namespace:   c.Query("namespace"),
	})
	if handleTopologyError(c, err, "query common topology dependencies failed") {
		return
	}
	success(c, result)
}

func (h *TopologyHandler) UpsertNode(c *gin.Context) {
	var request topologysvc.NodeInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	node, err := h.service.UpsertNode(c.Request.Context(), request)
	if handleTopologyError(c, err, "upsert topology node failed") {
		return
	}
	success(c, node)
}

func (h *TopologyHandler) ListNodeAliases(c *gin.Context) {
	id, ok := idFromParam(c)
	if !ok {
		return
	}
	aliases, err := h.service.ListNodeAliases(c.Request.Context(), id)
	if handleTopologyError(c, err, "list topology node aliases failed") {
		return
	}
	success(c, aliases)
}

func (h *TopologyHandler) AddNodeAlias(c *gin.Context) {
	id, ok := idFromParam(c)
	if !ok {
		return
	}
	var request topologysvc.AliasInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	alias, err := h.service.AddNodeAlias(c.Request.Context(), id, request)
	if handleTopologyError(c, err, "add topology node alias failed") {
		return
	}
	success(c, alias)
}

func (h *TopologyHandler) DeleteNodeAlias(c *gin.Context) {
	aliasID, err := strconv.ParseInt(c.Param("aliasId"), 10, 64)
	if err != nil || aliasID <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	if err := h.service.DeleteNodeAlias(c.Request.Context(), aliasID); handleTopologyError(c, err, "delete topology node alias failed") {
		return
	}
	success(c, gin.H{"deleted": true})
}

func (h *TopologyHandler) UpsertEdge(c *gin.Context) {
	var request topologysvc.EdgeInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	edge, err := h.service.UpsertEdge(c.Request.Context(), request)
	if handleTopologyError(c, err, "upsert topology edge failed") {
		return
	}
	success(c, edge)
}

func (h *TopologyHandler) SyncK8s(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request topologysvc.SyncK8sInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.SyncK8s(c.Request.Context(), actor, request)
	if handleTopologyError(c, err, "sync kubernetes topology failed") {
		return
	}
	success(c, result)
}

func (h *TopologyHandler) SyncTrace(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request topologysvc.TraceServiceGraphInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.SyncTraceServiceGraph(c.Request.Context(), actor, request)
	if handleTopologyError(c, err, "sync trace service graph failed") {
		return
	}
	success(c, result)
}

func (h *TopologyHandler) SyncComponent(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request topologysvc.ComponentTopologyInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	result, err := h.service.SyncComponentTopology(c.Request.Context(), actor, request)
	if handleTopologyError(c, err, "sync component topology failed") {
		return
	}
	success(c, result)
}

func (h *TopologyHandler) RunSourceSync(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := idFromParam(c)
	if !ok {
		return
	}
	var request topologysvc.RunTopologySyncInput
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	request.SourceConfigID = id
	run, err := h.service.RunTopologySync(c.Request.Context(), actor, request)
	if handleTopologyError(c, err, "run topology sync failed") {
		return
	}
	success(c, run)
}

func (h *TopologyHandler) ListSyncRuns(c *gin.Context) {
	sourceConfigID, _ := strconv.ParseInt(c.Query("sourceConfigId"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	runs, err := h.service.ListTopologySyncRuns(c.Request.Context(), sourceConfigID, limit)
	if handleTopologyError(c, err, "list topology sync runs failed") {
		return
	}
	success(c, runs)
}

func idFromParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return 0, false
	}
	return id, true
}

func traversalQueryFromRequest(c *gin.Context) topologysvc.TraversalQuery {
	hops, _ := strconv.Atoi(c.Query("hops"))
	maxNodes, _ := strconv.Atoi(c.Query("maxNodes"))
	return topologysvc.TraversalQuery{
		NodeKey:     c.Query("nodeKey"),
		Hops:        hops,
		MaxNodes:    maxNodes,
		Environment: c.Query("environment"),
		Cluster:     c.Query("cluster"),
		Namespace:   c.Query("namespace"),
	}
}

func handleTopologyError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, topologysvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, topologysvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40301, "forbidden")
	case errors.Is(err, topologysvc.ErrNodeLimitExceeded):
		failure(c, http.StatusBadRequest, 40002, "topology node limit exceeded")
	case errors.Is(err, topologysvc.ErrTopologyNodeAbsent), errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "topology node not found")
	case errors.Is(err, topologysvc.ErrTopologyTypeDisabled):
		failure(c, http.StatusBadRequest, 40003, "topology type disabled")
	case errors.Is(err, topologysvc.ErrTopologyTypeBuiltIn):
		failure(c, http.StatusBadRequest, 40004, "built-in topology type is protected")
	case errors.Is(err, topologysvc.ErrUnsupportedSource):
		failure(c, http.StatusBadRequest, 40005, "unsupported topology source")
	case errors.Is(err, topologysvc.ErrSensitiveConfig):
		failure(c, http.StatusBadRequest, 40006, "topology config contains sensitive fields")
	case errors.Is(err, topologysvc.ErrSyncAlreadyRunning):
		failure(c, http.StatusConflict, 40901, "topology sync already running")
	default:
		failure(c, http.StatusInternalServerError, 50096, fallback)
	}
	return true
}
