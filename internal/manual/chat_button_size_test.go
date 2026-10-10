package manual

import "testing"

// Height questions should reach the button page without needing the component's API name.
func TestDesktopButtonSizeQuestionsReachTheirPage(t *testing.T) {
	for _, question := range []string{"why are desktop buttons different heights", "can I make desktop buttons taller"} {
		found := false
		for _, section := range Chat().Search(question, DefaultResults) {
			if section.Page == "desktop-button-size" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q should reach desktop-button-size", question)
		}
	}
}
