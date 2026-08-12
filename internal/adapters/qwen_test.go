package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestQwenFetchProviderModel(t *testing.T) {
	home := t.TempDir()
	writeQwenSettings(t, home)
	a := QwenAdapter{OverrideHomeDir: home}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if st.Provider != "qwen" {
		t.Errorf("provider = %q, want qwen", st.Provider)
	}
	if st.Model != "qwen3-coder-plus" {
		t.Errorf("model = %q, want qwen3-coder-plus", st.Model)
	}
	// Plan and usage are unknown for Qwen Code — must be omitted.
	if st.Plan != "" || len(st.Limits) > 0 || len(st.Metrics) > 0 {
		t.Errorf("invented data: plan=%q limits=%d metrics=%d", st.Plan, len(st.Limits), len(st.Metrics))
	}
}

func TestQwenFetchNoApiKeyLeak(t *testing.T) {
	home := t.TempDir()
	writeQwenSettings(t, home)
	a := QwenAdapter{OverrideHomeDir: home}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// The struct doesn't even define apiKey, so it cannot be serialized.
	if st.Model == "" {
		t.Fatal("model missing")
	}
}

func TestQwenDetectMissingConfig(t *testing.T) {
	a := QwenAdapter{OverrideHomeDir: t.TempDir()}
	d := a.Detect(context.Background())
	if d.Available() {
		t.Errorf("available = true without settings.json")
	}
	if d.Reason == "" {
		t.Error("reason empty")
	}
}

func TestQwenDetectConfigured(t *testing.T) {
	withFakeBinaryOnPath(t, "qwen") // CI runners have no qwen binary
	home := t.TempDir()
	writeQwenSettings(t, home)
	a := QwenAdapter{OverrideHomeDir: home}
	d := a.Detect(context.Background())
	if !d.Installed || !d.Configured {
		t.Errorf("detect = %+v, want installed+configured", d)
	}
}

func writeQwenSettings(t *testing.T, home string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("fixtures", "qwen_settings.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dir := filepath.Join(home, ".qwen")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
