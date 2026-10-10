package manual

import "testing"

// These questions keep the bubble and folded-turn actions reachable without editing the shared probes.
func TestDesktopMessageMenuQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"right-click my message to copy it or edit a sent message",
		"copy the full answer from a folded turn",
		"open a folded turn in a new tab",
		"Shift+F10 or ContextMenu key on a message or folded turn",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-message-menus" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-message-menus", question)
		})
	}
}
