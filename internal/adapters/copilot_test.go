package adapters

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// newCopilotFixture creates a temp data dir with a data.db containing one
// account and a couple of sessions (one today, one older).
func newCopilotFixture(t *testing.T) CopilotAdapter {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	mustExec(t, db, `CREATE TABLE accounts (
		id TEXT, login TEXT, name TEXT, avatar_url TEXT, token_type TEXT,
		scope TEXT, is_default INTEGER, host TEXT, client_id TEXT,
		token_updated_at INTEGER, created_at INTEGER, updated_at INTEGER,
		access_token TEXT, kind TEXT, token_expires_at INTEGER)`)
	mustExec(t, db, `CREATE TABLE sessions (
		id TEXT, title TEXT, session_type TEXT, mode TEXT, model TEXT,
		reasoning_effort TEXT, is_running INTEGER, was_interrupted INTEGER,
		interruption_reason TEXT, created_at INTEGER, updated_at INTEGER,
		remote_control_enabled INTEGER, auto_approve INTEGER,
		enabled_experiments_json TEXT, forked_from_session_id TEXT,
		fork_original_history_event_count INTEGER, total_input_tokens INTEGER,
		total_output_tokens INTEGER, total_cached_tokens INTEGER,
		total_reasoning_tokens INTEGER, total_nano_aiu INTEGER,
		context_current_tokens INTEGER, context_input_token_limit INTEGER,
		context_output_token_limit INTEGER, remote_session_mode TEXT, agent TEXT,
		provider_id TEXT, remote_connect_target TEXT, execution_location TEXT,
		context_tier TEXT, context_system_tokens INTEGER,
		context_conversation_tokens INTEGER, context_tool_definitions_tokens INTEGER,
		context_mcp_tools_tokens INTEGER, context_buffer_tokens INTEGER,
		title_source TEXT, total_agent_merge_nano_aiu INTEGER, archived_at INTEGER,
		permission_mode TEXT)`)

	nowMs := localDayStartMs()
	yesterday := nowMs - 24*3600*1000
	// account row includes an access_token — the adapter must never surface it.
	mustExec(t, db, `INSERT INTO accounts (id, login, name, token_type, scope, is_default, host, access_token) VALUES
		('acc-1', 'octocat', 'Octo Cat', 'bearer', 'repo,user', 1, 'github.com', 'gho_SECRET_NEVER_SHOW')`)
	mustExec(t, db, `INSERT INTO sessions (id, model, created_at, updated_at, total_input_tokens, total_output_tokens, total_cached_tokens, total_reasoning_tokens, total_nano_aiu, provider_id) VALUES
		('s1', 'gpt-4.1', ?, ?, 1000, 200, 50, 10, 2500000000, 'openai'),
		('s2', 'gpt-4.1', ?, ?, 2000, 100, 0, 0, 0, 'openai'),
		('s3', 'claude-sonnet', ?, ?, 500, 50, 25, 5, 1000000000, 'anthropic')`,
		nowMs, nowMs+1000, nowMs+2000, nowMs+3000, yesterday, yesterday+1000)
	return CopilotAdapter{OverrideDataDir: dir}
}

func TestCopilotFetchToday(t *testing.T) {
	a := newCopilotFixture(t)
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if st.Account != "octocat" {
		t.Errorf("account = %q, want octocat", st.Account)
	}
	if len(st.Metrics) != 3 {
		t.Fatalf("metrics = %d, want 3 (sessions, tokens, AI credits)", len(st.Metrics))
	}
	byKind := map[string]float64{}
	for _, m := range st.Metrics {
		byKind[string(m.Kind)] = m.Value
	}
	if byKind["sessions"] != 2 {
		t.Errorf("sessions = %v, want 2 (yesterday excluded)", byKind["sessions"])
	}
	if byKind["tokens"] != 3350 { // 1000+200+50 + 2000+100+0
		t.Errorf("tokens = %v, want 3350", byKind["tokens"])
	}
	if byKind["credits"] != 2.5 { // 2.5e9 nano
		t.Errorf("credits = %v, want 2.5", byKind["credits"])
	}
	// No plan invented.
	if st.Plan != "" {
		t.Errorf("plan = %q, want empty", st.Plan)
	}
	// Model surfaced from most recent session (s2, today).
	if st.Model != "gpt-4.1" {
		t.Errorf("model = %q, want gpt-4.1", st.Model)
	}
}

func TestCopilotFetchNoSecrets(t *testing.T) {
	a := newCopilotFixture(t)
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// The access_token value must never appear in the status.
	for _, s := range []string{st.Account, st.Plan, st.Model} {
		if contains(s, "gho_SECRET") {
			t.Fatalf("secret leaked into status: %q", s)
		}
	}
	if st.Errors != nil && len(st.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", st.Errors)
	}
}

func TestCopilotDetect(t *testing.T) {
	withFakeBinaryOnPath(t, "copilot") // CI runners have no copilot binary
	a := newCopilotFixture(t)
	d := a.Detect(context.Background())
	if !d.Installed || !d.Authenticated {
		t.Errorf("detect = %+v, want installed+authenticated", d)
	}
}

func TestCopilotDetectNoAccount(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `CREATE TABLE accounts (id TEXT, login TEXT, is_default INTEGER, host TEXT)`)
	db.Close()
	a := CopilotAdapter{OverrideDataDir: dir}
	d := a.Detect(context.Background())
	if d.Authenticated {
		t.Error("authenticated = true without account rows")
	}
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %s: %v", q, err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
