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
	"aiops-platform/backend/internal/config"
	conversationsvc "aiops-platform/backend/internal/conversation"
	"aiops-platform/backend/internal/credential"
	"aiops-platform/backend/internal/database"
	datasourcesvc "aiops-platform/backend/internal/datasource"
	documentsvc "aiops-platform/backend/internal/document"
	"aiops-platform/backend/internal/handler"
	k8ssvc "aiops-platform/backend/internal/k8s"
	llmsvc "aiops-platform/backend/internal/llm"
	logssvc "aiops-platform/backend/internal/logs"
	metricssvc "aiops-platform/backend/internal/metrics"
	appmiddleware "aiops-platform/backend/internal/middleware"
	ragsvc "aiops-platform/backend/internal/rag"
	"aiops-platform/backend/internal/repository"
	"aiops-platform/backend/internal/skillframework"
	sshsftpsvc "aiops-platform/backend/internal/sshsftp"
	"aiops-platform/backend/internal/toolregistry"
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
	skillRunRepository := repository.NewSkillRunRepository(databaseConnection.GORM)
	agentRunRepository := repository.NewAgentRunRepository(databaseConnection.GORM)
	workflowRepository := repository.NewWorkflowRepository(databaseConnection.GORM)
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
	conversationService := conversationsvc.NewService(conversationRepository)
	llmService := llmsvc.NewService(llmRepository, credentialManager, llmsvc.NewOpenAICompatibleClient(nil))
	ragService := ragsvc.NewService(ragRepository, credentialManager, llmsvc.NewOpenAICompatibleClient(nil))
	dataSourceService := datasourcesvc.NewService(dataSourceRepository, credentialManager, cfg.Credential.KeyVersion)
	logsService := logssvc.NewService(dataSourceRepository, credentialManager, nil)
	sftpService := sshsftpsvc.NewService(dataSourceRepository, credentialManager, nil)
	k8sService := k8ssvc.NewService(dataSourceRepository, credentialManager, nil)
	metricsService := metricssvc.NewService(dataSourceRepository, credentialManager, nil)
	alertService := alertsvc.NewService(eventRepository)
	toolRegistry := toolregistry.NewBuiltinRegistry()
	skills := append(skillframework.BuiltinSkills(), skillframework.LogAndKnowledgeSkills(analysisRepository, logsService)...)
	skills = append(skills, skillframework.K8sAndMetricsSkills(k8sService, metricsService)...)
	skillRegistry, err := skillframework.NewRegistry(toolRegistry, skillRunRepository, skills...)
	if err != nil {
		return fmt.Errorf("initialize skill registry: %w", err)
	}
	agentRuntime, err := agentruntime.NewRuntime(skillRegistry, agentRunRepository, agentruntime.Limits{}, agentruntime.BuiltinAgents()...)
	if err != nil {
		return fmt.Errorf("initialize agent runtime: %w", err)
	}
	analysisService := analysissvc.NewService(analysisRepository, logsService, credentialManager, llmsvc.NewOpenAICompatibleClient(nil))
	documentService, err := documentsvc.NewService(userRepository, cfg.FileStorage.LocalFileDir, cfg.FileStorage.MaxUploadBytes, cfg.RAG.ChunkSize, cfg.RAG.ChunkOverlap)
	if err != nil {
		return fmt.Errorf("initialize document service: %w", err)
	}
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
	authHandler := handler.NewAuthHandler(authService)
	userHandler := handler.NewUserHandler(userService)
	conversationHandler := handler.NewConversationHandler(conversationService)
	llmHandler := handler.NewLLMHandler(llmService)
	documentHandler := handler.NewDocumentHandler(documentService, cfg.FileStorage.MaxUploadBytes)
	ragHandler := handler.NewRAGHandler(ragService)
	dataSourceHandler := handler.NewDataSourceHandler(dataSourceService)
	analysisHandler := handler.NewAnalysisHandler(logsService, analysisService)
	eventHandler := handler.NewEventHandler(alertService)
	toolHandler := handler.NewToolHandler(toolRegistry)
	skillHandler := handler.NewSkillHandler(skillRegistry)
	agentHandler := handler.NewAgentHandler(agentRuntime)
	workflowExecutor := workflowexec.NewExecutor(workflowRepository, agentRuntime, skillRegistry, 0)
	workflowHandler := handler.NewWorkflowHandler(workflowRepository, workflowExecutor, agentRuntime, skillRegistry)
	sftpHandler := handler.NewSFTPHandler(sftpService)
	k8sHandler := handler.NewK8sHandler(k8sService)
	metricsHandler := handler.NewMetricsHandler(metricsService)

	server := &http.Server{
		Addr: cfg.Address(),
		Handler: handler.NewRouter(logger, handler.RouterDependencies{
			AuthHandler:         authHandler,
			UserHandler:         userHandler,
			ConversationHandler: conversationHandler,
			LLMHandler:          llmHandler,
			DocumentHandler:     documentHandler,
			RAGHandler:          ragHandler,
			DataSourceHandler:   dataSourceHandler,
			AnalysisHandler:     analysisHandler,
			EventHandler:        eventHandler,
			ToolHandler:         toolHandler,
			SkillHandler:        skillHandler,
			AgentHandler:        agentHandler,
			WorkflowHandler:     workflowHandler,
			SFTPHandler:         sftpHandler,
			K8sHandler:          k8sHandler,
			MetricsHandler:      metricsHandler,
			Authenticate:        appmiddleware.Authenticate(authService),
			RequireAdmin:        appmiddleware.RequireAdmin(),
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
