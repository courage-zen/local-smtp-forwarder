package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration.
type Config struct {
	Listen   string         `yaml:"listen"`
	Upstream UpstreamConfig `yaml:"upstream"`
	// FromDisplayNames maps sender email addresses (lowercased at load) to
	// the display name to set on the From header when it is missing or bare.
	// File-only: there is no env override for this mapping.
	FromDisplayNames map[string]string `yaml:"from_display_names"`
}

// UpstreamConfig holds the real SMTP relay settings that actually deliver email.
type UpstreamConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	TLS      string `yaml:"tls"` // "starttls" (default), "implicit", "none"
}

// LoadConfig reads the YAML config file and applies env-var overrides.
func LoadConfig() (*Config, error) {
	path := os.Getenv("MAILER_CONFIG")
	if path == "" {
		path = "config.yaml"
	}

	cfg := &Config{
		Listen: "127.0.0.1:2525",
		Upstream: UpstreamConfig{
			Port: 587,
			TLS:  "starttls",
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// Missing config file is OK: fall back to defaults + env overrides.
		// This lets the binary run in environments (e.g. k8s) that inject
		// all config via environment variables and ship no config file.
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	} else {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
		// Lowercase mapping keys so lookups are case-insensitive.
		cfg.FromDisplayNames = lowerKeyMap(cfg.FromDisplayNames)
	}

	// Env-var overrides (take precedence over file).
	if v := os.Getenv("MAILER_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("MAILER_UPSTREAM_HOST"); v != "" {
		cfg.Upstream.Host = v
	}
	if v := os.Getenv("MAILER_UPSTREAM_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Upstream.Port = p
		}
	}
	if v := os.Getenv("MAILER_UPSTREAM_USERNAME"); v != "" {
		cfg.Upstream.Username = v
	}
	if v := os.Getenv("MAILER_UPSTREAM_PASSWORD"); v != "" {
		cfg.Upstream.Password = v
	}
	if v := os.Getenv("MAILER_UPSTREAM_TLS"); v != "" {
		cfg.Upstream.TLS = v
	}

	// Validate.
	if cfg.Upstream.Host == "" {
		return nil, fmt.Errorf("upstream.host is required")
	}
	if cfg.Upstream.Username == "" {
		return nil, fmt.Errorf("upstream.username is required")
	}
	if cfg.Upstream.Password == "" {
		return nil, fmt.Errorf("upstream.password is required")
	}

	return cfg, nil
}

// lowerKeyMap returns a copy of m with all keys lowercased. A nil map
// returns nil.
func lowerKeyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[strings.ToLower(k)] = v
	}
	return out
}
