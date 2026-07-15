package handler

import (
	"fmt"
	"net/http"

	appmiddleware "aiops-platform/backend/internal/middleware"
	"github.com/gin-gonic/gin"
)

func success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": data})
}

func failure(c *gin.Context, status, code int, message string) {
	if len(c.Errors) == 0 {
		_ = c.Error(fmt.Errorf("%s", message))
	}
	c.JSON(status, gin.H{
		"code":      code,
		"message":   message,
		"data":      nil,
		"requestId": appmiddleware.GetRequestID(c),
	})
}

func recordFailureError(c *gin.Context, err error, operation string) {
	if err == nil {
		return
	}
	if operation == "" {
		_ = c.Error(err)
		return
	}
	_ = c.Error(fmt.Errorf("%s: %w", operation, err))
}
