package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"statisfy/internal/core"
	"statisfy/internal/detect"
)

// AiderAdapter aggregates real usage from Aider's analytics log. Aider does not
// persist usage anywhere by default: chat history is plain markdown with no
// cost/model data, and the per-session "Tokens/Cost" summary is printed to the
// terminal only. The one structured, always-available source is the optional
// JSONL analytics log:
//
//	aider --analytics-log ~/.aider/analytics.jsonl
//
// Aider is a client/tool: this adapter reports Aider-local activity (requests,
// tokens, locally recorded cost, the model actually used) and never invents
// subscription or quota concepts.
//
// Source stability: the analytics event schema is an internal PostHog-style
// format — hence StabilityExperimental. Unparseable lines are skipped so one
// bad event never breaks the whole log. The anonymous user_id is never
// surfaced, and no credentials exist in these files.
type AiderAdapter struct {
	// OverrideHomeDir replaces the home directory (tests).
	OverrideHomeDir string
}

func (a AiderAdapter) ID() string   { return "aider" }
func (a AiderAdapter) Name() string { return "Aider" }

func (a AiderAdapter) home() string {
	if a.OverrideHomeDir != "" {
		return a.OverrideHomeDir
	}
	return detect.HomeDir()
}

// analyticsLog returns the documented analytics-log location statisfy reads.
func (a AiderAdapter) analyticsLog() string {
	return filepath.Join(a.home(), ".aider", "analytics.jsonl")
}

func (a AiderAdapter) Detect(ctx context.Context) core.Detection {
	d := core.Detection{}
	if !detect.InPath("aider") {
		d.ReasonKind = core.ReasonNotInstalled
		d.Reason = "aider binary not found on PATH"
		return d
	}
	d.Installed = true
	if !detect.FileExists(a.analyticsLog()) {
		d.ReasonKind = core.ReasonLocalState
		d.Reason = "no analytics log at ~/.aider/analytics.jsonl (enable with: aider --analytics-log ~/.aider/analytics.jsonl)"
		return d
	}
	d.Configured = true
	return d
}

// aiderAnalyticsEvent mirrors one JSONL analytics event. Only the fields this
// adapter needs are decoded; unknown fields and future payload shapes are
// ignored. The anonymous user_id is intentionally not in the struct, so it can
// never be rendered or serialized.
type aiderAnalyticsEvent struct {
	Event      string `json:"event"`
	Properties struct {
		MainModel        string  `json:"main_model"`
		PromptTokens     int64   `json:"prompt_tokens"`
		CompletionTokens int64   `json:"completion_tokens"`
		Cost             float64 `json:"cost"`
	} `json:"properties"`
	Time int64 `json:"time"` // unix seconds
}

// parseAiderLog parses the JSONL analytics log, keeping only message_send
// events. Invalid lines are skipped unless nothing at all parses (which is a
// structural failure, not an empty log).
func parseAiderLog(data []byte) ([]aiderAnalyticsEvent, error) {
	var events []aiderAnalyticsEvent
	parsed, failed := 0, 0
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e aiderAnalyticsEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			failed++
			continue
		}
		parsed++
		if e.Event == "message_send" {
			events = append(events, e)
		}
	}
	if failed > 0 && parsed == 0 {
		return nil, fmt.Errorf("%d unparseable line(s), no valid events", failed)
	}
	if err := sc.Err(); err != nil && parsed == 0 {
		return nil, err
	}
	return events, nil
}

func (a AiderAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	data, err := os.ReadFile(a.analyticsLog())
	if err != nil {
		return nil, fmt.Errorf("read aider analytics log: %w", err)
	}
	events, err := parseAiderLog(data)
	if err != nil {
		return nil, fmt.Errorf("parse aider analytics log: %w", err)
	}

	start := localDayStartMs() / 1000 // time is unix seconds
	var requests, tokens int64
	var cost float64
	var model string
	for _, e := range events {
		if e.Time < start {
			continue
		}
		requests++
		tokens += e.Properties.PromptTokens + e.Properties.CompletionTokens
		cost += e.Properties.Cost
		if e.Properties.MainModel != "" {
			model = e.Properties.MainModel
		}
	}

	st := &core.Status{
		Tool:      "aider",
		Name:      "Aider",
		Source:    core.SourceLocalState,
		Stability: core.StabilityExperimental,
	}
	if model != "" {
		// Model strings may carry a provider prefix ("provider/model"); the
		// provider is derived from the actual string, never assumed.
		st.Model = model
		if i := strings.IndexByte(model, '/'); i > 0 {
			st.Provider = model[:i]
		}
	}
	st.Metrics = []core.Metric{
		{Kind: core.MetricSessions, Label: "Requests", Value: float64(requests), Source: core.SourceLocalState},
		{Kind: core.MetricTokens, Label: "Today", Value: float64(tokens), Unit: "tokens", Source: core.SourceLocalState},
		{Kind: core.MetricCost, Label: "Cost", Value: cost, Unit: "cost", Source: core.SourceLocalState},
	}
	return st, nil
}
