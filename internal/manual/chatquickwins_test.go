package manual

import (
	"strings"
	"testing"
)

// Each answer is retrieved alone, so a qualification on another page cannot
// correct a promise in the section the model actually reads.
func quickWinsSection(t *testing.T, page, title string) string {
	t.Helper()
	for _, section := range Chat().Sections() {
		if section.Page == page && section.Title == title {
			return strings.Join(strings.Fields(section.Body), " ")
		}
	}
	t.Fatalf("missing manual section %s: %s", page, title)
	return ""
}

func TestHomeManualQualifiesTheFreshProjectConversationTab(t *testing.T) {
	answer := quickWinsSection(t, "home", "Open another project from home")
	for _, fact := range []string{"untouched conversation has no tab", "draft or first sent message"} {
		if !strings.Contains(answer, fact) {
			t.Errorf("opening another project does not explain %q:\n%s", fact, answer)
		}
	}
}

func TestModelManualCountsTheDefaultProviderWithoutAKey(t *testing.T) {
	answer := quickWinsSection(t, "commands", "/model — pick a model")
	for _, fact := range []string{"only the default provider", "even without its key", "Ollama", "headings"} {
		if !strings.Contains(answer, fact) {
			t.Errorf("model picker does not explain %q:\n%s", fact, answer)
		}
	}
	if strings.Contains(answer, "With one connected provider") {
		t.Error("model picker still counts connected providers instead of its registered default")
	}
}

func TestRateManualExplainsAbsentAndRoundedThroughput(t *testing.T) {
	answer := quickWinsSection(t, "models-and-cost", "Why the via name keeps changing on the model list")
	for _, fact := range []string{"unknown", "below 1 token per second", "`0 tok/s`", "work pulse", "model rows", "rounded `t/s`", "rounds to zero"} {
		if !strings.Contains(answer, fact) {
			t.Errorf("throughput answer does not explain %q:\n%s", fact, answer)
		}
	}
}
