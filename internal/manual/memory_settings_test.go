package manual

import (
	"strings"
	"testing"
)

func TestMemorySettingQuestionsReachCostAndConsent(t *testing.T) {
	for _, question := range []string{"turn memory on", "why isn't it learning", "what does memory cost"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if strings.Contains(section.Body, "off by default") && strings.Contains(section.Body, "39%") && strings.Contains(section.Body, "27%") && strings.Contains(section.Body, "38%") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach memory consent and measured costs", question)
		}
	}
}
