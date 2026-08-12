package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"statisfy/internal/core"
)

// fixedClaudeNow returns the pinned "now" used by transcript fixtures. All
// fixture timestamps are expressed relative to 2026-08-13, and the pinned
// clock uses the UTC location so the local-day boundary is exact on any
// machine.
func fixedClaudeNow() time.Time {
	return time.Date(2026, 8, 13, 15, 0, 0, 0, time.UTC)
}

func withFixedClaudeNow(t *testing.T) {
	t.Helper()
	old := claudeNow
	claudeNow = fixedClaudeNow
	t.Cleanup(func() { claudeNow = old })
}

func TestParseClaudeTranscriptsToday(t *testing.T) {
	withFixedClaudeNow(t)
	u, err := parseClaudeTranscripts([]string{filepath.Join("fixtures", "claude_transcript_normal.jsonl")}, fixedClaudeNow())
	if err != nil {
		t.Fatalf("parseClaudeTranscripts: %v", err)
	}
	if u.Records != 3 {
		t.Errorf("records = %d, want 3", u.Records)
	}
	// msg-1 final 150+80+10, msg-2 final 210+110+330, msg-3 50+10.
	if u.Tokens != 950 {
		t.Errorf("tokens = %d, want 950", u.Tokens)
	}
	if len(u.Sessions) != 1 || !u.Sessions["sess-abc"] {
		t.Errorf("sessions = %v, want {sess-abc}", u.Sessions)
	}
	if got := mostUsedModel(u.Models); got != "claude-sonnet-4-5" {
		t.Errorf("mostUsedModel = %q, want claude-sonnet-4-5", got)
	}
}

func TestParseClaudeTranscriptsMultiSession(t *testing.T) {
	u, err := parseClaudeTranscripts([]string{filepath.Join("fixtures", "claude_transcript_multisession.jsonl")}, fixedClaudeNow())
	if err != nil {
		t.Fatalf("parseClaudeTranscripts: %v", err)
	}
	// a1 120 + b1 final 590 + a2 15; the old sess-3 record is excluded.
	if u.Records != 3 {
		t.Errorf("records = %d, want 3", u.Records)
	}
	if u.Tokens != 725 {
		t.Errorf("tokens = %d, want 725", u.Tokens)
	}
	if len(u.Sessions) != 2 || !u.Sessions["sess-1"] || !u.Sessions["sess-2"] || u.Sessions["sess-3"] {
		t.Errorf("sessions = %v, want {sess-1, sess-2}", u.Sessions)
	}
	if got := mostUsedModel(u.Models); got != "claude-sonnet-4-5" {
		t.Errorf("mostUsedModel = %q, want claude-sonnet-4-5", got)
	}
}

func TestParseClaudeTranscriptsEmpty(t *testing.T) {
	u, err := parseClaudeTranscripts([]string{filepath.Join("fixtures", "claude_transcript_empty.jsonl")}, fixedClaudeNow())
	if err != nil {
		t.Fatalf("parseClaudeTranscripts: %v", err)
	}
	if u.Records != 0 || u.Tokens != 0 || len(u.Sessions) != 0 || len(u.Models) != 0 {
		t.Errorf("empty history produced data: %+v", u)
	}
}

func TestParseClaudeTranscriptsMissingFile(t *testing.T) {
	if _, err := parseClaudeTranscripts([]string{filepath.Join("fixtures", "does_not_exist.jsonl")}, fixedClaudeNow()); err == nil {
		t.Fatal("expected error for missing transcript file")
	}
}

// TestParseClaudeTranscriptsSkipsBadFile proves the P1 guarantee: one
// unreadable or oversized transcript must not destroy usage from the rest.
func TestParseClaudeTranscriptsSkipsBadFile(t *testing.T) {
	u, err := parseClaudeTranscripts([]string{
		filepath.Join("fixtures", "does_not_exist.jsonl"),
		filepath.Join("fixtures", "claude_transcript_normal.jsonl"),
	}, fixedClaudeNow())
	if err != nil {
		t.Fatalf("parseClaudeTranscripts: %v (bad file should be skipped)", err)
	}
	if u.Tokens != 950 {
		t.Errorf("tokens = %d, want 950 from the good file", u.Tokens)
	}
}

