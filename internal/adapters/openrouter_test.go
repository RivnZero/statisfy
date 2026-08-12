package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadOpenRouterFixture(t *testing.T) *openRouterKey {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("fixtures", "openrouter_key.json"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	var r openRouterKey
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return &r
}

func TestOpenRouterParseFixture(t *testing.T) {
	st, err := loadOpenRouterFixture(t).Data.status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	got := map[string]float64{}
	for _, m := range st.Metrics {
		got[m.Label] = m.Value
	}
	if got["Usage"] != 38.2 || got["Monthly"] != 25.1 || got["Daily"] != 1.24 {
		t.Errorf("usage metrics = %v", got)
	}
	if got["Limit"] != 100 || got["Remaining"] != 74.8 {
		t.Errorf("limit metrics = %v", got)
	}
	if st.Plan != "" {
		t.Errorf("plan = %q, want empty (not free tier)", st.Plan)
	}
	if st.Source != "official-api" || st.Stability != "documented" {
		t.Errorf("source/stability = %v/%v", st.Source, st.Stability)
	}
}

func TestOpenRouterFreeTierPlan(t *testing.T) {
	d := &openRouterKeyData{IsFreeTier: true, Usage: 0}
	st, err := d.status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Plan != "Free" {
		t.Errorf("plan = %q, want Free", st.Plan)
	}
	if len(st.Metrics) != 0 {
		t.Errorf("metrics = %v, want none (zero usage omitted)", st.Metrics)
	}
}

func TestOpenRouterDetect(t *testing.T) {
	// Force-empty the env var so the test is deterministic either way.
	t.Setenv("OPENROUTER_API_KEY", "")
	a := OpenRouterAdapter{}
	d := a.Detect(context.Background())
	if d.Available() {
		t.Errorf("detect without key = %+v, want unavailable", d)
	}
	if d.ReasonKind != "authentication_missing" {
		t.Errorf("reason_kind = %q, want authentication_missing", d.ReasonKind)
	}

	a2 := OpenRouterAdapter{OverrideKey: "sk-or-v1-test"}
	if d2 := a2.Detect(context.Background()); !d2.Available() {
		t.Errorf("detect with key = %+v, want available", d2)
	}
}

func TestOpenRouterFetchSingleGETNoLeak(t *testing.T) {
	requests := 0
	var auth string
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/key" {
			t.Errorf("path = %s, want /api/v1/key", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		ua = r.Header.Get("User-Agent")
		data, _ := os.ReadFile(filepath.Join("fixtures", "openrouter_key.json"))
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
	defer srv.Close()

	a := OpenRouterAdapter{OverrideBaseURL: srv.URL, OverrideKey: "sk-or-v1-test-secret"}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if requests != 1 {
		t.Errorf("requests = %d, want exactly 1", requests)
	}
	if auth != "Bearer sk-or-v1-test-secret" {
		t.Errorf("authorization = %q", auth)
	}
	if ua == "" || ua == "Go-http-client/1.1" {
		t.Errorf("user-agent = %q, want a real client string", ua)
	}
	// The key must never appear in the normalized status.
	blob, _ := json.Marshal(st)
	if strings.Contains(string(blob), "sk-or-v1-test-secret") {
		t.Error("API key leaked into the normalized status")
	}
	// The redacted label from the fixture must not be surfaced either.
	if strings.Contains(string(blob), "redacted") {
		t.Error("redacted key label leaked into the normalized status")
	}
}

func TestOpenRouterFetchNeverRetries(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := OpenRouterAdapter{OverrideBaseURL: srv.URL, OverrideKey: "sk-or-v1-test"}
	if _, err := a.Fetch(context.Background()); err == nil {
		t.Fatal("Fetch should fail on the single failed request")
	}
	if attempts != 1 {
		t.Errorf("requests = %d, want exactly 1 (never auto-retry)", attempts)
	}
}
