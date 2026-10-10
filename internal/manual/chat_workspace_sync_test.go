package manual

import (
	"testing"
)

// Desktop tab sets: two windows on one place share the tabs, and each window
// keeps the tab it is showing. The questions are the words a person types.
func TestDesktopWorkspaceSyncAnswersTheQuestionsPeopleAsk(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"why do two windows show the same tabs", "desktop-workspace-sync"},
		{"where did the tabs I already had go", "desktop-workspace-sync"},
		{"which tab is selected in each window", "desktop-workspace-sync"},
		{"what happens to my tabs when the engine is offline", "desktop-workspace-sync"},
		{"what if both windows change the tabs at the same time", "desktop-workspace-sync"},
	}
	for _, probe := range asked {
		found := false
		for _, section := range Chat().Search(probe.question, DefaultResults) {
			if section.Page == probe.page {
				found = true
				break
			}
		}
		if !found {
			got := Chat().Search(probe.question, DefaultResults)
			where := make([]string, 0, len(got))
			for _, section := range got {
				where = append(where, section.Page+" · "+section.Title)
			}
			t.Errorf("%q does not reach %s; it reached %v", probe.question, probe.page, where)
		}
	}
}