// TestClaudeDetectTranscriptsOnly proves the transcript source alone (no
// oauthAccount, no binary requirement) makes Claude configured/available.
func TestClaudeDetectTranscriptsOnly(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, ".claude", "projects", "p")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join("fixtures", "claude_transcript_empty.jsonl"))
	if err := os.WriteFile(filepath.Join(proj, "a.jsonl"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	a := ClaudeAdapter{OverrideHomeDir: home}
	d := a.Detect(context.Background())
	if !d.Installed {
		t.Error("installed = false, want true (transcripts present)")
	}
	if !d.Configured {
		t.Error("configured = false, want true")
	}
	if d.Authenticated {
		t.Error("authenticated = true, want false (no oauthAccount)")
	}
	if !d.Available() {
		t.Error("available = false, want true (transcripts are enough)")
	}
}

// TestClaudeFetchTranscriptUsage wires the real Fetch through a temp HOME:
// plan from oauthAccount + usage from the transcript fixture, no live state.
func TestClaudeFetchTranscriptUsage(t *testing.T) {
	withFixedClaudeNow(t)
	home := t.TempDir()
	writeClaudeJson(t, home, "claude_oauth_account.json")

	proj := filepath.Join(home, ".claude", "projects", "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join("fixtures", "claude_transcript_normal.jsonl"))
	if err := os.WriteFile(filepath.Join(proj, "session.jsonl"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	a := ClaudeAdapter{OverrideHomeDir: home}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if st.Plan != "Pro" {
		t.Errorf("plan = %q, want Pro", st.Plan)
	}
	if st.Model != "claude-sonnet-4-5" {
		t.Errorf("model = %q, want claude-sonnet-4-5", st.Model)
	}
	if len(st.Metrics) != 2 {
		t.Fatalf("metrics = %d, want 2", len(st.Metrics))
	}
	if st.Metrics[0].Kind != core.MetricTokens || st.Metrics[0].Value != 950 {
		t.Errorf("today metric = %+v, want tokens=950", st.Metrics[0])
	}
	if st.Metrics[0].Source != core.SourceLocalState {
		t.Errorf("today metric source = %q, want local-state", st.Metrics[0].Source)
	}
	if st.Metrics[1].Kind != core.MetricSessions || st.Metrics[1].Value != 1 {
		t.Errorf("sessions metric = %+v, want 1", st.Metrics[1])
	}
}

// TestClaudeFetchUsageWithoutPlan proves plan and usage degrade independently:
// transcripts alone (no oauthAccount, e.g. an API-plan user) still yields usage.
func TestClaudeFetchUsageWithoutPlan(t *testing.T) {
	withFixedClaudeNow(t)
	home := t.TempDir()
	proj := filepath.Join(home, ".claude", "projects", "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join("fixtures", "claude_transcript_multisession.jsonl"))
	if err := os.WriteFile(filepath.Join(proj, "session.jsonl"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	a := ClaudeAdapter{OverrideHomeDir: home}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if st.Plan != "" {
		t.Errorf("plan = %q, want empty (no account state)", st.Plan)
	}
	if len(st.Metrics) != 2 || st.Metrics[0].Value != 725 {
		t.Errorf("metrics = %+v, want today=725 tokens", st.Metrics)
	}
}

// TestClaudeFetchNoDataFails proves a home with neither plan state nor
// transcripts yields an error (so the tool is hidden, never shown empty).
func TestClaudeFetchNoDataFails(t *testing.T) {
	a := ClaudeAdapter{OverrideHomeDir: t.TempDir()}
	if _, err := a.Fetch(context.Background()); err == nil {
		t.Fatal("expected error when neither plan state nor transcripts exist")
	}
}
