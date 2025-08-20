package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/finlego/prometheus-oauth-gateway/internal/config"
	"github.com/finlego/prometheus-oauth-gateway/internal/domain"
	"github.com/finlego/prometheus-oauth-gateway/internal/handlers"
	"github.com/finlego/prometheus-oauth-gateway/internal/infrastructure/logger"
	"github.com/finlego/prometheus-oauth-gateway/internal/services/access"
	"github.com/finlego/prometheus-oauth-gateway/internal/services/auth"
	"github.com/finlego/prometheus-oauth-gateway/internal/services/metrics"
	"github.com/finlego/prometheus-oauth-gateway/internal/services/proxy"
	"github.com/finlego/prometheus-oauth-gateway/internal/services/tenant"
)

var (
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	var (
		configPath     = flag.String("config", "", "Path to configuration file")
		showVersion    = flag.Bool("version", false, "Show version information")
		logLevel       = flag.String("log-level", "", "Log level (debug, info, warn, error)")
		validateConfig = flag.Bool("validate-config", false, "Validate configuration and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("prometheus-oauth-gateway (clean architecture)\n")
		fmt.Printf("Version: %s\n", version)
		fmt.Printf("Build time: %s\n", buildTime)
		fmt.Printf("Git commit: %s\n", gitCommit)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Override log level if provided
	if *logLevel != "" {
		cfg.Logging.Level = *logLevel
	}

	// Validate configuration
	if *validateConfig {
		fmt.Println("Configuration is valid")
		os.Exit(0)
	}

	// Initialize logger
	appLogger := logger.NewStructuredLogger(cfg.Logging.Level, cfg.Logging.Format)

	appLogger.Info("Starting prometheus-oauth-gateway (clean architecture)",
		domain.Field{Key: "version", Value: version},
		domain.Field{Key: "build_time", Value: buildTime},
		domain.Field{Key: "git_commit", Value: gitCommit},
		domain.Field{Key: "config_path", Value: *configPath},
		domain.Field{Key: "server_address", Value: cfg.Server.Address},
		domain.Field{Key: "upstream_url", Value: cfg.Upstream.URL},
		domain.Field{Key: "log_level", Value: cfg.Logging.Level},
	)

	// Initialize all services using clean architecture
	authService := auth.NewService(cfg.Auth, cfg.TenantMaps, appLogger)
	tenantService := tenant.NewService(cfg.Upstream, appLogger)
	accessService := access.NewService(appLogger)
	proxyService := proxy.NewService(cfg.Upstream.URL, cfg.Upstream.Timeout, appLogger)
	metricsService := metrics.NewService(appLogger)

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
	healthHandler := handlers.NewHealthHandler(appLogger, version)

	// Setup HTTP router
	mux := http.NewServeMux()
	mux.Handle("/health", healthHandler)
	mux.Handle("/ready", healthHandler) // Same handler for both health and readiness
	if cfg.Metrics.Enabled {
		appLogger.Info("Metrics endpoint not yet implemented in clean architecture")
	}
	mux.Handle("/", gatewayHandler) // Catch-all for proxy requests

	// Create HTTP server
	server := &http.Server{
		Addr:         cfg.Server.Address,
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Start server in goroutine
	go func() {
		appLogger.Info("Server starting", domain.Field{Key: "address", Value: cfg.Server.Address})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Error("Server failed to start", domain.Field{Key: "error", Value: err.Error()})
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	appLogger.Info("Received shutdown signal", domain.Field{Key: "signal", Value: sig.String()})

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		appLogger.Error("Error during server shutdown", domain.Field{Key: "error", Value: err.Error()})
		os.Exit(1)
	}

	appLogger.Info("Server stopped gracefully")
}