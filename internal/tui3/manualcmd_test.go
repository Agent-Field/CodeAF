package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

// manualNote runs one /manual form through the dispatch and hands back the note
// it left, the way the loop would.
func manualNote(t *testing.T, a *app, line string) string {
	t.Helper()
	if cmd := a.slash(line); cmd != nil {
		a.Update(cmd())
	}
	return lastNote(t, a)
}

func TestManualCommandListsThePagesThereAre(t *testing.T) {
	a, _ := sheetApp(t)
	note := manualNote(t, a, "/manual")
	for _, page := range manual.Chat().Pages() {
		if !strings.Contains(note, page) {
			t.Errorf("the listing does not name the page %q", page)
		}
	}
}

// AS WRITTEN, NOT RETOLD — which is the whole reason this door exists beside the
// model's tool. A page that arrived summarized would be the paraphrase again,
// wearing a slash.
func TestManualCommandShowsAPageAsItIsWritten(t *testing.T) {
	a, _ := sheetApp(t)
	note := manualNote(t, a, "/manual permissions")
	page, found := manual.Chat().Page("permissions")
	if !found {
		t.Fatal("there is no permissions page to show")
	}
	if note != page {
		t.Errorf("the note is not the page as written (note %d bytes, page %d)", len(note), len(page))
	}
}

func TestManualCommandAnswersAQuestionWithLabelledSections(t *testing.T) {
	a, _ := sheetApp(t)
	note := manualNote(t, a, "/manual who can see my files")
	sections := manual.Chat().Search("who can see my files", manualChatSections)
	if len(sections) == 0 {
		t.Fatal("the question reaches nothing at all")
	}
	for _, section := range sections {
		if !strings.Contains(note, "## "+section.Page+" · "+section.Title) {
			t.Errorf("the answer does not say where %s · %s came from", section.Page, section.Title)
		}
	}
}

// A NAME IS AN EXACT REQUEST. A near miss is refused rather than answered with
// something else, and the refusal leaves the person able to act.
func TestManualCommandRefusesAPageThatDoesNotExist(t *testing.T) {
	a, _ := sheetApp(t)
	note := manualNote(t, a, "/manual no-such-page")
	if !strings.Contains(note, "there is no manual page named no-such-page") {
		t.Errorf("the refusal does not name what was asked for: %q", note)
	}
	for _, page := range manual.Chat().Pages() {
		if !strings.Contains(note, page) {
			t.Errorf("the refusal does not name the page %q that does exist", page)
		}
	}
}

// THE /crew ROWS NAME THE THREE SEATS A PERSON CAN PIN, by the words the
// router uses for them, and every shortcut the command takes has a row.
func TestTheCrewRowsNameTheSeatsAndTheShortcuts(t *testing.T) {
	var said []string
	for _, row := range commands {
		if row.name == "crew" {
			said = append(said, row.args+" "+row.desc)
		}
	}
	text := strings.Join(said, "\n")
	for _, want := range []string{"pin", "unpin", "models", "cap", "worker", "planner", "checker"} {
		if !strings.Contains(text, want) {
			t.Errorf("the /crew rows never say %q:\n%s", want, text)
		}
	}
	for _, retired := range []string{"frugal", "balanced", "preset", "mastermind"} {
		if strings.Contains(text, retired) {
			t.Errorf("the /crew rows still say the retired %q:\n%s", retired, text)
		}
	}
}
