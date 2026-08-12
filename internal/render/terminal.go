package render

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"statisfy/internal/core"
)

// Options controls terminal rendering.
type Options struct {
	// Color enables ANSI colors (default: true when TTY and NO_COLOR unset).
	Color bool
	// ShowErrors appends adapter errors under each tool (used by doctor-ish views).
	ShowErrors bool
}

// DetectColor decides whether ANSI color should be emitted.
func DetectColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

const (
	labelCol = 12 // label column width
	barWidth = 10
)

// Dashboard renders the default terminal view for the given results, including
// the section header. results are expected in registry order (already filtered
// by caller).
func Dashboard(w io.Writer, results []core.ToolResult, opts Options) {
	fmt.Fprintln(w, "AI CODING")
	fmt.Fprintln(w, strings.Repeat("─", 44))
	fmt.Fprintln(w)
	DashboardBody(w, results, opts)
}

// DashboardBody renders the tool blocks without the section header. Used by
// watch mode, which draws its own header with a refresh-age suffix.
func DashboardBody(w io.Writer, results []core.ToolResult, opts Options) {
	for _, r := range results {
		if r.Status == nil {
			if opts.ShowErrors {
				renderUnavailable(w, r)
				fmt.Fprintln(w)
			}
			continue
		}
		renderStatus(w, r.Status, opts)
		fmt.Fprintln(w)
	}
}

// renderStatus renders one available tool block.
func renderStatus(w io.Writer, st *core.Status, opts Options) {
	head := pad(st.Name, labelCol)
	bits := []string{}
	if st.Plan != "" {
		bits = append(bits, st.Plan)
	}
	if st.Multiplier > 0 {
		bits = append(bits, fmt.Sprintf("%dx", st.Multiplier))
	} else if st.Tier != "" {
		bits = append(bits, st.Tier)
	}
	if len(bits) > 0 {
		head = head + strings.Join(bits, " · ")
	}
	fmt.Fprintln(w, head)

	// Tool → Provider → Model relationship, when actually detected.
	if st.Provider != "" {
		fmt.Fprintf(w, "%s  %s\n", pad("Provider", labelCol), st.Provider)
	}
	if st.Model != "" {
		fmt.Fprintf(w, "%s  %s\n", pad("Model", labelCol), st.Model)
	}

	// Limits with progress bars.
	for _, l := range st.Limits {
		renderLimit(w, l, opts)
	}

	// Plain metrics.
	for _, m := range st.Metrics {
		renderMetric(w, m)
	}

	if opts.ShowErrors && len(st.Errors) > 0 {
		for _, e := range st.Errors {
			fmt.Fprintf(w, "  %s\n", dim(e, opts))
		}
	}
}

// renderLimit renders one quota line (bar + percentage, or sessions count).
func renderLimit(w io.Writer, l core.Limit, opts Options) {
	label := pad(shortLabel(l.Label, labelCol-2), labelCol)

	switch {
	case l.Unlimited:
		fmt.Fprintf(w, "%s  %s\n", label, "Unlimited")
		return
	case l.Kind == core.LimitSessions || l.Unit == "sessions":
		fmt.Fprintf(w, "%s  %s\n", label, formatSessions(l))
		return
	}

	pctLeft := l.PercentLeft()
	if pctLeft < 0 {
		fmt.Fprintf(w, "%s  %s\n", label, formatUsedTotal(l))
		return
	}
	bar := ProgressBar(pctLeft, barWidth)
	colored := bar
	if opts.Color {
		colored = pctColor(pctLeft) + bar + "\x1b[0m"
	}
	line := fmt.Sprintf("%s  %s  %s", label, colored, FormatPercentLeft(pctLeft))
	if l.ResetAt != nil {
		line += "  ·  reset " + FormatDurationUntil(*l.ResetAt, now())
	}
	fmt.Fprintln(w, line)
}

// renderMetric renders a non-quota metric line.
func renderMetric(w io.Writer, m core.Metric) {
	label := pad(shortLabel(m.Label, labelCol-2), labelCol)
	val := m.Value
	unit := m.Unit
	if unit == "cost" || unit == "$" {
		fmt.Fprintf(w, "%s  %s\n", label, FormatCost(val))
		return
	}
	if unit == "tokens" {
		fmt.Fprintf(w, "%s  %s tokens\n", label, FormatTokens(val))
		return
	}
	// unitless or "sessions" etc.
	if unit == "" {
		fmt.Fprintf(w, "%s  %s\n", label, FormatCount(val))
		return
	}
	fmt.Fprintf(w, "%s  %s %s\n", label, FormatCount(val), unit)
}

func formatSessions(l core.Limit) string {
	// Used may be unknown (e.g. a promo limit with no usage signal): show the
	// total only, never a fabricated number.
	if !l.Known() {
		if l.Total > 0 {
			return fmt.Sprintf("%s sessions", FormatCount(l.Total))
		}
		return "sessions"
	}
	used := FormatCount(l.Used)
	if l.Total > 0 {
		return fmt.Sprintf("%s / %s sessions", used, FormatCount(l.Total))
	}
	return fmt.Sprintf("%s sessions", used)
}

func formatUsedTotal(l core.Limit) string {
	if !l.Known() {
		if l.Total > 0 {
			return fmt.Sprintf("%s %s", FormatCount(l.Total), l.Unit)
		}
		return ""
	}
	if l.Total > 0 {
		return fmt.Sprintf("%s / %s %s", FormatCount(l.Used), FormatCount(l.Total), l.Unit)
	}
	return fmt.Sprintf("%s %s", FormatCount(l.Used), l.Unit)
}

// renderUnavailable shows an unavailable integration when explicitly requested.
func renderUnavailable(w io.Writer, r core.ToolResult) {
	reason := r.Detection.Reason
	if reason == "" {
		reason = "not available"
	}
	status := "unavailable"
	if r.FetchErr != nil {
		status = "error"
		reason = r.FetchErr.Error()
	}
	fmt.Fprintf(w, "%s  %s  (%s)\n", pad(r.Adapter.Name(), labelCol), status, reason)
}

// header helpers

func pad(s string, width int) string {
	if len(s) >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-len(s))
}

func shortLabel(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func dim(s string, opts Options) string {
	if !opts.Color {
		return s
	}
	return "\x1b[2m" + s + "\x1b[0m"
}

// now is injectable for tests.
var now = time.Now
