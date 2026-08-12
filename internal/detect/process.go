//go:build !windows

package detect

import (
	"errors"
	"syscall"
)

// ProcessAlive reports whether a process with the given PID is currently
// running. Used by safety guards (e.g. skipping live API calls while an
// inspected tool's instance is active). statisfy never signals the process —
// this only probes existence.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	// EPERM means the process exists but belongs to another user.
	return err == nil || errors.Is(err, syscall.EPERM)
}
