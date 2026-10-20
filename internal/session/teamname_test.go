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

// A NAME FOR ONE PIECE OF WORK IS ONE OR TWO WORDS (internal/tui3's helpers.go):
// the same cleaning, cut at two, and an echo of the short ask is nothing.
func TestCleanTeamNameForOnePieceOfWorkKeepsTwoWords(t *testing.T) {
	for raw, want := range map[string]string{
		"fizzbuzz":                  "fizzbuzz",
		"Guitar Pickups":            "guitar pickups",
		"the relay audit follow-up": "the relay",
		"\"flaky tests\".":          "flaky tests",
		"Give this work one short":  "",
		"":                          "",
	} {
		if got := cleanTeamNameIn(raw, 2, teamNameAskShort); got != want {
			t.Errorf("cleanTeamNameIn(%q, 2) = %q, want %q", raw, got, want)
		}
	}
}
