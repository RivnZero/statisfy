package adapters

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"statisfy/internal/core"
)

func withFakeDroidOnPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"droid", "droid.exe"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

func writeDroidSettings(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".factory")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDroidParseFixture(t *testing.T) {
	var s droidSettings
	if err := json.Unmarshal(readFixture(t, "droid_settings.json"), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Model != "claude-opus-4-7" {
		t.Errorf("model = %q, want claude-opus-4-7", s.Model)
	}
	// The struct does not define customModels/apiKey — compile-time proof that
	// credentials cannot be read into the adapter.
}

func TestDroidFetchFromFixture(t *testing.T) {
	home := t.TempDir()
	writeDroidSettings(t, home, string(readFixture(t, "droid_settings.json")))
	a := DroidAdapter{OverrideHomeDir: home}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if st.Model != "claude-opus-4-7" {
		t.Errorf("model = %q", st.Model)
	}
	// Droid's usage/credits are not locally exposed; nothing may be invented.
	if st.Plan != "" || len(st.Limits) > 0 || len(st.Metrics) > 0 || st.Provider != "" {
		t.Errorf("invented data: plan=%q limits=%d metrics=%d provider=%q",
			st.Plan, len(st.Limits), len(st.Metrics), st.Provider)
	}
}

func TestDroidFetchNoSecretLeak(t *testing.T) {
	home := t.TempDir()
	writeDroidSettings(t, home, string(readFixture(t, "droid_settings.json")))
	a := DroidAdapter{OverrideHomeDir: home}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	out, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"fk-fake", "apiKey", "customModels", "sk-"} {
		if containsStr(string(out), needle) {
			t.Fatalf("%q leaked into status JSON", needle)
		}
	}
}

func TestDroidParseMissingFields(t *testing.T) {
	var s droidSettings
	if err := json.Unmarshal([]byte(`{"reasoningEffort": "low"}`), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Model != "" {
		t.Errorf("model = %q, want empty", s.Model)
	}
}

func TestDroidParseMalformed(t *testing.T) {
	var s droidSettings
	if err := json.Unmarshal([]byte(`{not json`), &s); err == nil {
		t.Fatal("malformed settings parsed without error")
	}
}

func TestDroidDetectNotInstalled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir) // PATH with no droid
	a := DroidAdapter{OverrideHomeDir: t.TempDir()}
	d := a.Detect(context.Background())
	if d.Installed || d.Available() {
		t.Errorf("detect = %+v, want not installed", d)
	}
	if d.ReasonKind != core.ReasonNotInstalled {
		t.Errorf("reason_kind = %q", d.ReasonKind)
	}
}

func TestDroidDetectUnavailableWithoutSettings(t *testing.T) {
	withFakeDroidOnPath(t)
	a := DroidAdapter{OverrideHomeDir: t.TempDir()}
	d := a.Detect(context.Background())
	if !d.Installed {
		t.Errorf("installed = false with droid on PATH")
	}
	if d.Configured || d.Available() {
		t.Errorf("configured/available = true without settings.json")
	}
	if d.ReasonKind != core.ReasonNotConfigured {
		t.Errorf("reason_kind = %q, want configuration_missing", d.ReasonKind)
	}
}

func TestDroidDetectConfigured(t *testing.T) {
	withFakeDroidOnPath(t)
	home := t.TempDir()
	writeDroidSettings(t, home, string(readFixture(t, "droid_settings.json")))
	a := DroidAdapter{OverrideHomeDir: home}
	d := a.Detect(context.Background())
	if !d.Installed || !d.Configured || !d.Available() {
		t.Errorf("detect = %+v, want installed+configured+available", d)
	}
}

func TestDroidDetectEnvCredential(t *testing.T) {
	withFakeDroidOnPath(t)
	home := t.TempDir()
	writeDroidSettings(t, home, string(readFixture(t, "droid_settings.json")))
	t.Setenv("FACTORY_API_KEY", "fk-fake")
	a := DroidAdapter{OverrideHomeDir: home}
	d := a.Detect(context.Background())
	if !d.Authenticated || !d.Available() {
		t.Errorf("detect = %+v, want authenticated+available with key and settings", d)
	}
}

// TestDroidKeyOnlyNotAvailable guards a regression: a credential alone must
// not make Droid "available" — Fetch reads settings.json for the model, so a
// key without configuration could never produce a status.
func TestDroidKeyOnlyNotAvailable(t *testing.T) {
	withFakeDroidOnPath(t)
	t.Setenv("FACTORY_API_KEY", "fk-fake")
	a := DroidAdapter{OverrideHomeDir: t.TempDir()} // no settings.json
	d := a.Detect(context.Background())
	if d.Configured || d.Authenticated || d.Available() {
		t.Errorf("detect = %+v, want unavailable (key alone is not enough)", d)
	}
	if d.ReasonKind != core.ReasonNotConfigured {
		t.Errorf("reason_kind = %q, want configuration_missing", d.ReasonKind)
	}
}
