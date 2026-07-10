package handler

import (
	"net/http"

	appmiddleware "aiops-platform/backend/internal/middleware"
	"github.com/gin-gonic/gin"
)

func success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": data})
}

func failure(c *gin.Context, status, code int, message string) {
	c.JSON(status, gin.H{
		"code":      code,
		"message":   message,
		"data":      nil,
		"requestId": appmiddleware.GetRequestID(c),
	})
}
