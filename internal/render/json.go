package render

import (
	"encoding/json"
	"io"
	"time"

	"statisfy/internal/core"
) // JSONDoc is the stable machine-readable document emitted by `--json`.
// Field names and layout are part of the public contract; change carefully.
type JSONDoc struct {
	Version     int        `json:"version"`
	Generated   time.Time  `json:"generated_at"`
	Tools       []JSONTool `json:"tools"`
	Unavailable []JSONTool `json:"unavailable,omitempty"`
}

// NewJSONDoc returns a doc with empty (non-nil) slices so `tools` is always
// `[]` in the output, never `null`.
func NewJSONDoc() JSONDoc {
	return JSONDoc{Version: 1, Generated: time.Now(), Tools: []JSONTool{}}
} // JSONTool is the normalized status of a single tool.
type JSONTool struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Available     bool                   `json:"available"`
	Installed     bool                   `json:"installed"`
	Authenticated bool                   `json:"authenticated"`
	Configured    bool                   `json:"configured"`
	Account       string                 `json:"account,omitempty"`
	Plan          string                 `json:"plan,omitempty"`
	Tier          string                 `json:"tier,omitempty"`
	Multiplier    int                    `json:"multiplier,omitempty"`
	Provider      string                 `json:"provider,omitempty"`
	Model         string                 `json:"model,omitempty"`
	Source        core.Source            `json:"source,omitempty"`
	Stability     core.Stability         `json:"stability,omitempty"`
	LastChecked   time.Time              `json:"last_checked,omitempty"`
	Limits        []JSONLimit            `json:"limits,omitempty"`
	Metrics       []core.Metric          `json:"metrics,omitempty"`
	Errors        []string               `json:"errors,omitempty"`
	Reason        string                 `json:"reason,omitempty"`
	ReasonKind    core.UnavailableReason `json:"reason_kind,omitempty"`
}

// JSONLimit is the JSON-safe view of a Limit: unknown values (-1, 0 totals
// that were never reported) are omitted rather than rendered as sentinels.
type JSONLimit struct {
	Kind        core.LimitKind `json:"kind"`
	Label       string         `json:"label,omitempty"`
	Used        *float64       `json:"used,omitempty"`
	Total       *float64       `json:"total,omitempty"`
	PercentUsed *float64       `json:"percent_used,omitempty"`
	Unit        string         `json:"unit,omitempty"`
	Unlimited   bool           `json:"unlimited,omitempty"`
	ResetAt     *time.Time     `json:"reset_at,omitempty"`
	Source      core.Source    `json:"source,omitempty"`
}

// jsonLimit converts a Limit to its JSON-safe form.
func jsonLimit(l core.Limit) JSONLimit {
	out := JSONLimit{
		Kind:      l.Kind,
		Label:     l.Label,
		Unit:      l.Unit,
		Unlimited: l.Unlimited,
		ResetAt:   l.ResetAt,
		Source:    l.Source,
	}
	if l.Used >= 0 {
		v := l.Used
		out.Used = &v
	}
	if l.Total > 0 {
		v := l.Total
		out.Total = &v
	}
	if l.PercentUsed >= 0 {
		v := l.PercentUsed
		out.PercentUsed = &v
	}
	return out
}

// JSONDocFromResults builds the JSON document from collected results.
//
// A result lands in `tools` only when it produced a Status. Everything else
// appears under `unavailable` when requested — including tools whose
// detection passed but whose fetch failed (they must never silently
// disappear from --all --json).
func JSONDocFromResults(results []core.ToolResult, includeUnavailable bool) JSONDoc {
	doc := NewJSONDoc()
	for _, r := range results {
		if r.Status != nil && r.Detection.Available() {
			doc.Tools = append(doc.Tools, toolFromStatus(r))
			continue
		}
		if !includeUnavailable {
			continue
		}
		t := toolFromDetection(r)
		if r.FetchErr != nil {
			// A runtime fetch failure is not a detection class, so no
			// reason_kind is guessed; the (sanitized) error text is exposed
			// as the reason.
			t.Reason = sanitizeErr(r.FetchErr.Error())
			t.Errors = []string{t.Reason}
		}
		doc.Unavailable = append(doc.Unavailable, t)
	}
	return doc
}

func toolFromStatus(r core.ToolResult) JSONTool {
	st := r.Status
	t := JSONTool{
		ID:            st.Tool,
		Name:          st.Name,
		Available:     true,
		Installed:     true,
		Authenticated: r.Detection.Authenticated,
		Configured:    r.Detection.Configured,
		Account:       st.Account,
		Plan:          st.Plan,
		Tier:          st.Tier,
		Multiplier:    st.Multiplier,
		Provider:      st.Provider,
		Model:         st.Model,
		Source:        st.Source,
		Stability:     st.Stability,
		LastChecked:   st.LastChecked,
		Metrics:       st.Metrics,
		Errors:        st.Errors,
	}
	for _, l := range st.Limits {
		t.Limits = append(t.Limits, jsonLimit(l))
	}
	return t
}

func toolFromDetection(r core.ToolResult) JSONTool {
	return JSONTool{
		ID:            r.Adapter.ID(),
		Name:          r.Adapter.Name(),
		Available:     false,
		Installed:     r.Detection.Installed,
		Authenticated: r.Detection.Authenticated,
		Configured:    r.Detection.Configured,
		Reason:        r.Detection.Reason,
		ReasonKind:    r.Detection.ReasonKind,
	}
}

// WriteJSON writes the document with an indented layout.
func WriteJSON(w io.Writer, doc JSONDoc) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
