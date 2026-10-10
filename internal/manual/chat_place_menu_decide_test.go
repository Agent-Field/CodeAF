package manual

import (
	"strings"
	"testing"
)

// The place-menu decision entries have their own page and probe table, so the
// shared table in chat_test.go is left alone.
func TestPlaceMenuDecideManualAnswersWhatThePeopleAsk(t *testing.T) {
	asked := []struct {
		question string
		title    string
		says     []string
	}{
		{"how do I stop a place from deciding for me and make it always ask me", "Always ask me", []string{"checked toggle", "Undo"}},
		{"where do I change how sure a place must be before it decides on its own", "Decision confidence", []string{"90%", "50%"}},
	}
	for _, ask := range asked {
		reached := false
		for _, section := range Chat().Search(ask.question, DefaultResults) {
			if section.Page != "desktop-place-menu-decide" || !strings.Contains(section.Title, ask.title) {
				continue
			}
			reached = true
			for _, needle := range ask.says {
				if !strings.Contains(section.Body, needle) {
					t.Errorf("%q reached %q but its body omits %q", ask.question, section.Title, needle)
				}
			}
		}
		if !reached {
			t.Errorf("%q does not reach desktop-place-menu-decide · %q", ask.question, ask.title)
		}
	}
}
