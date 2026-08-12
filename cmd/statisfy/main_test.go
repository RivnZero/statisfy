package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"statisfy/internal/config"
)

// TestLoadConfigOrWarnAbsentIsSilent proves an absent config file is not an
// error condition: no diagnostic is printed and defaults are used.
func TestLoadConfigOrWarnAbsentIsSilent(t *testing.T) {
	var stderr bytes.Buffer
	cfg := loadConfigOrWarn(filepath.Join(t.TempDir(), "missing.toml"), &stderr)
	if stderr.Len() != 0 {
		t.Errorf("absent config printed a diagnostic: %q", stderr.String())
	}
	if cfg == nil || cfg.CacheTTL != config.DefaultCacheTTL {
		t.Errorf("expected defaults, got %+v", cfg)
	}
}

// TestLoadConfigOrWarnMalformedCannotFailSilently proves a file that exists
// but is not valid TOML always surfaces an actionable diagnostic on stderr and
// falls back to defaults — it is never silently accepted.
func TestLoadConfigOrWarnMalformedCannotFailSilently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("cache_ttl = [not valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cfg := loadConfigOrWarn(path, &stderr)
	if !strings.Contains(stderr.String(), "using built-in defaults") {
		t.Errorf("malformed config missing actionable diagnostic: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), path) {
		t.Errorf("diagnostic should name the config file: %q", stderr.String())
	}
	if cfg.CacheTTL != config.DefaultCacheTTL {
		t.Errorf("expected default fallback, got %v", cfg.CacheTTL)
	}
}

// TestLoadConfigOrWarnInvalidDurationCannotFailSilently proves an invalid
// duration value is reported, not silently reinterpreted.
func TestLoadConfigOrWarnInvalidDurationCannotFailSilently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("cache_ttl = \"banana\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cfg := loadConfigOrWarn(path, &stderr)
	if !strings.Contains(stderr.String(), "using built-in defaults") {
		t.Errorf("invalid duration missing diagnostic: %q", stderr.String())
	}
	if cfg.CacheTTL != config.DefaultCacheTTL {
		t.Errorf("expected default fallback, got %v", cfg.CacheTTL)
	}
}

// TestLoadConfigOrWarnValidIsSilent proves a valid config is used without any
// diagnostic.
func TestLoadConfigOrWarnValidIsSilent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("cache_ttl = \"5m\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cfg := loadConfigOrWarn(path, &stderr)
	if stderr.Len() != 0 {
		t.Errorf("valid config printed a diagnostic: %q", stderr.String())
	}
	if cfg.CacheTTL != 5*time.Minute {
		t.Errorf("cache_ttl = %v, want 5m", cfg.CacheTTL)
	}
}
