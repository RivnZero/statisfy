package adapters

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"

	"statisfy/internal/core"
	"statisfy/internal/detect"
)

// CopilotAdapter aggregates local session usage from the GitHub Copilot CLI's
// data.db. The accounts table contains an access_token column that statisfy
// NEVER reads, prints, or serializes — only the account login is surfaced.
// Plan is not locally detectable, so it is omitted rather than inferred from
// binary presence.
type CopilotAdapter struct {
	// OverrideDataDir points at the copilot data dir (tests).
	OverrideDataDir string
}

func (a CopilotAdapter) ID() string   { return "copilot" }
func (a CopilotAdapter) Name() string { return "Copilot" }

// copilotDataDir returns where the Copilot CLI keeps its state.
func (a CopilotAdapter) copilotDataDir() string {
	if a.OverrideDataDir != "" {
		return a.OverrideDataDir
	}
	return filepath.Join(detect.HomeDir(), ".copilot")
}

func (a CopilotAdapter) Detect(ctx context.Context) core.Detection {
	d := core.Detection{}
	if !detect.InPath("copilot") {
		d.ReasonKind = core.ReasonNotInstalled
		d.Reason = "copilot binary not found on PATH"
		return d
	}
	d.Installed = true
	db := filepath.Join(a.copilotDataDir(), "data.db")
	if !detect.FileExists(db) {
		d.ReasonKind = core.ReasonLocalState
		d.Reason = "missing data.db in ~/.copilot (Copilot CLI not initialized)"
		return d
	}
	// Authenticated = an account row exists (and is linked to github.com).
	if _, err := a.activeAccount(db); err != nil {
		d.ReasonKind = core.ReasonNotAuthenticated
		d.Reason = "no linked account in ~/.copilot/data.db (run `copilot login`)"
		return d
	}
	d.Authenticated = true
	return d
}

func (a CopilotAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	dbPath := filepath.Join(a.copilotDataDir(), "data.db")
	dsn := "file:" + filepath.ToSlash(dbPath) + "?mode=ro&_pragma=busy_timeout(2000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open copilot db: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open copilot db: %w", err)
	}

	login, err := a.activeAccount(dbPath)
	if err != nil {
		return nil, fmt.Errorf("copilot account: %w", err)
	}

	st := &core.Status{
		Tool:      "copilot",
		Name:      "Copilot",
		Account:   login,
		Source:    core.SourceLocalDatabase,
		Stability: core.StabilityLocal,
	}

	// Today's session aggregates. time_created is unix ms.
	start := localDayStartMs()
	var sessions int
	var in, out, cache int64
	var nanoAIU int64
	row := db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(total_input_tokens), 0),
		       COALESCE(SUM(total_output_tokens), 0),
		       COALESCE(SUM(total_cached_tokens), 0),
		       COALESCE(SUM(total_nano_aiu), 0)
		FROM sessions WHERE created_at >= ?`, start)
	if err := row.Scan(&sessions, &in, &out, &cache, &nanoAIU); err != nil {
		return nil, fmt.Errorf("query copilot sessions: %w", err)
	}

	if sessions > 0 {
		st.Metrics = append(st.Metrics,
			core.Metric{Kind: core.MetricSessions, Label: "Today", Value: float64(sessions), Unit: "sessions", Source: core.SourceLocalDatabase},
			core.Metric{Kind: core.MetricTokens, Label: "Tokens", Value: float64(in + out + cache), Unit: "tokens", Source: core.SourceLocalDatabase},
		)
		if nanoAIU > 0 {
			// Copilot measures usage in "AI credits"; the DB stores nano-units.
			st.Metrics = append(st.Metrics, core.Metric{
				Kind: core.MetricCredits, Label: "AI credits", Value: float64(nanoAIU) / 1e9, Unit: "credits", Source: core.SourceLocalDatabase,
			})
		}
	}

	// Most recent model, when the sessions table reports one.
	var lastModel string
	_ = db.QueryRowContext(ctx, `SELECT model FROM sessions WHERE model IS NOT NULL AND model != '' ORDER BY updated_at DESC LIMIT 1`).Scan(&lastModel)
	if lastModel != "" && lastModel != "auto" {
		st.Model = lastModel
	}
	return st, nil
}

// activeAccount returns the login of the default github.com account without
// reading any credential columns.
func (a CopilotAdapter) activeAccount(dbPath string) (string, error) {
	dsn := "file:" + filepath.ToSlash(dbPath) + "?mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var login string
	err = db.QueryRow(`SELECT login FROM accounts WHERE is_default = 1 AND host = 'github.com' LIMIT 1`).Scan(&login)
	if err != nil {
		// Fall back to any account row.
		err2 := db.QueryRow(`SELECT login FROM accounts ORDER BY created_at LIMIT 1`).Scan(&login)
		if err2 != nil {
			return "", fmt.Errorf("no accounts row")
		}
	}
	return login, nil
}
