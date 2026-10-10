package manual

import "testing"

// These probes keep the Design system places specimen reachable in the person's words.
func TestDesktopPlacesAppQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"where is the places specimen on the desktop design system",
		"are the tiles on the places specimen my real places",
		"does opening the places specimen change the desktop window colour",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-places-app" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-places-app", question)
		})
	}
}
