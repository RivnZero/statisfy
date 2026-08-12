package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"statisfy/internal/core"
	"statisfy/internal/detect"
)

// DroidAdapter reports Factory Droid's configured model from the documented
// local settings file (~/.factory/settings.json). Droid's usage/credits data
// is only available through the semi-documented api.factory.ai backend
// authenticated with the user's live token; statisfy deliberately does NOT
// call it (observational invariant — a status check must never touch the
// tool's account or sessions). Only locally verifiable state is surfaced.
//
// settings.json can contain API keys under customModels[].apiKey — this
// struct intentionally does not define those fields, so they can never be
// read, rendered, or serialized.
type DroidAdapter struct {
	// OverrideHomeDir replaces the home directory (tests).
	OverrideHomeDir string
}

func (a DroidAdapter) ID() string   { return "droid" }
func (a DroidAdapter) Name() string { return "Droid" }

func (a DroidAdapter) home() string {
	if a.OverrideHomeDir != "" {
		return a.OverrideHomeDir
	}
	return detect.HomeDir()
}

func (a DroidAdapter) settingsPath() string {
	return filepath.Join(a.home(), ".factory", "settings.json")
}

// hasAPIKeyEnv reports whether a Factory credential is present in the
// environment. The value itself is never read beyond existence.
func hasAPIKeyEnv() bool {
	return os.Getenv("FACTORY_API_KEY") != "" || os.Getenv("FACTORY_TOKEN") != ""
}

func (a DroidAdapter) Detect(ctx context.Context) core.Detection {
	d := core.Detection{}
	if !detect.InPath("droid") {
		d.ReasonKind = core.ReasonNotInstalled
		d.Reason = "droid binary not found on PATH"
		return d
	}
	d.Installed = true
	if !detect.FileExists(a.settingsPath()) {
		d.ReasonKind = core.ReasonNotConfigured
		d.Reason = "missing ~/.factory/settings.json (Droid not configured here)"
		return d
	}
	d.Configured = true
	// A FACTORY_API_KEY/FACTORY_TOKEN alone cannot produce any status (Fetch
	// reads the settings file), so availability hinges on Configured; the
	// credential is only reported as a bonus on top of a configured setup.
	if hasAPIKeyEnv() {
		d.Authenticated = true
	}
	return d
}

// droidSettings mirrors the safe fields of ~/.factory/settings.json. The
// customModels array (which can embed api keys) is intentionally absent.
type droidSettings struct {
	Model string `json:"model"`
}

func (a DroidAdapter) readSettings() (*droidSettings, error) {
	data, err := os.ReadFile(a.settingsPath())
	if err != nil {
		return nil, err
	}
	var s droidSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (a DroidAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	s, err := a.readSettings()
	if err != nil {
		return nil, fmt.Errorf("read droid settings: %w", err)
	}
	st := &core.Status{
		Tool:      "droid",
		Name:      "Droid",
		Model:     s.Model,
		Source:    core.SourceConfig,
		Stability: core.StabilityDocumented,
	}
	// No usage/credits are locally exposed and the backend is intentionally
	// not queried — nothing further is ever invented for Droid.
	return st, nil
}
