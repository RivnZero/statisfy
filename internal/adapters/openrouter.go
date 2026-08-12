package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"statisfy/internal/core"
)

// OpenRouterAdapter reports usage and limit state for the OpenRouter provider
// via its official, documented GET /api/v1/key endpoint. The API key is read
// from the OPENROUTER_API_KEY environment variable, sent only in the
// Authorization header, and never printed, cached, or serialized.
//
// OpenRouter is primarily a provider, not a coding agent: this adapter shows
// account-level usage/limits so it can be matched against tool-level usage
// without double-counting (tool usage and provider billing are distinct).
type OpenRouterAdapter struct {
	// OverrideBaseURL points requests elsewhere (tests).
	OverrideBaseURL string
	// OverrideKey supplies the key for tests; empty means OPENROUTER_API_KEY.
	OverrideKey string
}

func (a OpenRouterAdapter) ID() string   { return "openrouter" }
func (a OpenRouterAdapter) Name() string { return "OpenRouter" }

func (a OpenRouterAdapter) key() string {
	if a.OverrideKey != "" {
		return a.OverrideKey
	}
	return os.Getenv("OPENROUTER_API_KEY")
}

func (a OpenRouterAdapter) Detect(ctx context.Context) core.Detection {
	d := core.Detection{}
	if a.key() == "" {
		d.ReasonKind = core.ReasonNotAuthenticated
		d.Reason = "no OPENROUTER_API_KEY in the environment"
		return d
	}
	d.Installed = true // the provider is present and usable
	d.Authenticated = true
	return d
}

// openRouterKey mirrors the documented GET /api/v1/key response. The `label`
// field is deliberately omitted: it can contain a redacted form of the key
// itself and must never be surfaced.
type openRouterKey struct {
	Data *openRouterKeyData `json:"data"`
}

type openRouterKeyData struct {
	Usage          float64  `json:"usage"`
	UsageDaily     float64  `json:"usage_daily"`
	UsageWeekly    float64  `json:"usage_weekly"`
	UsageMonthly   float64  `json:"usage_monthly"`
	Limit          *float64 `json:"limit"`
	LimitRemaining *float64 `json:"limit_remaining"`
	LimitReset     string   `json:"limit_reset"`
	IsFreeTier     bool     `json:"is_free_tier"`
}

func (a OpenRouterAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	key := a.key()
	if key == "" {
		return nil, errors.New("no OPENROUTER_API_KEY in the environment")
	}
	base := a.OverrideBaseURL
	if base == "" {
		base = "https://openrouter.ai"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/key", nil)
	if err != nil {
		return nil, err
	}
	setUA(req)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")

	// Exactly one request: a documented read-only GET, never retried (keeps
	// statisfy's request surface minimal and consistent with the safety rule).
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter key request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter key: HTTP %d", resp.StatusCode)
	}
	var r openRouterKey
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("parse openrouter key: %w", err)
	}
	if r.Data == nil {
		return nil, errors.New("openrouter key: empty response")
	}
	return r.Data.status()
}

// status converts the key payload into a normalized Status. Pure for tests.
func (d *openRouterKeyData) status() (*core.Status, error) {
	st := &core.Status{
		Tool:      "openrouter",
		Name:      "OpenRouter",
		Source:    core.SourceOfficialAPI,
		Stability: core.StabilityDocumented,
	}
	if d.IsFreeTier {
		st.Plan = "Free" // server-reported; never inferred
	}
	addCost := func(label string, v float64) {
		if v > 0 {
			st.Metrics = append(st.Metrics, core.Metric{
				Kind: core.MetricCost, Label: label, Value: v, Unit: "cost", Source: core.SourceOfficialAPI,
			})
		}
	}
	addCost("Usage", d.Usage)
	addCost("Monthly", d.UsageMonthly)
	addCost("Daily", d.UsageDaily)
	if d.Limit != nil {
		addCost("Limit", *d.Limit)
	}
	if d.LimitRemaining != nil {
		addCost("Remaining", *d.LimitRemaining)
	}
	return st, nil
}
