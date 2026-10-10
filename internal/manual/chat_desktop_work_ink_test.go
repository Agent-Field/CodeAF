package manual

import "testing"

// Work feedback questions must reach the desktop's hover and press rules.
func TestDesktopWorkInkQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why does a completed step stay dim when I hover it",
		"how do file chips link chips and the model picker react to clicks",
	} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-work-ink" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q should reach desktop-work-ink", question)
		}
	}
}
