package manual

import "testing"

// The place home lists a discussion while it is still going, and History
// lists the chat even before anyone has typed. These are the questions a
// person asks about that.
func TestDesktopCouncilHomeAnswersTheQuestionsPeopleAsk(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"where does a running discussion show on the place home", "desktop-council-home"},
		{"what does 2 of 6 turns mean on the place home", "desktop-council-home"},
		{"why is closed work still listed on the place home", "desktop-council-home"},
		{"where do council chats appear in history", "desktop-council-home"},
		{"how do I steer a discussion that is still going", "desktop-council-home"},
		{"what does the decided count on a place home mean", "desktop-council-home"},
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
