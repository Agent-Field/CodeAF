package manual

import "testing"

// Copy questions about the work block must reach its menus page in a person's own words.
func TestDesktopWorkMenuQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"how do I copy the log of what codeaf did", "copy the command a step ran", "copy a tool's output from the work block"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-work-menus" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q should reach desktop-work-menus", question)
		}
	}
}
