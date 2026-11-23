package manual

import (
	"strings"
	"testing"
)

func TestUpgradeRecoveryQuestionsReachTheirInstructions(t *testing.T) {
	for question, page := range map[string]string{
		"where did my standing orders go":                   "automations",
		"my rules are gone":                                 "what-i-remember",
		"turns typed in an older codeaf are not shown here": "continuing-a-chat-on-another-computer",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == page {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not reach %s", question, page)
		}
	}
	for _, page := range []string{"automations", "what-i-remember", "starting-codeaf"} {
		text, _ := Chat().Page(page)
		for _, want := range []string{"not carried over", "standing-upgrade-report.txt", "/automations", "/remember", "first interactive launch"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks %q", page, want)
			}
		}
	}
	text, _ := Chat().Page("continuing-a-chat-on-another-computer")
	for _, want := range []string{"not merged into the copy", "2 turns typed in an older codeaf", "transcript.jsonl", "grows again"} {
		if !strings.Contains(text, want) {
			t.Errorf("continuing a chat lacks %q", want)
		}
	}
}
