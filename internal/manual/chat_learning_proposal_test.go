package manual

import "testing"

// These probes keep the learning-mode proposal card reachable in the person's words.
func TestDesktopLearningProposalQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"what is the card that says a place is learning",
		"how do I agree with a proposal",
		"how do I take the other answer instead of the proposal",
		"what does the proposal say it relied on",
		"does the proposal countdown stop when I start reading it",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-learning-proposal" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-learning-proposal", question)
		})
	}
}
