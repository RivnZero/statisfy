package core

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Registry holds all compiled-in adapters.
type Registry struct {
	adapters []Adapter
}

// NewRegistry builds a registry from the given adapters.
func NewRegistry(adapters ...Adapter) *Registry {
	return &Registry{adapters: adapters}
}

// All returns the registered adapters.
func (r *Registry) All() []Adapter {
	return r.adapters
}

// Get returns the adapter with the given ID, if present.
func (r *Registry) Get(id string) (Adapter, bool) {
	for _, a := range r.adapters {
		if a.ID() == id {
			return a, true
		}
	}
	return nil, false
}

// IDs returns the sorted list of adapter IDs.
func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.adapters))
	for _, a := range r.adapters {
		ids = append(ids, a.ID())
	}
	sort.Strings(ids)
	return ids
}

// ToolResult couples an adapter with its detection and fetch outcome.
type ToolResult struct {
	Adapter   Adapter
	Detection Detection
	Status    *Status // nil when unavailable or fetch failed
	FetchErr  error   // fetch error, kept for diagnostics
	Cached    bool    // true when served from cache
}

// Options controls how the registry runs detection and fetching.
type Options struct {
	// Refresh bypasses the cache.
	Refresh bool
	// All includes unavailable integrations.
	All bool
	// PerFetchTimeout bounds each adapter's Fetch call.
	PerFetchTimeout time.Duration
	// Cache stores per-adapter Status values.
	Cache Cacher
}

// Cacher is the minimal cache contract the core depends on.
type Cacher interface {
	Get(key string) (*Status, bool)
	Put(key string, st *Status) error
}

// DetectAll runs Detect on every adapter concurrently (bounded).
func DetectAll(ctx context.Context, reg *Registry) map[string]Detection {
	detections := make(map[string]Detection, len(reg.adapters))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, a := range reg.adapters {
		wg.Add(1)
		go func(a Adapter) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d := a.Detect(ctx)
			mu.Lock()
			detections[a.ID()] = d
			mu.Unlock()
		}(a)
	}
	wg.Wait()
	return detections
}

// Collect runs detection, then fetches status for available adapters
// concurrently. A failure in one adapter never blocks the others.
func Collect(ctx context.Context, reg *Registry, detections map[string]Detection, opts Options) []ToolResult {
	results := make([]ToolResult, 0, len(reg.adapters))
	for _, a := range reg.adapters {
		d := detections[a.ID()]
		results = append(results, ToolResult{Adapter: a, Detection: d})
	}

	// Fetch phase for available adapters, concurrently.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	var mu sync.Mutex
	for i := range results {
		if !results[i].Detection.Available() && !opts.All {
			continue
		}
		if !results[i].Detection.Available() {
			continue // status stays nil; --all shows detection only
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			a := results[i].Adapter
			key := a.ID()

			if opts.Cache != nil && !opts.Refresh {
				if st, ok := opts.Cache.Get(key); ok && st != nil {
					mu.Lock()
					results[i].Status = st
					results[i].Cached = true
					mu.Unlock()
					return
				}
			}

			fetchCtx := ctx
			var cancel context.CancelFunc
			if opts.PerFetchTimeout > 0 {
				fetchCtx, cancel = context.WithTimeout(ctx, opts.PerFetchTimeout)
				defer cancel()
			}
			st, err := a.Fetch(fetchCtx)
			if err != nil {
				mu.Lock()
				results[i].FetchErr = err
				mu.Unlock()
				return
			}
			if st == nil {
				mu.Lock()
				results[i].FetchErr = fmt.Errorf("adapter returned nil status")
				mu.Unlock()
				return
			}
			st.LastChecked = time.Now()
			if opts.Cache != nil && !st.SkipCache {
				_ = opts.Cache.Put(key, st) // cache failure must not break the CLI
			}
			mu.Lock()
			results[i].Status = st
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	return results
}

// FilterAvailable returns only results whose detection is available.
func FilterAvailable(results []ToolResult) []ToolResult {
	out := make([]ToolResult, 0, len(results))
	for _, r := range results {
		if r.Detection.Available() {
			out = append(out, r)
		}
	}
	return out
}
