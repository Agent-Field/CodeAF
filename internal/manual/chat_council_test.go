package manual

import "testing"

// A discussion between two places is an ordinary chat. These are the questions
// a person asks when that chat appears, and when a second one will not.
func TestDesktopCouncilAnswersTheQuestionsPeopleAsk(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"why is there a chat called Marketing with Software", "desktop-council"},
		{"why two places cannot reopen the same topic for an hour", "desktop-council"},
		{"what limits a discussion between two places", "desktop-council"},
		{"how long before two places can reopen the same topic", "desktop-council"},
		{"desktop council", "desktop-council"},
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
