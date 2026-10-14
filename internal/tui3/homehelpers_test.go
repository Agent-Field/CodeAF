package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// homeLines is home's left column as a reader sees it.
func homeLines(a *app) []string {
	frame, _, _, _ := a.homeFrame(a.width, a.height)
	out := make([]string, 0, len(frame))
	for _, line := range frame {
		out = append(out, plain(line))
	}
	return out
}

// homeRowAt is the index of the first drawn row containing a string.
func homeRowAt(lines []string, want string) int {
	for i, line := range lines {
		if strings.Contains(line, want) {
			return i
		}
	}
	return -1
}

// typeDraft types into the box without sending.
func typeDraft(t *testing.T, a *app, line string) {
	t.Helper()
	for _, r := range line {
		drive(t, a, key(string(r)))
	}
}

// key2 spells a key the surface's own way, for the digits the answers take.
func key2(s string) tea.KeyPressMsg { return key(s) }
