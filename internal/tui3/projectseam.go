package tui3

import "github.com/charmbracelet/x/ansi"

// seamWithProject appends the project after approvals on home and conversation
// seams. Controls and conversation telemetry keep their space; a long path
// yields its right end, and a field with no room for a path is absent entirely.
// The returned span covers the displayed path alone, for home's cycle control.
func seamWithProject(left, path string, room int) (string, hudSpan) {
	if path == "" {
		return left, hudSpan{}
	}
	prefix := targetProjectLead
	if left != "" {
		prefix = legendJoin + prefix
	}
	from := ansi.StringWidth(left) + ansi.StringWidth(prefix)
	if room-from < 3 {
		return left, hudSpan{}
	}
	path = fit(path, room-from)
	return left + prefix + path, hudSpan{from: from, to: from + ansi.StringWidth(path)}
}
