package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"statisfy/internal/core"
	"statisfy/internal/detect"
)

// FreebuffAdapter shows per-model session limits for the Freebuff coding agent
// by calling its backend (the CLI stores an auth token under
// ~/.config/manicode/credentials.json). The token is sent only in the
// Authorization header and never printed or serialized.
type FreebuffAdapter struct {
	// OverrideBaseURL points requests elsewhere (tests).
	OverrideBaseURL string
	// OverrideConfigDir points at the manicode config dir (tests).
	OverrideConfigDir string
}

func (a FreebuffAdapter) ID() string   { return "freebuff" }
func (a FreebuffAdapter) Name() string { return "Freebuff" }

// manicodeConfigDir returns where the Freebuff CLI keeps its state.
func (a FreebuffAdapter) manicodeConfigDir() string {
	if a.OverrideConfigDir != "" {
		return a.OverrideConfigDir
	}
	return filepath.Join(detect.HomeDir(), ".config", "manicode")
}

func (a FreebuffAdapter) Detect(ctx context.Context) core.Detection {
	d := core.Detection{}
	if !detect.DirExists(a.manicodeConfigDir()) {
		d.ReasonKind = core.ReasonNotConfigured
		d.Reason = "missing ~/.config/manicode (Freebuff CLI not set up)"
		return d
	}
	d.Installed = true
	creds := filepath.Join(a.manicodeConfigDir(), "credentials.json")
	if !detect.FileExists(creds) {
		d.ReasonKind = core.ReasonNotConfigured
		d.Reason = "missing credentials.json in ~/.config/manicode"
		return d
	}
	if _, err := a.readToken(); err != nil {
		d.ReasonKind = core.ReasonNotAuthenticated
		d.Reason = "no authToken in ~/.config/manicode/credentials.json"
		return d
	}
	d.Configured = true
	d.Authenticated = true
	return d
}

// freebuffCredentials mirrors ~/.config/manicode/credentials.json.
type freebuffCredentials struct {
	Default *struct {
		AuthToken string `json:"authToken"`
	} `json:"default"`
}

func (a FreebuffAdapter) readToken() (string, error) {
	data, err := os.ReadFile(filepath.Join(a.manicodeConfigDir(), "credentials.json"))
	if err != nil {
		return "", err
	}
	var c freebuffCredentials
	if err := json.Unmarshal(data, &c); err != nil {
		return "", err
	}
	if c.Default == nil || c.Default.AuthToken == "" {
		return "", fmt.Errorf("authToken missing")
	}
	return c.Default.AuthToken, nil
}

// freebuffSession mirrors the verified GET /api/v1/freebuff/session response.
type freebuffSession struct {
	Status     string `json:"status"`
	AccessTier string `json:"accessTier"`
	Standing   *struct {
		Limits *struct {
			UserMessagesPerDay int `json:"userMessagesPerDay"`
			MessagesPer5Hours  int `json:"messagesPer5Hours"`
			MessagesPerDay     int `json:"messagesPerDay"`
		} `json:"limits"`
	} `json:"standing"`
	RateLimitsByModel map[string]*freebuffModelLimit `json:"rateLimitsByModel"`
	GLMPromo          *struct {
		DailySessions int    `json:"dailySessions"`
		EndsAt        string `json:"endsAt"`
	} `json:"glmPromo"`
	Message string `json:"message"`
}

type freebuffModelLimit struct {
	Model       string  `json:"model"`
	Limit       int     `json:"limit"`
	RecentCount float64 `json:"recentCount"`
	ResetAt     string  `json:"resetAt"`
	WindowHours int     `json:"windowHours"`
	Unlimited   bool    `json:"unlimited"`
}

// ownerFileFresh bounds how recent freebuff-instance-owner.json may be before
// it is treated as evidence of an active instance. Kept generous so the guard
// errs toward NOT querying the session endpoint.
const ownerFileFresh = 15 * time.Minute

// instanceOwner mirrors freebuff-instance-owner.json, which the Freebuff CLI
// writes at startup to track the single live instance (instanceId + pid).
type instanceOwner struct {
	InstanceID string `json:"instanceId"`
	PID        int    `json:"pid"`
}

// instanceActive reports whether a Freebuff CLI instance is currently running.
// The CLI writes freebuff-instance-owner.json at startup; the guard is
// deliberately conservative (fail-safe toward NOT querying):
//   - the recorded pid is alive → active;
//   - the owner file cannot be parsed but is recent → active;
//   - the owner file is recent (instance started recently, even if its pid
//     record is unusable) → active.
//
// Read-only; never signals or modifies the process.
func (a FreebuffAdapter) instanceActive() bool {
	p := filepath.Join(a.manicodeConfigDir(), "freebuff-instance-owner.json")
	fi, err := os.Stat(p)
	if err != nil {
		return false // no owner file at all: nothing locally indicates a live instance
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return time.Since(fi.ModTime()) < ownerFileFresh
	}
	var o instanceOwner
	if json.Unmarshal(data, &o) != nil {
		return time.Since(fi.ModTime()) < ownerFileFresh
	}
	if o.PID > 0 && detect.ProcessAlive(o.PID) {
		return true
	}
	return time.Since(fi.ModTime()) < ownerFileFresh
}

