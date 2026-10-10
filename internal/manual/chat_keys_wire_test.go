package manual

import (
	"strings"
	"testing"
)

// The Next up, history and up-a-level chords must be reachable in a person's own words. The shared probe table in
// chat_test.go is left alone.
func TestKeysWireManualAnswersTheChordQuestions(t *testing.T) {
	asked := []struct{ question, title string }{
		{"which key walks my waiting questions", "Which key walks my waiting questions"},
		{"what does command up do on home", "What does Command Up do"},
	}
	for _, ask := range asked {
		reached := false
		for _, section := range Chat().Search(ask.question, DefaultResults) {
			if section.Page == "desktop-keys-wire" && strings.Contains(section.Title, ask.title) {
				reached = true
			}
		}
		if !reached {
			t.Errorf("%q does not reach desktop-keys-wire · %q", ask.question, ask.title)
		}
	}
}
