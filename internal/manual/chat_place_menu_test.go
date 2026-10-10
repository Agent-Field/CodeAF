package manual

import (
	"strings"
	"testing"
)

// The place tile menu, the Home ⋯ and the Home tab menu have their own page.
// The shared probe table in chat_test.go is left alone.
func TestPlaceMenuManualAnswersWhatPeopleAsk(t *testing.T) {
	asked := []struct {
		question string
		title    string
		says     []string
	}{
		{"what is on the right-click menu of a place tile", "right-click menu of a place tile", []string{"Quick Look", "Pin to rail", "Delete place…"}},
		{"how do I pin a place to the rail or unpin it", "pin a place to the rail", []string{"Unpin", "Pin to rail"}},
		{"where is Add to another place on a place tile", "Add to another place", []string{"one more parent"}},
		{"why is Delete place red words with no icon", "red words with no icon", []string{"danger colour", "no icon"}},
		{"what is on the Home page place actions menu", "Home page place actions menu", []string{"Open in new window", "All places"}},
		{"does the home tab place menu include archive and delete", "home tab place menu", []string{"Close place", "not on the home tab"}},
	}
	for _, ask := range asked {
		reached := false
		for _, section := range Chat().Search(ask.question, DefaultResults) {
			if section.Page != "desktop-place-menu" || !strings.Contains(section.Title, ask.title) {
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
			t.Errorf("%q does not reach desktop-place-menu · %q", ask.question, ask.title)
		}
	}
}
