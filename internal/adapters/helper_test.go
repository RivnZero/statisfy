package adapters

import (
	"os"
	"path/filepath"
	"testing"
)

// withFakeBinaryOnPath puts a fake executable named `name` on PATH (both bare
// and .exe so the test is cross-platform) and returns the temp dir. This keeps
// Detect tests hermetic: they must pass on CI runners that do not have the
// real binary installed.
func withFakeBinaryOnPath(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{name, name + ".exe"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}
