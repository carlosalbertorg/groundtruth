// Package config loads groundtruth's runtime configuration from environment
// variables. There is no config file in v1 — everything is 12-factor-style.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds groundtruth's runtime configuration.
type Config struct {
	// Addr is the address the HTTP server listens on, e.g. ":8080".
	Addr string

	// DataDir holds everything groundtruth persists: its SQLite database,
	// the shared provider plugin cache, and per-check scratch space. It's
	// created owner-only (see store.RestrictAccess).
	DataDir string

	// BaseURL is the externally-visible URL groundtruth is reached at
	// (e.g. "https://groundtruth.example.com"). It decides whether the
	// session cookie is marked Secure, and is the base of the dashboard
	// link in alert payloads. Unset means "assume plain HTTP," so local
	// development works without any configuration.
	BaseURL string

	// MaxConcurrentChecks bounds how many drift checks the scheduler
	// runs at the same time, across all workspaces.
	MaxConcurrentChecks int
}

// DBPath is the SQLite database file path, inside DataDir.
func (c Config) DBPath() string {
	return filepath.Join(c.DataDir, "groundtruth.db")
}

// CheckTmpDir holds the disposable per-check working directories the
// executor creates and removes for every drift check.
func (c Config) CheckTmpDir() string {
	return filepath.Join(c.DataDir, "tmp")
}

// PluginCacheDir is shared and persistent across every check and
// workspace, so provider plugins are downloaded once rather than on
// every scheduled run.
func (c Config) PluginCacheDir() string {
	return filepath.Join(c.DataDir, "plugin-cache")
}

// SecureCookies reports whether the session cookie should be marked
// Secure (HTTPS-only). True whenever BaseURL explicitly says https.
func (c Config) SecureCookies() bool {
	return strings.HasPrefix(c.BaseURL, "https://")
}

// Load reads configuration from environment variables, applying defaults
// for anything unset.
func Load() Config {
	return Config{
		Addr:                getEnv("GROUNDTRUTH_ADDR", ":8080"),
		DataDir:             getEnv("GROUNDTRUTH_DATA_DIR", "./data"),
		BaseURL:             getEnv("GROUNDTRUTH_BASE_URL", ""),
		MaxConcurrentChecks: getEnvInt("GROUNDTRUTH_MAX_CONCURRENT_CHECKS", 3),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
