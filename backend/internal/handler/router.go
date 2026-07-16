package handler

import (
	"log/slog"

	appmiddleware "aiops-platform/backend/internal/middleware"
	"github.com/gin-gonic/gin"
)

type RouterDependencies struct {
	AuditHandler               *AuditHandler
	AuditRecorder              appmiddleware.AuditRecorder
	AuthHandler                *AuthHandler
	UserHandler                *UserHandler
	ConversationHandler        *ConversationHandler
	LLMHandler                 *LLMHandler
	DocumentHandler            *DocumentHandler
	QualityStandardHandler     *QualityStandardHandler
	QualityEvaluationHandler   *QualityEvaluationHandler
	EmbeddingIndexHandler      *EmbeddingIndexHandler
	RetrievalEvaluationHandler *RetrievalEvaluationHandler
	RAGHandler                 *RAGHandler
	DataSourceHandler          *DataSourceHandler
	AnalysisHandler            *AnalysisHandler
	EventHandler               *EventHandler
	EvidenceHandler            *EvidenceHandler
	TopologyHandler            *TopologyHandler
	TimelineHandler            *TimelineHandler
	CorrelationHandler         *CorrelationHandler
	IncidentHandler            *IncidentHandler
	ToolHandler                *ToolHandler
	SkillHandler               *SkillHandler
	AgentHandler               *AgentHandler
	WorkflowHandler            *WorkflowHandler
	SFTPHandler                *SFTPHandler
	K8sHandler                 *K8sHandler
	MetricsHandler             *MetricsHandler
	ReadinessCheck             ReadinessChecker
	Authenticate               gin.HandlerFunc
	RequireAdmin               gin.HandlerFunc
}

