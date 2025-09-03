package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/handlers"
	"github.com/edelwud/vm-proxy-auth/internal/infrastructure/logger"
	"github.com/edelwud/vm-proxy-auth/internal/services/access"
	"github.com/edelwud/vm-proxy-auth/internal/services/auth"
	"github.com/edelwud/vm-proxy-auth/internal/services/memberlist"
	"github.com/edelwud/vm-proxy-auth/internal/services/metrics"
	"github.com/edelwud/vm-proxy-auth/internal/services/proxy"
	"github.com/edelwud/vm-proxy-auth/internal/services/statestorage"
	"github.com/edelwud/vm-proxy-auth/internal/services/tenant"
)

//nolint:gochecknoglobals // We are using global variables for version information.
var (
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

//nolint:funlen
func main() {
	var (
		configPath     = flag.String("config", "", "Path to configuration file")
		showVersion    = flag.Bool("version", false, "Show version information")
		logLevel       = flag.String("log-level", "", "Log level (debug, info, warn, error)")
		validateConfig = flag.Bool("validate-config", false, "Validate configuration and exit")
	)
	flag.Parse()

	if *showVersion {
		showVersionInfo()
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.LoadViperConfig(*configPath)
	if err != nil {
		// For configuration load errors, we output to stderr before logger initialization
		// This is necessary as we can't initialize logger without configuration
		_, _ = fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Override log level if provided
	if *logLevel != "" {
		cfg.Logging.Level = *logLevel
	}

	// Validate configuration
	if *validateConfig {
		showValidationSuccess()
		os.Exit(0)
	}

	// Initialize logger
	appLogger := logger.NewEnhancedStructuredLogger(cfg.Logging.Level, cfg.Logging.Format)

	appLogger.Info("Starting vm-proxy-auth (VictoriaMetrics Proxy with Authentication)",
		domain.Field{Key: "version", Value: version},
		domain.Field{Key: "build_time", Value: buildTime},
		domain.Field{Key: "git_commit", Value: gitCommit},
		domain.Field{Key: "config_path", Value: *configPath},
		domain.Field{Key: "server_address", Value: cfg.Server.Address},
		domain.Field{Key: "backends_count", Value: len(cfg.Backends)},
		domain.Field{Key: "log_level", Value: cfg.Logging.Level},
	)

	// Initialize all services using clean architecture
	metricsService := metrics.NewService(appLogger)
	authService, err := auth.NewService(cfg.Auth, cfg.TenantMapping, appLogger, metricsService)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to initialize auth service")
	}
	tenantService := tenant.NewService(&cfg.TenantFilter, appLogger, metricsService)
	accessService := access.NewService(appLogger)

	// Initialize enhanced proxy service
	if validationErr := cfg.ValidateBackends(); validationErr != nil {
		appLogger.Error("Invalid backend configuration",
			domain.Field{Key: "error", Value: validationErr.Error()})
		os.Exit(1)
	}

	// Convert configuration to proxy service config
	enhancedConfig, err := cfg.ToProxyServiceConfig()
	if err != nil {
		appLogger.Error("Failed to convert proxy configuration",
			domain.Field{Key: "error", Value: err.Error()})
		os.Exit(1)
	}

	// Create state storage based on configuration
	stateStorageConfig, storageType, err := cfg.ToStateStorageConfig()
	if err != nil {
		appLogger.Error("Invalid state storage configuration",
			domain.Field{Key: "error", Value: err.Error()})
		os.Exit(1)
	}

	stateStorage, err := createStateStorage(stateStorageConfig, storageType, "gateway-node", appLogger)
	if err != nil {
		appLogger.Error("Failed to create state storage",
			domain.Field{Key: "type", Value: storageType},
			domain.Field{Key: "error", Value: err.Error()})
		os.Exit(1)
	}

	ctx := context.Background()

	// Initialize memberlist for Raft cluster if using Raft storage
	if storageType == string(domain.StateStorageTypeRaft) {
		initializeMemberlist(ctx, cfg, stateStorage, appLogger)
	}

	proxyService, err := proxy.NewEnhancedService(enhancedConfig, appLogger, metricsService, stateStorage)
	if err != nil {
		appLogger.Error("Failed to create enhanced proxy service",
			domain.Field{Key: "error", Value: err.Error()})
		os.Exit(1)
	}

	// Start enhanced service
	if startErr := proxyService.Start(ctx); startErr != nil {
		appLogger.Error("Failed to start enhanced proxy service",
			domain.Field{Key: "error", Value: startErr.Error()})
		os.Exit(1)
	}

	appLogger.Info("Proxy service started",
		domain.Field{Key: "backends", Value: len(enhancedConfig.Backends)},
		domain.Field{Key: "strategy", Value: string(enhancedConfig.LoadBalancing.Strategy)})

	// Initialize main gateway handler
	gatewayHandler := handlers.NewGatewayHandler(
		authService,
		tenantService,
		accessService,
		proxyService,
		metricsService,
		appLogger,
	)

	// Initialize health check handler
	healthHandler := handlers.NewHealthHandler(appLogger, version, proxyService)

	// Setup HTTP router
	mux := http.NewServeMux()
	mux.Handle("/health", healthHandler)
	mux.Handle("/ready", healthHandler) // Same handler for both health and readiness

	// Add metrics endpoint if enabled
	if cfg.Metrics.Enabled {
		mux.Handle("/metrics", metricsService.Handler())
		appLogger.Info("Metrics endpoint enabled", domain.Field{Key: "path", Value: "/metrics"})
	}

	mux.Handle("/", gatewayHandler) // Catch-all for proxy requests

	// Create HTTP server
	server := &http.Server{
		Addr:         cfg.Server.Address,
		Handler:      mux,
		ReadTimeout:  cfg.Server.Timeouts.ReadTimeout,
		WriteTimeout: cfg.Server.Timeouts.WriteTimeout,
		IdleTimeout:  cfg.Server.Timeouts.IdleTimeout,
	}

	// Channel to signal server startup errors
	startupErr := make(chan error, 1)

	// Start server in goroutine
	go func() {
		appLogger.Info("Server starting", domain.Field{Key: "address", Value: cfg.Server.Address})
		if serverErr := server.ListenAndServe(); serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
			appLogger.Error("Server failed to start", domain.Field{Key: "error", Value: serverErr.Error()})
			startupErr <- serverErr
		}
	}()

	// Wait for shutdown signal or startup error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case startupErr := <-startupErr:
		appLogger.Error("Server startup failed", domain.Field{Key: "error", Value: startupErr.Error()})
		os.Exit(1)
	case sig := <-sigChan:
		appLogger.Info("Received shutdown signal", domain.Field{Key: "signal", Value: sig.String()})
	}

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), domain.DefaultShutdownTimeout)
	defer cancel()

	if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
		appLogger.Error("Error during server shutdown", domain.Field{Key: "error", Value: shutdownErr.Error()})
		//nolint:gocritic // Ignore this error as it is expected during shutdown
		os.Exit(1)
	}

	appLogger.Info("Server stopped gracefully")
}

