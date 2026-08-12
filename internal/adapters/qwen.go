package adapters

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"statisfy/internal/core"
	"statisfy/internal/detect"
)

// QwenAdapter reports whether Qwen Code is configured (settings.json with an
// auth type) and exposes the detected provider/model relationship. Qwen Code's
// billing provider may differ from the tool itself, so Provider/Model are only
// surfaced when actually present in settings — never assumed. The apiKey field
// is never read or printed.
type QwenAdapter struct {
	// OverrideHomeDir replaces the home directory (tests).
	OverrideHomeDir string
}

func (a QwenAdapter) ID() string   { return "qwen" }
func (a QwenAdapter) Name() string { return "Qwen Code" }

func (a QwenAdapter) home() string {
	if a.OverrideHomeDir != "" {
		return a.OverrideHomeDir
	}
	return detect.HomeDir()
}

func (a QwenAdapter) Detect(ctx context.Context) core.Detection {
	d := core.Detection{}
	if !detect.InPath("qwen") {
		d.ReasonKind = core.ReasonNotInstalled
		d.Reason = "qwen binary not found on PATH"
		return d
	}
	d.Installed = true
	s, err := a.readSettings()
	if err != nil {
		d.ReasonKind = core.ReasonNotConfigured
		d.Reason = "missing ~/.qwen/settings.json (Qwen Code not configured)"
		return d
	}
	if s.AuthType == "" {
		d.ReasonKind = core.ReasonNotAuthenticated
		d.Reason = "no auth type configured in ~/.qwen/settings.json"
		return d
	}
	d.Configured = true
	d.Authenticated = true
	return d
}

// qwenSettings mirrors the relevant fields of ~/.qwen/settings.json.
// The apiKey field is intentionally NOT included in the struct so it can never
// be read or serialized by this adapter.
type qwenSettings struct {
	AuthType string `json:"authType"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	ModelID  string `json:"modelId"`
}

func (a QwenAdapter) readSettings() (*qwenSettings, error) {
	candidates := []string{
		filepath.Join(a.home(), ".qwen", "settings.json"),
	}
	data, err := os.ReadFile(candidates[0])
	if err != nil {
		return nil, err
	}
	var s qwenSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (a QwenAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	s, err := a.readSettings()
	if err != nil {
		return nil, err
	}
	st := &core.Status{
		Tool:      "qwen",
		Name:      "Qwen Code",
		Source:    core.SourceConfig,
		Stability: core.StabilityLocal,
		Provider:  s.Provider,
		Model:     firstNonEmpty(s.Model, s.ModelID),
	}
	if st.Provider == "" && st.Model != "" {
		// Qwen's own model family is the provider when no custom one is set.
		st.Provider = "Qwen"
	}
	return st, nil
}
