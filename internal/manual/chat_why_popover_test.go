package manual

import (
	"strings"
	"testing"
)

// The Why? card on an automatic-decision receipt has its own page. These
// questions are the words a person uses for it. The shared probe table in
// chat_test.go is left alone.
func TestWhyPopoverManualAnswersWhatTheWhyCardShowsAndDoes(t *testing.T) {
	asked := []struct {
		question string
		page     string
		title    string
		says     []string
	}{
		{
			question: "why did the place decide that, what does the Why? card show",
			page:     "desktop-why-popover",
			title:    "Why did the place decide that",
			says:     []string{"Decided automatically", "Sure", "left out of the card"},
		},
		{
			question: "how do I overturn a decision or make a place always ask me next time",
			page:     "desktop-why-popover",
			title:    "Overturn a decision",
			says:     []string{"Always ask me", "Fine, keep deciding", "Open the decision"},
		},
		{
			question: "how do I close the Why? card with escape and where does focus go",
			page:     "desktop-why-popover",
			title:    "Closing the Why? card",
			says:     []string{"Escape", "keyboard"},
		},
	}
	for _, ask := range asked {
		reached := false
		for _, section := range Chat().Search(ask.question, DefaultResults) {
			if section.Page != ask.page || !strings.Contains(section.Title, ask.title) {
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
			found := Chat().Search(ask.question, DefaultResults)
			where := make([]string, 0, len(found))
			for _, section := range found {
				where = append(where, section.Page+" · "+section.Title)
			}
			t.Errorf("%q does not reach %s · %q; it reached %v", ask.question, ask.page, ask.title, where)
		}
	}
}
