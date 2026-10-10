package manual

import (
	"strings"
	"testing"
)

// The decision routes have their own page. The shared probe table in
// chat_test.go is left alone, so this lane does not collide with others.
func TestDecisionRoutesManualAnswersWhatThePeopleAsk(t *testing.T) {
	asked := []struct {
		question string
		title    string
		says     []string
	}{
		{"where can I see everything codeaf decided for me automatically", "The decision log", []string{"newest first", "200"}},
		{"why does the home status line say learning or deciding automatically", "The Home status line", []string{"Always ask", "last seven days"}},
		{"what happens when I overturn a decision twice or it cannot be undone", "Overturn a decision", []string{"This decision cannot be undone.", "twice"}},
		{"how do I change how sure it has to be before it decides without asking", "The sureness setting", []string{"1 to 100", "90"}},
	}
	for _, ask := range asked {
		reached := false
		for _, section := range Chat().Search(ask.question, DefaultResults) {
			if section.Page != "desktop-decision-routes" || !strings.Contains(section.Title, ask.title) {
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
			t.Errorf("%q does not reach desktop-decision-routes · %q", ask.question, ask.title)
		}
	}
}