func (a FreebuffAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	// SAFETY (blocking invariant): statisfy is observational. The session
	// endpoint authenticates as the user and is tied to Freebuff's single-
	// instance session ownership; querying it while a Freebuff instance is
	// running risks interfering with the active workflow. So while an instance
	// is alive we make ZERO network requests and surface only locally-known
	// state. Live session data is shown only when no instance is running.
	if a.instanceActive() {
		st := &core.Status{
			Tool:      "freebuff",
			Name:      "Freebuff",
			Source:    core.SourceLocalState,
			Stability: core.StabilityLocal,
			// Never cache this: the guard must be re-evaluated on every run so
			// the "while Freebuff is running → zero requests" guarantee is fresh.
			SkipCache: true,
		}
		st.Errors = append(st.Errors, "live metrics skipped: a Freebuff instance is running (statisfy is read-only); run statisfy with Freebuff closed for live session data")
		return st, nil
	}

	token, err := a.readToken()
	if err != nil {
		return nil, err
	}
	base := a.OverrideBaseURL
	if base == "" {
		base = "https://www.codebuff.com"
	}
	// Exactly one request. This endpoint may be stateful (session ownership), so
	// it must never be retried automatically — a retry could repeat a side
	// effect. A single failure degrades to "no data", which is safe.
	resp, err := a.doSessionRequest(ctx, httpClient(), base, token)
	if err != nil {
		return nil, fmt.Errorf("freebuff session request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("freebuff session: HTTP %d: %s", resp.StatusCode, sanitize(string(body)))
	}
	var s freebuffSession
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("parse freebuff session: %w", err)
	}

	st := &core.Status{
		Tool:      "freebuff",
		Name:      "Freebuff",
		Source:    core.SourceInternalAPI,
		Stability: core.StabilityInternal,
	}
	if s.AccessTier != "" && s.AccessTier != "none" {
		st.Tier = strings.ToUpper(s.AccessTier[:1]) + s.AccessTier[1:]
	}
	if s.Message != "" && s.Status == "none" {
		st.Errors = append(st.Errors, sanitize(s.Message))
	}

	// Per-model session limits. Iterate deterministically (sorted by model
	// id) so the JSON output is stable across runs.
	modelKeys := make([]string, 0, len(s.RateLimitsByModel))
	for k := range s.RateLimitsByModel {
		modelKeys = append(modelKeys, k)
	}
	sort.Strings(modelKeys)
	for _, k := range modelKeys {
		ml := s.RateLimitsByModel[k]
		if ml == nil {
			continue
		}
		l := core.Limit{
			Kind:   core.LimitSessions,
			Label:  modelDisplayName(ml.Model),
			Used:   ml.RecentCount,
			Total:  float64(ml.Limit),
			Unit:   "sessions",
			Source: core.SourceInternalAPI,
		}
		if ml.Unlimited || ml.Limit <= 0 {
			l.Unlimited = true
			l.Total = 0
		}
		if t, err := time.Parse(time.RFC3339, ml.ResetAt); err == nil {
			l.ResetAt = &t
		}
		st.Limits = append(st.Limits, l)
	}

	// GLM promo limit.
	if s.GLMPromo != nil && s.GLMPromo.DailySessions > 0 {
		l := core.Limit{
			Kind:   core.LimitSessions,
			Label:  "GLM",
			Used:   -1,
			Total:  float64(s.GLMPromo.DailySessions),
			Unit:   "sessions",
			Source: core.SourceInternalAPI,
		}
		if t, err := time.Parse(time.RFC3339, s.GLMPromo.EndsAt); err == nil {
			l.ResetAt = &t
		}
		st.Limits = append(st.Limits, l)
	}

	// Message quotas as metrics.
	if s.Standing != nil && s.Standing.Limits != nil {
		lm := s.Standing.Limits
		if lm.MessagesPerDay > 0 {
			st.Metrics = append(st.Metrics, core.Metric{
				Kind: core.MetricMessages, Label: "Daily msgs", Value: float64(lm.MessagesPerDay), Unit: "msgs",
			})
		}
		if lm.MessagesPer5Hours > 0 {
			st.Metrics = append(st.Metrics, core.Metric{
				Kind: core.MetricMessages, Label: "5h msgs", Value: float64(lm.MessagesPer5Hours), Unit: "msgs",
			})
		}
	}
	return st, nil
}

// doSessionRequest performs one unauthenticated-agnostic GET (token via header).
func (a FreebuffAdapter) doSessionRequest(ctx context.Context, client *http.Client, base, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/freebuff/session", nil)
	if err != nil {
		return nil, err
	}
	setUA(req)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return client.Do(req)
}

// modelDisplayName turns "deepseek/deepseek-v4-flash" into "DeepSeek".
func modelDisplayName(model string) string {
	if model == "" {
		return "Model"
	}
	parts := strings.Split(model, "/")
	provider := parts[0]
	switch provider {
	case "deepseek":
		return "DeepSeek"
	case "mimo":
		return "MiMo"
	case "glm":
		return "GLM"
	case "openai":
		return "OpenAI"
	case "anthropic":
		return "Anthropic"
	case "opencode":
		return "OpenCode"
	default:
		return strings.ToUpper(provider[:1]) + provider[1:]
	}
}

// sanitize removes newlines/control chars from free-text server messages.
func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
