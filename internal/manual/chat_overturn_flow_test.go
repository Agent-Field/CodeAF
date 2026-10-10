package manual

import (
	"strings"
	"testing"
)

// The overturn confirmation has its own page. These questions are the words a
// person uses for it. The shared probe table in chat_test.go is left alone.
func TestOverturnFlowManualAnswersWhatTheConfirmationOffers(t *testing.T) {
	asked := []struct {
		question string
		title    string
		says     []string
	}{
		{
			question: "I overturned a decision that two tasks used, can I notify or pause them",
			title:    "Overturn a decision that other tasks used",
			says:     []string{"2 tasks used this", "Notify them", "Pause them", "Leave them", "never cascades"},
		},
		{
			question: "how do I undo an overturn after the toast says decision overturned",
			title:    "Taking an overturn back",
			says:     []string{"Decision overturned.", "Undo"},
		},
	}
	for _, ask := range asked {
		reached := false
		for _, section := range Chat().Search(ask.question, DefaultResults) {
			if section.Page != "desktop-overturn-flow" || !strings.Contains(section.Title, ask.title) {
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
			t.Errorf("%q does not reach desktop-overturn-flow · %q", ask.question, ask.title)
		}
	}
}
