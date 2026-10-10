package manual

import "testing"

// These probes keep the Accept delay, its Undo, the failure line and the
// per-window Skip reachable in the words a person actually asks.
func TestTheChatManualAnswersNextUpAcceptQuestions(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"what happens when I accept suggestions in Next up", "desktop-nextup-accept"},
		{"can I undo Accept suggestions before they are sent", "desktop-nextup-accept"},
		{"what if accepting a suggestion fails", "desktop-nextup-accept"},
		{"does Skip in Next up stay on this window only", "desktop-nextup-accept"},
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
	const page = "desktop-nextup-accept"
	for _, section := range Chat().Search("desktop nextup accept", 8) {
		if section.Page == page {
			return
		}
	}
	t.Fatalf("searching for the page name never reaches %s", page)
}
