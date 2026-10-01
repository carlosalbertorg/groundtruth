// Package config loads groundtruth's runtime configuration from environment
// variables. There is no config file in v1 — everything is 12-factor-style.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Config holds groundtruth's runtime configuration.
type Config struct {
	// Addr is the address the HTTP server listens on, e.g. ":8080".
	Addr string

	// DataDir holds groundtruth's SQLite database file.
	DataDir string

	// BaseURL is the externally-visible URL groundtruth is reached at
	// (e.g. "https://groundtruth.example.com"), used only to decide
	// whether the session cookie should be marked Secure. Unset means
	// "assume plain HTTP," so local development works without any
	// configuration.
	BaseURL string
}

// DBPath is the SQLite database file path, inside DataDir.
func (c Config) DBPath() string {
	return filepath.Join(c.DataDir, "groundtruth.db")
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
		Addr:    getEnv("GROUNDTRUTH_ADDR", ":8080"),
		DataDir: getEnv("GROUNDTRUTH_DATA_DIR", "./data"),
		BaseURL: getEnv("GROUNDTRUTH_BASE_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
