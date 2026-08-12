// Package core defines the domain model for statisfy: the normalized status
// representation shared by all adapters, the Adapter contract, and the
// registry/orchestration that keeps provider-specific logic out of the core.
package core

import (
	"context"
	"time"
)

// LimitKind classifies the kind of quota or usage a Limit represents.
type LimitKind string

const (
	LimitRollingWindow LimitKind = "rolling_window"
	LimitWeekly        LimitKind = "weekly"
	LimitDaily         LimitKind = "daily"
	LimitSessions      LimitKind = "sessions"
	LimitCredits       LimitKind = "credits"
	LimitTokens        LimitKind = "tokens"
	LimitCost          LimitKind = "cost"
	LimitUnlimited     LimitKind = "unlimited"
	LimitUnknown       LimitKind = "unknown"
)

// Source records where a value came from. Every non-trivial metric keeps this
// metadata so consumers can judge reliability.
type Source string

const (
	SourceOfficialAPI   Source = "official-api"
	SourceCLIOutput     Source = "cli-output"
	SourceLocalState    Source = "local-state"
	SourceLocalDatabase Source = "local-database"
	SourceConfig        Source = "config"
	SourceInternalAPI   Source = "internal-api"
	SourceDerived       Source = "derived"
)

// Stability describes how much an adapter relies on interfaces that can
// change without notice.
type Stability string

const (
	StabilityStable       Stability = "stable"
	StabilityDocumented   Stability = "documented"
	StabilityLocal        Stability = "local"
	StabilityInternal     Stability = "internal"
	StabilityExperimental Stability = "experimental"
)

// MetricKind classifies metrics that are not quota limits (e.g. today's cost).
type MetricKind string

const (
	MetricTokens   MetricKind = "tokens"
	MetricCost     MetricKind = "cost"
	MetricSessions MetricKind = "sessions"
	MetricCredits  MetricKind = "credits"
	MetricMessages MetricKind = "messages"
	MetricOther    MetricKind = "other"
)

// Limit is a single piece of quota/usage information.
//
//	Used < 0          → unknown usage
//	Total <= 0        → no total (unbounded or unknown)
//	PercentUsed >= 0  → directly reported or derived percentage (0-100)
//	Unlimited         → the source explicitly reported unlimited
type Limit struct {
	Kind        LimitKind  `json:"kind"`
	Label       string     `json:"label,omitempty"`
	Used        float64    `json:"used,omitempty"`
	Total       float64    `json:"total,omitempty"`
	PercentUsed float64    `json:"percent_used,omitempty"`
	Unit        string     `json:"unit,omitempty"`
	Unlimited   bool       `json:"unlimited,omitempty"`
	ResetAt     *time.Time `json:"reset_at,omitempty"`
	Source      Source     `json:"source,omitempty"`
}

// Known reports whether usage was actually detected (not fabricated).
func (l Limit) Known() bool { return l.Used >= 0 }

// PercentLeft returns the remaining percentage (0-100) when known, else -1.
func (l Limit) PercentLeft() float64 {
	if l.PercentUsed < 0 {
		return -1
	}
	return 100 - l.PercentUsed
}

// Metric is a non-quota usage value, e.g. today's tokens or cost.
type Metric struct {
	Kind  MetricKind `json:"kind"`
	Label string     `json:"label,omitempty"`
	Value float64    `json:"value"`
	Unit  string     `json:"unit,omitempty"`
	// Source is where this metric's value came from (provenance).
	Source Source `json:"source,omitempty"`
}

// Status is the normalized status for one tool. Zero-value fields are simply
// omitted by renderers — never invented.
type Status struct {
	// SkipCache marks statuses that must be re-derived on every run and never
	// stored in the cache (e.g. safety-guard results whose correctness depends
	// on fresh evaluation). Never serialized.
	SkipCache  bool   `json:"-"`
	Tool       string `json:"tool"`
	Name       string `json:"name"`
	Account    string `json:"account,omitempty"`
	Plan       string `json:"plan,omitempty"`
	Tier       string `json:"tier,omitempty"`
	Multiplier int    `json:"multiplier,omitempty"`
	// Provider and Model are the tool → provider → model relationship when
	// actually detectable (e.g. OpenCode → OpenRouter → Claude Sonnet).
	// Optional: adapters that cannot detect them leave them empty.
	Provider    string    `json:"provider,omitempty"`
	Model       string    `json:"model,omitempty"`
	Limits      []Limit   `json:"limits,omitempty"`
	Metrics     []Metric  `json:"metrics,omitempty"`
	Source      Source    `json:"source,omitempty"`
	Stability   Stability `json:"stability,omitempty"`
	LastChecked time.Time `json:"last_checked"`
	Errors      []string  `json:"errors,omitempty"`
}

// Detection describes whether an integration is installed/configured and why
// it is or is not usable. It must be cheap: no process spawning, no network.
// UnavailableReason categorizes why an integration is unavailable so `doctor`
// can distinguish failure classes rather than relying on free-form text alone.
type UnavailableReason string

const (
	ReasonNotInstalled         UnavailableReason = "binary_missing"
	ReasonNotConfigured        UnavailableReason = "configuration_missing"
	ReasonNotAuthenticated     UnavailableReason = "authentication_missing"
	ReasonUnsupportedVersion   UnavailableReason = "unsupported_version"
	ReasonInterfaceUnavailable UnavailableReason = "interface_unavailable"
	ReasonParseFailure         UnavailableReason = "parse_failure"
	ReasonNetworkTimeout       UnavailableReason = "network_timeout"
	ReasonPermission           UnavailableReason = "permission_failure"
	ReasonLocalState           UnavailableReason = "local_state_unavailable"
)

type Detection struct {
	Installed     bool              `json:"installed"`
	Authenticated bool              `json:"authenticated"`
	Configured    bool              `json:"configured"`
	ReasonKind    UnavailableReason `json:"reason_kind,omitempty"` // category, when unavailable
	Reason        string            `json:"reason,omitempty"`      // why unavailable
}

// Available means enough configuration exists to obtain meaningful status.
func (d Detection) Available() bool {
	return d.Installed && (d.Authenticated || d.Configured)
}

// Adapter is the pluggable contract for one external tool. The core knows
// nothing about individual tools beyond this interface.
type Adapter interface {
	// ID is a stable machine identifier, e.g. "codex".
	ID() string
	// Name is the human display name, e.g. "Codex".
	Name() string
	// Detect checks installation/configuration state without running the tool.
	Detect(ctx context.Context) Detection
	// Fetch retrieves a normalized Status. Should honor ctx cancellation.
	Fetch(ctx context.Context) (*Status, error)
}
