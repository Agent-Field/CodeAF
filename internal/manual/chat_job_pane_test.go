package manual

import "testing"

// The body of a job tab (header words, Stop, the trimmed-log line, Ask, the
// job-is-gone line) must be reachable in a person's own words.
func TestTheJobPanePageAnswersWhatTheTabShows(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"what does a background job tab show", "desktop-job-pane"},
		{"how do I stop a running background job", "desktop-job-pane"},
		{"why does the job log say earlier output was trimmed", "desktop-job-pane"},
		{"ask about a job's output", "desktop-job-pane"},
		{"the engine no longer has this job", "desktop-job-pane"},
	}
	for _, ask := range asked {
		found := Chat().Search(ask.question, DefaultResults)
		var reached bool
		pages := make([]string, 0, len(found))
		for _, section := range found {
			pages = append(pages, section.Page)
			if section.Page == ask.page {
				reached = true
			}
		}
		if !reached {
			t.Errorf("%q should reach %s; it reached %v", ask.question, ask.page, pages)
		}
	}
}
