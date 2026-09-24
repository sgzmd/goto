package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"goto/internal/auth"
	"goto/internal/store/sqlite"
	"goto/internal/web"
)

type Config struct {
	Port             string
	BaseURL          string
	DBPath           string
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	SessionSecret    string
	SecureCookies    bool
}

func loadConfig() (*Config, error) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		return nil, errors.New("BASE_URL is required")
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/goto.db"
	}

	issuerURL := os.Getenv("OIDC_ISSUER_URL")
	if issuerURL == "" {
		return nil, errors.New("OIDC_ISSUER_URL is required")
	}

	clientID := os.Getenv("OIDC_CLIENT_ID")
	if clientID == "" {
		return nil, errors.New("OIDC_CLIENT_ID is required")
	}

	clientSecret := os.Getenv("OIDC_CLIENT_SECRET")
	if clientSecret == "" {
		return nil, errors.New("OIDC_CLIENT_SECRET is required")
	}

	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		return nil, errors.New("SESSION_SECRET is required")
	}
	if len(sessionSecret) < 32 {
		return nil, errors.New("SESSION_SECRET must be at least 32 characters long")
	}

	secureCookies := strings.HasPrefix(strings.ToLower(baseURL), "https://")
	if val := os.Getenv("SECURE_COOKIES"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return nil, fmt.Errorf("invalid SECURE_COOKIES boolean value: %w", err)
		}
		secureCookies = b
	}

	return &Config{
		Port:             port,
		BaseURL:          baseURL,
		DBPath:           dbPath,
		OIDCIssuerURL:    issuerURL,
		OIDCClientID:     clientID,
		OIDCClientSecret: clientSecret,
		SessionSecret:    sessionSecret,
		SecureCookies:    secureCookies,
	}, nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("Configuration error", "error", err)
		os.Exit(1)
	}

	slog.Info("Starting Goto link service",
		"port", cfg.Port,
		"base_url", cfg.BaseURL,
		"db_path", cfg.DBPath,
		"oidc_issuer", cfg.OIDCIssuerURL,
		"oidc_client_id", cfg.OIDCClientID,
		"secure_cookies", cfg.SecureCookies,
	)

	// Ensure parent directory for SQLite DB exists
	dir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		slog.Error("Failed to create database directory", "dir", dir, "error", err)
		os.Exit(1)
	}

	store, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		slog.Error("Failed to open SQLite database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	authHandler, err := auth.New(ctx, auth.Config{
		IssuerURL:     cfg.OIDCIssuerURL,
		ClientID:      cfg.OIDCClientID,
		ClientSecret:  cfg.OIDCClientSecret,
		BaseURL:       cfg.BaseURL,
		SessionSecret: []byte(cfg.SessionSecret),
		SecureCookies: cfg.SecureCookies,
	})
	if err != nil {
		slog.Error("Failed to initialize OIDC authentication", "error", err)
		os.Exit(1)
	}

	router := web.NewRouter(store, authHandler)

	addr := net.JoinHostPort("", cfg.Port)
	if !strings.Contains(cfg.Port, ":") {
		addr = ":" + cfg.Port
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("Server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		slog.Error("Server error", "error", err)
		os.Exit(1)
	case sig := <-shutdownChan:
		slog.Info("Shutdown signal received", "signal", sig.String())
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Graceful shutdown failed", "error", err)
	}

	slog.Info("Server stopped cleanly")
}
