package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"statisfy/internal/core"
)

// withFakeAiderOnPath puts a fake `aider` executable on PATH (both bare and
// .exe so the test is cross-platform) and returns a cleanup.
func withFakeAiderOnPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"aider", "aider.exe"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

func writeAiderLog(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".aider")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "analytics.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAiderParseFixture(t *testing.T) {
	events, err := parseAiderLog(readFixture(t, "aider_analytics.jsonl"))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (launched/cli-session ignored, corrupt line skipped)", len(events))
	}
	if events[0].Properties.MainModel != "gemini/gemini-2.5-pro" || events[0].Properties.PromptTokens != 10006 {
		t.Errorf("event 0 = %+v", events[0])
	}
	if events[1].Properties.Cost != 0.05 {
		t.Errorf("event 1 cost = %v, want 0.05", events[1].Properties.Cost)
	}
}

func TestAiderParseEmptyLog(t *testing.T) {
	events, err := parseAiderLog(nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("events = %d, want 0", len(events))
	}
}

func TestAiderParseAllCorrupt(t *testing.T) {
	if _, err := parseAiderLog([]byte("not json\nalso not json\n")); err == nil {
		t.Fatal("fully corrupt log parsed without error")
	}
}

func TestAiderParsePartialCorruptionTolerated(t *testing.T) {
	data := []byte("{\"event\":\"message_send\",\"properties\":{},\"time\":1}\ncorrupt line\n")
	events, err := parseAiderLog(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("events = %d, want 1 (corrupt line tolerated)", len(events))
	}
}

func TestAiderFetchAggregatesToday(t *testing.T) {
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	lines := []string{
		fmt.Sprintf(`{"event":"message_send","properties":{"main_model":"gemini/gemini-2.5-pro","prompt_tokens":1000,"completion_tokens":100,"cost":0.01},"time":%d}`, dayStart+10),
		fmt.Sprintf(`{"event":"message_send","properties":{"main_model":"anthropic/claude-sonnet-4","prompt_tokens":2000,"completion_tokens":200,"cost":0.05},"time":%d}`, dayStart+20),
		fmt.Sprintf(`{"event":"message_send","properties":{"main_model":"old/old-model","prompt_tokens":9999,"completion_tokens":9999,"cost":9.99},"time":%d}`, dayStart-1000),
	}
	home := t.TempDir()
	writeAiderLog(t, home, joinLines(lines))
	a := AiderAdapter{OverrideHomeDir: home}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if st.Metrics[0].Value != 2 {
		t.Errorf("requests = %v, want 2 (yesterday excluded)", st.Metrics[0].Value)
	}
	if st.Metrics[1].Value != 3300 {
		t.Errorf("tokens = %v, want 3300", st.Metrics[1].Value)
	}
	if d := st.Metrics[2].Value - 0.06; d > 1e-9 || d < -1e-9 {
		t.Errorf("cost = %v, want ~0.06", st.Metrics[2].Value)
	}
	// Model comes from the latest today event; provider derived from the
	// actual string prefix.
	if st.Model != "anthropic/claude-sonnet-4" {
		t.Errorf("model = %q", st.Model)
	}
	if st.Provider != "anthropic" {
		t.Errorf("provider = %q, want anthropic", st.Provider)
	}
	// Aider owns no quota concepts: nothing may be invented.
	if st.Plan != "" || len(st.Limits) > 0 || st.Multiplier != 0 {
		t.Errorf("invented quota: plan=%q limits=%d multiplier=%d", st.Plan, len(st.Limits), st.Multiplier)
	}
}

func TestAiderFetchNoSecretLeak(t *testing.T) {
	home := t.TempDir()
	writeAiderLog(t, home, string(readFixture(t, "aider_analytics.jsonl")))
	a := AiderAdapter{OverrideHomeDir: home}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	out, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	// The anonymous user_id and any key-shaped strings must never surface.
	for _, needle := range []string{"11111111-2222", "user_id", "sk-", "apiKey"} {
		if containsStr(string(out), needle) {
			t.Fatalf("%q leaked into status JSON", needle)
		}
	}
}

func TestAiderDetectNotInstalled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir) // PATH with no aider
	a := AiderAdapter{OverrideHomeDir: t.TempDir()}
	d := a.Detect(context.Background())
	if d.Installed || d.Available() {
		t.Errorf("detect = %+v, want not installed", d)
	}
	if d.ReasonKind != core.ReasonNotInstalled {
		t.Errorf("reason_kind = %q", d.ReasonKind)
	}
}

func TestAiderDetectUnavailableWithoutLog(t *testing.T) {
	withFakeAiderOnPath(t)
	a := AiderAdapter{OverrideHomeDir: t.TempDir()}
	d := a.Detect(context.Background())
	if !d.Installed {
		t.Errorf("installed = false with aider on PATH")
	}
	if d.Configured || d.Available() {
		t.Errorf("configured/available = true without analytics log")
	}
	if d.ReasonKind != core.ReasonLocalState {
		t.Errorf("reason_kind = %q, want local_state_unavailable", d.ReasonKind)
	}
}

func TestAiderDetectConfigured(t *testing.T) {
	withFakeAiderOnPath(t)
	home := t.TempDir()
	writeAiderLog(t, home, string(readFixture(t, "aider_analytics.jsonl")))
	a := AiderAdapter{OverrideHomeDir: home}
	d := a.Detect(context.Background())
	if !d.Installed || !d.Configured || !d.Available() {
		t.Errorf("detect = %+v, want installed+configured+available", d)
	}
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}
