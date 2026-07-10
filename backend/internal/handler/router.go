package handler

import (
	"log/slog"

	appmiddleware "aiops-platform/backend/internal/middleware"
	"github.com/gin-gonic/gin"
)

type RouterDependencies struct {
	AuthHandler  *AuthHandler
	Authenticate gin.HandlerFunc
}

// NewRouter creates the HTTP router and installs the common middleware stack.
func NewRouter(logger *slog.Logger, dependencies RouterDependencies) *gin.Engine {
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	router.Use(
		appmiddleware.RequestID(),
		appmiddleware.Logger(logger),
		appmiddleware.Recovery(logger),
	)

	router.GET("/api/health", health)
	if dependencies.AuthHandler != nil && dependencies.Authenticate != nil {
		authRoutes := router.Group("/api/auth")
		authRoutes.POST("/login", dependencies.AuthHandler.Login)

		protectedAuthRoutes := authRoutes.Group("")
		protectedAuthRoutes.Use(dependencies.Authenticate)
		protectedAuthRoutes.GET("/me", dependencies.AuthHandler.Me)
		protectedAuthRoutes.POST("/change-password", dependencies.AuthHandler.ChangePassword)
		protectedAuthRoutes.POST("/logout", dependencies.AuthHandler.Logout)
	}
	return router
}
