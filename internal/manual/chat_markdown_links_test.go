package manual

import "testing"

// These probes keep the desktop reply-link colour and its hover line reachable
// in the words a person uses when the line is missing or the colour looks wrong.
func TestDesktopMarkdownLinkQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why is a link in a desktop reply accent coloured",
		"when does a desktop markdown link underline on hover",
		"desktop markdown links",
	} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-markdown-links" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-markdown-links", question)
		})
	}
}
