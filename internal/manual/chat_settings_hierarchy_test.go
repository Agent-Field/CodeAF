package manual

import "testing"

// Probes for the desktop hierarchy defaults and settings sections.
func TestTheChatManualAnswersSettingsHierarchyQuestions(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"how many places can codeaf create in the desktop app", "desktop-settings-hierarchy"},
		{"how deep can a desktop place be", "desktop-settings-hierarchy"},
		{"can I put fifty reports under one place", "desktop-settings-hierarchy"},
		{"what are the desktop settings categories", "desktop-settings-hierarchy"},
	}
	for _, ask := range asked {
		t.Run(ask.question, func(t *testing.T) {
			for _, section := range Chat().Search(ask.question, DefaultResults) {
				if section.Page == ask.page {
					return
				}
			}
			t.Fatalf("%q did not reach %s", ask.question, ask.page)
		})
	}
}
