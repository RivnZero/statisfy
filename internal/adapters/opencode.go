package adapters

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"statisfy/internal/core"
	"statisfy/internal/detect"
)

// OpenCodeAdapter aggregates session usage from OpenCode's local SQLite store.
// OpenCode is a local-first tool: today's sessions, tokens, and cost are read
// from the session table without any account or network access.
type OpenCodeAdapter struct {
	// OverrideDataDir points at the opencode data dir (tests).
	OverrideDataDir string
}

func (a OpenCodeAdapter) ID() string   { return "opencode" }
func (a OpenCodeAdapter) Name() string { return "OpenCode" }

// opencodeDataDir returns the data directory; OpenCode on Windows/macOS/Linux
// defaults to ~/.local/share/opencode.
func (a OpenCodeAdapter) opencodeDataDir() string {
	if a.OverrideDataDir != "" {
		return a.OverrideDataDir
	}
	return filepath.Join(detect.HomeDir(), ".local", "share", "opencode")
}

func (a OpenCodeAdapter) Detect(ctx context.Context) core.Detection {
	d := core.Detection{}
	db := filepath.Join(a.opencodeDataDir(), "opencode.db")
	if !detect.FileExists(db) {
		d.ReasonKind = core.ReasonLocalState
		d.Reason = "missing opencode.db in ~/.local/share/opencode (OpenCode not used here)"
		return d
	}
	d.Installed = true
	// Configured means the DB exists and is readable — that is the state that
	// yields meaningful information.
	d.Configured = true
	return d
}

func (a OpenCodeAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	dbPath := filepath.Join(a.opencodeDataDir(), "opencode.db")
	dsn := "file:" + filepath.ToSlash(dbPath) + "?mode=ro&_pragma=busy_timeout(2000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open opencode db: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open opencode db: %w", err)
	}

	start := localDayStartMs()
	var sessions int
	var tokensIn, tokensOut, tokensRea int64
	var cost float64
	row := db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(tokens_input), 0),
		       COALESCE(SUM(tokens_output), 0),
		       COALESCE(SUM(tokens_reasoning), 0),
		       COALESCE(SUM(cost), 0)
		FROM session WHERE time_created >= ?`, start)
	if err := row.Scan(&sessions, &tokensIn, &tokensOut, &tokensRea, &cost); err != nil {
		return nil, fmt.Errorf("query opencode sessions: %w", err)
	}

	st := &core.Status{
		Tool:      "opencode",
		Name:      "OpenCode",
		Source:    core.SourceLocalDatabase,
		Stability: core.StabilityLocal,
	}
	st.Metrics = append(st.Metrics,
		core.Metric{Kind: core.MetricTokens, Label: "Today", Value: float64(tokensIn + tokensOut + tokensRea), Unit: "tokens", Source: core.SourceLocalDatabase},
		core.Metric{Kind: core.MetricCost, Label: "Cost", Value: cost, Unit: "cost", Source: core.SourceLocalDatabase},
		core.Metric{Kind: core.MetricSessions, Label: "Sessions", Value: float64(sessions), Unit: "sessions", Source: core.SourceLocalDatabase},
	)
	return st, nil
}

// localDayStartMs returns today's local midnight in unix milliseconds.
func localDayStartMs() int64 {
	now := time.Now()
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location()).UnixMilli()
}
