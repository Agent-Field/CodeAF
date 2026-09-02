package manual

import (
	"strings"
	"testing"
)

// The two questions the manual exists to answer: the one the user asked for by
// name, and the one that is really "explain your own behaviour to me". Both are
// asked in words nobody wrote a page heading in, which is the whole test.
func TestSearchAnswersTheQuestionsPeopleActuallyAsk(t *testing.T) {
	for _, probe := range []struct {
		question string
		page     string
		phrase   string
	}{
		{"what happens when I'm gone every day", "daily-rhythm", "while you are gone"},
		{"why did you ask before cancelling", "steering-work", "confirm"},
		{"how does the boost model work", "models", "boost"},
		{"what is a charter", "standing-goals", "charter"},
		// The ↻ line carries what ended the attempt, not only how much was
		// picked up, and the page shows it that way.
		{"why was my work picked up again", "surfaces",
			"picked up again from 45 recorded turns — it was still working when it ran out of its time"},
	} {
		found := Search(probe.question, DefaultResults)
		if len(found) == 0 {
			t.Fatalf("%q found nothing", probe.question)
		}
		if found[0].Page != probe.page {
			t.Fatalf("%q ranked %s · %q first, want page %s",
				probe.question, found[0].Page, found[0].Title, probe.page)
		}
		blob := strings.ToLower(found[0].Title + " " + found[0].Body)
		if !strings.Contains(blob, probe.phrase) {
			t.Fatalf("%q returned %q without %q", probe.question, found[0].Title, probe.phrase)
		}
	}
}

func TestSearchRespectsKAndReturnsNothingForUnknownWords(t *testing.T) {
	if found := Search("cancel", 2); len(found) != 2 {
		t.Fatalf("k=2 returned %d sections", len(found))
	}
	if found := Search("xyzzyplugh frobnicate", DefaultResults); len(found) != 0 {
		t.Fatalf("unknown words returned %d sections", len(found))
	}
	if found := Search("", DefaultResults); len(found) != 0 {
		t.Fatalf("empty query returned %d sections", len(found))
	}
}

func TestPageReadsWholeTopicAndSectionsCarryTheirPage(t *testing.T) {
	text, ok := Page("daily-rhythm")
	if !ok || !strings.Contains(text, "standing watch") {
		t.Fatalf("daily-rhythm page ok=%v len=%d", ok, len(text))
	}
	if _, ok := Page("daily-rhythm.md"); !ok {
		t.Fatal("page name with its extension did not resolve")
	}
	if _, ok := Page("no-such-page"); ok {
		t.Fatal("an unknown page resolved")
	}
	if len(Pages()) < 10 {
		t.Fatalf("manual has only %d pages", len(Pages()))
	}
	for _, section := range Sections() {
		if section.Page == "" || section.Title == "" || strings.TrimSpace(section.Body) == "" {
			t.Fatalf("malformed section %+v", section)
		}
	}
}

// The cue set is derived from the pages themselves, so it must stay both broad
// enough to catch a self-question and narrow enough to ignore ordinary work.
func TestCuesCoverTheVocabularyAndIgnoreOrdinaryRequests(t *testing.T) {
	for _, message := range []string{
		"how does the boost model work", "what is a charter",
		"what does the practice loop do", "how does the daily rail work",
		"why does it slow down when my machine is busy",
	} {
		if !Cued(message) {
			t.Fatalf("%q did not reach the manual's vocabulary", message)
		}
	}
	for _, message := range []string{
		"build me a parser", "cancel the queued ones",
		"write the launch note and save it as launch.md",
		"find out which dependencies changed",
	} {
		if Cued(message) {
			t.Fatalf("%q was read as a question about aforge itself", message)
		}
	}
}

func TestRenderNamesThePageEverySectionCameFrom(t *testing.T) {
	rendered := Context("what happens when I'm gone every day", 2)
	if !strings.Contains(rendered, "[daily-rhythm · ") {
		t.Fatalf("rendered context lost its attribution:\n%s", rendered)
	}
}
