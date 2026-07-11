package handler

import (
	"errors"
	"net/http"
	"strconv"

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

func handleTopologyError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, topologysvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, topologysvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40301, "forbidden")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "not found")
	default:
		failure(c, http.StatusInternalServerError, 50096, fallback)
	}
	return true
}
