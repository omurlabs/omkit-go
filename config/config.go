// config.go — config module.
//
// exports: Base | Load | PostgresDSN | ValkeyAddr | CORSOriginsList | EnvStr | EnvInt | EnvBool | EnvInt64
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package config provides base configuration for all Omur Go services.
// It reads environment variables with sensible defaults, mirroring
// the Python BaseServiceSettings pattern.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Base holds common env vars shared across all Omur backend services.
type Base struct {
	// Omur runtime
	OmurMode        string // OMUR_MODE (standalone|connected)
	OmurTenantToken string // OMUR_TENANT_TOKEN
	OmurSettingsKey string // OMUR_SETTINGS_KEY
	AppVersion      string // APP_VERSION (semantic version injected at build time)

	// CORS
	CORSOrigins string // CORS_ORIGINS

	// PostgreSQL
	PostgresHost     string
	PostgresPort     int
	PostgresDB       string
	PostgresUser     string
	PostgresPassword string

	// Valkey
	ValkeyHost     string
	ValkeyPort     int
	ValkeyPassword string

	// Ollama
	OllamaHost      string
	OllamaChatModel string
}

// Load reads all base config from environment variables.
func Load() Base {
	return Base{
		OmurMode:        envStr("OMUR_MODE", "standalone"),
		OmurTenantToken: envStr("OMUR_TENANT_TOKEN", ""),
		OmurSettingsKey: envStr("OMUR_SETTINGS_KEY", ""),
		AppVersion:      envStr("APP_VERSION", "dev"),

		CORSOrigins: envStr("CORS_ORIGINS", "https://omur.local,http://localhost:3000"),

		PostgresHost:     envStr("POSTGRES_HOST", "pgbouncer"),
		PostgresPort:     envInt("POSTGRES_PORT", 6432),
		PostgresDB:       envStr("POSTGRES_DB", "omur"),
		PostgresUser:     envStr("POSTGRES_USER", "omur"),
		PostgresPassword: envStr("POSTGRES_PASSWORD", ""),

		ValkeyHost:     envStr("VALKEY_HOST", "valkey"),
		ValkeyPort:     envInt("VALKEY_PORT", 6379),
		ValkeyPassword: envStr("VALKEY_PASSWORD", ""),

		OllamaHost:      envStr("OLLAMA_HOST", "http://ollama:11434"),
		OllamaChatModel: envStr("OLLAMA_CHAT_MODEL", "qwen3:8b"),
	}
}

// PostgresDSN returns a pgx-compatible connection string.
func (b Base) PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		b.PostgresUser, b.PostgresPassword, b.PostgresHost, b.PostgresPort, b.PostgresDB)
}

// ValkeyAddr returns host:port for the Valkey connection.
func (b Base) ValkeyAddr() string {
	return fmt.Sprintf("%s:%d", b.ValkeyHost, b.ValkeyPort)
}

// CORSOriginsList splits CORS_ORIGINS into a slice.
func (b Base) CORSOriginsList() []string {
	if b.CORSOrigins == "" {
		return nil
	}
	return strings.Split(b.CORSOrigins, ",")
}

// Env helpers

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// EnvStr is exported for service-specific config to reuse.
func EnvStr(key, fallback string) string { return envStr(key, fallback) }

// EnvInt is exported for service-specific config to reuse.
func EnvInt(key string, fallback int) int { return envInt(key, fallback) }

// EnvBool reads a boolean env var (true/1/yes are truthy).
func EnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return fallback
}

// EnvInt64 reads an int64 env var with a fallback for empty/invalid values.
func EnvInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}
