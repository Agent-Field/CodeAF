package manual

import (
	"strings"
	"testing"
)

func TestQuitQuestionExplainsOtherHeldConversations(t *testing.T) {
	for _, s := range Chat().Search("closing a tab versus quitting codeaf /quit ctrl+c", DefaultResults) {
		if s.Page == "screen" && strings.Contains(s.Body, "`/quit` closes the conversation in front") && strings.Contains(s.Body, "another conversation") && strings.Contains(s.Body, "`ctrl+c` ends the whole program") {
			return
		}
	}
	t.Fatal("quitting question does not reach the corrected conversation scope")
}
