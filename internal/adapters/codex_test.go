package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"statisfy/internal/core"
)

func TestParseRateLimits(t *testing.T) {
	raw := loadFixture(t, "codex_rate_limits.json")
	st, err := parseRateLimits(raw)
	if err != nil {
		t.Fatalf("parseRateLimits: %v", err)
	}
	if st.Plan != "Plus" {
		t.Errorf("plan = %q, want Plus", st.Plan)
	}
	if len(st.Limits) != 1 {
		t.Fatalf("limits = %d, want 1", len(st.Limits))
	}
	l := st.Limits[0]
	if l.Kind != core.LimitWeekly || l.Label != "Weekly" {
		t.Errorf("limit = %s/%s, want weekly/Weekly", l.Kind, l.Label)
	}
	if l.PercentUsed != 4 {
		t.Errorf("percentUsed = %v, want 4", l.PercentUsed)
	}
	if l.PercentLeft() != 96 {
		t.Errorf("percentLeft = %v, want 96", l.PercentLeft())
	}
	if l.ResetAt == nil {
		t.Error("resetAt missing")
	}
	// Credits metric should be absent (hasCredits=false).
	if len(st.Metrics) != 0 {
		t.Errorf("metrics = %d, want 0", len(st.Metrics))
	}
}

func TestParseRateLimitsFiveHourSecondary(t *testing.T) {
	raw := json.RawMessage(`{"rateLimits":{
		"planType":"pro",
		"primary":{"usedPercent":82,"windowDurationMins":300,"resetsAt":1787000000},
		"secondary":{"usedPercent":51,"windowDurationMins":10080,"resetsAt":1787040374},
		"credits":{"hasCredits":true,"unlimited":false,"balance":"14.72"}
	}}`)
	st, err := parseRateLimits(raw)
	if err != nil {
		t.Fatalf("parseRateLimits: %v", err)
	}
	if st.Plan != "Pro" {
		t.Errorf("plan = %q, want Pro", st.Plan)
	}
	if len(st.Limits) != 2 {
		t.Fatalf("limits = %d, want 2", len(st.Limits))
	}
	if st.Limits[0].Label != "5h" {
		t.Errorf("first limit label = %q, want 5h", st.Limits[0].Label)
	}
	if st.Limits[0].PercentUsed != 82 {
		t.Errorf("first limit percent = %v, want 82", st.Limits[0].PercentUsed)
	}
	if len(st.Metrics) != 1 || st.Metrics[0].Value != 14.72 {
		t.Errorf("credits metric = %+v, want 14.72", st.Metrics)
	}
}

// TestParseRateLimitsPro5x: an explicitly reported 5x multiplier with a Pro
// plan surfaces as Pro · 5x.
func TestParseRateLimitsPro5x(t *testing.T) {
	st, err := parseRateLimits(loadFixture(t, "codex_rate_limits_pro_5x.json"))
	if err != nil {
		t.Fatalf("parseRateLimits: %v", err)
	}
	if st.Plan != "Pro" {
		t.Errorf("plan = %q, want Pro", st.Plan)
	}
	if st.Multiplier != 5 {
		t.Errorf("multiplier = %d, want 5", st.Multiplier)
	}
}

// TestParseRateLimitsPro20x: an explicitly reported 20x multiplier with a Pro
// plan surfaces as Pro · 20x.
func TestParseRateLimitsPro20x(t *testing.T) {
	st, err := parseRateLimits(loadFixture(t, "codex_rate_limits_pro_20x.json"))
	if err != nil {
		t.Fatalf("parseRateLimits: %v", err)
	}
	if st.Plan != "Pro" {
		t.Errorf("plan = %q, want Pro", st.Plan)
	}
	if st.Multiplier != 20 {
		t.Errorf("multiplier = %d, want 20", st.Multiplier)
	}
}

// TestParseRateLimitsPlusNeverMultiplied is the core regression: even if the
// upstream payload contains a multiplier on a Plus plan, it must never
// surface — Plus can never render as Plus · 5x or Plus · 20x.
func TestParseRateLimitsPlusNeverMultiplied(t *testing.T) {
	st, err := parseRateLimits(loadFixture(t, "codex_rate_limits_plus_multiplier.json"))
	if err != nil {
		t.Fatalf("parseRateLimits: %v", err)
	}
	if st.Plan != "Plus" {
		t.Errorf("plan = %q, want Plus", st.Plan)
	}
	if st.Multiplier != 0 {
		t.Errorf("multiplier = %d, want 0 (5x/20x are Pro variants, never Plus)", st.Multiplier)
	}
}

// TestParseRateLimitsProNoMultiplier: Pro without any explicit multiplier
// stays plain Pro — never guessed.
func TestParseRateLimitsProNoMultiplier(t *testing.T) {
	st, err := parseRateLimits(loadFixture(t, "codex_rate_limits_pro_5x.json"))
	if err != nil {
		t.Fatalf("parseRateLimits: %v", err)
	}
	// Same fixture minus the multiplier field → plain Pro.
	raw := []byte(replaceAll(string(loadFixture(t, "codex_rate_limits_pro_5x.json")), `"multiplier": 5,`, ""))
	st, err = parseRateLimits(raw)
	if err != nil {
		t.Fatalf("parseRateLimits: %v", err)
	}
	if st.Plan != "Pro" {
		t.Errorf("plan = %q, want Pro", st.Plan)
	}
	if st.Multiplier != 0 {
		t.Errorf("multiplier = %d, want 0 (never inferred from plan alone)", st.Multiplier)
	}
}

// TestParseRateLimitsUnknownPlanMultiplierIgnored: a multiplier on an unknown
// or non-Pro plan is dropped, not rendered.
func TestParseRateLimitsUnknownPlanMultiplierIgnored(t *testing.T) {
	raw := json.RawMessage(`{"rateLimits":{"planType":"business","multiplier":20}}`)
	st, err := parseRateLimits(raw)
	if err != nil {
		t.Fatalf("parseRateLimits: %v", err)
	}
	if st.Plan != "Business" {
		t.Errorf("plan = %q, want Business", st.Plan)
	}
	if st.Multiplier != 0 {
		t.Errorf("multiplier = %d, want 0", st.Multiplier)
	}
}

func TestParseRateLimitsEmpty(t *testing.T) {
	st, err := parseRateLimits(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("parseRateLimits: %v", err)
	}
	if st.Plan != "" {
		t.Errorf("plan = %q, want empty (never inferred)", st.Plan)
	}
	if len(st.Limits) != 0 {
		t.Errorf("limits = %d, want 0", len(st.Limits))
	}
}

func loadFixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return json.RawMessage(data)
}
