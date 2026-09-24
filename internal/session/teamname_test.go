package session

import "testing"

// A TEAM NAME IS ONE TO THREE PLAIN WORDS, and an answer that is not a name is
// nothing, so the surface keeps the word it already offered.
func TestCleanTeamName(t *testing.T) {
	for raw, want := range map[string]string{
		"harbor":                            "harbor",
		"Parser Port":                       "parser port",
		"\"release prep\".":                 "release prep",
		"**TUI polish work**":               "tui polish work",
		"the relay audit and its follow-up": "the relay audit",
		"port-b fixes":                      "port-b fixes",
		"":                                  "",
		"Give these conversations one short shared name": "",
	} {
		if got := cleanTeamName(raw); got != want {
			t.Errorf("cleanTeamName(%q) = %q, want %q", raw, got, want)
		}
	}
}
