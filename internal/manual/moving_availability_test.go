package manual

import (
	"strings"
	"testing"
)

// K1, K5, C15, C27.1, C27.6 and C24 require independently retrieved moving-chat introductions
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
		for _, want := range []string{"available by default", "Linux, macOS and native Windows", "every build", "Pair both computers first", "Only chats started after", "Older chats stay where they are"} {
			if !strings.Contains(section, want) {
				t.Errorf("%s first section lacks %q", page, want)
			}
		}
	}
	for _, query := range []string{"continue on another computer", "add a second computer"} {
		found := false
		for _, section := range Chat().Search(query, DefaultResults) {
			if strings.Contains(section.Body, "available by default") && strings.Contains(section.Body, "Pair both computers first") && strings.Contains(section.Body, "native Windows") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q reaches no section explaining availability", query)
		}
	}
}

// TestK5WindowsMoveQuestionsReachTheFacts proves K5: native movement's real
// limits are retrievable without repeating the retired Windows refusal.
func TestK5WindowsMoveQuestionsReachTheFacts(t *testing.T) {
	for _, probe := range []struct{ question, fact string }{
		{"moving chats on Windows", "Native Windows moves chats"},
		{"why is con missing after moving my chat to Windows", "Names Windows refuses"},
		{"symlinks on Windows Developer Mode", "Developer Mode"},
		{"Windows vault permissions", "user profile"},
		{"hard links Windows chat", "Hard-linked names"},
	} {
		found := false
		for _, section := range Chat().Search(probe.question, DefaultResults) {
			if strings.Contains(section.Body, probe.fact) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q does not reach %q", probe.question, probe.fact)
		}
	}
}
