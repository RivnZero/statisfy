package render

import (
	"fmt"
	"io"
	"strings"

	"statisfy/internal/core"
)

// Doctor renders the diagnostic table for `statisfy doctor`. Unlike the normal
// dashboard this includes every integration and explains why each is or is not
// available. Diagnostic output only — never used in the default view.
func Doctor(w io.Writer, results []core.ToolResult, opts Options) {
	fmt.Fprintln(w, "STATISFY DOCTOR")
	fmt.Fprintln(w, strings.Repeat("─", 44))
	fmt.Fprintln(w)

	// Only show state columns that are meaningful for at least one adapter
	// (e.g. OpenCode is configured but never authenticated).
	showAuth, showConfig := false, false
	for _, r := range results {
		if r.Detection.Authenticated {
			showAuth = true
		}
		if r.Detection.Configured {
			showConfig = true
		}
	}

	ok := func(b bool) string {
		if b {
			return "✓"
		}
		return "✗"
	}

	for _, r := range results {
		d := r.Detection
		line := fmt.Sprintf("%-12s  %s installed", r.Adapter.Name(), ok(d.Installed))
		if showAuth {
			line += fmt.Sprintf("   %s authenticated", ok(d.Authenticated))
		}
		if showConfig {
			line += fmt.Sprintf("   %s configured", ok(d.Configured))
		}
		if opts.Color {
			if d.Available() {
				line = "\x1b[32m" + line + "\x1b[0m"
			} else {
				line = "\x1b[31m" + line + "\x1b[0m"
			}
		}
		fmt.Fprintln(w, line)

		if d.Reason != "" {
			tag := ""
			if k := reasonLabel(d.ReasonKind); k != "" {
				tag = k + ": "
			}
			fmt.Fprintf(w, "      %s\n", dim(tag+d.Reason, opts))
		}
		if r.FetchErr != nil {
			fmt.Fprintf(w, "      fetch error: %s\n", dim(sanitizeErr(r.FetchErr.Error()), opts))
		}
		if r.Status != nil {
			if r.Status.Plan != "" {
				fmt.Fprintf(w, "      plan: %s\n", dim(r.Status.Plan, opts))
			}
			if len(r.Status.Errors) > 0 {
				for _, e := range r.Status.Errors {
					fmt.Fprintf(w, "      note: %s\n", dim(sanitizeErr(e), opts))
				}
			}
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run `statisfy --all` to see unavailable integrations in the dashboard.")
}

// reasonLabel maps a structured unavailability kind to a short human label.
func reasonLabel(k core.UnavailableReason) string {
	switch k {
	case core.ReasonNotInstalled:
		return "binary missing"
	case core.ReasonNotConfigured:
		return "configuration missing"
	case core.ReasonNotAuthenticated:
		return "authentication missing"
	case core.ReasonUnsupportedVersion:
		return "unsupported version"
	case core.ReasonInterfaceUnavailable:
		return "interface unavailable"
	case core.ReasonParseFailure:
		return "parse failure"
	case core.ReasonNetworkTimeout:
		return "network timeout"
	case core.ReasonPermission:
		return "permission failure"
	case core.ReasonLocalState:
		return "local state unavailable"
	default:
		return ""
	}
}

// sanitizeErr keeps diagnostic messages safe: collapse newlines.
func sanitizeErr(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
