package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"statisfy/internal/core"
	"statisfy/internal/detect"
)

// claudeNow is injectable so transcript tests can pin a fixed "today" without
// depending on the wall clock or the machine's timezone.
var claudeNow = time.Now

// ClaudeAdapter reports the Claude Code plan from local CLI state
// (~/.claude.json → oauthAccount) and local usage from Claude Code's own
// transcript files (~/.claude/projects/**/*.jsonl).
//
// The two sources are independent: plan detection never depends on the token
// files, and usage never depends on plan state. Claude Code writes one JSONL
// transcript per session on every platform — including Windows, where the
// OAuth token lives in the OS credential vault and was previously a hard
// blocker for usage. Official usage windows are still fetched when an OAuth
// access token file is present (macOS/Linux); everywhere else the transcript
// metrics are the usage story, and nothing is ever fabricated.
//
// Observational invariant: this adapter only reads files and makes one GET
// to api.anthropic.com/api/oauth/usage when a token file exists. It never
// modifies transcripts, never locks them, and never touches authentication.
type ClaudeAdapter struct {
	// OverrideBaseURL points usage requests elsewhere (tests).
	OverrideBaseURL string
	// OverrideHomeDir replaces the home directory (tests).
	OverrideHomeDir string
	// OverrideProjectsDir replaces ~/.claude/projects (tests).
	OverrideProjectsDir string
}

func (a ClaudeAdapter) ID() string   { return "claude" }
func (a ClaudeAdapter) Name() string { return "Claude" }

func (a ClaudeAdapter) home() string {
	if a.OverrideHomeDir != "" {
		return a.OverrideHomeDir
	}
	return detect.HomeDir()
}

// projectsDir returns the Claude Code transcript root. Claude Code writes one
// JSONL file per session under ~/.claude/projects on macOS, Linux, and
// Windows alike.
func (a ClaudeAdapter) projectsDir() string {
	if a.OverrideProjectsDir != "" {
		return a.OverrideProjectsDir
	}
	return filepath.Join(a.home(), ".claude", "projects")
}

func (a ClaudeAdapter) Detect(ctx context.Context) core.Detection {
	d := core.Detection{}
	hasState := detect.DirExists(a.projectsDir()) ||
		detect.FileExists(filepath.Join(a.home(), ".claude.json"))
	if !detect.InPath("claude") && !hasState {
		d.ReasonKind = core.ReasonNotInstalled
		d.Reason = "claude binary not on PATH and no ~/.claude state found"
		return d
	}
	d.Installed = true
	if _, err := a.readOAuthAccount(); err == nil {
		d.Authenticated = true
	}
	if a.hasTranscripts() {
		d.Configured = true
	}
	if !d.Authenticated && !d.Configured {
		d.ReasonKind = core.ReasonLocalState
		d.Reason = "no oauthAccount in ~/.claude.json and no transcripts under ~/.claude/projects"
	}
	return d
}

