package manual

import (
	"strings"
	"testing"
)

func TestRecentConversationsQuestionNamesHomeActivity(t *testing.T) {
	for _, s := range Chat().Search("home recent conversations sessions panel", DefaultResults) {
		if s.Page == "home" && strings.Contains(s.Body, "conversation list under activity") {
			return
		}
	}
	t.Fatal("recent conversation question does not reach the activity panel")
}
