// Package config loads groundtruth's runtime configuration from environment
// variables. There is no config file in v1 — everything is 12-factor-style.
package config

import "os"

// Config holds groundtruth's runtime configuration.
type Config struct {
	// Addr is the address the HTTP server listens on, e.g. ":8080".
	Addr string
}

// Load reads configuration from environment variables, applying defaults
// for anything unset.
func Load() Config {
	return Config{
		Addr: getEnv("GROUNDTRUTH_ADDR", ":8080"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
