package render

import (
	"fmt"
	"io"
	"strings"

	"statisfy/internal/core"
)

// SourceLabel maps a Source to a human description of where the value came from.
func SourceLabel(s core.Source) string {
	switch s {
	case core.SourceOfficialAPI:
		return "official API"
	case core.SourceCLIOutput:
		return "CLI output"
	case core.SourceLocalState:
		return "local state"
	case core.SourceLocalDatabase:
		return "local database"
	case core.SourceConfig:
		return "config"
	case core.SourceInternalAPI:
		return "internal API"
	case core.SourceDerived:
		return "derived"
	default:
		return "unknown"
	}
}

// Sources renders `statisfy sources`: per-tool provenance showing where each
// value came from and its stability. This is the transparency surface — the
// normal dashboard stays clean.
func Sources(w io.Writer, results []core.ToolResult, opts Options) {
	fmt.Fprintln(w, "STATISFY SOURCES")
	fmt.Fprintln(w, strings.Repeat("─", 44))
	fmt.Fprintln(w)

	for _, r := range results {
		fmt.Fprintf(w, "%s\n", r.Adapter.Name())
		if r.Status == nil {
			fmt.Fprintf(w, "  %s\n", dim("unavailable: "+r.Detection.Reason, opts))
			fmt.Fprintln(w)
			continue
		}
		st := r.Status
		if st.Plan != "" {
			fmt.Fprintf(w, "  %-12s %-18s %s\n", "Plan", SourceLabel(st.Source), stabilityTag(st.Stability))
		}
		if st.Tier != "" {
			fmt.Fprintf(w, "  %-12s %-18s %s\n", "Tier", SourceLabel(st.Source), stabilityTag(st.Stability))
		}
		for _, l := range st.Limits {
			src := l.Source
			if src == "" {
				src = st.Source
			}
			fmt.Fprintf(w, "  %-12s %-18s %s\n", l.Label, SourceLabel(src), stabilityTag(st.Stability))
		}
		for _, m := range st.Metrics {
			src := m.Source
			if src == "" {
				src = st.Source
			}
			fmt.Fprintf(w, "  %-12s %-18s %s\n", m.Label, SourceLabel(src), stabilityTag(st.Stability))
		}
		if len(st.Limits) == 0 && len(st.Metrics) == 0 && st.Plan == "" && st.Tier == "" {
			fmt.Fprintf(w, "  %s\n", dim("no metrics detected", opts))
		}
		fmt.Fprintln(w)
	}
}

func stabilityTag(s core.Stability) string {
	switch s {
	case core.StabilityStable:
		return "stability: stable"
	case core.StabilityDocumented:
		return "stability: documented"
	case core.StabilityLocal:
		return "stability: local"
	case core.StabilityInternal:
		return "stability: internal"
	case core.StabilityExperimental:
		return "stability: experimental"
	default:
		return ""
	}
}