// createStateStorage creates a state storage instance based on configuration.
func createStateStorage(
	storageConfig interface{},
	storageType string,
	nodeID string,
	logger domain.Logger,
) (domain.StateStorage, error) {
	return statestorage.NewStateStorage(storageConfig, storageType, nodeID, logger)
}

// showVersionInfo displays version information to stdout.
// This function is allowed to use fmt.Printf for CLI utility purposes.
//
//nolint:forbidigo // We are using fmt.Printf for CLI utility purposes.
func showVersionInfo() {
	fmt.Printf("vm-proxy-auth (VictoriaMetrics Proxy with Authentication)\n")
	fmt.Printf("Version: %s\n", version)
	fmt.Printf("Build time: %s\n", buildTime)
	fmt.Printf("Git commit: %s\n", gitCommit)
}

// showValidationSuccess displays configuration validation success message.
// This function is allowed to use fmt.Println for CLI utility purposes.
//
//nolint:forbidigo // We are using fmt.Println for CLI utility purposes.
func showValidationSuccess() {
	fmt.Println("Configuration is valid")
}

// initializeMemberlist initializes memberlist service for Raft cluster.
func initializeMemberlist(
	ctx context.Context,
	cfg *config.ViperConfig,
	stateStorage domain.StateStorage,
	logger domain.Logger,
) {
	// Create memberlist service
	mlService, mlErr := memberlist.NewMemberlistService(cfg.Memberlist, logger)
	if mlErr != nil {
		logger.Error("Failed to create memberlist service",
			domain.Field{Key: "error", Value: mlErr.Error()})
		os.Exit(1)
	}

	// Integrate with Raft storage
	if raftStorage, ok := stateStorage.(*statestorage.RaftStorage); ok {
		mlService.SetRaftManager(raftStorage)

		// Create node metadata for this instance
		nodeMetadata, metaErr := memberlist.CreateNodeMetadata(
			"gateway-node",
			cfg.Server.Address,
			cfg.StateStorage.Raft.BindAddress,
			cfg.Memberlist.Metadata,
		)
		if metaErr != nil {
			logger.Error("Failed to create node metadata",
				domain.Field{Key: "error", Value: metaErr.Error()})
			os.Exit(1)
		}

		mlService.GetDelegate().SetNodeMetadata(nodeMetadata)
	}

	// Start memberlist
	if startErr := mlService.Start(ctx); startErr != nil {
		logger.Error("Failed to start memberlist service",
			domain.Field{Key: "error", Value: startErr.Error()})
		os.Exit(1)
	}

	// Join cluster if nodes are configured
	if len(cfg.Memberlist.JoinNodes) > 0 {
		if joinErr := mlService.Join(cfg.Memberlist.JoinNodes); joinErr != nil {
			logger.Warn("Failed to join memberlist cluster",
				domain.Field{Key: "error", Value: joinErr.Error()},
				domain.Field{Key: "join_nodes", Value: cfg.Memberlist.JoinNodes})
		}
	}

	logger.Info("Memberlist service initialized",
		domain.Field{Key: "cluster_size", Value: mlService.GetClusterSize()})
}
