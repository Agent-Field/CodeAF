package manual

import (
	"strings"
	"testing"
)

// A page layered over the chat corpus is listed, read and searched beside the
// packed pages, and the packed corpus is not changed by it.
func TestAnOverlayPageIsSearchedReadAndListedBesideThePackedOnes(t *testing.T) {
	page := "# senior-dev\n\n## What /senior-dev does — hand a large change to senior-dev\n\nsenior-dev is an autonomous coding agent. Type `/senior-dev <brief>`.\n\n## What senior-dev cannot do\n\nIt cannot ask you anything.\n"
	layered := Chat().WithPages(map[string]string{"delegate-senior-dev": page})
	if layered == Chat() {
		t.Fatal("WithPages with a page answered the same corpus")
	}
	text, ok := layered.Page("delegate-senior-dev")
	if !ok || !strings.Contains(text, "autonomous coding agent") {
		t.Fatalf("the overlay page cannot be read: %v %q", ok, text)
	}
	if !layered.Mentions("/senior-dev") {
		t.Fatal("the layered corpus does not mention the delegate's command")
	}
	found := false
	for _, name := range layered.Pages() {
		found = found || name == "delegate-senior-dev"
	}
	if !found {
		t.Fatalf("the overlay page is not listed: %v", layered.Pages())
	}
	hits := layered.Search("what does /senior-dev do", 4)
	if len(hits) == 0 || hits[0].Page != "delegate-senior-dev" {
		t.Fatalf("the question did not reach the overlay page first: %+v", hits)
	}
	// And a packed page is still there, unchanged.
	if _, ok := layered.Page("delegates"); !ok {
		t.Fatal("the packed delegates page is gone from the layered corpus")
	}
	if _, ok := Chat().Page("delegate-senior-dev"); ok {
		t.Fatal("the packed corpus learnt the overlay page")
	}
}

func TestAnOverlayNeverReplacesAPackedPageAndNoPagesIsTheSameCorpus(t *testing.T) {
	if Chat().WithPages(nil) != Chat() {
		t.Fatal("no pages answered a new corpus")
	}
	if Chat().WithPages(map[string]string{"empty": "  "}) != Chat() {
		t.Fatal("an empty page answered a new corpus")
	}
	layered := Chat().WithPages(map[string]string{"delegates": "# an impostor\n\n## nothing\n\nnothing\n"})
	text, _ := layered.Page("delegates")
	if strings.Contains(text, "impostor") {
		t.Fatal("an overlay replaced a packed page")
	}
}
