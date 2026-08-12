package adapters

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"statisfy/internal/core"
	"statisfy/internal/detect"
)

// GeminiAdapter reports whether the Gemini CLI has an active Google account
// signed in (via ~/.gemini/google_accounts.json) and surfaces the account's
// email/display name. The Gemini CLI does not expose a local quota/usage file
// on this setup, so only actually-detectable account info is normalized —
// missing plan/usage is omitted, never fabricated.
type GeminiAdapter struct {
	// OverrideHomeDir replaces the home directory (tests).
	OverrideHomeDir string
}

func (a GeminiAdapter) ID() string   { return "gemini" }
func (a GeminiAdapter) Name() string { return "Gemini" }

func (a GeminiAdapter) home() string {
	if a.OverrideHomeDir != "" {
		return a.OverrideHomeDir
	}
	return detect.HomeDir()
}

func (a GeminiAdapter) Detect(ctx context.Context) core.Detection {
	d := core.Detection{}
	if !detect.InPath("gemini") {
		d.ReasonKind = core.ReasonNotInstalled
		d.Reason = "gemini binary not found on PATH"
		return d
	}
	d.Installed = true
	acc, err := a.readAccounts()
	if err != nil {
		d.ReasonKind = core.ReasonNotAuthenticated
		d.Reason = "no google_accounts.json in ~/.gemini (run `gemini` and sign in)"
		return d
	}
	if acc.Active == nil {
		d.ReasonKind = core.ReasonNotAuthenticated
		d.Reason = "no active Google account in ~/.gemini/google_accounts.json"
		return d
	}
	d.Authenticated = true
	return d
}

// googleAccounts mirrors ~/.gemini/google_accounts.json.
type googleAccounts struct {
	Active *googleAccount  `json:"active"`
	Old    []googleAccount `json:"old"`
}

type googleAccount struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

func (a GeminiAdapter) readAccounts() (*googleAccounts, error) {
	path := filepath.Join(a.home(), ".gemini", "google_accounts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var acc googleAccounts
	if err := json.Unmarshal(data, &acc); err != nil {
		return nil, err
	}
	return &acc, nil
}

func (a GeminiAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	acc, err := a.readAccounts()
	if err != nil {
		return nil, err
	}
	st := &core.Status{
		Tool:      "gemini",
		Name:      "Gemini",
		Source:    core.SourceLocalState,
		Stability: core.StabilityLocal,
	}
	if acc.Active != nil {
		st.Account = firstNonEmpty(acc.Active.Email, acc.Active.DisplayName)
	}
	return st, nil
}
