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
	RAGHandler          *RAGHandler
	DataSourceHandler   *DataSourceHandler
	AnalysisHandler     *AnalysisHandler
	EventHandler        *EventHandler
	EvidenceHandler     *EvidenceHandler
	ToolHandler         *ToolHandler
	SkillHandler        *SkillHandler
	AgentHandler        *AgentHandler
	WorkflowHandler     *WorkflowHandler
	SFTPHandler         *SFTPHandler
	K8sHandler          *K8sHandler
	MetricsHandler      *MetricsHandler
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
	if dependencies.EventHandler != nil {
		eventRoutes := router.Group("/api/events")
		eventRoutes.POST("/alertmanager", dependencies.EventHandler.Alertmanager)
		if dependencies.Authenticate != nil {
			protectedEventRoutes := eventRoutes.Group("")
			protectedEventRoutes.Use(dependencies.Authenticate)
			protectedEventRoutes.POST("/manual", dependencies.EventHandler.Manual)
			protectedEventRoutes.GET("", dependencies.EventHandler.List)
			protectedEventRoutes.GET("/:id", dependencies.EventHandler.Get)
		}
	}
	if dependencies.EvidenceHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		evidenceRoutes := router.Group("/api/evidence")
		evidenceRoutes.Use(dependencies.Authenticate)
		evidenceRoutes.GET("", dependencies.EvidenceHandler.List)
		evidenceRoutes.GET("/:idOrKey", dependencies.EvidenceHandler.Get)
		evidenceRoutes.POST("", dependencies.RequireAdmin, dependencies.EvidenceHandler.Create)
		evidenceRoutes.POST("/validate", dependencies.RequireAdmin, dependencies.EvidenceHandler.Validate)
	}
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
		if dependencies.RAGHandler != nil {
			knowledgeRoutes.POST("/ask", dependencies.RAGHandler.Ask)
		}
	}
	if dependencies.DataSourceHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		dataSourceRoutes := router.Group("/api/data-sources")
		dataSourceRoutes.Use(dependencies.Authenticate)
		dataSourceRoutes.GET("", dependencies.DataSourceHandler.List)
		dataSourceRoutes.GET("/:id", dependencies.DataSourceHandler.Get)
		dataSourceRoutes.POST("", dependencies.RequireAdmin, dependencies.DataSourceHandler.Create)
		dataSourceRoutes.PUT("/:id", dependencies.RequireAdmin, dependencies.DataSourceHandler.Update)
		dataSourceRoutes.DELETE("/:id", dependencies.RequireAdmin, dependencies.DataSourceHandler.Delete)
		dataSourceRoutes.POST("/:id/test", dependencies.RequireAdmin, dependencies.DataSourceHandler.Test)
	}
	if dependencies.ToolHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		toolRoutes := router.Group("/api/tools")
		toolRoutes.Use(dependencies.Authenticate)
		toolRoutes.GET("", dependencies.ToolHandler.List)
		toolRoutes.GET("/:name", dependencies.ToolHandler.Get)
		toolRoutes.POST("/:name/test", dependencies.RequireAdmin, dependencies.ToolHandler.Test)
		toolRoutes.POST("/:name/enable", dependencies.RequireAdmin, dependencies.ToolHandler.Enable)
		toolRoutes.POST("/:name/disable", dependencies.RequireAdmin, dependencies.ToolHandler.Disable)
	}
	if dependencies.SkillHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		skillRoutes := router.Group("/api/skills")
		skillRoutes.Use(dependencies.Authenticate)
		skillRoutes.GET("", dependencies.SkillHandler.List)
		skillRoutes.GET("/:name", dependencies.SkillHandler.Get)
		skillRoutes.POST("/:name/execute", dependencies.RequireAdmin, dependencies.SkillHandler.Execute)
		skillRoutes.POST("/:name/enable", dependencies.RequireAdmin, dependencies.SkillHandler.Enable)
		skillRoutes.POST("/:name/disable", dependencies.RequireAdmin, dependencies.SkillHandler.Disable)

		skillRunRoutes := router.Group("/api/skill-runs")
		skillRunRoutes.Use(dependencies.Authenticate, dependencies.RequireAdmin)
		skillRunRoutes.GET("", dependencies.SkillHandler.ListRuns)
	}
	if dependencies.AgentHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		agentRoutes := router.Group("/api/agents")
		agentRoutes.Use(dependencies.Authenticate)
		agentRoutes.GET("", dependencies.AgentHandler.List)
		agentRoutes.GET("/:name", dependencies.AgentHandler.Get)
		agentRoutes.POST("/:name/test", dependencies.RequireAdmin, dependencies.AgentHandler.Test)

		agentRunRoutes := router.Group("/api/agent-runs")
		agentRunRoutes.Use(dependencies.Authenticate, dependencies.RequireAdmin)
		agentRunRoutes.GET("", dependencies.AgentHandler.ListRuns)
		agentRunRoutes.GET("/:id", dependencies.AgentHandler.GetRun)
	}
	if dependencies.WorkflowHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		workflowRoutes := router.Group("/api/workflows")
		workflowRoutes.Use(dependencies.Authenticate)
		workflowRoutes.GET("", dependencies.WorkflowHandler.List)
		workflowRoutes.POST("", dependencies.RequireAdmin, dependencies.WorkflowHandler.Create)
		workflowRoutes.GET("/:id", dependencies.WorkflowHandler.Get)
		workflowRoutes.PUT("/:id", dependencies.RequireAdmin, dependencies.WorkflowHandler.Update)
		workflowRoutes.POST("/:id/validate", dependencies.RequireAdmin, dependencies.WorkflowHandler.Validate)
		workflowRoutes.POST("/:id/run", dependencies.RequireAdmin, dependencies.WorkflowHandler.Run)

		workflowRunRoutes := router.Group("/api/workflow-runs")
		workflowRunRoutes.Use(dependencies.Authenticate, dependencies.RequireAdmin)
		workflowRunRoutes.GET("", dependencies.WorkflowHandler.ListRuns)
		workflowRunRoutes.GET("/:id", dependencies.WorkflowHandler.GetRun)
		workflowRunRoutes.POST("/:id/cancel", dependencies.WorkflowHandler.CancelRun)
	}
	if dependencies.AnalysisHandler != nil && dependencies.Authenticate != nil {
		analysisRoutes := router.Group("/api/analysis")
		analysisRoutes.Use(dependencies.Authenticate)
		analysisRoutes.POST("/logs", dependencies.AnalysisHandler.QueryLogs)
		analysisRoutes.POST("/logs/preprocess", dependencies.AnalysisHandler.PreprocessLogs)
		analysisRoutes.POST("/general", dependencies.AnalysisHandler.RunGeneral)
		analysisRoutes.GET("/tasks", dependencies.AnalysisHandler.ListTasks)
		analysisRoutes.GET("/tasks/:id", dependencies.AnalysisHandler.GetTask)
		if dependencies.SFTPHandler != nil {
			analysisRoutes.POST("/sftp/read", dependencies.SFTPHandler.ReadFile)
		}
		if dependencies.K8sHandler != nil {
			analysisRoutes.POST("/k8s/test", dependencies.K8sHandler.Test)
			analysisRoutes.POST("/k8s/resources", dependencies.K8sHandler.Resources)
			analysisRoutes.POST("/k8s/pod-diagnose", dependencies.K8sHandler.DiagnosePod)
		}
		if dependencies.MetricsHandler != nil {
			analysisRoutes.POST("/metrics/test", dependencies.MetricsHandler.Test)
			analysisRoutes.POST("/metrics/query", dependencies.MetricsHandler.Query)
		}
	}
	return router
}