func (a ClaudeAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	st := &core.Status{
		Tool:      "claude",
		Name:      "Claude",
		Source:    core.SourceLocalState,
		Stability: core.StabilityLocal,
	}

	// Plan/account from local CLI state — independent of transcript usage.
	if acct, err := a.readOAuthAccount(); err == nil {
		st.Account = firstNonEmpty(acct.EmailAddress, acct.DisplayName)
		st.Plan = planFromOrganizationType(acct.OrganizationType)
		if acct.OrganizationRateLimitTier != "" && acct.OrganizationRateLimitTier != "default_claude_ai" {
			st.Tier = acct.OrganizationRateLimitTier
		}
	}

	// Official usage windows only when an OAuth token file exists on disk
	// (not on Windows, where the token is in the OS credential vault).
	st.Limits = append(st.Limits, a.usageLimits(ctx)...)

	// Local usage from transcripts — works on every platform, including
	// Windows, with no token access at all.
	if paths, err := a.findTranscripts(); err == nil {
		u, perr := parseClaudeTranscripts(paths, claudeNow())
		if perr != nil {
			st.Errors = append(st.Errors, fmt.Sprintf("transcript parse: %v", perr))
		} else if u.Records > 0 {
			st.Metrics = append(st.Metrics,
				core.Metric{Kind: core.MetricTokens, Label: "Today", Value: float64(u.Tokens), Unit: "tokens", Source: core.SourceLocalState},
				core.Metric{Kind: core.MetricSessions, Label: "Sessions", Value: float64(len(u.Sessions)), Source: core.SourceLocalState},
			)
			st.Model = mostUsedModel(u.Models)
		}
	}

	if st.Plan == "" && st.Tier == "" && len(st.Limits) == 0 && len(st.Metrics) == 0 {
		return nil, fmt.Errorf("no usable Claude data (no plan state and no transcripts)")
	}
	return st, nil
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
// and official usage windows are skipped — the transcript metrics still work.
// Never exposes the token itself.
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

// hasTranscripts is a cheap detection probe: it stops at the first JSONL
// transcript instead of walking the whole tree (Detect runs for every tool
// every invocation).
func (a ClaudeAdapter) hasTranscripts() bool {
	root := a.projectsDir()
	if !detect.DirExists(root) {
		return false
	}
	found := false
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// findTranscripts returns the sorted list of JSONL transcripts under
// ~/.claude/projects. Unreadable subtrees are skipped rather than failing.
func (a ClaudeAdapter) findTranscripts() ([]string, error) {
	root := a.projectsDir()
	if !detect.DirExists(root) {
		return nil, os.ErrNotExist
	}
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries instead of aborting
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// claudeTranscriptRecord mirrors the subset of a Claude Code JSONL transcript
// line that carries usage. Only the fields below are read; anything else
// (including content and tool payloads) is ignored and can never surface.
type claudeTranscriptRecord struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Timestamp string `json:"timestamp"`
	Message   *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens        int64 `json:"input_tokens"`
			OutputTokens       int64 `json:"output_tokens"`
			CacheReadInput     int64 `json:"cache_read_input_tokens"`
			CacheCreationInput int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// claudeTranscriptUsage is the aggregated local usage for a window (today).
type claudeTranscriptUsage struct {
	Records  int             // assistant records with token usage in the window
	Tokens   int64           // input + output + cache read + cache creation
	Sessions map[string]bool // distinct session IDs with activity in the window
	Models   map[string]int  // model name → record count in the window
}

func newClaudeTranscriptUsage() *claudeTranscriptUsage {
	return &claudeTranscriptUsage{
		Sessions: map[string]bool{},
		Models:   map[string]int{},
	}
}

func (u *claudeTranscriptUsage) add(rec claudeTranscriptRecord) {
	us := rec.Message.Usage
	u.Records++
	u.Tokens += us.InputTokens + us.OutputTokens + us.CacheReadInput + us.CacheCreationInput
	u.Sessions[rec.SessionID] = true
	if rec.Message.Model != "" {
		u.Models[rec.Message.Model]++
	}
}

// parseClaudeTranscripts aggregates usage across transcript files for the
// local calendar day containing now. It is deliberately tolerant: malformed
// lines are skipped, additive unknown fields are ignored, records whose
// timestamp cannot be parsed are excluded, and a single unreadable or
// oversized file is skipped rather than destroying the whole aggregation.
// It only fails when no transcript could be parsed at all.
func parseClaudeTranscripts(paths []string, now time.Time) (*claudeTranscriptUsage, error) {
	start := localDayStart(now)
	u := newClaudeTranscriptUsage()
	var firstErr error
	parsed := 0
	for _, p := range paths {
		if err := parseClaudeTranscriptFile(p, start, u); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		parsed++
	}
	if parsed == 0 && len(paths) > 0 {
		return u, firstErr
	}
	return u, nil
}

func parseClaudeTranscriptFile(path string, start time.Time, u *claudeTranscriptUsage) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	// Claude Code streams multiple JSONL records per API response, all sharing
	// the same message.id; only the last one carries the final usage tallies.
	// Dedup keeps the last record per id.
	seen := map[string]claudeTranscriptRecord{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec claudeTranscriptRecord
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue // malformed line: skip, keep scanning
		}
		if rec.Type != "assistant" || rec.SessionID == "" || rec.Message == nil || rec.Message.Usage == nil {
			continue
		}
		us := rec.Message.Usage
		if us.InputTokens+us.OutputTokens+us.CacheReadInput+us.CacheCreationInput == 0 {
			continue // no usage recorded on this record
		}
		ts, err := time.Parse(time.RFC3339, rec.Timestamp)
		if err != nil || ts.Before(start) {
			continue // unclassifiable or outside today's window
		}
		if rec.Message.ID != "" {
			seen[rec.Message.ID] = rec
		} else {
			u.add(rec)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	for _, rec := range seen {
		u.add(rec)
	}
	return nil
}

// localDayStart returns midnight (start of the local calendar day) for t.
func localDayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// mostUsedModel returns the model with the most records in the window, or ""
// when none is known. Ties break lexicographically so the result is stable
// across runs.
func mostUsedModel(models map[string]int) string {
	best, max := "", 0
	for m, c := range models {
		if c > max || (c == max && (best == "" || m < best)) {
			best, max = m, c
		}
	}
	return best
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
