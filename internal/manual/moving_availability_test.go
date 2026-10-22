package manual

import (
	"strings"
	"testing"
)

// C15, C26 and C24 require independently retrieved moving-chat introductions
// to explain rollout, platform availability and which conversations can move.
func TestMovingChatIntroductionsStateAvailabilityAndLegacyLimits(t *testing.T) {
	for _, page := range []string{"continuing-a-chat-on-another-computer", "add-or-remove-a-computer", "use-this-on-another-computer", "relay-hosted-or-your-own"} {
		raw, ok := Chat().Page(page)
		if !ok {
			t.Fatal(page)
		}
		_, section, ok := strings.Cut(raw, "\n## ")
		if !ok {
			t.Fatalf("%s has no first section", page)
		}
		section, _, _ = strings.Cut(section, "\n## ")
		for _, want := range []string{"off on every build", "CODEAF_CELLS=1", "both computers", "shell profile", "Windows does not", "Only chats started after", "Older chats stay where they are"} {
			if !strings.Contains(section, want) {
				t.Errorf("%s first section lacks %q", page, want)
			}
		}
	}
	for _, query := range []string{"continue on another computer", "add a second computer"} {
		found := false
		for _, section := range Chat().Search(query, DefaultResults) {
			if strings.Contains(section.Body, "off on every build") && strings.Contains(section.Body, "CODEAF_CELLS=1") && strings.Contains(section.Body, "Windows does not") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q reaches no section explaining availability", query)
		}
	}
}
