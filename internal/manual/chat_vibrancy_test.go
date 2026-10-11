package manual

import "testing"

func TestDesktopVibrancyQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{
		"why is the desktop sidebar translucent or blurred on macOS",
		"why does the desktop frame turn solid with reduced transparency",
	} {
		reached := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-vibrancy" {
				reached = true
			}
		}
		if !reached {
			t.Errorf("%q should reach desktop-vibrancy", question)
		}
	}
}
