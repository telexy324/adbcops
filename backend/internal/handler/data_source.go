package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	datasourcesvc "aiops-platform/backend/internal/datasource"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

type DataSourceHandler struct {
	service *datasourcesvc.Service
}

func NewDataSourceHandler(service *datasourcesvc.Service) *DataSourceHandler {
	return &DataSourceHandler{service: service}
}

type saveDataSourceRequest struct {
	Name          string          `json:"name" binding:"required"`
	SourceType    string          `json:"sourceType" binding:"required"`
	Environment   *string         `json:"environment"`
	SystemName    *string         `json:"systemName"`
	ComponentName *string         `json:"componentName"`
	Config        json.RawMessage `json:"config"`
	Credential    json.RawMessage `json:"credential"`
	Enabled       *bool           `json:"enabled"`
	ReadOnly      *bool           `json:"readOnly"`
}

type updateDataSourceRequest struct {
	Name          *string         `json:"name"`
	SourceType    *string         `json:"sourceType"`
	Environment   *string         `json:"environment"`
	SystemName    *string         `json:"systemName"`
	ComponentName *string         `json:"componentName"`
	Config        json.RawMessage `json:"config"`
	Credential    json.RawMessage `json:"credential"`
	Enabled       *bool           `json:"enabled"`
	ReadOnly      *bool           `json:"readOnly"`
}

func (h *DataSourceHandler) List(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	dataSources, err := h.service.List(c.Request.Context(), actor)
	if handleDataSourceError(c, err, "list data sources failed") {
		return
	}
	success(c, dataSources)
}

func (h *DataSourceHandler) Create(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	var request saveDataSourceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	readOnly := true
	if request.ReadOnly != nil {
		readOnly = *request.ReadOnly
	}
	createdBy := actor.ID
	dataSource, err := h.service.Create(c.Request.Context(), actor, datasourcesvc.SaveInput{
		Name:          request.Name,
		SourceType:    request.SourceType,
		Environment:   request.Environment,
		SystemName:    request.SystemName,
		ComponentName: request.ComponentName,
		Config:        request.Config,
		Credential:    request.Credential,
		Enabled:       enabled,
		ReadOnly:      readOnly,
		CreatedBy:     &createdBy,
	})
	if handleDataSourceError(c, err, "create data source failed") {
		return
	}
	success(c, dataSource)
}

func (h *DataSourceHandler) Get(c *gin.Context) {
	actor, id, ok := currentUserAndDataSourceID(c)
	if !ok {
		return
	}
	dataSource, err := h.service.Get(c.Request.Context(), actor, id)
	if handleDataSourceError(c, err, "get data source failed") {
		return
	}
	success(c, dataSource)
}

func (h *DataSourceHandler) Update(c *gin.Context) {
	actor, id, ok := currentUserAndDataSourceID(c)
	if !ok {
		return
	}
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	var request updateDataSourceRequest
	if value, ok := raw["name"]; ok {
		var name string
		if err := json.Unmarshal(value, &name); err != nil {
			failure(c, http.StatusBadRequest, 40001, "invalid request")
			return
		}
		request.Name = &name
	}
	if value, ok := raw["sourceType"]; ok {
		var sourceType string
		if err := json.Unmarshal(value, &sourceType); err != nil {
			failure(c, http.StatusBadRequest, 40001, "invalid request")
			return
		}
		request.SourceType = &sourceType
	}
	if value, ok := raw["environment"]; ok {
		environment, ok := optionalJSONString(c, value)
		if !ok {
			return
		}
		request.Environment = environment
	}
	if value, ok := raw["systemName"]; ok {
		systemName, ok := optionalJSONString(c, value)
		if !ok {
			return
		}
		request.SystemName = systemName
	}
	if value, ok := raw["componentName"]; ok {
		componentName, ok := optionalJSONString(c, value)
		if !ok {
			return
		}
		request.ComponentName = componentName
	}
	if value, ok := raw["config"]; ok {
		request.Config = value
	}
	if value, ok := raw["credential"]; ok {
		request.Credential = value
	}
	if value, ok := raw["enabled"]; ok {
		var enabled bool
		if err := json.Unmarshal(value, &enabled); err != nil {
			failure(c, http.StatusBadRequest, 40001, "invalid request")
			return
		}
		request.Enabled = &enabled
	}
	if value, ok := raw["readOnly"]; ok {
		var readOnly bool
		if err := json.Unmarshal(value, &readOnly); err != nil {
			failure(c, http.StatusBadRequest, 40001, "invalid request")
			return
		}
		request.ReadOnly = &readOnly
	}
	dataSource, err := h.service.Update(c.Request.Context(), actor, id, datasourcesvc.UpdateInput{
		Name:           request.Name,
		SourceType:     request.SourceType,
		Environment:    request.Environment,
		EnvironmentSet: hasKey(raw, "environment"),
		SystemName:     request.SystemName,
		SystemNameSet:  hasKey(raw, "systemName"),
		ComponentName:  request.ComponentName,
		ComponentSet:   hasKey(raw, "componentName"),
		Config:         request.Config,
		Credential:     request.Credential,
		Enabled:        request.Enabled,
		ReadOnly:       request.ReadOnly,
	})
	if handleDataSourceError(c, err, "update data source failed") {
		return
	}
	success(c, dataSource)
}

func (h *DataSourceHandler) Delete(c *gin.Context) {
	actor, id, ok := currentUserAndDataSourceID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), actor, id); handleDataSourceError(c, err, "delete data source failed") {
		return
	}
	success(c, gin.H{"deleted": true})
}

func (h *DataSourceHandler) Test(c *gin.Context) {
	actor, id, ok := currentUserAndDataSourceID(c)
	if !ok {
		return
	}
	result, err := h.service.Test(c.Request.Context(), actor, id)
	if handleDataSourceError(c, err, "test data source failed") {
		return
	}
	success(c, result)
}

func currentUserAndDataSourceID(c *gin.Context) (*model.AppUser, int64, bool) {
	actor, ok := currentUser(c)
	if !ok {
		return nil, 0, false
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return nil, 0, false
	}
	return actor, id, true
}

func handleDataSourceError(c *gin.Context, err error, fallback string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, datasourcesvc.ErrSensitiveConfig):
		failure(c, http.StatusBadRequest, 40007, "config contains sensitive credential fields")
	case errors.Is(err, datasourcesvc.ErrUnsafeEndpoint):
		failure(c, http.StatusBadRequest, 40010, "endpoint is not allowed")
	case errors.Is(err, datasourcesvc.ErrInvalidInput):
		failure(c, http.StatusBadRequest, 40001, "invalid request")
	case errors.Is(err, datasourcesvc.ErrAdminRequired), errors.Is(err, datasourcesvc.ErrForbidden):
		failure(c, http.StatusForbidden, 40306, "data source access forbidden")
	case errors.Is(err, repository.ErrNotFound):
		failure(c, http.StatusNotFound, 40401, "data source not found")
	default:
		failure(c, http.StatusInternalServerError, 50060, fallback)
	}
	return true
}

func optionalJSONString(c *gin.Context, raw json.RawMessage) (*string, bool) {
	if string(raw) == "null" {
		return nil, true
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		failure(c, http.StatusBadRequest, 40001, "invalid request")
		return nil, false
	}
	return &value, true
}

func hasKey(raw map[string]json.RawMessage, key string) bool {
	_, ok := raw[key]
	return ok
}
