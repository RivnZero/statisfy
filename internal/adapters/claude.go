package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"statisfy/internal/core"
	"statisfy/internal/detect"
)

// ClaudeAdapter detects the Claude Code plan from local CLI state
// (~/.claude.json → oauthAccount). Usage windows are only fetched when an
// OAuth access token is available on disk (macOS/Linux); on Windows the token
// lives in the OS credential vault and usage is omitted — never fabricated.
type ClaudeAdapter struct {
	// OverrideBaseURL points usage requests elsewhere (tests).
	OverrideBaseURL string
	// OverrideHomeDir replaces the home directory (tests).
	OverrideHomeDir string
}

func (a ClaudeAdapter) ID() string   { return "claude" }
func (a ClaudeAdapter) Name() string { return "Claude" }

func (a ClaudeAdapter) home() string {
	if a.OverrideHomeDir != "" {
		return a.OverrideHomeDir
	}
	return detect.HomeDir()
}

func (a ClaudeAdapter) Detect(ctx context.Context) core.Detection {
	d := core.Detection{}
	if !detect.InPath("claude") {
		d.ReasonKind = core.ReasonNotInstalled
		d.Reason = "claude binary not found on PATH"
		return d
	}
	d.Installed = true
	if _, err := a.readOAuthAccount(); err != nil {
		d.ReasonKind = core.ReasonNotAuthenticated
		d.Reason = "no oauthAccount in ~/.claude.json (run `claude` and sign in)"
		return d
	}
	d.Authenticated = true
	return d
}

// oauthAccount mirrors the relevant fields of ~/.claude.json → oauthAccount.
type oauthAccount struct {
	EmailAddress              string `json:"emailAddress"`
	DisplayName               string `json:"displayName"`
	OrganizationType          string `json:"organizationType"`
	OrganizationRateLimitTier string `json:"organizationRateLimitTier"`
	BillingType               string `json:"billingType"`
	HasExtraUsageEnabled      bool   `json:"hasExtraUsageEnabled"`
}

func (a ClaudeAdapter) readOAuthAccount() (*oauthAccount, error) {
	path := filepath.Join(a.home(), ".claude.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root struct {
		OAuthAccount *oauthAccount `json:"oauthAccount"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if root.OAuthAccount == nil {
		return nil, fmt.Errorf("oauthAccount missing")
	}
	return root.OAuthAccount, nil
}

func (a ClaudeAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	acct, err := a.readOAuthAccount()
	if err != nil {
		return nil, err
	}
	st := &core.Status{
		Tool:      "claude",
		Name:      "Claude",
		Source:    core.SourceLocalState,
		Stability: core.StabilityLocal,
		Account:   firstNonEmpty(acct.EmailAddress, acct.DisplayName),
		Plan:      planFromOrganizationType(acct.OrganizationType),
	}
	if acct.OrganizationRateLimitTier != "" && acct.OrganizationRateLimitTier != "default_claude_ai" {
		st.Tier = acct.OrganizationRateLimitTier
	}
	st.Limits = append(st.Limits, a.usageLimits(ctx)...)
	return st, nil
}

func planFromOrganizationType(t string) string {
	switch t {
	case "claude_free", "free":
		return "Free"
	case "claude_pro", "pro":
		return "Pro"
	case "claude_max", "max":
		return "Max"
	case "claude_business", "business":
		return "Business"
	case "claude_enterprise", "enterprise":
		return "Enterprise"
	default:
		return ""
	}
}

// oauthUsageResponse mirrors api.anthropic.com/api/oauth/usage.
type oauthUsageResponse struct {
	FiveHour  *oauthWindow `json:"five_hour"`
	SevenDay  *oauthWindow `json:"seven_day"`
	ThirtyDay *oauthWindow `json:"thirty_day"`
}

type oauthWindow struct {
	Utilization float64 `json:"utilization"` // 0..1 fraction used
	ResetsAt    string  `json:"resets_at"`
}

// usageLimits fetches usage windows when a token file exists. Returns empty
// (and a note) otherwise; never fails the whole fetch.
func (a ClaudeAdapter) usageLimits(ctx context.Context) []core.Limit {
	token, err := a.readClaudeAccessToken()
	if err != nil {
		return nil
	}
	base := a.OverrideBaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/oauth/usage", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Content-Type", "application/json")

	client := httpClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	var u oauthUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil
	}
	var limits []core.Limit
	for _, w := range []struct {
		kind  core.LimitKind
		label string
		win   *oauthWindow
	}{{core.LimitRollingWindow, "5h", u.FiveHour}, {core.LimitWeekly, "Weekly", u.SevenDay}} {
		if w.win == nil {
			continue
		}
		l := core.Limit{
			Kind:        w.kind,
			Label:       w.label,
			Used:        -1,
			PercentUsed: w.win.Utilization * 100,
			Unit:        "percent",
			Source:      core.SourceOfficialAPI,
		}
		if t, err := time.Parse(time.RFC3339, w.win.ResetsAt); err == nil {
			l.ResetAt = &t
		}
		limits = append(limits, l)
	}
	return limits
}

// readClaudeAccessToken locates the OAuth access token on disk. On Windows the
// token is not stored as a file (OS credential vault), so it returns an error
// and usage is skipped. Never exposes the token itself.
func (a ClaudeAdapter) readClaudeAccessToken() (string, error) {
	candidates := []string{
		filepath.Join(a.home(), ".claude", ".credentials.json"),
		filepath.Join(a.home(), ".claude", "credentials.json"),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var creds struct {
			ClaudeAiOauth *struct {
				AccessToken string `json:"accessToken"`
			} `json:"claudeAiOauth"`
		}
		if json.Unmarshal(data, &creds) != nil {
			continue
		}
		if creds.ClaudeAiOauth != nil && creds.ClaudeAiOauth.AccessToken != "" {
			return creds.ClaudeAiOauth.AccessToken, nil
		}
	}
	return "", fmt.Errorf("no claude credentials file on disk")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
