// Package adapters holds the compiled-in integrations. Adding a new tool
// means implementing core.Adapter here and listing it in Defaults.
package adapters

import "statisfy/internal/core"

// Defaults returns the registered adapters in display order.
func Defaults() []core.Adapter {
	return []core.Adapter{
		CodexAdapter{},
		ClaudeAdapter{},
		GeminiAdapter{},
		CopilotAdapter{},
		QwenAdapter{},
		OpenRouterAdapter{},
		ClineAdapter{},
		AiderAdapter{},
		DroidAdapter{},
		OpenCodeAdapter{},
		FreebuffAdapter{},
	}
}

// IDs returns the list of adapter IDs for help text.
func IDs() []string {
	ids := make([]string, 0, len(Defaults()))
	for _, a := range Defaults() {
		ids = append(ids, a.ID())
	}
	return ids
}
