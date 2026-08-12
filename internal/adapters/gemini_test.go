package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGeminiFetchActiveAccount(t *testing.T) {
	home := t.TempDir()
	writeGeminiAccounts(t, home, "gemini_accounts.json")
	a := GeminiAdapter{OverrideHomeDir: home}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if st.Account != "user@example.com" {
		t.Errorf("account = %q, want user@example.com", st.Account)
	}
	if st.Plan != "" {
		t.Errorf("plan = %q, want empty (not detectable)", st.Plan)
	}
	if st.Source != "local-state" {
		t.Errorf("source = %q", st.Source)
	}
}

func TestGeminiFetchNoActiveAccount(t *testing.T) {
	home := t.TempDir()
	// active:null — the real shape on machines without a signed-in account.
	if err := os.MkdirAll(filepath.Join(home, ".gemini"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gemini", "google_accounts.json"),
		[]byte(`{"active": null, "old": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a := GeminiAdapter{OverrideHomeDir: home}
	st, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if st.Account != "" {
		t.Errorf("account = %q, want empty when no active account", st.Account)
	}
}

func TestGeminiDetectRequiresAuthState(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".gemini"), 0o700)
	os.WriteFile(filepath.Join(home, ".gemini", "google_accounts.json"),
		[]byte(`{"active": null, "old": []}`), 0o600)
	a := GeminiAdapter{OverrideHomeDir: home}
	d := a.Detect(context.Background())
	if d.Available() {
		t.Errorf("available = true with no active account, want false")
	}
	if d.Reason == "" {
		t.Error("reason empty")
	}
}

func writeGeminiAccounts(t *testing.T, home, fixture string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("fixtures", fixture))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dir := filepath.Join(home, ".gemini")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "google_accounts.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
