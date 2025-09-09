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
	"github.com/edelwud/vm-proxy-auth/internal/services/discovery"
	"github.com/edelwud/vm-proxy-auth/internal/services/memberlist"
	"github.com/edelwud/vm-proxy-auth/internal/services/metrics"
	"github.com/edelwud/vm-proxy-auth/internal/services/proxy"
	"github.com/edelwud/vm-proxy-auth/internal/services/statestorage"
	"github.com/edelwud/vm-proxy-auth/internal/services/tenant"
	netutils "github.com/edelwud/vm-proxy-auth/pkg/utils"
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

	// Load configuration using new modular system
	cfg, err := config.LoadConfig(*configPath)
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
		domain.Field{Key: "backends_count", Value: len(cfg.Proxy.Upstreams)},
		domain.Field{Key: "log_level", Value: cfg.Logging.Level},
		domain.Field{Key: "node_id", Value: cfg.Metadata.NodeID},
		domain.Field{Key: "environment", Value: cfg.Metadata.Environment},
	)

	// Initialize all services using clean architecture
	metricsService := metrics.NewService(appLogger)

	// Use new modular configuration directly
	authService, err := auth.NewService(cfg.Auth, cfg.Tenants.Mappings, appLogger, metricsService)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to initialize auth service")
	}

	tenantService := tenant.NewService(cfg.Tenants.Filter, appLogger, metricsService)
	accessService := access.NewService(appLogger)

	// Create state storage based on configuration
	stateStorageConfig, storageType := createStateStorageConfig(cfg)

	stateStorage, err := statestorage.NewStateStorage(stateStorageConfig, storageType, cfg.Metadata.NodeID, appLogger)
	if err != nil {
		appLogger.Error("Failed to create state storage",
			domain.Field{Key: "type", Value: storageType},
			domain.Field{Key: "error", Value: err.Error()})
		os.Exit(1)
	}

	ctx := context.Background()

	// Initialize memberlist for Raft cluster if using Raft storage
	if cfg.IsClusterEnabled() {
		initializeMemberlist(ctx, cfg, stateStorage, appLogger)
	}

	proxyService, err := proxy.NewEnhancedService(cfg.Proxy, appLogger, metricsService, stateStorage)
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
		domain.Field{Key: "backends", Value: len(cfg.Proxy.Upstreams)},
		domain.Field{Key: "strategy", Value: cfg.Proxy.Routing.Strategy})

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
		ReadTimeout:  cfg.Server.Timeouts.Read,
		WriteTimeout: cfg.Server.Timeouts.Write,
		IdleTimeout:  cfg.Server.Timeouts.Idle,
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

// createStateStorageConfig creates state storage configuration from new config structure.
func createStateStorageConfig(cfg *config.Config) (interface{}, string) {
	switch cfg.Storage.Type {
	case "redis":
		return cfg.Storage.Redis, "redis"
	case "raft":
		return cfg.Storage.Raft, "raft"
	default:
		return nil, "local"
	}
}

// initializeMemberlist initializes memberlist service for Raft cluster.
func initializeMemberlist(
	ctx context.Context,
	cfg *config.Config,
	stateStorage domain.StateStorage,
	logger domain.Logger,
) {
	// Integrate with Raft storage and prepare memberlist
	var nodeMetadata *memberlist.NodeMetadata
	var raftStorage *statestorage.RaftStorage
	logger.Debug("Checking state storage type for Raft integration",
		domain.Field{Key: "storage_type", Value: fmt.Sprintf("%T", stateStorage)})
	if rs, ok := stateStorage.(*statestorage.RaftStorage); ok {
		logger.Info("State storage is RaftStorage, preparing memberlist integration")
		raftStorage = rs

		// Create node metadata for this instance BEFORE creating memberlist
		// Convert bind address to advertise address for peer communication
		raftAdvertiseAddr, addrErr := netutils.ConvertBindToAdvertiseAddress(cfg.Storage.Raft.BindAddress)
		if addrErr != nil {
			logger.Error("Failed to convert Raft bind address to advertise address",
				domain.Field{Key: "bind_address", Value: cfg.Storage.Raft.BindAddress},
				domain.Field{Key: "error", Value: addrErr.Error()})
			os.Exit(1)
		}

		var metaErr error
		nodeMetadata, metaErr = memberlist.CreateNodeMetadata(
			cfg.Metadata.NodeID,
			cfg.Server.Address,
			raftAdvertiseAddr,
			cfg.Cluster.Memberlist.Metadata,
		)
		if metaErr != nil {
			logger.Error("Failed to create node metadata",
				domain.Field{Key: "error", Value: metaErr.Error()})
			os.Exit(1)
		}

		logger.Info("Created node metadata for memberlist",
			domain.Field{Key: "node_id", Value: nodeMetadata.NodeID},
			domain.Field{Key: "role", Value: nodeMetadata.Role},
			domain.Field{Key: "http_address", Value: nodeMetadata.HTTPAddress},
			domain.Field{Key: "raft_address", Value: nodeMetadata.RaftAddress})
	}

	// Create memberlist service with metadata and Raft manager
	mlService, mlErr := memberlist.NewMemberlistServiceWithMetadata(
		cfg.Cluster.Memberlist,
		logger,
		nodeMetadata,
		raftStorage,
	)
	if mlErr != nil {
		logger.Error("Failed to create memberlist service",
			domain.Field{Key: "error", Value: mlErr.Error()})
		os.Exit(1)
	}

	// Create discovery service if enabled
	if cfg.Cluster.Discovery.Enabled {
		discoveryService := discovery.NewService(cfg.Cluster.Discovery, logger)
		mlService.SetDiscoveryService(discoveryService)

		// Connect discovery service to Raft manager for delayed bootstrap
		if raftStorage != nil {
			discoveryService.SetRaftManager(raftStorage)
			logger.Info("Discovery service connected to Raft manager for delayed bootstrap")
		}

		logger.Info("Discovery service configured",
			domain.Field{Key: "providers", Value: cfg.Cluster.Discovery.Providers})
	}

	// Start memberlist (this will also start discovery if configured)
	if startErr := mlService.Start(ctx); startErr != nil {
		logger.Error("Failed to start memberlist service",
			domain.Field{Key: "error", Value: startErr.Error()})
		os.Exit(1)
	}

	// Join cluster if nodes are explicitly configured (fallback)
	if len(cfg.Cluster.Memberlist.Peers.Join) > 0 {
		logger.Info("Using manual join nodes as fallback",
			domain.Field{Key: "join_nodes", Value: cfg.Cluster.Memberlist.Peers.Join})
		if joinErr := mlService.Join(cfg.Cluster.Memberlist.Peers.Join); joinErr != nil {
			logger.Warn("Failed to join memberlist cluster",
				domain.Field{Key: "error", Value: joinErr.Error()},
				domain.Field{Key: "join_nodes", Value: cfg.Cluster.Memberlist.Peers.Join})
		}
	}

	logger.Info("Memberlist service initialized",
		domain.Field{Key: "cluster_size", Value: mlService.GetClusterSize()})
}
