package main

import (
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

// watchPollInterval is how often the watch loop re-checks for input while no
// key is pending. Bounded, so a quiet terminal never delays cancellation or
// the periodic refresh.
const watchPollInterval = 100 * time.Millisecond

// watchKeys reads single keypresses from stdin through exactly one reader
// goroutine.
//
// Lifecycle invariant: after watchKeys.close() (or natural EOF) the reader
// goroutine has terminated — it never depends on process destruction for
// cleanup. close() closes the stop channel and then calls the platform's
// unblockRead(), which is the one mechanism guaranteed to interrupt the
// reader's wait:
//
//   - Unix: closing a pipe's read end does NOT interrupt a pending read, so
//     the reader blocks in poll(2) on {stdin, wakeup-pipe} and close() writes
//     a byte to the wakeup pipe.
//   - Windows: closing the file interrupts a pending read immediately
//     (verified empirically), so close() just closes it.
//
// Raw mode is restored by close() on every exit path.
type watchKeys struct {
	r    io.Reader
	f    *os.File // non-nil when r is an *os.File
	old  *term.State
	raw  bool
	done bool // input unavailable (EOF, closed, or not an *os.File); main loop only

	ch   chan rune
	eof  chan struct{}
	stop chan struct{}
	once sync.Once

	wakeR *os.File // unix: wakeup pipe read end (nil on windows)
	wakeW *os.File // unix: wakeup pipe write end (nil on windows)
}

// newWatchKeys wraps stdin for key reading, starting the single reader
// goroutine. Raw mode is entered only on a real TTY (best effort; failure
// disables key input rather than risking the terminal).
func newWatchKeys(r io.Reader) *watchKeys {
	w := &watchKeys{
		r:    r,
		ch:   make(chan rune, 1),
		eof:  make(chan struct{}),
		stop: make(chan struct{}),
	}
	f, ok := r.(*os.File)
	if !ok {
		// Cannot reliably terminate a read on a non-*os.File reader; disable
		// key input so watch runs on the periodic ticker alone.
		w.done = true
		return w
	}
	w.f = f
	if term.IsTerminal(int(f.Fd())) {
		if old, err := term.MakeRaw(int(f.Fd())); err == nil {
			w.old = old
			w.raw = true
		}
	}
	initWakeup(w)
	if !w.done {
		go w.readLoop()
	}
	return w
}

// readLoop is the single key reader. It exits on EOF (closing eof), on stop
// (close()), or when close() interrupts the current wait.
func (w *watchKeys) readLoop() {
	var b [1]byte
	for {
		if !w.waitReadable() {
			return // stop requested (close() ran)
		}
		n, err := w.r.Read(b[:])
		if err != nil || n == 0 {
			close(w.eof)
			return
		}
		select {
		case w.ch <- rune(b[0]):
		case <-w.stop:
			return
		}
	}
}

// close terminates the reader goroutine and restores the terminal. Idempotent
// and safe on every exit path (q, context cancellation, panic). After close()
// returns, the reader goroutine terminates promptly — no goroutine outlives
// it by relying on process exit.
func (w *watchKeys) close() {
	w.once.Do(func() {
		close(w.stop)
		w.unblockRead()
		w.restore()
	})
}

// restore returns the terminal to its pre-raw state. Idempotent and safe on
// every exit path, including when raw mode was never entered.
func (w *watchKeys) restore() {
	if w != nil && w.raw && w.f != nil {
		_ = term.Restore(int(w.f.Fd()), w.old)
		w.raw = false
	}
}

// read waits up to timeout for a single key byte. ok=false on timeout, EOF, or
// unavailable input; done is set once EOF has been observed so the caller can
// stop polling.
//
// EOF never outranks a pending key: a final key written just before the write
// end closed (e.g. "rq" then close) must not be lost. EOF is therefore kept
// OUT of the blocking select — read waits only on the key channel, and checks
// eof non-blockingly after a timeout. If eof closed just as a key arrived,
// both would be "ready" and a select would pick uniformly at random; checking
// eof only on timeout makes key delivery unconditional. The only cost is that
// EOF is observed up to one poll interval late, which merely delays the
// (optional) stop-polling transition.
func (w *watchKeys) read(timeout time.Duration) (rune, bool) {
	if w == nil || w.done {
		return 0, false
	}
	select {
	case k, ok := <-w.ch:
		if !ok {
			w.done = true
			return 0, false
		}
		return k, true
	case <-time.After(timeout):
	}
	select {
	case <-w.eof:
		w.done = true
		return 0, false
	default:
	}
	return 0, false
}
