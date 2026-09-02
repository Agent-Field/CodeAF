package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
)

// THE POINT OF THIS COMMAND IS THAT IT COSTS NOTHING TO ASK, so every test here
// runs it with no key, no store and no network, exactly as a person meets it on
// the machine where they have not decided yet whether to set aforge up. A test
// that needed a fixture would be a test of a different command.

func TestManualListsEveryPageWithItsTitle(t *testing.T) {
	var out bytes.Buffer
	if err := runManualWith(nil, &out); err != nil {
		t.Fatalf("aforge manual: %v", err)
	}
	printed := out.String()
	for _, page := range manual.Chat().Pages() {
		if !strings.Contains(printed, page) {
			t.Errorf("the listing does not name the page %q", page)
		}
	}
	// The names are the payload and each carries the title the page gives
	// itself, so a person scanning the list decides from the page's own words
	// rather than from a file name.
	title := manual.Chat().PageTitle("permissions")
	var titled bool
	for _, line := range strings.Split(printed, "\n") {
		if strings.HasPrefix(line, "permissions ") && strings.HasSuffix(line, title) {
			titled = true
		}
	}
	if !titled {
		t.Errorf("the listing does not carry permissions' own title %q:\n%s", title, printed)
	}
}

func TestManualPrintsAPageAsItIsWritten(t *testing.T) {
	var out bytes.Buffer
	if err := runManualWith([]string{"permissions"}, &out); err != nil {
		t.Fatalf("aforge manual permissions: %v", err)
	}
	page, found := manual.Chat().Page("permissions")
	if !found {
		t.Fatal("there is no permissions page to print")
	}
	// AS WRITTEN AND WHOLE. Nothing is cut, summarized or reordered — a person
	// who asked for the page has the page, which is the entire reason this
	// command exists beside the model's tool.
	if strings.TrimSpace(out.String()) != page {
		t.Errorf("the printed page is not the page as written (printed %d bytes, page is %d)",
			len(strings.TrimSpace(out.String())), len(page))
	}
}

func TestManualAnswersAQuestionWithLabelledSections(t *testing.T) {
	var out bytes.Buffer
	if err := runManualWith([]string{"who", "can", "see", "my", "files"}, &out); err != nil {
		t.Fatalf("aforge manual \"who can see my files\": %v", err)
	}
	printed := out.String()
	sections := manual.Chat().Search("who can see my files", manualQuestionSections)
	if len(sections) == 0 {
		t.Fatal("the question reaches nothing at all")
	}
	for _, section := range sections {
		// Every section says which page and which heading it came from, so a
		// quoted answer can be traced back to the page that authorized it.
		if !strings.Contains(printed, "["+section.Page+" · "+section.Title+"]") {
			t.Errorf("the answer does not label the section %s · %s", section.Page, section.Title)
		}
		if !strings.Contains(printed, section.Body) {
			t.Errorf("the section %s · %s was cut rather than printed whole", section.Page, section.Title)
		}
	}
}

// AND THE ANSWER IS ASSERTED AGAINST A PAGE, NOT AGAINST ANOTHER SEARCH. The
// test above proves the printer prints what it was handed; this one proves the
// door reaches an answer, by asking the manual the question a person asks about
// the manual itself and demanding the page that now documents it.
func TestManualAnswersTheQuestionAboutItsOwnDoor(t *testing.T) {
	var out bytes.Buffer
	if err := runManualWith([]string{"how", "do", "I", "read", "the", "manual"}, &out); err != nil {
		t.Fatalf("aforge manual \"how do I read the manual\": %v", err)
	}
	if !strings.Contains(out.String(), "[commands · ") {
		t.Errorf("the question about reading the manual reached no section of the commands page:\n%s", out.String())
	}
}

// A NAME THAT IS NOT A PAGE IS A FAILURE, not a search that quietly answers
// something else — and the refusal has to leave the person able to act, so it
// names every page there is.
func TestManualRefusesAPageThatDoesNotExist(t *testing.T) {
	var out bytes.Buffer
	err := runManualWith([]string{"no-such-page"}, &out)
	if err == nil {
		t.Fatal("aforge manual no-such-page exited 0")
	}
	if out.Len() != 0 {
		t.Errorf("a refusal wrote to stdout:\n%s", out.String())
	}
	for _, page := range manual.Chat().Pages() {
		if !strings.Contains(err.Error(), page) {
			t.Errorf("the refusal does not name the page %q that does exist", page)
		}
	}
}

// A question the manual has nothing on is an ANSWER — usually "no, it does not
// do that" — so it exits 0 and says so, unlike a page asked for by name.
func TestManualSaysWhenItHasNothingOnAQuestion(t *testing.T) {
	var out bytes.Buffer
	if err := runManualWith([]string{"zzqx", "wfjb", "quixotry"}, &out); err != nil {
		t.Fatalf("a question with no answer failed: %v", err)
	}
	if !strings.Contains(out.String(), "the manual has nothing on that") {
		t.Errorf("nothing was said about the empty result:\n%s", out.String())
	}
}

// The dispatch is the seam a person actually crosses, and a subcommand that
// works only when it is called directly is a subcommand nobody can run.
func TestManualDispatchesFromTheCommandLine(t *testing.T) {
	previousArgs := os.Args
	t.Cleanup(func() { os.Args = previousArgs })

	os.Args = []string{"aforge", "manual"}
	printed, err := captureStdout(t, run)
	if err != nil {
		t.Fatalf("aforge manual: %v", err)
	}
	if !strings.Contains(printed, "permissions") {
		t.Errorf("the dispatched listing named no pages:\n%s", printed)
	}
}

// The help text is the only place a person finds out the subcommand exists.
func TestUsageMentionsManual(t *testing.T) {
	if !strings.Contains(usageText, "aforge manual") {
		t.Fatal("usageText does not mention `aforge manual`")
	}
}
