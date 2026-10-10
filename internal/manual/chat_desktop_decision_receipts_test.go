package manual

import (
	"strings"
	"testing"
)

// Decision receipts are the lines a place leaves when it answered for you. The
// shared probe table in chat_test.go is left alone.
func TestDecisionReceiptsManualAnswersWhatTheLinesMean(t *testing.T) {
	asked := []struct {
		question string
		title    string
		says     []string
	}{
		{"what does allowed automatically by a place mean in the conversation", "Allowed automatically by a place", []string{"“Allowed”", "“Why?”"}},
		{"why does it say did 3 things", "Did 3 things", []string{"one line", "never wrapped in a group"}},
		{"are the automatic lines still there when I reopen a conversation", "Reopening a conversation", []string{"saved with the conversation"}},
	}
	for _, ask := range asked {
		reached := false
		for _, section := range Chat().Search(ask.question, DefaultResults) {
			if section.Page != "desktop-decision-receipts" || !strings.Contains(section.Title, ask.title) {
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
			t.Errorf("%q does not reach desktop-decision-receipts · %q", ask.question, ask.title)
		}
	}
}
