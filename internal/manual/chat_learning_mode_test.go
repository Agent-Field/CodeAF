package manual

import "testing"

// These probes keep learning-mode graduation reachable in the person's words.
func TestDesktopLearningModeQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"when does a place start deciding for me",
		"what does 14 of 20 agreed mean",
		"I overturned one decision why is it asking again",
		"does overturning a git permission make it ask about shell commands too",
		"why is this place still learning after I agreed seventeen times",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-learning-mode" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-learning-mode", question)
		})
	}
}
