package manual

import "testing"

// Certificate help must be reachable using the sentence the sheet displays.
func TestChatManualDesktopWebCertificate(t *testing.T) {
	for _, question := range []string{"This site's certificate isn't trusted in a desktop web tab", "desktop web certificate errors"} {
		t.Run(question, func(t *testing.T) {
			for _, section := range Chat().Search(question, DefaultResults) {
				if section.Page == "desktop-web-certificate" {
					return
				}
			}
			t.Fatalf("%q does not reach desktop-web-certificate", question)
		})
	}
}
