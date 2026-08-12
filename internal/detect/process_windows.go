//go:build windows

package detect

import "golang.org/x/sys/windows"

// ProcessAlive reports whether a process with the given PID is currently
// running. Used by safety guards (e.g. skipping live API calls while an
// inspected tool's instance is active). statisfy never signals the process —
// this only probes existence with the minimum query rights.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	windows.CloseHandle(handle)
	return true
}
