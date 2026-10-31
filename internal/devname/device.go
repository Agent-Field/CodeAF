package devname

import (
	"fmt"
	"strings"
)

// TailLength is shared by the terminal list and the chat's device picker.
const TailLength = 4

// DeviceLabel gives duplicate device names the same selectable suffix everywhere.
func DeviceLabel(name, id string, duplicate bool) string {
	if !duplicate {
		return name
	}
	return DistinctName(name, id)
}

// MatchesDevice accepts both the printed label and its space-free terminal form.
func MatchesDevice(typed, name, id string) bool {
	if strings.EqualFold(strings.TrimSpace(typed), name) {
		return true
	}
	label := DeviceLabel(name, id, true)
	return strings.EqualFold(strings.ReplaceAll(strings.TrimSpace(typed), " #", "#"), strings.ReplaceAll(label, " #", "#"))
}

// Ambiguous names the selectors a person can use in their next terminal command.
func Ambiguous(name string, choices []string) error {
	return fmt.Errorf("more than one device is called %q: %s — name one, for example: codeaf devices revoke %s", name, strings.Join(choices, ", "), strings.ReplaceAll(choices[0], " #", "#"))
}
