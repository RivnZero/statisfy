package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"statisfy/internal/core"
)

func TestFormatTokens(t *testing.T) {
	cases := map[float64]string{
		0:          "0",
		842:        "842",
		999:        "999",
		1000:       "1.0K",
		3800000:    "3.8M",
		100000000:  "100.0M",
		796300000:  "796.3M",
		4500000:    "4.5M",
		26500:      "26.5K",
		1000000000: "1.0B",
	}
	for in, want := range cases {
		if got := FormatTokens(in); got != want {
			t.Errorf("FormatTokens(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatCost(t *testing.T) {
	cases := map[float64]string{
		0:     "$0.00",
		2.71:  "$2.71",
		0.06:  "$0.06",
		38.20: "$38.20",
	}
	for in, want := range cases {
		if got := FormatCost(in); got != want {
			t.Errorf("FormatCost(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		reset time.Time
		want  string
	}{
		{now.Add(3*time.Hour + 42*time.Minute), "3h 42m"},
		{now.Add(5*24*time.Hour + 14*time.Hour), "5d 14h"},
		{now.Add(12 * time.Minute), "12m"},
		{now.Add(-time.Hour), "0m"},
	}
	for _, c := range cases {
		if got := FormatDurationUntil(c.reset, now); got != c.want {
			t.Errorf("FormatDurationUntil(%v) = %q, want %q", c.reset, got, c.want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	if got := ProgressBar(100, 10); got != "██████████" {
		t.Errorf("100%%: %q", got)
	}
	if got := ProgressBar(0, 10); got != "░░░░░░░░░░" {
		t.Errorf("0%%: %q", got)
	}
	if got := ProgressBar(50, 10); got != "█████░░░░░" {
		t.Errorf("50%%: %q", got)
	}
	if got := ProgressBar(-1, 10); got != "░░░░░░░░░░" {
		t.Errorf("unknown: %q", got)
	}
}

func TestFormatPercentLeft(t *testing.T) {
	if got := FormatPercentLeft(82); got != "82% left" {
		t.Errorf("got %q", got)
	}
	if got := FormatPercentLeft(-1); got != "" {
		t.Errorf("unknown got %q, want empty", got)
	}
}

func TestDashboardRendersOnlyStatus(t *testing.T) {
	res := []core.ToolResult{
		{Adapter: fakeAdapter{id: "ok"}, Detection: core.Detection{Installed: true, Authenticated: true}, Status: &core.Status{
			Tool: "ok", Name: "OkTool", Plan: "Pro",
			Limits: []core.Limit{{Kind: core.LimitWeekly, Label: "Weekly", PercentUsed: 64, Unit: "percent"}},
		}},
		{Adapter: fakeAdapter{id: "hidden"}, Detection: core.Detection{Installed: true}}, // unavailable, no status
	}
	var buf bytes.Buffer
	Dashboard(&buf, res, Options{})
	out := buf.String()
	if !strings.Contains(out, "OkTool") {
		t.Errorf("missing available tool in output:\n%s", out)
	}
	if strings.Contains(out, "hidden") {
		t.Errorf("unavailable tool leaked into dashboard:\n%s", out)
	}
	if !strings.Contains(out, "36% left") {
		t.Errorf("missing percent in output:\n%s", out)
	}
}

func TestDashboardSessionsUnknownUsed(t *testing.T) {
	res := []core.ToolResult{{
		Adapter: fakeAdapter{id: "fb"}, Detection: core.Detection{Installed: true, Configured: true},
		Status: &core.Status{
			Tool: "fb", Name: "Freebuff",
			Limits: []core.Limit{
				{Kind: core.LimitSessions, Label: "DeepSeek", Used: 0.7, Total: 6, Unit: "sessions"},
				{Kind: core.LimitSessions, Label: "GLM", Used: -1, Total: 6, Unit: "sessions"},
			},
		},
	}}
	var buf bytes.Buffer
	Dashboard(&buf, res, Options{})
	out := buf.String()
	if !strings.Contains(out, "0 / 6 sessions") {
		t.Errorf("expected floored used sessions, got:\n%s", out)
	}
	if strings.Contains(out, "-1") {
		t.Errorf("unknown sentinel leaked to terminal:\n%s", out)
	}
	if !strings.Contains(out, "6 sessions") {
		t.Errorf("expected total-only for GLM, got:\n%s", out)
	}
}

func TestJSONNoANSINoSecrets(t *testing.T) {
	res := []core.ToolResult{{
		Adapter: fakeAdapter{id: "codex"}, Detection: core.Detection{Installed: true, Authenticated: true},
		Status: &core.Status{
			Tool: "codex", Name: "Codex", Plan: "Plus",
			Limits: []core.Limit{{Kind: core.LimitWeekly, Label: "Weekly", Used: -1, PercentUsed: 4, Unit: "percent"}},
		},
	}}
	doc := JSONDocFromResults(res, false)
	var buf bytes.Buffer
	if err := WriteJSON(&buf, doc); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "\x1b") {
		t.Error("ANSI escape in JSON output")
	}
	if strings.Contains(out, "\"used\": -1") {
		t.Errorf("unknown sentinel leaked to JSON:\n%s", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("json invalid: %v", err)
	}
	if parsed["version"] != float64(1) {
		t.Errorf("version = %v, want 1", parsed["version"])
	}
}

func TestJSONIncludesUnavailableOnlyWhenRequested(t *testing.T) {
	res := []core.ToolResult{
		{Adapter: fakeAdapter{id: "a"}, Detection: core.Detection{Installed: true, Authenticated: true}, Status: &core.Status{Tool: "a", Name: "A"}},
		{Adapter: fakeAdapter{id: "b"}, Detection: core.Detection{Installed: true, Reason: "no auth"}},
	}
	doc := JSONDocFromResults(res, false)
	if len(doc.Tools) != 1 || len(doc.Unavailable) != 0 {
		t.Errorf("without --all: tools=%d unavailable=%d", len(doc.Tools), len(doc.Unavailable))
	}
	doc2 := JSONDocFromResults(res, true)
	if len(doc2.Unavailable) != 1 || doc2.Unavailable[0].Reason != "no auth" {
		t.Errorf("with --all: unavailable=%+v", doc2.Unavailable)
	}
}

// TestJSONFetchFailureNotDropped guards a regression: a tool whose detection
// passed but whose fetch failed (Status == nil, FetchErr set) must appear in
// --all --json rather than silently disappearing.
func TestJSONFetchFailureNotDropped(t *testing.T) {
	res := []core.ToolResult{
		{Adapter: fakeAdapter{id: "codex"}, Detection: core.Detection{Installed: true, Authenticated: true},
			FetchErr: errors.New("rpc error -32603: upstream down")},
	}
	doc := JSONDocFromResults(res, true)
	if len(doc.Tools) != 0 {
		t.Errorf("failed fetch must not appear as available: tools=%d", len(doc.Tools))
	}
	if len(doc.Unavailable) != 1 {
		t.Fatalf("failed fetch dropped from unavailable: %+v", doc.Unavailable)
	}
	ut := doc.Unavailable[0]
	if ut.ID != "codex" || ut.Available {
		t.Errorf("unavailable entry = %+v", ut)
	}
	if !strings.Contains(ut.Reason, "upstream down") {
		t.Errorf("reason missing fetch error: %q", ut.Reason)
	}
	if len(ut.Errors) != 1 {
		t.Errorf("errors missing fetch error: %+v", ut.Errors)
	}
	// Without --all the failed fetch stays hidden (consistent with the
	// dashboard contract: unavailable tools do not appear by default).
	doc2 := JSONDocFromResults(res, false)
	if len(doc2.Tools) != 0 || len(doc2.Unavailable) != 0 {
		t.Errorf("without --all: tools=%d unavailable=%d", len(doc2.Tools), len(doc2.Unavailable))
	}
}

func TestDoctorShowsAll(t *testing.T) {
	res := []core.ToolResult{
		{Adapter: fakeAdapter{id: "a"}, Detection: core.Detection{Installed: true, Authenticated: true}},
		{Adapter: fakeAdapter{id: "b"}, Detection: core.Detection{Reason: "not installed"}},
	}
	var buf bytes.Buffer
	Doctor(&buf, res, Options{})
	out := buf.String()
	if !strings.Contains(out, "not installed") {
		t.Errorf("doctor missing reason:\n%s", out)
	}
}

func TestDoctorCategorizesReasons(t *testing.T) {
	res := []core.ToolResult{
		{Adapter: fakeAdapter{id: "a"}, Detection: core.Detection{
			Installed:  true,
			ReasonKind: core.ReasonNotAuthenticated,
			Reason:     "no oauthAccount in ~/.claude.json (run `claude` and sign in)",
		}},
		{Adapter: fakeAdapter{id: "b"}, Detection: core.Detection{
			ReasonKind: core.ReasonNotInstalled,
			Reason:     "qwen binary not found on PATH",
		}},
	}
	var buf bytes.Buffer
	Doctor(&buf, res, Options{})
	out := buf.String()
	if !strings.Contains(out, "authentication missing: no oauthAccount") {
		t.Errorf("doctor missing auth category:\n%s", out)
	}
	if !strings.Contains(out, "binary missing: qwen binary not found") {
		t.Errorf("doctor missing install category:\n%s", out)
	}
}

func TestReasonLabel(t *testing.T) {
	cases := map[core.UnavailableReason]string{
		core.ReasonNotInstalled:     "binary missing",
		core.ReasonNotConfigured:    "configuration missing",
		core.ReasonNotAuthenticated: "authentication missing",
		core.ReasonNetworkTimeout:   "network timeout",
		core.ReasonPermission:       "permission failure",
		core.ReasonLocalState:       "local state unavailable",
	}
	for k, want := range cases {
		if got := reasonLabel(k); got != want {
			t.Errorf("reasonLabel(%q) = %q, want %q", k, got, want)
		}
	}
	if got := reasonLabel(""); got != "" {
		t.Errorf("reasonLabel(unknown) = %q, want empty", got)
	}
}

type fakeAdapter struct{ id string }

func (f fakeAdapter) ID() string   { return f.id }
func (f fakeAdapter) Name() string { return f.id }
func (f fakeAdapter) Detect(ctx context.Context) core.Detection {
	return core.Detection{Installed: true, Authenticated: true}
}
func (f fakeAdapter) Fetch(ctx context.Context) (*core.Status, error) { return nil, nil }
