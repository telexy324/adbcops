package handler

import (
	"log/slog"

	appmiddleware "aiops-platform/backend/internal/middleware"
	"github.com/gin-gonic/gin"
)

type RouterDependencies struct {
	AuthHandler         *AuthHandler
	UserHandler         *UserHandler
	ConversationHandler *ConversationHandler
	LLMHandler          *LLMHandler
	DocumentHandler     *DocumentHandler
	Authenticate        gin.HandlerFunc
	RequireAdmin        gin.HandlerFunc
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
	if dependencies.UserHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		userRoutes := router.Group("/api/users")
		userRoutes.Use(dependencies.Authenticate, dependencies.RequireAdmin)
		userRoutes.GET("", dependencies.UserHandler.List)
		userRoutes.POST("", dependencies.UserHandler.Create)
		userRoutes.PUT("/:id", dependencies.UserHandler.Update)
		userRoutes.POST("/:id/reset-password", dependencies.UserHandler.ResetPassword)
		userRoutes.POST("/:id/enable", dependencies.UserHandler.Enable)
		userRoutes.POST("/:id/disable", dependencies.UserHandler.Disable)
	}
	if dependencies.ConversationHandler != nil && dependencies.Authenticate != nil {
		conversationRoutes := router.Group("/api/conversations")
		conversationRoutes.Use(dependencies.Authenticate)
		conversationRoutes.GET("", dependencies.ConversationHandler.List)
		conversationRoutes.POST("", dependencies.ConversationHandler.Create)
		conversationRoutes.GET("/:id", dependencies.ConversationHandler.Get)
		conversationRoutes.DELETE("/:id", dependencies.ConversationHandler.Delete)
		conversationRoutes.GET("/:id/summary", dependencies.ConversationHandler.Summary)
		conversationRoutes.POST("/:id/messages", dependencies.ConversationHandler.AddMessage)
	}
	if dependencies.LLMHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		llmRoutes := router.Group("/api/llm-configs")
		llmRoutes.Use(dependencies.Authenticate, dependencies.RequireAdmin)
		llmRoutes.GET("", dependencies.LLMHandler.List)
		llmRoutes.POST("", dependencies.LLMHandler.Create)
		llmRoutes.GET("/:id", dependencies.LLMHandler.Get)
		llmRoutes.PUT("/:id", dependencies.LLMHandler.Update)
		llmRoutes.DELETE("/:id", dependencies.LLMHandler.Delete)
		llmRoutes.POST("/:id/default", dependencies.LLMHandler.SetDefault)
		llmRoutes.POST("/:id/test", dependencies.LLMHandler.Test)
	}
	if dependencies.DocumentHandler != nil && dependencies.Authenticate != nil {
		documentRoutes := router.Group("/api/documents")
		documentRoutes.Use(dependencies.Authenticate)
		documentRoutes.POST("/upload", dependencies.DocumentHandler.Upload)
		documentRoutes.GET("", dependencies.DocumentHandler.List)
		documentRoutes.GET("/:id", dependencies.DocumentHandler.Get)
		documentRoutes.GET("/:id/chunks", dependencies.DocumentHandler.Chunks)
		if dependencies.RequireAdmin != nil {
			documentRoutes.POST("/:id/review", dependencies.RequireAdmin, dependencies.DocumentHandler.Review)
		}
		documentRoutes.POST("/:id/reprocess", dependencies.DocumentHandler.Reprocess)

		knowledgeRoutes := router.Group("/api/knowledge")
		knowledgeRoutes.Use(dependencies.Authenticate)
		knowledgeRoutes.POST("/search", dependencies.DocumentHandler.Search)
	}
	return router
}
