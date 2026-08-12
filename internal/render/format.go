// Package render turns normalized Status values into terminal output and
// stable JSON. It contains no provider-specific logic.
package render

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// FormatTokens renders a token count compactly: 842 → "842", 3.8M, 100.0M.
func FormatTokens(n float64) string {
	abs := math.Abs(n)
	switch {
	case abs >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", n/1_000_000_000)
	case abs >= 1_000_000:
		return fmt.Sprintf("%.1fM", n/1_000_000)
	case abs >= 1_000:
		return fmt.Sprintf("%.1fK", n/1_000)
	default:
		return fmt.Sprintf("%.0f", n)
	}
}

// FormatCost renders a currency value: $2.71, $0.06, $38.20.
func FormatCost(n float64) string {
	if n == 0 {
		return "$0.00"
	}
	abs := math.Abs(n)
	if abs < 1 {
		return fmt.Sprintf("$%.2f", n)
	}
	return fmt.Sprintf("$%.2f", n)
}

// FormatCount renders a whole-ish count (sessions etc). Flooring avoids
// inflating usage when a source reports fractional rolling counts (e.g. 0.7
// sessions used stays "0").
func FormatCount(n float64) string {
	if n <= 0 {
		return "0"
	}
	return fmt.Sprintf("%.0f", math.Floor(n))
}

// FormatDurationUntil renders the time until a reset as "3h 42m", "4d 2h".
func FormatDurationUntil(resetAt time.Time, now time.Time) string {
	d := resetAt.Sub(now)
	if d < 0 {
		d = 0
	}
	return FormatDuration(d)
}

// FormatDuration renders a duration compactly.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		if hours > 0 {
			return fmt.Sprintf("%dd %dh", days, hours)
		}
		return fmt.Sprintf("%dd", days)
	case hours > 0:
		if mins > 0 {
			return fmt.Sprintf("%dh %dm", hours, mins)
		}
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

// FormatPercentLeft renders "82% left".
func FormatPercentLeft(pctLeft float64) string {
	if pctLeft < 0 {
		return ""
	}
	return fmt.Sprintf("%.0f%% left", math.Round(pctLeft))
}

// ProgressBar renders a 10-cell bar: full=█, empty=░.
func ProgressBar(percentLeft float64, width int) string {
	if width <= 0 {
		width = 10
	}
	if percentLeft < 0 {
		percentLeft = 0
	}
	if percentLeft > 100 {
		percentLeft = 100
	}
	full := int(math.Round(percentLeft / 100 * float64(width)))
	if full < 0 {
		full = 0
	}
	if full > width {
		full = width
	}
	return strings.Repeat("█", full) + strings.Repeat("░", width-full)
}

// pctColor returns an ANSI color for a remaining percentage.
func pctColor(pctLeft float64) string {
	switch {
	case pctLeft < 0:
		return ""
	case pctLeft < 30:
		return "\x1b[31m" // red
	case pctLeft < 60:
		return "\x1b[33m" // yellow
	default:
		return "\x1b[32m" // green
	}
}
