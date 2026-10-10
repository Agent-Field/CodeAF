package manual

import "testing"

// Page identity must be reachable through the words someone asks about their web tab.
func TestDesktopWebEventsQuestions(t *testing.T) {
	for _, question := range []string{
		"why did my web tab title change and does Rename tab win",
		"does a web tab remember the address after a relaunch",
		"where is the web loading spinner favicon or site letter",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-web-events" {
					return
				}
			}
			t.Errorf("%q did not reach desktop-web-events", question)
		})
	}
}
