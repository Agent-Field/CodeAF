package manual

import (
	"strings"
	"testing"
)

// TestPlaceLinksAnswersWhatPeopleAsk is this feature's own probe table.
// The shared table in chat_test.go is left alone; a question that stops
// reaching desktop-place-links is fixed on the page, not by widening this list.
func TestPlaceLinksAnswersWhatPeopleAsk(t *testing.T) {
	asked := []struct {
		question string
		page     string
		says     []string
	}{
		{
			question: "why does a chat say also in this place",
			page:     "desktop-place-links",
			says:     []string{"Also in this place.", "at most two"},
		},
		{
			question: "how many other places can one chat also be in",
			page:     "desktop-place-links",
			says:     []string{"At most two."},
		},
		{
			question: "what is a view of this place by month",
			page:     "desktop-place-links",
			says:     []string{"A view of this place by month.", "15 chats"},
		},
		{
			question: "is a view of these chats its own place",
			page:     "desktop-place-links",
			says:     []string{"A view of these chats, not a place."},
		},
		{
			question: "what does by project show on a place",
			page:     "desktop-place-links",
			says:     []string{"A view of this place by project."},
		},
	}
	for _, ask := range asked {
		found := Chat().Search(ask.question, DefaultResults)
		reached := false
		for _, section := range found {
			if section.Page != ask.page {
				continue
			}
			ok := true
			for _, needle := range ask.says {
				if !strings.Contains(section.Body, needle) {
					ok = false
					break
				}
			}
			if ok {
				reached = true
				break
			}
		}
		if !reached {
			where := make([]string, 0, len(found))
			for _, section := range found {
				where = append(where, section.Page+" · "+section.Title)
			}
			t.Errorf("%q should reach %s; it reached %v", ask.question, ask.page, where)
		}
	}
}
