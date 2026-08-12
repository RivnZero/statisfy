package main

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"statisfy/internal/cache"
	"statisfy/internal/config"
	"statisfy/internal/core"
)

// stubWatchAdapter is a deterministic adapter used to exercise watch mode
// without touching any real tool.
type stubWatchAdapter struct{}

func (stubWatchAdapter) ID() string   { return "stub" }
func (stubWatchAdapter) Name() string { return "Stub" }
func (stubWatchAdapter) Detect(ctx context.Context) core.Detection {
	return core.Detection{Installed: true, Authenticated: true, Reason: "ok"}
}
func (stubWatchAdapter) Fetch(ctx context.Context) (*core.Status, error) {
	return &core.Status{Tool: "stub", Name: "Stub", Plan: "Test"}, nil
}

func newWatchFixture(t *testing.T) (context.Context, context.CancelFunc, *core.Registry, *config.Config, core.Cacher) {
	t.Helper()
	cfg := config.Default()
	cfg.WatchInterval = 20 * time.Millisecond
	c := cache.New(t.TempDir(), time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	return ctx, cancel, core.NewRegistry(stubWatchAdapter{}), cfg, c
}

// newWatchPipe returns a real OS pipe (read end is an *os.File, so watch uses
// its production poll-based key path) plus a cleanup that closes both ends.
func newWatchPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { pr.Close(); pw.Close() })
	return pr, pw
}

// waitForRender polls until the output contains the given substring.
func waitForRender(t *testing.T, out *bytes.Buffer, substr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if strings.Contains(out.String(), substr) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %q in output:\n%s", substr, out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWatchQuitsOnQ(t *testing.T) {
	ctx, cancel, reg, cfg, c := newWatchFixture(t)
	defer cancel()

	pr, pw := newWatchPipe(t)
	var out bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- runWatch(ctx, reg, pr, &out, cfg, c, true) }()

	waitForRender(t, &out, "AI CODING")

	if _, err := pw.Write([]byte("q")); err != nil {
		t.Fatalf("write q: %v", err)
	}
	pw.Close()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not quit on q")
	}

	if !strings.Contains(out.String(), "Press q to quit") {
		t.Errorf("missing footer:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Stub") {
		t.Errorf("dashboard missing adapter output:\n%s", out.String())
	}
}

func TestWatchRefreshesPeriodically(t *testing.T) {
	ctx, cancel, reg, cfg, c := newWatchFixture(t)
	defer cancel()

	pr, pw := newWatchPipe(t)
	var out bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- runWatch(ctx, reg, pr, &out, cfg, c, true) }()

	// A 20ms tick interval should produce several dashboard renders quickly.
	// The 10s budget absorbs slow first-run compiles/heavy CI load; a healthy
	// run finishes in well under a second.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if n := bytes.Count(out.Bytes(), []byte("AI CODING")); n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("periodic refresh did not re-render (renders=%d):\n%s",
				bytes.Count(out.Bytes(), []byte("AI CODING")), out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A manual 'r' also triggers an immediate render; 'q' then exits cleanly.
	pw.Write([]byte("rq"))
	pw.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not quit on q after periodic refresh")
	}
}

func TestWatchEOFStillRunsAndCancels(t *testing.T) {
	ctx, cancel, reg, cfg, c := newWatchFixture(t)

	pr, pw := newWatchPipe(t)
	pw.Close() // immediate EOF: stdin is closed
	var out bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- runWatch(ctx, reg, pr, &out, cfg, c, true) }()

	// With EOF stdin the dashboard still renders (periodic ticker).
	waitForRender(t, &out, "AI CODING")

	// Context cancellation must terminate watch cleanly.
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not exit on context cancellation")
	}

	if strings.Contains(out.String(), "panic") {
		t.Errorf("watch panicked:\n%s", out.String())
	}
}

// TestWatchLeavesNoGoroutines proves the lifecycle invariant: after runWatch
// returns, every Statisfy-owned goroutine (the periodic ticker and the key
// reader) terminates — none may depend on process destruction for cleanup.
// The pipe's write end is deliberately left OPEN and empty after 'q' so the
// reader goroutine is blocked in a read when close() runs: only the file
// close in keys.close() can unblock it.
func TestWatchLeavesNoGoroutines(t *testing.T) {
	ctx, cancel, reg, cfg, c := newWatchFixture(t)
	defer cancel()

	pr, pw := newWatchPipe(t)
	before := runtime.NumGoroutine()
	var out bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- runWatch(ctx, reg, pr, &out, cfg, c, true) }()

	waitForRender(t, &out, "AI CODING")
	if _, err := pw.Write([]byte("q")); err != nil {
		t.Fatalf("write q: %v", err)
	}
	// Intentionally NOT closing pw: the reader must be unblocked by close().

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not quit")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if n := runtime.NumGoroutine(); n <= before {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutines did not settle after exit: %d > %d baseline",
				runtime.NumGoroutine(), before)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestWatchKeysKeyBeforeEOFNotLost proves EOF never outranks a pending key:
// when a key is written and the write end closes immediately after (the exact
// "rq" + close pattern), read() must return the key, not declare EOF. This
// guards against a select race where both the key channel and the EOF channel
// are ready simultaneously.
func TestWatchKeysKeyBeforeEOFNotLost(t *testing.T) {
	pr, pw := newWatchPipe(t)
	w := newWatchKeys(pr)
	defer pw.Close()

	if _, err := pw.Write([]byte("q")); err != nil {
		t.Fatalf("write: %v", err)
	}
	pw.Close() // data and EOF are now both pending

	k, ok := w.read(time.Second)
	if !ok || k != 'q' {
		t.Fatalf("read = %q, %v; want 'q', true (a pending key must outrank EOF)", k, ok)
	}

	// After the key is drained, EOF is observable and read stops polling.
	if k, ok := w.read(50 * time.Millisecond); ok || k != 0 {
		t.Fatalf("read after drain = %q, %v; want EOF", k, ok)
	}
	if !w.done {
		t.Fatal("done not set after EOF")
	}
}

// TestWatchKeysReaderTerminatesOnClose proves the reader-goroutine lifecycle
// at the watchKeys level: a reader blocked on an empty pipe (write end open,
// no data) terminates the moment close() runs.
func TestWatchKeysReaderTerminatesOnClose(t *testing.T) {
	pr, pw := newWatchPipe(t)
	defer pw.Close()

	w := newWatchKeys(pr)

	// A key arrives and is readable.
	if _, err := pw.Write([]byte("k")); err != nil {
		t.Fatalf("write: %v", err)
	}
	k, ok := w.read(time.Second)
	if !ok || k != 'k' {
		t.Fatalf("read = %q, %v; want 'k', true", k, ok)
	}

	// The reader is now blocked in Read on the empty pipe (write end still
	// open). close() must terminate it without relying on process exit.
	before := runtime.NumGoroutine()
	w.close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if n := runtime.NumGoroutine(); n <= before {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reader goroutine did not terminate after close (leak)")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Subsequent reads must not panic or spin.
	if k, ok := w.read(10 * time.Millisecond); ok || k != 0 {
		t.Fatalf("read after close = %q, %v; want none", k, ok)
	}
}
