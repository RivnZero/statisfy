//go:build !windows

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// initWakeup creates the wakeup pipe used to interrupt the reader goroutine.
// Closing a pipe's read end does NOT unblock a pending read on Unix, so the
// reader must wait in poll(2) instead and close() signals through this pipe.
func initWakeup(w *watchKeys) {
	pr, pw, err := os.Pipe()
	if err != nil {
		// Without a wakeup channel the reader could not be terminated
		// reliably; disable key input rather than risk a leak.
		w.done = true
		return
	}
	w.wakeR = pr
	w.wakeW = pw
}

// waitReadable blocks until stdin has input or reached EOF (returns true) or
// until close() signals the wakeup pipe (returns false). It never returns
// spuriously without one of the two fds firing.
func (w *watchKeys) waitReadable() bool {
	fds := []unix.PollFd{
		{Fd: int32(w.f.Fd()), Events: unix.POLLIN},
		{Fd: int32(w.wakeR.Fd()), Events: unix.POLLIN},
	}
	for {
		n, err := unix.Poll(fds, -1)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false
		}
		if n == 0 {
			continue // timeout is -1, so this is only a spurious wakeup
		}
		if fds[1].Revents&unix.POLLIN != 0 {
			// close() signalled: drain the byte so a later wakeup is fresh.
			var b [1]byte
			_, _ = w.wakeR.Read(b[:])
			return false
		}
		if fds[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			return true // stdin readable or closed → the following read returns immediately
		}
	}
}

// unblockRead interrupts a reader blocked in waitReadable. Writes never block
// here: the pipe buffer is empty unless a wakeup is already pending, and at
// most one byte is written.
func (w *watchKeys) unblockRead() {
	if w.wakeW != nil {
		var b [1]byte
		_, _ = w.wakeW.Write(b[:])
	}
}
