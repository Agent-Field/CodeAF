package manual

import "testing"

// People ask about visible text and keyboard scrolling without knowing the palette's token names.
func TestDesktopTextContrastQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"why are desktop link titles and warning messages different colours", "how do I scroll a desktop decision question with the keyboard"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-text-contrast" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q should reach desktop-text-contrast", question)
		}
	}
}
