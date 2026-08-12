// Package config loads the optional user configuration file. The application
// must work correctly with no config file at all; config only overrides
// built-in defaults and is itself overridden by CLI flags.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Defaults that apply when no config file exists.
const (
	DefaultCacheTTL = 60 * time.Second
	DefaultTimeout  = 30 * time.Second
	DefaultWatch    = 10 * time.Second
)

// Config is the fully-resolved configuration.
type Config struct {
	CacheTTL      time.Duration
	Timeout       time.Duration
	WatchInterval time.Duration
	// Disabled maps adapter ID → disabled (from [adapters.X] enabled=false).
	Disabled map[string]bool
	// AdapterTTL maps adapter ID → per-adapter cache TTL override.
	AdapterTTL map[string]time.Duration
	// Path is the config file that was loaded ("" when none).
	Path string
}

// raw mirrors the config file schema. Durations are TOML strings like "5m".
type raw struct {
	CacheTTL      string `toml:"cache_ttl"`
	Timeout       string `toml:"timeout"`
	WatchInterval string `toml:"watch_interval"`
	Adapters      map[string]rawAdapter
}

type rawAdapter struct {
	Enabled  *bool  `toml:"enabled"`
	CacheTTL string `toml:"cache_ttl"`
}

// Load reads the config file at path, if present. A malformed config returns
// an error (caller decides how to surface it); a missing file is not an error.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var r raw
	if err := toml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.applyRaw(&r, path); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyRaw(r *raw, path string) error {
	c.Path = path
	var errs []string
	if r.CacheTTL != "" {
		if d, err := time.ParseDuration(r.CacheTTL); err != nil {
			errs = append(errs, fmt.Sprintf("cache_ttl: %v", err))
		} else {
			c.CacheTTL = d
		}
	}
	if r.Timeout != "" {
		if d, err := time.ParseDuration(r.Timeout); err != nil {
			errs = append(errs, fmt.Sprintf("timeout: %v", err))
		} else {
			c.Timeout = d
		}
	}
	if r.WatchInterval != "" {
		if d, err := time.ParseDuration(r.WatchInterval); err != nil {
			errs = append(errs, fmt.Sprintf("watch_interval: %v", err))
		} else {
			c.WatchInterval = d
		}
	}
	for id, a := range r.Adapters {
		if a.Enabled != nil && !*a.Enabled {
			c.Disabled[id] = true
		}
		if a.CacheTTL != "" {
			d, err := time.ParseDuration(a.CacheTTL)
			if err != nil {
				errs = append(errs, fmt.Sprintf("adapters.%s.cache_ttl: %v", id, err))
				continue
			}
			c.AdapterTTL[id] = d
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid config %s: %s", path, strings.Join(errs, "; "))
	}
	return nil
}

// Default returns the built-in defaults.
func Default() *Config {
	return &Config{
		CacheTTL:      DefaultCacheTTL,
		Timeout:       DefaultTimeout,
		WatchInterval: DefaultWatch,
		Disabled:      map[string]bool{},
		AdapterTTL:    map[string]time.Duration{},
	}
}

// ConfigDir returns the platform-aware user config directory for statisfy.
func ConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(homeFallback(), ".config", "statisfy")
	}
	return filepath.Join(dir, "statisfy")
}

// ConfigPath returns the default config file location.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.toml")
}

// AdapterEnabled reports whether the adapter is enabled.
func (c *Config) AdapterEnabled(id string) bool {
	if c == nil {
		return true
	}
	return !c.Disabled[id]
}

// TTLFor returns the effective cache TTL for an adapter (override or default).
func (c *Config) TTLFor(id string) time.Duration {
	if c == nil {
		return DefaultCacheTTL
	}
	if d, ok := c.AdapterTTL[id]; ok {
		return d
	}
	return c.CacheTTL
}

func homeFallback() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "."
}
