package manual

import (
	"strings"
	"testing"
)

// Background tabs on the desktop read the world stream. These questions are
// the words a person uses for that, and they have to reach the page that
// answers them. The shared probe table in chat_test.go is left alone.
func TestBackgroundTabsManualAnswersHowATabYouAreNotLookingAtStaysCurrent(t *testing.T) {
	asked := []struct {
		question string
		page     string
		title    string
		says     []string
	}{
		{
			question: "how does a background tab know it needs you",
			page:     "desktop-background-tabs",
			title:    "How a background tab knows it needs you",
			says:     []string{"one world stream", "how many things need you", "amber mark"},
		},
		{
			question: "does every open tab keep its own connection to the conversation",
			page:     "desktop-background-tabs",
			title:    "Does every open tab keep its own connection",
			says:     []string{"do not call `GET /sessions/{id}`", "One world stream covers them"},
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
