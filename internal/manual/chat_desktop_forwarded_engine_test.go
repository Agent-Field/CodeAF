package manual

import "testing"

// These probes keep forwarded desktop connection rules reachable in the person's words.
func TestDesktopForwardedEngineQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"can the desktop use a remote engine forwarded to this computer",
		"why does a forwarded localhost engine work with the desktop security policy",
		"why is my forwarded desktop engine connection refused",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-forwarded-engine" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-forwarded-engine", question)
		})
	}
}
