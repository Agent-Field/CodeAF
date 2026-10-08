package manual

import (
	"strings"
	"testing"
)

// The questions a person asks about a factory stage that runs as a
// conversation reach the page that answers them
// (chat/factory-stage-conversations.md).
func TestStageConversationQuestionsReachTheirPage(t *testing.T) {
	for _, asked := range []string{
		"how does a factory stage end",
		"what is stage_result",
		"what does plan_edit do",
		"is a stage a conversation",
		"does a stage post to github",
	} {
		found := false
		for _, section := range Chat().Search(asked, DefaultResults) {
			if section.Page == "factory-stage-conversations" && strings.Contains(section.Title, "stage_result") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach the stage conversation section", asked)
		}
	}
}
