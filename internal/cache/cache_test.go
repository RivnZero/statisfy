package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"statisfy/internal/core"
)

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, time.Minute)
	st := &core.Status{Tool: "codex", Name: "Codex", Plan: "Plus"}
	if err := c.Put("codex", st); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := c.Get("codex")
	if !ok {
		t.Fatal("Get miss after Put")
	}
	if got.Plan != "Plus" {
		t.Errorf("plan = %q, want Plus", got.Plan)
	}
}

func TestCachePerAdapterTTLIndependence(t *testing.T) {
	dir := t.TempDir()
	// gemini has a 1ms override; codex uses the 1h default.
	c := New(dir, time.Hour, WithOverride("gemini", time.Millisecond))
	_ = c.Put("codex", &core.Status{Tool: "codex", Name: "Codex"})
	_ = c.Put("gemini", &core.Status{Tool: "gemini", Name: "Gemini"})
	time.Sleep(5 * time.Millisecond)

	if _, ok := c.Get("codex"); !ok {
		t.Error("codex entry must survive gemini's expiry (independent TTLs)")
	}
	if _, ok := c.Get("gemini"); ok {
		t.Error("gemini entry should be stale after its short TTL")
	}
}

func TestCacheExpiryDoesNotAffectOtherKeys(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, time.Hour)
	_ = c.Put("a", &core.Status{Tool: "a", Name: "A"})
	_ = c.Put("b", &core.Status{Tool: "b", Name: "B"})
	// Deleting one entry's file must not affect the other.
	_ = os.Remove(filepath.Join(dir, "a.json"))
	if _, ok := c.Get("b"); !ok {
		t.Error("removing a.json invalidated b.json")
	}
}

func TestCacheExpiry(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, 1*time.Millisecond)
	st := &core.Status{Tool: "x", Name: "X"}
	_ = c.Put("x", st)
	time.Sleep(5 * time.Millisecond)
	if _, ok := c.Get("x"); ok {
		t.Error("stale entry returned")
	}
}

func TestCacheCorruptIgnored(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, time.Minute)
	// Write garbage over the cache file.
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got, ok := c.Get("corrupt"); ok || got != nil {
		t.Error("corrupt cache returned a value")
	}
	// And it must not crash; a subsequent Put works.
	if err := c.Put("corrupt", &core.Status{Tool: "c", Name: "C"}); err != nil {
		t.Fatalf("Put after corruption: %v", err)
	}
}

func TestCacheMissing(t *testing.T) {
	c := New(t.TempDir(), time.Minute)
	if _, ok := c.Get("nope"); ok {
		t.Error("miss expected")
	}
}

func TestCacheNil(t *testing.T) {
	var c *Cache
	if _, ok := c.Get("x"); ok {
		t.Error("nil cache returned value")
	}
	if err := c.Put("x", &core.Status{Tool: "x"}); err != nil {
		t.Errorf("nil cache Put error: %v", err)
	}
}
