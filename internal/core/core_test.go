package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDetectionAvailable(t *testing.T) {
	cases := []struct {
		name string
		d    Detection
		want bool
	}{
		{"installed+authenticated", Detection{Installed: true, Authenticated: true}, true},
		{"installed+configured", Detection{Installed: true, Configured: true}, true},
		{"installed only", Detection{Installed: true}, false},
		{"authenticated only", Detection{Authenticated: true}, false},
		{"nothing", Detection{}, false},
	}
	for _, c := range cases {
		if got := c.d.Available(); got != c.want {
			t.Errorf("%s: Available() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLimitPercentLeft(t *testing.T) {
	l := Limit{PercentUsed: 82}
	if l.PercentLeft() != 18 {
		t.Errorf("PercentLeft = %v, want 18", l.PercentLeft())
	}
	u := Limit{PercentUsed: -1}
	if u.PercentLeft() != -1 {
		t.Errorf("unknown PercentLeft = %v, want -1", u.PercentLeft())
	}
}

func TestCollectIsolatesFailures(t *testing.T) {
	ok := &stubAdapter{id: "ok", det: Detection{Installed: true, Authenticated: true}}
	bad := &stubAdapter{id: "bad", det: Detection{Installed: true, Authenticated: true}, err: errors.New("boom")}
	reg := NewRegistry(ok, bad)
	detections := DetectAll(context.Background(), reg)

	results := Collect(context.Background(), reg, detections, Options{PerFetchTimeout: 5 * time.Second})
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	var okStatus, okErr bool
	for _, r := range results {
		switch r.Adapter.ID() {
		case "ok":
			okStatus = r.Status != nil
		case "bad":
			okErr = r.FetchErr != nil
		}
	}
	if !okStatus {
		t.Error("ok adapter did not produce status")
	}
	if !okErr {
		t.Error("bad adapter did not record fetch error")
	}
}

func TestCollectDoesNotCacheSkipCacheStatus(t *testing.T) {
	mc := &memCache{data: map[string]*Status{}}
	reg := NewRegistry(&stubAdapter{id: "sk", det: Detection{Installed: true, Authenticated: true}, skipCache: true})
	detections := DetectAll(context.Background(), reg)
	results := Collect(context.Background(), reg, detections, Options{Cache: mc})
	if len(results) != 1 || results[0].Status == nil {
		t.Fatalf("results = %+v", results)
	}
	if _, ok := mc.data["sk"]; ok {
		t.Error("SkipCache status must not be written to the cache")
	}
}

func TestCollectFiltersUnavailable(t *testing.T) {
	avail := &stubAdapter{id: "avail", det: Detection{Installed: true, Authenticated: true}}
	notAvail := &stubAdapter{id: "not", det: Detection{Installed: true}}
	reg := NewRegistry(avail, notAvail)
	detections := DetectAll(context.Background(), reg)
	results := Collect(context.Background(), reg, detections, Options{})
	results = FilterAvailable(results)
	if len(results) != 1 || results[0].Adapter.ID() != "avail" {
		t.Errorf("filtered = %d results, want only avail", len(results))
	}
}

type stubAdapter struct {
	id        string
	det       Detection
	err       error
	skipCache bool
}

func (s *stubAdapter) ID() string   { return s.id }
func (s *stubAdapter) Name() string { return s.id }
func (s *stubAdapter) Detect(ctx context.Context) Detection {
	return s.det
}
func (s *stubAdapter) Fetch(ctx context.Context) (*Status, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &Status{Tool: s.id, Name: s.id, SkipCache: s.skipCache}, nil
}

type memCache struct {
	data map[string]*Status
}

func (m *memCache) Get(key string) (*Status, bool) {
	st, ok := m.data[key]
	return st, ok
}
func (m *memCache) Put(key string, st *Status) error {
	m.data[key] = st
	return nil
}
