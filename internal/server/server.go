package server

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/finlego/prometheus-oauth-gateway/internal/auth"
	"github.com/finlego/prometheus-oauth-gateway/internal/config"
	"github.com/finlego/prometheus-oauth-gateway/internal/middleware"
	"github.com/finlego/prometheus-oauth-gateway/internal/proxy"
	"github.com/finlego/prometheus-oauth-gateway/pkg/tenant"
)

type Server struct {
	config   *config.Config
	server   *http.Server
	handlers *Handlers
	logger   *logrus.Logger
}

func New(cfg *config.Config, logger *logrus.Logger) (*Server, error) {
	jwtConfig := &auth.JWTConfig{
		JWKSURL:          cfg.Auth.JWKSURL,
		Secret:           cfg.Auth.JWTSecret,
		Algorithm:        cfg.Auth.JWTAlgorithm,
		ValidateAudience: cfg.Auth.ValidateAudience,
		ValidateIssuer:   cfg.Auth.ValidateIssuer,
		RequiredIssuer:   cfg.Auth.RequiredIssuer,
		RequiredAudience: cfg.Auth.RequiredAudience,
		CacheTTL:         cfg.Auth.CacheTTL,
	}

	jwtVerifier := auth.NewJWTVerifier(jwtConfig, logger)

	var tenantMappings []tenant.Mapping
	for _, mapping := range cfg.TenantMaps {
		tenantMappings = append(tenantMappings, tenant.Mapping{
			Groups:   mapping.Groups,
			Tenants:  mapping.Tenants,
			ReadOnly: mapping.ReadOnly,
		})
	}
	tenantMapper := tenant.NewMapper(tenantMappings)

	proxyConfig := &proxy.Config{
		UpstreamURL:  cfg.Upstream.URL,
		Timeout:      cfg.Upstream.Timeout,
		MaxRetries:   cfg.Upstream.MaxRetries,
		RetryDelay:   cfg.Upstream.RetryDelay,
		TenantHeader: cfg.Upstream.TenantHeader,
	}
	proxyHandler, err := proxy.NewWithTenantMapper(proxyConfig, logger, tenantMapper)
	if err != nil {
		return nil, err
	}

	handlers := NewHandlers(proxyHandler, tenantMapper, logger)
	universalHandler := NewUniversalHandler(proxyHandler, tenantMapper, logger)

	authMiddleware := middleware.NewAuthMiddleware(jwtVerifier, tenantMapper, cfg.Auth.UserGroupsClaim, logger)
	metricsMiddleware := middleware.NewMetricsMiddleware()
	loggingMiddleware := middleware.NewLoggingMiddleware(logger)

	router := mux.NewRouter()

	router.HandleFunc("/health", handlers.HealthCheck).Methods("GET")
	router.HandleFunc("/readiness", handlers.ReadinessCheck).Methods("GET")

	if cfg.Metrics.Enabled {
		router.Handle(cfg.Metrics.Path, promhttp.Handler()).Methods("GET")
	}

	// Setup universal VictoriaMetrics proxying
	server := &Server{
		config:   cfg,
		handlers: handlers,
		logger:   logger,
	}
	server.setupUniversalProxying(router, universalHandler, authMiddleware, metricsMiddleware, loggingMiddleware)

	httpServer := &http.Server{
		Addr:         cfg.Server.Address,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	return &Server{
		config:   cfg,
		server:   httpServer,
		handlers: handlers,
		logger:   logger,
	}, nil
}

// setupUniversalProxying configures universal proxying for all VictoriaMetrics requests
func (s *Server) setupUniversalProxying(
	router *mux.Router,
	universalHandler *UniversalHandler,
	authMiddleware *middleware.AuthMiddleware,
	metricsMiddleware *middleware.MetricsMiddleware,
	loggingMiddleware *middleware.LoggingMiddleware,
) {
	// Create authenticated subrouter for all API endpoints
	apiRouter := router.PathPrefix("/").Subrouter()
	apiRouter.Use(loggingMiddleware.LogRequests)
	apiRouter.Use(metricsMiddleware.RecordMetrics)
	apiRouter.Use(authMiddleware.Authenticate)

	// Universal handler for all VictoriaMetrics requests
	// This catches all requests and handles them intelligently
	apiRouter.PathPrefix("/").HandlerFunc(universalHandler.HandleRequest)

	s.logger.Info("Configured universal VictoriaMetrics proxying - all requests will be proxied through intelligent detection")
}

func (s *Server) Start() error {
	s.logger.WithFields(logrus.Fields{
		"address":      s.config.Server.Address,
		"upstream_url": s.config.Upstream.URL,
	}).Info("Starting prometheus-oauth-gateway server")

	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server...")
	return s.server.Shutdown(ctx)
}
