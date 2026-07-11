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
	default:
		failure(c, http.StatusInternalServerError, 50096, fallback)
	}
	return true
}
