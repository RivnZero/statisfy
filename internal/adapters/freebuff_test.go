package adapters

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFreebuffFetchFromFixture(t *testing.T) {
	dir := t.TempDir()
	writeCredentials(t, dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/freebuff/session" {
			http.NotFound(w, r)
			return
		}
		// Verify the token is sent and the UA is not Go's default (WAF rule).
		if r.Header.Get("Authorization") != "Bearer test-token-abc" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if ua := r.Header.Get("User-Agent"); ua == "" || ua == "Go-http-client/1.1" {
			t.Errorf("user-agent = %q, want a real client string", ua)
		}
		data, err := os.ReadFile(filepath.Join("fixtures", "freebuff_session.json"))
		if err != nil {
			t.Errorf("fixture: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
	defer srv.Close()

	a := FreebuffAdapter{OverrideBaseURL: srv.URL, OverrideConfigDir: dir}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(st.Limits) != 3 {
		t.Fatalf("limits = %d, want 3 (deepseek, mimo, glm)", len(st.Limits))
	}
	// DeepSeek limit: limit 6, recentCount 0.7 (kept raw in JSON).
	ds := st.Limits[0]
	if ds.Label != "DeepSeek" || ds.Total != 6 || ds.Used != 0.7 {
		t.Errorf("deepseek limit = %+v", ds)
	}
	// Sorting is deterministic: deepseek before mimo.
	if st.Limits[1].Label != "MiMo" {
		t.Errorf("second limit = %+v, want MiMo", st.Limits[1])
	}
	if ds.ResetAt == nil {
		t.Error("deepseek resetAt missing")
	}
	// GLM promo has unknown usage — must not be fabricated.
	glm := st.Limits[2]
	if glm.Label != "GLM" || glm.Total != 6 {
		t.Errorf("glm limit = %+v", glm)
	}
	if glm.Known() {
		t.Errorf("glm used is known (%v), want unknown", glm.Used)
	}
	// Message quotas from standing.limits.
	if len(st.Metrics) != 2 {
		t.Errorf("metrics = %d, want 2", len(st.Metrics))
	}
	if st.Tier != "Limited" {
		t.Errorf("tier = %q, want Limited", st.Tier)
	}
}

func TestFreebuffDetect(t *testing.T) {
	dir := t.TempDir()
	writeCredentials(t, dir)
	a := FreebuffAdapter{OverrideConfigDir: dir}
	d := a.Detect(context.Background())
	if !d.Installed || !d.Authenticated || !d.Configured {
		t.Errorf("detect = %+v, want fully available", d)
	}
	if !d.Available() {
		t.Error("available = false, want true")
	}
}

func TestFreebuffDetectMissing(t *testing.T) {
	a := FreebuffAdapter{OverrideConfigDir: t.TempDir()}
	d := a.Detect(context.Background())
	if d.Available() {
		t.Errorf("detect = %+v, want unavailable", d)
	}
	if d.Reason == "" {
		t.Error("reason empty")
	}
}

// TestFreebuffNeverRetriesSessionRequest is the safety regression for the
// blocking interference bug: the session endpoint may be stateful (session
// ownership), so a failed request must NEVER be retried automatically. The old
// behavior fired it up to 3 times, multiplying any side effect.
func TestFreebuffNeverRetriesSessionRequest(t *testing.T) {
	dir := t.TempDir()
	writeCredentials(t, dir)
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		// Force a transport-level failure; a retry loop would hit us again.
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := FreebuffAdapter{OverrideBaseURL: srv.URL, OverrideConfigDir: dir}
	if _, err := a.Fetch(context.Background()); err == nil {
		t.Fatal("Fetch should fail when the single request fails")
	}
	if attempts != 1 {
		t.Errorf("requests = %d, want exactly 1 (never auto-retry a potentially stateful request)", attempts)
	}
}

// TestFreebuffSkipsNetworkWhileInstanceRunning is the safety regression for
// the blocking interference bug: while a Freebuff instance is alive, Fetch must
// make ZERO network requests — statisfy must never risk the active session.
func TestFreebuffSkipsNetworkWhileInstanceRunning(t *testing.T) {
	dir := t.TempDir()
	writeCredentials(t, dir)
	// Point the owner file at this live test process: it is provably alive.
	owner := fmt.Sprintf(`{"instanceId": "test", "pid": %d}`, os.Getpid())
	if err := os.WriteFile(filepath.Join(dir, "freebuff-instance-owner.json"), []byte(owner), 0o600); err != nil {
		t.Fatalf("write owner file: %v", err)
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should never be called", 500)
	}))
	defer srv.Close()

	a := FreebuffAdapter{OverrideBaseURL: srv.URL, OverrideConfigDir: dir}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0 (no API call while a Freebuff instance is running)", requests)
	}
	if len(st.Limits) != 0 {
		t.Errorf("limits = %d, want 0 (no live session data while active)", len(st.Limits))
	}
	if len(st.Errors) == 0 {
		t.Error("expected an explanatory note when live fetch is skipped")
	}
}

// TestFreebuffQueriesOnceWhenInstanceNotRunning verifies the safe path: with no
// live instance, exactly one read-only GET is performed.
func TestFreebuffQueriesOnceWhenInstanceNotRunning(t *testing.T) {
	dir := t.TempDir()
	writeCredentials(t, dir)
	// A pid that cannot be alive anywhere, plus an old owner file, keeps the
	// guard inactive (a fresh file would be treated conservatively as active).
	owner := `{"instanceId": "test", "pid": 99999999}`
	ownerPath := filepath.Join(dir, "freebuff-instance-owner.json")
	if err := os.WriteFile(ownerPath, []byte(owner), 0o600); err != nil {
		t.Fatalf("write owner file: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(ownerPath, old, old); err != nil {
		t.Fatalf("age owner file: %v", err)
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET only", r.Method)
		}
		data, _ := os.ReadFile(filepath.Join("fixtures", "freebuff_session.json"))
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
	defer srv.Close()

	a := FreebuffAdapter{OverrideBaseURL: srv.URL, OverrideConfigDir: dir}
	if _, err := a.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if requests != 1 {
		t.Errorf("requests = %d, want exactly 1 (single read-only GET)", requests)
	}
}

func writeCredentials(t *testing.T, dir string) {
	t.Helper()
	creds := `{"default": {"id": "u1", "name": "tester", "email": "t@example.com", "authToken": "test-token-abc"}}`
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
}
