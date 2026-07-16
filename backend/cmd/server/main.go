package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aiops-platform/backend/internal/agentruntime"
	alertsvc "aiops-platform/backend/internal/alert"
	analysissvc "aiops-platform/backend/internal/analysis"
	"aiops-platform/backend/internal/auth"
	changesvc "aiops-platform/backend/internal/change"
	"aiops-platform/backend/internal/config"
	conversationsvc "aiops-platform/backend/internal/conversation"
	correlationsvc "aiops-platform/backend/internal/correlation"
	"aiops-platform/backend/internal/credential"
	"aiops-platform/backend/internal/database"
	datasourcesvc "aiops-platform/backend/internal/datasource"
	documentsvc "aiops-platform/backend/internal/document"
	embeddingsvc "aiops-platform/backend/internal/embeddingindex"
	evidencesvc "aiops-platform/backend/internal/evidence"
	"aiops-platform/backend/internal/handler"
	incidentsvc "aiops-platform/backend/internal/incident"
	k8ssvc "aiops-platform/backend/internal/k8s"
	llmsvc "aiops-platform/backend/internal/llm"
	logssvc "aiops-platform/backend/internal/logs"
	metricssvc "aiops-platform/backend/internal/metrics"
	appmiddleware "aiops-platform/backend/internal/middleware"
	nacossvc "aiops-platform/backend/internal/nacos"
	nginxsvc "aiops-platform/backend/internal/nginx"
	qualityeval "aiops-platform/backend/internal/qualityevaluation"
	qualitysvc "aiops-platform/backend/internal/qualitystandard"
	ragsvc "aiops-platform/backend/internal/rag"
	redissvc "aiops-platform/backend/internal/redis"
	"aiops-platform/backend/internal/repository"
	retrievaleval "aiops-platform/backend/internal/retrievalevaluation"
	"aiops-platform/backend/internal/skillframework"
	sshsftpsvc "aiops-platform/backend/internal/sshsftp"
	tidbsvc "aiops-platform/backend/internal/tidb"
	timelinesvc "aiops-platform/backend/internal/timeline"
	"aiops-platform/backend/internal/toolregistry"
	topologysvc "aiops-platform/backend/internal/topology"
	usersvc "aiops-platform/backend/internal/user"
	workflowexec "aiops-platform/backend/internal/workflow"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
	databaseTimeout   = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logger := newLogger(cfg.Environment)
	slog.SetDefault(logger)
	setGinMode(cfg.Environment)
	databaseContext, cancelDatabase := context.WithTimeout(context.Background(), databaseTimeout)
	databaseConnection, err := database.Open(databaseContext, cfg.Database)
	cancelDatabase()
	if err != nil {
		return err
	}
	defer func() {
		if err := databaseConnection.Close(); err != nil {
			logger.Warn("database connection did not close cleanly", "error", err)
		}
	}()
	logger.Info("database connection verified")
	userRepository := repository.NewUserRepository(databaseConnection.GORM)
	conversationRepository := repository.NewConversationRepository(databaseConnection.GORM)
	llmRepository := repository.NewLLMRepository(databaseConnection.GORM)
	ragRepository := repository.NewRAGRepository(databaseConnection.GORM)
	dataSourceRepository := repository.NewDataSourceRepository(databaseConnection.GORM)
	analysisRepository := repository.NewAnalysisRepository(databaseConnection.GORM)
	eventRepository := repository.NewEventRepository(databaseConnection.GORM)
	evidenceRepository := repository.NewEvidenceRepository(databaseConnection.GORM)
	topologyRepository := repository.NewTopologyRepository(databaseConnection.GORM)
	incidentRepository := repository.NewIncidentRepository(databaseConnection.GORM)
	skillRunRepository := repository.NewSkillRunRepository(databaseConnection.GORM)
	agentRunRepository := repository.NewAgentRunRepository(databaseConnection.GORM)
	workflowRepository := repository.NewWorkflowRepository(databaseConnection.GORM)
	auditLogRepository := repository.NewAuditLogRepository(databaseConnection.GORM)
	credentialManager, err := credential.NewManager(cfg.Credential.MasterKey, cfg.Credential.KeyVersion)
	if err != nil {
		return fmt.Errorf("initialize credential manager: %w", err)
	}
	tokenManager, err := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.JWTExpiry)
	if err != nil {
		return fmt.Errorf("initialize JWT manager: %w", err)
	}
	authService, err := auth.NewService(userRepository, tokenManager, bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("initialize authentication service: %w", err)
	}
	userService, err := usersvc.NewService(userRepository, bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("initialize user service: %w", err)
	}
	llmClient := llmsvc.NewLimitedClient(llmsvc.NewOpenAICompatibleClient(nil), 4)
	conversationService := conversationsvc.NewService(conversationRepository)
	llmService := llmsvc.NewService(llmRepository, credentialManager, llmClient)
	ragService := ragsvc.NewService(ragRepository, credentialManager, llmClient)
	dataSourceService := datasourcesvc.NewService(dataSourceRepository, credentialManager, cfg.Credential.KeyVersion)
	logsService := logssvc.NewService(dataSourceRepository, credentialManager, nil)
	sftpService := sshsftpsvc.NewService(dataSourceRepository, credentialManager, nil)
	k8sService := k8ssvc.NewService(dataSourceRepository, credentialManager, nil)
	metricsService := metricssvc.NewService(dataSourceRepository, credentialManager, nil)
	nacosService := nacossvc.NewService(dataSourceRepository, credentialManager, nil)
	nginxService := nginxsvc.NewService(dataSourceRepository, credentialManager, nil)
	redisService := redissvc.NewService(dataSourceRepository, credentialManager, nil)
	tidbService := tidbsvc.NewService(dataSourceRepository, credentialManager, nil)
	changeService := changesvc.NewService(dataSourceRepository, credentialManager, nil)
	alertService := alertsvc.NewService(eventRepository)
	evidenceService := evidencesvc.NewService(evidenceRepository)
	topologyService := topologysvc.NewService(topologyRepository, k8sService)
	timelineService := timelinesvc.NewService(eventRepository, evidenceRepository)
	correlationService := correlationsvc.NewService(eventRepository, topologyRepository)
	incidentService := incidentsvc.NewService(incidentRepository, analysisRepository)
	toolRegistry := toolregistry.NewBuiltinRegistry()
	skills := []skillframework.Skill{skillframework.EchoSkill{}}
	skills = append(skills, skillframework.ComponentDiagnosisSkillsWithServices(nacosService, redisService, tidbService, nginxService)...)
	skills = append(skills, skillframework.LogAndKnowledgeSkills(analysisRepository, logsService)...)
	skills = append(skills, skillframework.K8sAndMetricsSkills(k8sService, metricsService)...)
	skills = append(skills, skillframework.ChangeSkills(changeService)...)
	skills = append(skills, skillframework.IncidentAnalysisSkills(timelineService, correlationService)...)
	skillRegistry, err := skillframework.NewRegistry(toolRegistry, skillRunRepository, skills...)
	if err != nil {
		return fmt.Errorf("initialize skill registry: %w", err)
	}
	agentRuntime, err := agentruntime.NewRuntime(skillRegistry, agentRunRepository, agentruntime.Limits{}, agentruntime.BuiltinAgents()...)
	if err != nil {
		return fmt.Errorf("initialize agent runtime: %w", err)
	}
	analysisService := analysissvc.NewService(analysisRepository, logsService, credentialManager, llmClient)
	documentService, err := documentsvc.NewService(userRepository, cfg.FileStorage.LocalFileDir, cfg.FileStorage.MaxUploadBytes, cfg.RAG.ChunkSize, cfg.RAG.ChunkOverlap)
	if err != nil {
		return fmt.Errorf("initialize document service: %w", err)
	}
	parserRegistry, err := documentsvc.NewDefaultParserRegistry(documentsvc.ParseLimits{
		Timeout:   cfg.KnowledgeParse.Timeout,
		MaxBlocks: cfg.KnowledgeParse.MaxBlocks,
		MaxPages:  cfg.KnowledgeParse.MaxPages,
		MaxBytes:  cfg.FileStorage.MaxUploadBytes,
	})
	if err != nil {
		return fmt.Errorf("initialize document parser registry: %w", err)
	}
	documentService.WithParserRegistry(parserRegistry)
	documentService.WithQualityLLM(credentialManager, llmClient)
	bootstrapContext, cancelBootstrap := context.WithTimeout(context.Background(), databaseTimeout)
	err = authService.BootstrapAdmin(
		bootstrapContext,
		cfg.Auth.InitialAdminUsername,
		cfg.Auth.InitialAdminPassword,
	)
	cancelBootstrap()
	if err != nil {
		return fmt.Errorf("initialize admin user: %w", err)
	}
	logger.Info("initial admin verified")
	bootstrapWorkflowContext, cancelBootstrapWorkflow := context.WithTimeout(context.Background(), databaseTimeout)
	err = workflowexec.BootstrapBuiltinDefinitions(bootstrapWorkflowContext, workflowRepository, nil)
	cancelBootstrapWorkflow()
	if err != nil {
		return fmt.Errorf("bootstrap builtin workflows: %w", err)
	}
	logger.Info("builtin workflows verified")
	authHandler := handler.NewAuthHandler(authService)
	userHandler := handler.NewUserHandler(userService)
	conversationHandler := handler.NewConversationHandler(conversationService)
	llmHandler := handler.NewLLMHandler(llmService)
	documentHandler := handler.NewDocumentHandler(documentService, cfg.FileStorage.MaxUploadBytes)
	qualityStandardService := qualitysvc.NewService(userRepository)
	if err := qualityStandardService.ConfigureImporter(cfg.FileStorage.LocalFileDir, cfg.FileStorage.MaxUploadBytes); err != nil {
		return fmt.Errorf("initialize quality standard importer: %w", err)
	}
	qualityStandardHandler := handler.NewQualityStandardHandler(qualityStandardService, cfg.FileStorage.MaxUploadBytes)
	qualityEvaluationHandler := handler.NewQualityEvaluationHandler(qualityeval.NewService(userRepository).WithLLM(credentialManager, llmClient))
	embeddingIndexHandler := handler.NewEmbeddingIndexHandler(embeddingsvc.NewService(userRepository, credentialManager, llmClient))
	retrievalEvaluationHandler := handler.NewRetrievalEvaluationHandler(retrievaleval.NewService(userRepository, ragService))
	ragHandler := handler.NewRAGHandler(ragService)
	dataSourceHandler := handler.NewDataSourceHandler(dataSourceService)
	analysisHandler := handler.NewAnalysisHandler(logsService, analysisService)
	eventHandler := handler.NewEventHandler(alertService)
	evidenceHandler := handler.NewEvidenceHandler(evidenceService)
	topologyHandler := handler.NewTopologyHandler(topologyService)
	timelineHandler := handler.NewTimelineHandler(timelineService)
	correlationHandler := handler.NewCorrelationHandler(correlationService)
	incidentHandler := handler.NewIncidentHandler(incidentService)
	toolHandler := handler.NewToolHandler(toolRegistry)
	skillHandler := handler.NewSkillHandler(skillRegistry)
	agentHandler := handler.NewAgentHandler(agentRuntime)
	workflowExecutor := workflowexec.NewExecutor(workflowRepository, agentRuntime, skillRegistry, 0)
	workflowHandler := handler.NewWorkflowHandler(workflowRepository, workflowExecutor, agentRuntime, skillRegistry)
	auditHandler := handler.NewAuditHandler(auditLogRepository)
	sftpHandler := handler.NewSFTPHandler(sftpService)
	k8sHandler := handler.NewK8sHandler(k8sService)
	metricsHandler := handler.NewMetricsHandler(metricsService)

	server := &http.Server{
		Addr: cfg.Address(),
		Handler: handler.NewRouter(logger, handler.RouterDependencies{
			AuditHandler:               auditHandler,
			AuditRecorder:              auditLogRepository,
			AuthHandler:                authHandler,
			UserHandler:                userHandler,
			ConversationHandler:        conversationHandler,
			LLMHandler:                 llmHandler,
			DocumentHandler:            documentHandler,
			QualityStandardHandler:     qualityStandardHandler,
			QualityEvaluationHandler:   qualityEvaluationHandler,
			EmbeddingIndexHandler:      embeddingIndexHandler,
			RetrievalEvaluationHandler: retrievalEvaluationHandler,
			RAGHandler:                 ragHandler,
			DataSourceHandler:          dataSourceHandler,
			AnalysisHandler:            analysisHandler,
			EventHandler:               eventHandler,
			EvidenceHandler:            evidenceHandler,
			TopologyHandler:            topologyHandler,
			TimelineHandler:            timelineHandler,
			CorrelationHandler:         correlationHandler,
			IncidentHandler:            incidentHandler,
			ToolHandler:                toolHandler,
			SkillHandler:               skillHandler,
			AgentHandler:               agentHandler,
			WorkflowHandler:            workflowHandler,
			SFTPHandler:                sftpHandler,
			K8sHandler:                 k8sHandler,
			MetricsHandler:             metricsHandler,
			ReadinessCheck: func(ctx context.Context) error {
				return databaseConnection.SQL.PingContext(ctx)
			},
			Authenticate: appmiddleware.Authenticate(authService),
			RequireAdmin: appmiddleware.RequireAdmin(),
		}),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "address", cfg.Address(), "environment", cfg.Environment)
		serverErrors <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case sig := <-signals:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server stopped unexpectedly: %w", err)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}
	logger.Info("http server stopped")
	return nil
}

func newLogger(environment string) *slog.Logger {
	level := slog.LevelInfo
	if environment == "dev" {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

func setGinMode(environment string) {
	if environment == "dev" || environment == "test" {
		gin.SetMode(gin.DebugMode)
		return
	}
	gin.SetMode(gin.ReleaseMode)
}
