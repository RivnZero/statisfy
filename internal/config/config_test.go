package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultNoConfig(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if cfg.CacheTTL != DefaultCacheTTL || cfg.Timeout != DefaultTimeout {
		t.Errorf("defaults = %v/%v", cfg.CacheTTL, cfg.Timeout)
	}
	if !cfg.AdapterEnabled("codex") {
		t.Error("codex should be enabled by default")
	}
}

func TestLoadFullConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
cache_ttl = "5m"
timeout = "8s"
watch_interval = "15s"

[adapters.codex]
enabled = true

[adapters.freebuff]
enabled = false
cache_ttl = "2m"

[adapters.gemini]
enabled = true
cache_ttl = "2m"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CacheTTL != 5*time.Minute {
		t.Errorf("cache_ttl = %v", cfg.CacheTTL)
	}
	if cfg.Timeout != 8*time.Second {
		t.Errorf("timeout = %v", cfg.Timeout)
	}
	if !cfg.AdapterEnabled("codex") {
		t.Error("codex disabled unexpectedly")
	}
	if cfg.AdapterEnabled("freebuff") {
		t.Error("freebuff should be disabled")
	}
	if cfg.TTLFor("freebuff") != 2*time.Minute {
		t.Errorf("freebuff ttl = %v", cfg.TTLFor("freebuff"))
	}
	if cfg.TTLFor("codex") != 5*time.Minute {
		t.Errorf("codex ttl = %v, want global default", cfg.TTLFor("codex"))
	}
}

func TestMalformedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("cache_ttl = [not valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("malformed config should error")
	}
}

func TestInvalidDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "cache_ttl = \"banana\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("invalid duration should error")
	}
}

func TestUnknownFieldsTolerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "future_feature = true\ncache_ttl = \"3m\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unknown fields should be tolerated: %v", err)
	}
	if cfg.CacheTTL != 3*time.Minute {
		t.Errorf("cache_ttl = %v", cfg.CacheTTL)
	}
}
