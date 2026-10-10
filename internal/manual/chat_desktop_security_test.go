package manual

import (
	"strings"
	"testing"
)

// The desktop bridge's security answers have to be reachable in the words a
// person would type. The shared table in chat_test.go is left alone; this
// file is the probe for desktop-bridge-security only.
func TestTheDesktopSecurityPageAnswers(t *testing.T) {
	asked := []struct {
		question string
		title    string
		says     []string
	}{
		{
			question: "can a website talk to the desktop engine if it knows the token",
			title:    "Can a website talk to the desktop engine if it knows the token",
			says:     []string{"this connection is only for the codeaf app", "engine connection required"},
		},
		{
			question: "does the desktop save the engine token in localStorage",
			title:    "Does the desktop save the engine token in localStorage",
			says:     []string{"not written to localStorage or sessionStorage", "never sends the key"},
		},
		{
			question: "why the desktop engine refuses to listen on 0.0.0.0",
			title:    "Why the desktop engine refuses to listen on 0.0.0.0",
			says:     []string{"desktop transport must listen on a loopback address", "does not receive the engine token"},
		},
	}
	for _, ask := range asked {
		reached := false
		for _, section := range Chat().Search(ask.question, DefaultResults) {
			if section.Page != "desktop-bridge-security" || section.Title != ask.title {
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
			t.Errorf("%q does not reach desktop-bridge-security · %q; it reached %v", ask.question, ask.title, where)
		}
	}
}
