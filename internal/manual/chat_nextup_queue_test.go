package manual

import "testing"

// These probes keep the Next up walk order, Skip, the frame-pill scope and
// Accept N reachable in the words a person actually asks.
func TestTheChatManualAnswersNextUpQueueQuestions(t *testing.T) {
	asked := []struct {
		question string
		page     string
	}{
		{"what order does Next up walk questions", "desktop-nextup-queue"},
		{"what does Skip do in Next up", "desktop-nextup-queue"},
		{"does Next up count the conversation I am in", "desktop-nextup-queue"},
		{"which questions does Accept suggestions answer in Next up", "desktop-nextup-queue"},
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
	// The page-name search used by the chat manual's reachability gate.
	const page = "desktop-nextup-queue"
	for _, section := range Chat().Search("desktop nextup queue", 8) {
		if section.Page == page {
			return
		}
	}
	t.Fatalf("searching for the page name never reaches %s", page)
}
