package handler

import (
	"github.com/gin-gonic/gin"
)

func health(c *gin.Context) {
	success(c, gin.H{
		"status": "ok",
	})
}
