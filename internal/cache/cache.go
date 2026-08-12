// Package cache implements a tiny per-adapter file cache. Each adapter is
// cached under its own key with its own TTL, so expiring or refreshing one
// adapter never affects another. Corrupt or unreadable cache files are treated
// as misses; cache failures never break normal CLI operation.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"statisfy/internal/core"
)

// Cache persists core.Status values as JSON files in a cache directory.
type Cache struct {
	dir string
	// defaultTTL applies when no per-adapter override is set.
	defaultTTL time.Duration
	// overrides maps adapter key → TTL.
	overrides map[string]time.Duration
}

// entry wraps the cached payload with a timestamp.
type entry struct {
	FetchedAt time.Time    `json:"fetched_at"`
	Status    *core.Status `json:"status"`
}

// Option configures a Cache.
type Option func(*Cache)

// WithTTL sets the global default TTL.
func WithTTL(ttl time.Duration) Option {
	return func(c *Cache) { c.defaultTTL = ttl }
}

// WithOverride sets a per-adapter TTL override.
func WithOverride(key string, ttl time.Duration) Option {
	return func(c *Cache) { c.overrides[key] = ttl }
}

// New creates a cache rooted at dir with the given default TTL.
func New(dir string, defaultTTL time.Duration, opts ...Option) *Cache {
	c := &Cache{
		dir:        dir,
		defaultTTL: defaultTTL,
		overrides:  map[string]time.Duration{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// DefaultDir returns the per-user cache directory for statisfy.
func DefaultDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "statisfy-cache")
	}
	return filepath.Join(dir, "statisfy")
}

func (c *Cache) path(key string) string {
	return filepath.Join(c.dir, key+".json")
}

func (c *Cache) ttlFor(key string) time.Duration {
	if c == nil {
		return 0
	}
	if t, ok := c.overrides[key]; ok {
		return t
	}
	return c.defaultTTL
}

// Get returns a cached status if present, fresh, and parseable.
func (c *Cache) Get(key string) (*core.Status, bool) {
	if c == nil || c.dir == "" {
		return nil, false
	}
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		_ = os.Remove(c.path(key)) // corrupt cache: drop it safely
		return nil, false
	}
	if e.Status == nil || e.Status.Tool == "" {
		return nil, false
	}
	if ttl := c.ttlFor(key); ttl > 0 && time.Since(e.FetchedAt) > ttl {
		return nil, false // stale; caller re-fetches
	}
	return e.Status, true
}

// Put writes a status to the cache. Failures are silently ignored.
func (c *Cache) Put(key string, st *core.Status) error {
	if c == nil || c.dir == "" {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	e := entry{FetchedAt: time.Now(), Status: st}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	path := c.path(key)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	// os.Rename cannot overwrite an existing destination on Windows, so drop
	// the old entry first. Not atomic, but correctness beats atomicity here
	// and a lost update is harmless (cache is best-effort).
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}
