package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"statisfy/internal/core"
	"statisfy/internal/detect"
)

// ClineAdapter aggregates task usage from Cline's local VS Code-family state:
// state/taskHistory.json inside each editor's globalStorage for the
// saoudrizwan.claude-dev extension.
//
// Cline is a client/tool, not a billing provider: this adapter reports
// Cline-owned activity (tasks, tokens, locally recorded cost) and never labels
// an underlying provider's quota as "Cline quota". Cline's provider
// credentials live in its settings/providers files and are never read or
// verified — authentication is deliberately not evaluated here.
//
// Source stability: the globalStorage path is a long-established convention,
// but the taskHistory.json schema is community-documented and can shift across
// extension versions — hence StabilityExperimental.
type ClineAdapter struct {
	// OverrideStateDir points at a claude-dev state dir (tests).
	OverrideStateDir string
}

func (a ClineAdapter) ID() string   { return "cline" }
func (a ClineAdapter) Name() string { return "Cline" }

// clineStateDirs returns the candidate globalStorage dirs for the Cline
// extension across the common VS Code-family editors.
func (a ClineAdapter) clineStateDirs() []string {
	if a.OverrideStateDir != "" {
		return []string{a.OverrideStateDir}
	}
	base := editorConfigBase()
	names := []string{"Code", "Code - Insiders", "Cursor", "Windsurf", "VSCodium"}
	dirs := make([]string, 0, len(names))
	for _, n := range names {
		dirs = append(dirs, filepath.Join(base, n, "User", "globalStorage", "saoudrizwan.claude-dev"))
	}
	return dirs
}

// editorConfigBase returns the platform config root for VS Code-family editors.
func editorConfigBase() string {
	switch runtime.GOOS {
	case "windows":
		if app := os.Getenv("APPDATA"); app != "" {
			return app
		}
		return filepath.Join(detect.HomeDir(), "AppData", "Roaming")
	case "darwin":
		return filepath.Join(detect.HomeDir(), "Library", "Application Support")
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return xdg
		}
		return filepath.Join(detect.HomeDir(), ".config")
	}
}

// findTaskHistory returns the taskHistory.json path from the first state dir
// that contains one.
func (a ClineAdapter) findTaskHistory() (string, error) {
	for _, dir := range a.clineStateDirs() {
		p := filepath.Join(dir, "state", "taskHistory.json")
		if detect.FileExists(p) {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}

func (a ClineAdapter) Detect(ctx context.Context) core.Detection {
	d := core.Detection{}
	any := false
	for _, dir := range a.clineStateDirs() {
		if detect.DirExists(dir) {
			any = true
			break
		}
	}
	if !any {
		d.ReasonKind = core.ReasonNotInstalled
		d.Reason = "no Cline state found (VS Code-family Cline extension not set up on this machine)"
		return d
	}
	d.Installed = true
	if _, err := a.findTaskHistory(); err != nil {
		d.ReasonKind = core.ReasonLocalState
		d.Reason = "Cline state exists but state/taskHistory.json is missing or unreadable"
		return d
	}
	d.Configured = true
	return d
}

// clineTask mirrors one entry of state/taskHistory.json. Only numeric usage
// fields are kept; the free-form task text and any credential-like fields are
// never read into the struct, so they can never be rendered or serialized.
type clineTask struct {
	TokensIn  int64   `json:"tokensIn"`
	TokensOut int64   `json:"tokensOut"`
	TotalCost float64 `json:"totalCost"`
	TS        int64   `json:"ts"` // epoch milliseconds (local calendar day used for "today")
}

// parseClineTasks parses state/taskHistory.json, normally an array of task
// records. An object wrapper {"tasks": [...]} is accepted as well. Unknown or
// additional fields are tolerated.
func parseClineTasks(data []byte) ([]clineTask, error) {
	var arr []clineTask
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr, nil
	}
	var wrapped struct {
		Tasks []clineTask `json:"tasks"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Tasks, nil
}

func (a ClineAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	path, err := a.findTaskHistory()
	if err != nil {
		return nil, fmt.Errorf("cline task history: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cline task history: %w", err)
	}
	tasks, err := parseClineTasks(data)
	if err != nil {
		return nil, fmt.Errorf("parse cline task history: %w", err)
	}

	start := localDayStartMs()
	var sessions, tokens int64
	var cost float64
	for _, t := range tasks {
		if t.TS < start {
			continue
		}
		sessions++
		tokens += t.TokensIn + t.TokensOut
		cost += t.TotalCost
	}

	st := &core.Status{
		Tool:      "cline",
		Name:      "Cline",
		Source:    core.SourceLocalState,
		Stability: core.StabilityExperimental,
	}
	st.Metrics = []core.Metric{
		{Kind: core.MetricSessions, Label: "Tasks", Value: float64(sessions), Source: core.SourceLocalState},
		{Kind: core.MetricTokens, Label: "Today", Value: float64(tokens), Unit: "tokens", Source: core.SourceLocalState},
		{Kind: core.MetricCost, Label: "Cost", Value: cost, Unit: "cost", Source: core.SourceLocalState},
	}
	return st, nil
}
