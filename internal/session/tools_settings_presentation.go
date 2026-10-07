package session

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/config"
	"strings"
)

func settingLabel(row config.Setting) string {
	if label := row.ChatPresentation().Label; label != "" {
		return label
	}
	return row.Label
}
func settingHint(row config.Setting) string {
	if hint := row.ChatPresentation().Description; hint != "" {
		return hint
	}
	return row.Hint
}

// Expose both vocabularies: the displayed option is never silently substituted
// for the raw argument accepted by the long-lived tool API.
func settingDisplayValue(row config.Setting, raw string) string {
	label := config.ChatChoiceLabel(row.Key, raw, raw)
	if label != raw {
		return fmt.Sprintf("%s (raw value: %s)", settingReading(label), settingReading(raw))
	}
	return settingReading(raw)
}
func settingOptions(row config.Setting) string {
	values := row.Choices
	if row.Kind == config.SettingBool {
		values = []string{"on", "off"}
	}
	if len(values) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("Write the raw value, not the display label. Options:\n")
	for _, raw := range values {
		fmt.Fprintf(&out, "  %q => %s: %s\n", raw, settingLabel(row), config.ChatChoiceLabel(row.Key, raw, raw))
	}
	return out.String()
}

// toolSettingActivation describes this writer, which saves the profile without
// calling the TUI's live transport seams.
func toolSettingActivation(row config.Setting) string {
	switch row.Key {
	case config.KeyRouting, config.KeyLaneGuard, config.LaneSettingKey(config.LaneSlotTalk), config.LaneBorrowKey(config.LaneSlotTalk):
		return "Restart the CLI to ensure already-open chats use this change."
	default:
		return row.ChatPresentation().Activation
	}
}
