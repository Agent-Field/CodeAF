package manual

import "testing"

// The desktop header is retrieved by the words a reader uses when returning from a task.
func TestDesktopInTabBackQuestions(t *testing.T) {
	for _, question := range []string{
		"How do I go back from a desktop task to its conversation?",
		"Does the desktop parent button use window focus history?",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-in-tab-back" {
					return
				}
			}
			t.Fatalf("%q did not reach desktop-in-tab-back", question)
		})
	}
}
