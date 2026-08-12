package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"statisfy/internal/core"
	"statisfy/internal/detect"
	"statisfy/internal/exec"
)

// CodexAdapter reads plan and rate-limit info by talking to the Codex CLI's
// built-in `app-server` over stdio JSON-RPC. The CLI authenticates itself by
// reading ~/.codex/auth.json; statisfy never reads credentials directly.
type CodexAdapter struct{}

func (CodexAdapter) ID() string   { return "codex" }
func (CodexAdapter) Name() string { return "Codex" }

func (a CodexAdapter) Detect(ctx context.Context) core.Detection {
	d := core.Detection{}
	if !detect.InPath("codex") {
		d.ReasonKind = core.ReasonNotInstalled
		d.Reason = "codex binary not found on PATH"
		return d
	}
	d.Installed = true
	auth := filepath.Join(detect.HomeDir(), ".codex", "auth.json")
	if !detect.FileExists(auth) {
		d.ReasonKind = core.ReasonNotAuthenticated
		d.Reason = "missing ~/.codex/auth.json (run `codex login`)"
		return d
	}
	d.Authenticated = true
	return d
}

// rateLimitsResponse mirrors the verified `account/rateLimits/read` payload.
type rateLimitsResponse struct {
	RateLimits struct {
		LimitID   string     `json:"limitId"`
		PlanType  string     `json:"planType"`
		Primary   *rlWindow  `json:"primary"`
		Secondary *rlWindow  `json:"secondary"`
		Credits   *rlCredits `json:"credits"`
	} `json:"rateLimits"`
	RateLimitResetCredits struct {
		AvailableCount int `json:"availableCount"`
	} `json:"rateLimitResetCredits"`
}

type rlWindow struct {
	UsedPercent        int   `json:"usedPercent"`
	WindowDurationMins int   `json:"windowDurationMins"`
	ResetsAt           int64 `json:"resetsAt"` // unix seconds
}

type rlCredits struct {
	HasCredits bool   `json:"hasCredits"`
	Unlimited  bool   `json:"unlimited"`
	Balance    string `json:"balance"`
}

func (a CodexAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	client, err := exec.StartRPC(ctx, "codex", "app-server")
	if err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}
	defer client.Close()

	// initialize handshake (verified working with these params).
	initParams := map[string]any{
		"clientInfo": map[string]any{
			"name":    "statisfy",
			"title":   "Statisfy",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	}
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := client.Call(initCtx, 0, "initialize", initParams); err != nil {
		return nil, fmt.Errorf("codex initialize: %w", err)
	}
	if err := client.Notify(initCtx, "initialized", map[string]any{}); err != nil {
		return nil, fmt.Errorf("codex initialized: %w", err)
	}

	reqCtx, cancel2 := context.WithTimeout(ctx, 20*time.Second)
	defer cancel2()
	resp, err := client.Call(reqCtx, 1, "account/rateLimits/read", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("codex account/rateLimits/read: %w", err)
	}

	st, err := parseRateLimits(resp.Result)
	if err != nil {
		return nil, err
	}
	return st, nil
}

// parseRateLimits converts a raw `account/rateLimits/read` result into a
// normalized Status. Kept pure for fixture-based testing.
func parseRateLimits(raw json.RawMessage) (*core.Status, error) {
	var pl rateLimitsResponse
	if err := json.Unmarshal(raw, &pl); err != nil {
		return nil, fmt.Errorf("parse codex rate limits: %w", err)
	}

	st := &core.Status{
		Tool:      "codex",
		Name:      "Codex",
		Source:    core.SourceOfficialAPI,
		Stability: core.StabilityInternal,
		Plan:      normalizePlan(pl.RateLimits.PlanType),
	}
	addWindow(st, pl.RateLimits.Primary)
	addWindow(st, pl.RateLimits.Secondary)
	if pl.RateLimits.Credits != nil && pl.RateLimits.Credits.HasCredits {
		bal, _ := strconv.ParseFloat(pl.RateLimits.Credits.Balance, 64)
		st.Metrics = append(st.Metrics, core.Metric{
			Kind: core.MetricCredits, Label: "Credits", Value: bal,
			Unit: "credits",
		})
	}
	return st, nil
}

// addWindow converts a rate-limit window into a Limit.
func addWindow(st *core.Status, w *rlWindow) {
	if w == nil {
		return
	}
	kind, label := classifyWindow(w.WindowDurationMins)
	l := core.Limit{
		Kind:        kind,
		Label:       label,
		Used:        -1, // we know percentage only; usage amount not exposed
		Total:       0,
		PercentUsed: float64(w.UsedPercent),
		Unit:        "percent",
		Source:      core.SourceOfficialAPI,
	}
	if w.ResetsAt > 0 {
		t := time.Unix(w.ResetsAt, 0)
		l.ResetAt = &t
	}
	st.Limits = append(st.Limits, l)
}

func classifyWindow(mins int) (core.LimitKind, string) {
	switch mins {
	case 300:
		return core.LimitRollingWindow, "5h"
	case 1440:
		return core.LimitDaily, "Daily"
	case 10080:
		return core.LimitWeekly, "Weekly"
	default:
		if mins > 0 {
			return core.LimitRollingWindow, fmt.Sprintf("%dh", mins/60)
		}
		return core.LimitUnknown, ""
	}
}

// normalizePlan title-cases plan values the API returns (e.g. "plus" → "Plus").
func normalizePlan(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
