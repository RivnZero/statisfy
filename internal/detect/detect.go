// Package detect centralizes filesystem/PATH checks so adapters stay thin and
// platform-specific path logic lives in one place.
package detect

import (
	"os"
	"os/exec"
	"path/filepath"
)

// InPath reports whether an executable named `name` exists on PATH.
func InPath(name string) bool {
	p, err := exec.LookPath(name)
	return err == nil && p != ""
}

// FileExists reports whether path exists and is a regular file.
func FileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// DirExists reports whether path exists and is a directory.
func DirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// HomeDir returns the current user's home directory.
func HomeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return dir
}

// UserCacheDir returns the OS user cache directory.
func UserCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = filepath.Join(HomeDir(), ".cache")
	}
	return dir
}