// NewRouter creates the HTTP router and installs the common middleware stack.
func NewRouter(logger *slog.Logger, dependencies RouterDependencies) *gin.Engine {
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	router.Use(
		appmiddleware.RequestID(),
		appmiddleware.CORS(),
		appmiddleware.Logger(logger),
		appmiddleware.Metrics(),
		appmiddleware.Audit(dependencies.AuditRecorder, logger),
		appmiddleware.Recovery(logger),
	)

	router.GET("/api/health", health)
	router.GET("/api/live", liveness)
	router.GET("/api/ready", readiness(dependencies.ReadinessCheck))
	router.GET("/api/metrics", platformMetrics)
	if dependencies.AuditHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		auditRoutes := router.Group("/api/audit-logs")
		auditRoutes.Use(dependencies.Authenticate, dependencies.RequireAdmin)
		auditRoutes.GET("", dependencies.AuditHandler.List)
	}
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
	if dependencies.TopologyHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		topologyRoutes := router.Group("/api/topology")
		topologyRoutes.Use(dependencies.Authenticate)
		topologyRoutes.GET("/graph", dependencies.TopologyHandler.Graph)
		topologyRoutes.GET("/upstream", dependencies.TopologyHandler.Upstream)
		topologyRoutes.GET("/downstream", dependencies.TopologyHandler.Downstream)
		topologyRoutes.GET("/expand", dependencies.TopologyHandler.Expand)
		topologyRoutes.POST("/explain-path", dependencies.TopologyHandler.ExplainPath)
		topologyRoutes.GET("/common-dependencies", dependencies.TopologyHandler.CommonDependencies)
		topologyRoutes.GET("/blast-radius", dependencies.TopologyHandler.BlastRadius)
		topologyRoutes.POST("/find-node", dependencies.TopologyHandler.FindNode)
		topologyRoutes.GET("/views", dependencies.TopologyHandler.ListSavedViews)
		topologyRoutes.POST("/views", dependencies.TopologyHandler.CreateSavedView)
		topologyRoutes.GET("/views/:id", dependencies.TopologyHandler.GetSavedView)
		topologyRoutes.PUT("/views/:id", dependencies.TopologyHandler.UpdateSavedView)
		topologyRoutes.DELETE("/views/:id", dependencies.TopologyHandler.DeleteSavedView)
		topologyRoutes.POST("/views/:id/clone", dependencies.TopologyHandler.CloneSavedView)
		topologyRoutes.GET("/conflicts", dependencies.TopologyHandler.ListConflicts)
		topologyRoutes.GET("/conflicts/:id", dependencies.TopologyHandler.GetConflict)
		topologyRoutes.POST("/conflicts/:id/resolve", dependencies.RequireAdmin, dependencies.TopologyHandler.ResolveConflict)
		topologyRoutes.GET("/node-types", dependencies.TopologyHandler.ListNodeTypes)
		topologyRoutes.POST("/node-types", dependencies.RequireAdmin, dependencies.TopologyHandler.CreateNodeType)
		topologyRoutes.PUT("/node-types/:id", dependencies.RequireAdmin, dependencies.TopologyHandler.UpdateNodeType)
		topologyRoutes.POST("/node-types/:id/enable", dependencies.RequireAdmin, dependencies.TopologyHandler.EnableNodeType)
		topologyRoutes.POST("/node-types/:id/disable", dependencies.RequireAdmin, dependencies.TopologyHandler.DisableNodeType)
		topologyRoutes.GET("/relation-types", dependencies.TopologyHandler.ListRelationTypes)
		topologyRoutes.POST("/relation-types", dependencies.RequireAdmin, dependencies.TopologyHandler.CreateRelationType)
		topologyRoutes.PUT("/relation-types/:id", dependencies.RequireAdmin, dependencies.TopologyHandler.UpdateRelationType)
		topologyRoutes.POST("/relation-types/:id/enable", dependencies.RequireAdmin, dependencies.TopologyHandler.EnableRelationType)
		topologyRoutes.POST("/relation-types/:id/disable", dependencies.RequireAdmin, dependencies.TopologyHandler.DisableRelationType)
		topologyRoutes.GET("/sources", dependencies.TopologyHandler.ListSources)
		topologyRoutes.POST("/sources", dependencies.RequireAdmin, dependencies.TopologyHandler.CreateSource)
		topologyRoutes.GET("/sources/:id", dependencies.TopologyHandler.GetSource)
		topologyRoutes.PUT("/sources/:id", dependencies.RequireAdmin, dependencies.TopologyHandler.UpdateSource)
		topologyRoutes.DELETE("/sources/:id", dependencies.RequireAdmin, dependencies.TopologyHandler.DeleteSource)
		topologyRoutes.POST("/sources/:id/test", dependencies.RequireAdmin, dependencies.TopologyHandler.TestSource)
		topologyRoutes.POST("/sources/:id/preview", dependencies.RequireAdmin, dependencies.TopologyHandler.PreviewSource)
		topologyRoutes.POST("/sources/:id/run", dependencies.RequireAdmin, dependencies.TopologyHandler.RunSourceSync)
		topologyRoutes.GET("/sync-runs", dependencies.TopologyHandler.ListSyncRuns)
		topologyRoutes.GET("/nodes", dependencies.TopologyHandler.ListNodes)
		topologyRoutes.POST("/nodes", dependencies.RequireAdmin, dependencies.TopologyHandler.UpsertNode)
		topologyRoutes.GET("/nodes/:id", dependencies.TopologyHandler.GetNode)
		topologyRoutes.PUT("/nodes/:id", dependencies.RequireAdmin, dependencies.TopologyHandler.UpdateNode)
		topologyRoutes.DELETE("/nodes/:id", dependencies.RequireAdmin, dependencies.TopologyHandler.DeleteNode)
		topologyRoutes.GET("/nodes/:id/aliases", dependencies.TopologyHandler.ListNodeAliases)
		topologyRoutes.POST("/nodes/:id/aliases", dependencies.RequireAdmin, dependencies.TopologyHandler.AddNodeAlias)
		topologyRoutes.DELETE("/nodes/:id/aliases/:aliasId", dependencies.RequireAdmin, dependencies.TopologyHandler.DeleteNodeAlias)
		topologyRoutes.GET("/edges", dependencies.TopologyHandler.ListEdges)
		topologyRoutes.POST("/edges", dependencies.RequireAdmin, dependencies.TopologyHandler.UpsertEdge)
		topologyRoutes.GET("/edges/:id", dependencies.TopologyHandler.GetEdge)
		topologyRoutes.PUT("/edges/:id", dependencies.RequireAdmin, dependencies.TopologyHandler.UpdateEdge)
		topologyRoutes.DELETE("/edges/:id", dependencies.RequireAdmin, dependencies.TopologyHandler.DeleteEdge)
		topologyRoutes.POST("/sync/k8s", dependencies.RequireAdmin, dependencies.TopologyHandler.SyncK8s)
		topologyRoutes.POST("/sync/trace", dependencies.RequireAdmin, dependencies.TopologyHandler.SyncTrace)
		topologyRoutes.POST("/sync/component", dependencies.RequireAdmin, dependencies.TopologyHandler.SyncComponent)
	}
	if dependencies.TimelineHandler != nil && dependencies.Authenticate != nil {
		timelineRoutes := router.Group("/api/timeline")
		timelineRoutes.Use(dependencies.Authenticate)
		timelineRoutes.GET("", dependencies.TimelineHandler.Build)
	}
	if dependencies.CorrelationHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		correlationRoutes := router.Group("/api/correlation")
		correlationRoutes.Use(dependencies.Authenticate)
		correlationRoutes.POST("/analyze", dependencies.RequireAdmin, dependencies.CorrelationHandler.Analyze)
	}
	if dependencies.IncidentHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		incidentRoutes := router.Group("/api/incidents")
		incidentRoutes.Use(dependencies.Authenticate)
		incidentRoutes.GET("", dependencies.IncidentHandler.List)
		incidentRoutes.POST("", dependencies.RequireAdmin, dependencies.IncidentHandler.Create)
		incidentRoutes.POST("/promote-analysis", dependencies.RequireAdmin, dependencies.IncidentHandler.PromoteAnalysis)
		incidentRoutes.GET("/:id/similar", dependencies.IncidentHandler.Similar)
		incidentRoutes.GET("/:id", dependencies.IncidentHandler.Get)
		incidentRoutes.PUT("/:id", dependencies.RequireAdmin, dependencies.IncidentHandler.Update)
		incidentRoutes.POST("/:id/root-causes/:candidateId/confirm", dependencies.RequireAdmin, dependencies.IncidentHandler.ConfirmRootCause)
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
		documentRoutes.GET("/quality-standards", dependencies.DocumentHandler.QualityStandards)
		if dependencies.RequireAdmin != nil {
			documentRoutes.POST("/quality-standards/upload", dependencies.RequireAdmin, dependencies.DocumentHandler.UploadQualityStandard)
		}
		documentRoutes.GET("", dependencies.DocumentHandler.List)
		documentRoutes.GET("/:id", dependencies.DocumentHandler.Get)
		documentRoutes.GET("/:id/versions/latest", dependencies.DocumentHandler.LatestVersion)
		documentRoutes.GET("/:id/versions", dependencies.DocumentHandler.Versions)
		documentRoutes.POST("/:id/versions/upload", dependencies.DocumentHandler.UploadVersion)
		documentRoutes.GET("/:id/chunks", dependencies.DocumentHandler.Chunks)
		if dependencies.RequireAdmin != nil {
			documentRoutes.POST("/:id/review", dependencies.RequireAdmin, dependencies.DocumentHandler.Review)
		}
		documentRoutes.POST("/:id/reprocess", dependencies.DocumentHandler.Reprocess)

		knowledgeRoutes := router.Group("/api/knowledge")
		knowledgeRoutes.Use(dependencies.Authenticate)
		knowledgeRoutes.POST("/search", dependencies.DocumentHandler.Search)
		knowledgeRoutes.GET("/chunk-strategies", dependencies.DocumentHandler.ChunkStrategies)
		knowledgeRoutes.GET("/chunk-strategies/:strategyId", dependencies.DocumentHandler.ChunkStrategy)
		if dependencies.RequireAdmin != nil {
			knowledgeRoutes.POST("/chunk-strategies", dependencies.RequireAdmin, dependencies.DocumentHandler.CreateChunkStrategy)
		}
		knowledgeRoutes.GET("/document-versions/:versionId", dependencies.DocumentHandler.GetVersion)
		knowledgeRoutes.GET("/document-versions/:versionId/publication-gate", dependencies.DocumentHandler.PublicationGate)
		knowledgeRoutes.GET("/document-versions/:versionId/diff/:otherVersionId", dependencies.DocumentHandler.DiffVersions)
		knowledgeRoutes.GET("/document-versions/:versionId/citations/:chunkId", dependencies.DocumentHandler.HistoricalCitation)
		if dependencies.RequireAdmin != nil {
			knowledgeRoutes.POST("/document-versions/:versionId/publish", dependencies.RequireAdmin, dependencies.DocumentHandler.PublishVersion)
			knowledgeRoutes.POST("/document-versions/:versionId/deprecate", dependencies.RequireAdmin, dependencies.DocumentHandler.DeprecateVersion)
		}
		knowledgeRoutes.POST("/document-versions/:versionId/parse", dependencies.DocumentHandler.ParseVersion)
		knowledgeRoutes.GET("/document-versions/:versionId/blocks", dependencies.DocumentHandler.ParsedStructure)
		knowledgeRoutes.POST("/document-versions/:versionId/chunk", dependencies.DocumentHandler.ChunkVersion)
		knowledgeRoutes.GET("/document-versions/:versionId/chunks", dependencies.DocumentHandler.VersionChunks)
		if dependencies.RAGHandler != nil {
			knowledgeRoutes.POST("/ask", dependencies.RAGHandler.Ask)
		}
	}
	if dependencies.QualityStandardHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		qualityRoutes := router.Group("/api/knowledge")
		qualityRoutes.Use(dependencies.Authenticate)
		qualityRoutes.GET("/quality-standards", dependencies.QualityStandardHandler.List)
		qualityRoutes.GET("/quality-standards/:id", dependencies.QualityStandardHandler.Get)
		qualityRoutes.GET("/quality-profiles/:id", dependencies.QualityStandardHandler.GetProfile)
		qualityRoutes.POST("/quality-standards", dependencies.RequireAdmin, dependencies.QualityStandardHandler.Create)
		qualityRoutes.POST("/quality-standards/import", dependencies.RequireAdmin, dependencies.QualityStandardHandler.Import)
		qualityRoutes.PUT("/quality-standards/:id", dependencies.RequireAdmin, dependencies.QualityStandardHandler.Update)
		qualityRoutes.POST("/quality-standards/:id/validate", dependencies.RequireAdmin, dependencies.QualityStandardHandler.Validate)
		qualityRoutes.POST("/quality-standards/:id/publish", dependencies.RequireAdmin, dependencies.QualityStandardHandler.Publish)
		qualityRoutes.POST("/quality-standards/:id/deprecate", dependencies.RequireAdmin, dependencies.QualityStandardHandler.Deprecate)
		qualityRoutes.POST("/quality-profiles", dependencies.RequireAdmin, dependencies.QualityStandardHandler.CreateProfile)
		qualityRoutes.PUT("/quality-profiles/:id", dependencies.RequireAdmin, dependencies.QualityStandardHandler.UpdateProfile)
		qualityRoutes.POST("/quality-profiles/:id/clone", dependencies.RequireAdmin, dependencies.QualityStandardHandler.CloneProfile)
	}
	if dependencies.QualityEvaluationHandler != nil && dependencies.Authenticate != nil {
		evaluationRoutes := router.Group("/api/knowledge/evaluations")
		evaluationRoutes.Use(dependencies.Authenticate)
		evaluationRoutes.POST("", dependencies.QualityEvaluationHandler.Create)
		evaluationRoutes.GET("/:id", dependencies.QualityEvaluationHandler.Get)
		evaluationRoutes.GET("/:id/rule-results", dependencies.QualityEvaluationHandler.RuleResults)
		evaluationRoutes.GET("/:id/overrides", dependencies.QualityEvaluationHandler.Overrides)
		evaluationRoutes.POST("/:id/rerun", dependencies.QualityEvaluationHandler.Rerun)
		if dependencies.RequireAdmin != nil {
			evaluationRoutes.POST("/:id/override", dependencies.RequireAdmin, dependencies.QualityEvaluationHandler.Override)
			evaluationRoutes.POST("/:id/publish", dependencies.RequireAdmin, dependencies.QualityEvaluationHandler.Publish)
		}
	}
	if dependencies.EmbeddingIndexHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		indexRoutes := router.Group("/api/knowledge")
		indexRoutes.Use(dependencies.Authenticate)
		indexRoutes.POST("/index-jobs", dependencies.RequireAdmin, dependencies.EmbeddingIndexHandler.Create)
		indexRoutes.GET("/index-jobs/:id", dependencies.EmbeddingIndexHandler.Get)
		indexRoutes.POST("/index-jobs/:id/build", dependencies.RequireAdmin, dependencies.EmbeddingIndexHandler.Build)
		indexRoutes.POST("/index-jobs/:id/retry", dependencies.RequireAdmin, dependencies.EmbeddingIndexHandler.Retry)
		indexRoutes.POST("/indexes/rebuild", dependencies.RequireAdmin, dependencies.EmbeddingIndexHandler.Rebuild)
		indexRoutes.GET("/indexes/status", dependencies.EmbeddingIndexHandler.Status)
	}
	if dependencies.RetrievalEvaluationHandler != nil && dependencies.Authenticate != nil && dependencies.RequireAdmin != nil {
		routes := router.Group("/api/knowledge/retrieval-evaluations")
		routes.Use(dependencies.Authenticate)
		routes.GET("/test-cases", dependencies.RetrievalEvaluationHandler.ListTestCases)
		routes.GET("/runs", dependencies.RetrievalEvaluationHandler.ListRuns)
		routes.GET("/runs/:id", dependencies.RetrievalEvaluationHandler.GetRun)
		routes.POST("/test-cases", dependencies.RequireAdmin, dependencies.RetrievalEvaluationHandler.CreateTestCase)
		routes.POST("/smoke", dependencies.RequireAdmin, dependencies.RetrievalEvaluationHandler.Smoke)
		routes.POST("/lab", dependencies.RequireAdmin, dependencies.RetrievalEvaluationHandler.Lab)
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
