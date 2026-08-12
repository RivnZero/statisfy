package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeFetchPlanFromLocalState(t *testing.T) {
	home := t.TempDir()
	writeClaudeJson(t, home, "claude_oauth_account.json")

	a := ClaudeAdapter{OverrideHomeDir: home}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if st.Plan != "Pro" {
		t.Errorf("plan = %q, want Pro", st.Plan)
	}
	if st.Account != "user@example.com" {
		t.Errorf("account = %q, want user@example.com", st.Account)
	}
	if st.Source != "local-state" {
		t.Errorf("source = %q, want local-state", st.Source)
	}
	// No usage limits without a credentials file (Windows case): omit, never fake.
	if len(st.Limits) != 0 {
		t.Errorf("limits = %d, want 0 (no token file)", len(st.Limits))
	}
}

func TestClaudeFetchMaxPlan(t *testing.T) {
	home := t.TempDir()
	writeClaudeJson(t, home, "claude_oauth_account.json")
	// Mutate plan to claude_max.
	path := filepath.Join(home, ".claude.json")
	data, _ := os.ReadFile(path)
	data = []byte(replaceAll(string(data), `"organizationType": "claude_pro"`, `"organizationType": "claude_max"`))
	os.WriteFile(path, data, 0o600)

	a := ClaudeAdapter{OverrideHomeDir: home}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if st.Plan != "Max" {
		t.Errorf("plan = %q, want Max", st.Plan)
	}
}

func TestClaudeDetectMissingState(t *testing.T) {
	a := ClaudeAdapter{OverrideHomeDir: t.TempDir()}
	d := a.Detect(context.Background())
	// Detection also checks PATH, which may contain claude on this machine;
	// the invariant is: without oauthAccount state, the adapter must not be
	// available.
	if d.Available() {
		t.Errorf("available = true for empty home, want false")
	}
	if d.Reason == "" {
		t.Error("reason empty")
	}
}

func writeClaudeJson(t *testing.T, home, fixture string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("fixtures", fixture))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), data, 0o600); err != nil {
		t.Fatalf("write claude.json: %v", err)
	}
}

func replaceAll(s, old, new string) string {
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return out + s
		}
		out += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
