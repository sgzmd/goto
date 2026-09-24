package main

import (
	"os"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	setEnv := func(env map[string]string) {
		os.Clearenv()
		for k, v := range env {
			os.Setenv(k, v)
		}
	}

	validEnv := map[string]string{
		"PORT":               "9090",
		"BASE_URL":           "https://go.example.com",
		"DB_PATH":            "/tmp/test.db",
		"OIDC_ISSUER_URL":    "https://auth.example.com",
		"OIDC_CLIENT_ID":     "client-1",
		"OIDC_CLIENT_SECRET": "secret-1",
		"SESSION_SECRET":     "a-very-long-secret-key-that-is-32-bytes!",
		"SECURE_COOKIES":     "true",
	}

	t.Run("Valid configuration", func(t *testing.T) {
		setEnv(validEnv)
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Port != "9090" || cfg.BaseURL != "https://go.example.com" || !cfg.SecureCookies {
			t.Errorf("unexpected config: %+v", cfg)
		}
	})

	t.Run("Missing BASE_URL", func(t *testing.T) {
		env := make(map[string]string)
		for k, v := range validEnv {
			env[k] = v
		}
		delete(env, "BASE_URL")
		setEnv(env)
		_, err := loadConfig()
		if err == nil {
			t.Fatal("expected error for missing BASE_URL")
		}
	})

	t.Run("Missing OIDC_ISSUER_URL", func(t *testing.T) {
		env := make(map[string]string)
		for k, v := range validEnv {
			env[k] = v
		}
		delete(env, "OIDC_ISSUER_URL")
		setEnv(env)
		_, err := loadConfig()
		if err == nil {
			t.Fatal("expected error for missing OIDC_ISSUER_URL")
		}
	})

	t.Run("Short SESSION_SECRET", func(t *testing.T) {
		env := make(map[string]string)
		for k, v := range validEnv {
			env[k] = v
		}
		env["SESSION_SECRET"] = "short-secret"
		setEnv(env)
		_, err := loadConfig()
		if err == nil {
			t.Fatal("expected error for short SESSION_SECRET")
		}
	})

	t.Run("Default PORT and DB_PATH", func(t *testing.T) {
		env := make(map[string]string)
		for k, v := range validEnv {
			env[k] = v
		}
		delete(env, "PORT")
		delete(env, "DB_PATH")
		setEnv(env)
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Port != "8080" {
			t.Errorf("expected default port 8080, got %s", cfg.Port)
		}
		if cfg.DBPath != "/data/goto.db" {
			t.Errorf("expected default db path /data/goto.db, got %s", cfg.DBPath)
		}
	})
}
