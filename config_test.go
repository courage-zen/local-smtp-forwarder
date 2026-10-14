package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFromDisplayNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
listen: "127.0.0.1:9999"
upstream:
  host: "smtp.example.com"
  port: 465
  username: "u"
  password: "p"
  tls: "implicit"
from_display_names:
  Alice@Example.com: "张三"
  BOB@example.com: "Bob"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAILER_CONFIG", path)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if got := cfg.FromDisplayNames["alice@example.com"]; got != "张三" {
		t.Errorf("alice key not lowercased or missing: got %q", got)
	}
	if got := cfg.FromDisplayNames["bob@example.com"]; got != "Bob" {
		t.Errorf("bob key not lowercased or missing: got %q", got)
	}
	if len(cfg.FromDisplayNames) != 2 {
		t.Errorf("want 2 entries, got %d", len(cfg.FromDisplayNames))
	}
}

func TestLoadConfigNoFromDisplayNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
listen: "127.0.0.1:9999"
upstream:
  host: "smtp.example.com"
  username: "u"
  password: "p"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAILER_CONFIG", path)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.FromDisplayNames != nil {
		t.Errorf("want nil FromDisplayNames, got %v", cfg.FromDisplayNames)
	}
}
