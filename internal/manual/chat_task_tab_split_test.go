package manual

import "testing"

// A chat and the task opened from it are two views. These are the questions that ask for that.
func TestDesktopTaskTabSplitQuestions(t *testing.T) {
	for _, question := range []string{
		"Can I put a chat and its task side by side?",
		"Does opening a task in a new tab change the conversation?",
		"If I go back in a task tab, does the chat beside it change?",
		"I opened a task inside the chat and then opened it in a new tab",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-task-tab-split" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-task-tab-split", question)
		})
	}
}
