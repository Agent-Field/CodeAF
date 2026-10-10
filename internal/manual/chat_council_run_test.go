package manual

import "testing"

// How a discussion between two places is run: who speaks, when it stops, and
// what a person can do in it.
func TestDesktopCouncilRunAnswersTheQuestionsPeopleAsk(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"how do two places talk to each other", "desktop-council-run"},
		{"what happens when two places cannot agree", "desktop-council-run"},
		{"how do I step into a discussion between two places", "desktop-council-run"},
		{"what happens if a place fails to reply", "desktop-council-run"},
		{"capped councils", "desktop-council-run"},
	}
	for _, probe := range asked {
		found := false
		got := Chat().Search(probe.question, DefaultResults)
		for _, section := range got {
			if section.Page == probe.page {
				found = true
				break
			}
		}
		if !found {
			where := make([]string, 0, len(got))
			for _, section := range got {
				where = append(where, section.Page+" · "+section.Title)
			}
			t.Errorf("%q does not reach %s; it reached %v", probe.question, probe.page, where)
		}
	}
}
