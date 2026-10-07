package tui3

import "github.com/Agent-Field/codeaf/internal/config"

// settingValueWord changes only the reading; persisted values remain unchanged.
func settingValueWord(row config.Setting) string {
	return config.ChatChoiceLabel(row.Key, row.Value(), row.Reading())
}
func settingChoiceWord(key, raw, fallback string) string {
	return config.ChatChoiceLabel(key, raw, fallback)
}
