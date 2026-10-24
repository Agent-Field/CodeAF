package config

import "strings"

// KeyChatModelRotation holds an ordered preference, separate from automatic fallbacks.
const KeyChatModelRotation = "models.quick_switch"

// ChatModelRotationAt reads the saved order without introducing catalog defaults.
func ChatModelRotationAt(dir string) []string {
	raw, _ := persistedString(dir, KeyChatModelRotation)
	return ParseModelFallbacks(raw)
}

// WriteChatModelRotation uses the profile writer so unrelated preferences survive.
func WriteChatModelRotation(dir, raw string) error {
	return writeProfileValue(dir, KeyChatModelRotation, strings.Join(ParseModelFallbacks(raw), ", "))
}
