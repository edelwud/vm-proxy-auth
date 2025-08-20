package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/finlego/prometheus-oauth-gateway/internal/config"
	"github.com/finlego/prometheus-oauth-gateway/internal/server"
)

var (
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	var (
		configPath    = flag.String("config", "", "Path to configuration file")
		showVersion   = flag.Bool("version", false, "Show version information")
		logLevel      = flag.String("log-level", "", "Log level (debug, info, warn, error)")
		validateConfig = flag.Bool("validate-config", false, "Validate configuration and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("prometheus-oauth-gateway\n")
		fmt.Printf("Version: %s\n", version)
		fmt.Printf("Build time: %s\n", buildTime)
		fmt.Printf("Git commit: %s\n", gitCommit)
		os.Exit(0)
	}

	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339,
	})

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.WithError(err).Fatal("Failed to load configuration")
	}

	if *logLevel != "" {
		cfg.Logging.Level = *logLevel
	}

	level, err := logrus.ParseLevel(cfg.Logging.Level)
	if err != nil {
		logger.WithError(err).Fatal("Invalid log level")
	}
	logger.SetLevel(level)

	if cfg.Logging.Format == "text" {
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
		})
	}

	if *validateConfig {
		logger.Info("Configuration is valid")
		os.Exit(0)
	}

	logger.WithFields(logrus.Fields{
		"version":        version,
		"build_time":     buildTime,
		"git_commit":     gitCommit,
		"config_path":    *configPath,
		"server_address": cfg.Server.Address,
		"upstream_url":   cfg.Upstream.URL,
		"log_level":      cfg.Logging.Level,
	}).Info("Starting prometheus-oauth-gateway")

	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create server")
	}

	go func() {
		if err := srv.Start(); err != nil {
			logger.WithError(err).Fatal("Server failed to start")
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	logger.WithField("signal", sig.String()).Info("Received shutdown signal")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Error("Error during server shutdown")
		os.Exit(1)
	}

	logger.Info("Server stopped gracefully")
}