package manual

import "testing"

// These probes keep filing by drag and drop reachable in the person's words.
func TestDesktopFilingQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"can I drag a file onto a place to add it as a source",
		"drop a link onto a place in the rail",
		"drag a folder onto a place on Home",
		"drag a tab onto a place to file its chat there",
		"why was the file I dropped onto a place skipped",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-filing" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-filing", question)
		})
	}
}
