package adapters

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"statisfy/internal/core"
)

// writeClineState lays out a fake VS Code-family editor state dir and returns
// the claude-dev state dir path (for OverrideStateDir).
func writeClineState(t *testing.T, root, history string) string {
	t.Helper()
	dir := filepath.Join(root, "Code", "User", "globalStorage", "saoudrizwan.claude-dev")
	if err := os.MkdirAll(filepath.Join(dir, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if history != "" {
		if err := os.WriteFile(filepath.Join(dir, "state", "taskHistory.json"), []byte(history), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestClineParseFixture(t *testing.T) {
	tasks, err := parseClineTasks(readFixture(t, "cline_task_history.json"))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("tasks = %d, want 3", len(tasks))
	}
	if tasks[0].TokensIn != 12450 || tasks[0].TokensOut != 420 {
		t.Errorf("task 0 tokens = %d/%d, want 12450/420", tasks[0].TokensIn, tasks[0].TokensOut)
	}
	if tasks[1].TotalCost != 0.0214719 {
		t.Errorf("task 1 cost = %v, want 0.0214719", tasks[1].TotalCost)
	}
	if tasks[2].TS != 1754089200000 {
		t.Errorf("task 2 ts = %d", tasks[2].TS)
	}
	// The fixture's api.apiKey must be ignored — the struct does not define it.
	// (Proven implicitly by this test compiling; the leak test asserts output.)
}

func TestClineParseWrappedObject(t *testing.T) {
	data := []byte(`{"tasks": [{"tokensIn": 1, "tokensOut": 2, "totalCost": 0.5, "ts": 1}]}`)
	tasks, err := parseClineTasks(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tasks) != 1 || tasks[0].TokensIn != 1 {
		t.Errorf("tasks = %+v", tasks)
	}
}

func TestClineParseEmptyArray(t *testing.T) {
	tasks, err := parseClineTasks([]byte(`[]`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("tasks = %d, want 0", len(tasks))
	}
}

func TestClineParseMissingFields(t *testing.T) {
	// Missing numeric fields must decode to zeros, not fail.
	data := []byte(`[{"id": "x"}]`)
	tasks, err := parseClineTasks(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tasks) != 1 || tasks[0].TokensIn != 0 || tasks[0].TotalCost != 0 {
		t.Errorf("missing fields not zeroed: %+v", tasks)
	}
}

func TestClineParseMalformed(t *testing.T) {
	if _, err := parseClineTasks([]byte(`{not json`)); err == nil {
		t.Fatal("malformed history parsed without error")
	}
}

func TestClineFetchAggregatesToday(t *testing.T) {
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
	hist := []map[string]any{
		{"tokensIn": 100, "tokensOut": 50, "totalCost": 0.50, "ts": dayStart + 1000},
		{"tokensIn": 200, "tokensOut": 60, "totalCost": 0.70, "ts": dayStart + 2000},
		{"tokensIn": 999, "tokensOut": 999, "totalCost": 9.99, "ts": dayStart - 1000}, // yesterday: excluded
	}
	data, err := json.Marshal(hist)
	if err != nil {
		t.Fatal(err)
	}
	dir := writeClineState(t, t.TempDir(), string(data))
	a := ClineAdapter{OverrideStateDir: dir}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(st.Metrics) != 3 {
		t.Fatalf("metrics = %d, want 3", len(st.Metrics))
	}
	if st.Metrics[0].Value != 2 {
		t.Errorf("tasks = %v, want 2 (yesterday excluded)", st.Metrics[0].Value)
	}
	if st.Metrics[1].Value != 410 {
		t.Errorf("tokens = %v, want 410", st.Metrics[1].Value)
	}
	if st.Metrics[2].Value != 1.20 {
		t.Errorf("cost = %v, want 1.20", st.Metrics[2].Value)
	}
	// No plan/limit/account is ever invented for Cline.
	if st.Plan != "" || len(st.Limits) > 0 || st.Provider != "" || st.Model != "" {
		t.Errorf("invented data: plan=%q limits=%d provider=%q model=%q", st.Plan, len(st.Limits), st.Provider, st.Model)
	}
}

func TestClineFetchNoSecretLeak(t *testing.T) {
	dir := writeClineState(t, t.TempDir(), string(readFixture(t, "cline_task_history.json")))
	a := ClineAdapter{OverrideStateDir: dir}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	out, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"sk-ant", "apiKey", "api_key", "Bearer"} {
		if containsStr(string(out), needle) {
			t.Fatalf("secret-like %q leaked into status JSON", needle)
		}
	}
}

func TestClineDetectNotSetUp(t *testing.T) {
	a := ClineAdapter{OverrideStateDir: filepath.Join(t.TempDir(), "Code", "User", "globalStorage", "saoudrizwan.claude-dev")}
	d := a.Detect(context.Background())
	if d.Available() {
		t.Errorf("available = true without any Cline state")
	}
	if d.ReasonKind != core.ReasonNotInstalled {
		t.Errorf("reason_kind = %q, want binary_missing", d.ReasonKind)
	}
}

func TestClineDetectConfigured(t *testing.T) {
	dir := writeClineState(t, t.TempDir(), string(readFixture(t, "cline_task_history.json")))
	a := ClineAdapter{OverrideStateDir: dir}
	d := a.Detect(context.Background())
	if !d.Installed || !d.Configured {
		t.Errorf("detect = %+v, want installed+configured", d)
	}
	if !d.Available() {
		t.Errorf("available = false with parseable task history")
	}
}

func TestClineDetectStateWithoutHistory(t *testing.T) {
	dir := writeClineState(t, t.TempDir(), "") // state dir exists, no taskHistory.json
	a := ClineAdapter{OverrideStateDir: dir}
	d := a.Detect(context.Background())
	if !d.Installed {
		t.Errorf("installed = false with state dir present")
	}
	if d.Configured || d.Available() {
		t.Errorf("configured/available = true without taskHistory.json")
	}
	if d.ReasonKind != core.ReasonLocalState {
		t.Errorf("reason_kind = %q, want local_state_unavailable", d.ReasonKind)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
