package manual

import (
	"strings"
	"testing"
)

func TestMemorySettingQuestionsReachCostAndConsent(t *testing.T) {
	for _, question := range []string{"turn memory on", "why isn't it learning", "what does memory cost"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if strings.Contains(section.Body, "on by default") && strings.Contains(section.Body, "saved off") && strings.Contains(section.Body, "actual recorded costs") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach learning defaults, saved choices and actual costs", question)
		}
	}
}
