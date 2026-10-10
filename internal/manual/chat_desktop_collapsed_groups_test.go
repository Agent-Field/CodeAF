package manual

import "testing"

// These questions keep the collapsed group's surviving label discoverable in the person's words.
func TestDesktopCollapsedGroupsAndKeyboardMenuQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"how do I collapse and expand a desktop tab group",
		"does collapsing a desktop tab group stop work",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-collapsed-groups" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-collapsed-groups", question)
		})
	}
}
