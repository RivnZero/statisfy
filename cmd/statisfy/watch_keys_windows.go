//go:build windows

package main

// initWakeup is a no-op on Windows: closing a file interrupts a pending read
// there immediately (verified empirically), so no wakeup pipe is needed.
func initWakeup(w *watchKeys) {}

// waitReadable always reports ready: the reader goroutine blocks directly in
// Read, which close() unblocks via unblockRead's file close.
func (w *watchKeys) waitReadable() bool { return true }

// unblockRead interrupts a reader blocked in Read by closing the file.
func (w *watchKeys) unblockRead() {
	if w.f != nil {
		_ = w.f.Close()
	}
}
