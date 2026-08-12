package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"statisfy/internal/config"
	"statisfy/internal/core"
	"statisfy/internal/render"
)

// runWatch implements `statisfy watch`: a live refreshing dashboard. It stays
// intentionally simple — no full-screen TUI — and reuses the same registry,
// fetching, cache, and rendering as the one-shot CLI. q quits, r refreshes,
// and the dashboard re-renders on a fixed interval.
//
// stdin/stdout/cache are injected so the lifecycle is unit-testable.
//
// Lifecycle invariant: after runWatch returns, every Statisfy-owned goroutine
// has terminated — none depends on process destruction for cleanup. Two
// goroutines exist: the periodic ticker (exits via stop, closed by defer) and
// the single key reader owned by watchKeys (see watch_keys.go) — its pending
// read is unblocked by keys.close(), which closes the stdin file, on every
// exit path. Terminal raw mode is restored by that same deferred close().
func runWatch(ctx context.Context, reg *core.Registry, stdin io.Reader, stdout io.Writer, cfg *config.Config, c core.Cacher, initialRefresh bool) int {
	interval := cfg.WatchInterval
	if interval <= 0 {
		interval = config.DefaultWatch
	}
	color := render.DetectColor() && term.IsTerminal(int(os.Stdout.Fd()))
	ropts := render.Options{Color: color}

	keys := newWatchKeys(stdin)
	defer keys.close() // stop the reader goroutine and restore the terminal on every exit path (q, ctx, panic)

	// Periodic refresh: single-sender channel; the ticker goroutine exits via
	// stop, which is closed by defer so it always terminates.
	stop := make(chan struct{})
	defer close(stop)
	tickCh := make(chan struct{}, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				select {
				case tickCh <- struct{}{}:
				default:
				}
			case <-stop:
				return
			}
		}
	}()

	refresh := initialRefresh
	last := time.Time{}
	for {
		clearScreen(stdout, ropts)
		fmt.Fprintf(stdout, "AI CODING%s\n", refreshAge(last))
		fmt.Fprintln(stdout, strings.Repeat("─", 44))
		fmt.Fprintln(stdout)

		fetchCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		detections := core.DetectAll(fetchCtx, reg)
		results := core.Collect(fetchCtx, reg, detections, core.Options{
			Refresh:         refresh,
			PerFetchTimeout: cfg.Timeout,
			Cache:           c,
		})
		cancel()

		avail := core.FilterAvailable(results)
		if len(avail) == 0 {
			fmt.Fprintln(stdout, "No supported AI coding tools were detected.")
			fmt.Fprintln(stdout)
		} else {
			render.DashboardBody(stdout, avail, ropts)
		}
		last = time.Now()
		refresh = false

		fmt.Fprintln(stdout)
		if color {
			fmt.Fprintln(stdout, "\x1b[2mPress q to quit · r to refresh\x1b[0m")
		} else {
			fmt.Fprintln(stdout, "Press q to quit · r to refresh")
		}

		for {
			// Stop conditions are checked every iteration so a quiet terminal
			// never delays cancellation or the periodic refresh.
			select {
			case <-ctx.Done():
				return 0
			case <-tickCh:
				refresh = true
				goto refresh
			default:
			}

			if keys == nil {
				// stdin is closed or not key-readable: run on the ticker alone.
				select {
				case <-ctx.Done():
					return 0
				case <-tickCh:
					refresh = true
					goto refresh
				}
			}

			k, ok := keys.read(watchPollInterval)
			if !ok {
				if keys.done {
					// stdin reached EOF: stop polling (a closed fd would
					// otherwise report readable forever and spin).
					keys = nil
				}
				continue
			}
			switch k {
			case 'q', 'Q', 0x03: // q or Ctrl-C
				return 0
			case 'r', 'R':
				refresh = true
				goto refresh
			}
		}
	refresh:
	}
}

func clearScreen(w io.Writer, opts render.Options) {
	if opts.Color {
		fmt.Fprint(w, "\x1b[2J\x1b[H") // clear + home (only when color/TTY enabled)
	}
}

func refreshAge(last time.Time) string {
	if last.IsZero() {
		return ""
	}
	return fmt.Sprintf("    refreshed %s ago", render.FormatDuration(time.Since(last)))
}
