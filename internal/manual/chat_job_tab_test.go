package manual

import "testing"

// Opening a job is a terminal tab, and a second open selects the one already
// showing it. A person who asks in those words has to land on that page, the
// same way TestTheChatManualAnswersTheQuestionsPeopleAsk checks the corpus.
func TestTheJobTabPageAnswersHowAJobOpens(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"how do I open a background job log", "desktop-job-tab"},
		{"opening the same job again selects its tab", "desktop-job-tab"},
		{"open a job without leaving this tab", "desktop-job-tab"},
		{"background job finished nightly-bench", "desktop-job-tab"},
	}
	for _, ask := range asked {
		found := Chat().Search(ask.question, DefaultResults)
		if len(found) == 0 {
			t.Errorf("%q reaches nothing in the chat manual", ask.question)
			continue
		}
		var reached bool
		for _, section := range found {
			if section.Page == ask.page {
				reached = true
				break
			}
		}
		if !reached {
			pages := make([]string, 0, len(found))
			for _, section := range found {
				pages = append(pages, section.Page)
			}
			t.Errorf("%q should reach %s; it reached %v", ask.question, ask.page, pages)
		}
	}
	const named = "desktop job tab"
	found := Chat().Search(named, 8)
	var reached bool
	for _, section := range found {
		if section.Page == "desktop-job-tab" {
			reached = true
			break
		}
	}
	if !reached {
		t.Errorf("%q should reach desktop-job-tab", named)
	}
}
