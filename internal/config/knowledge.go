package config

import "strings"

// DefaultKnowledgeModel keeps background learning independent of chat and task
// tiers. A chosen model is used exactly; compilation never substitutes one.
const DefaultKnowledgeModel = "deepseek/deepseek-v4.1-flash"

// KnowledgeModelAt reads the profile for each compilation, so a saved choice
// affects the next batch without restarting ongoing conversations.
func KnowledgeModelAt(profileDir string) string {
	if value, ok := persistedString(profileDir, KeyKnowledgeModel); ok {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return DefaultKnowledgeModel
}
