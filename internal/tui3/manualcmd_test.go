package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/manual"
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

// THE ROW COUNTS THE SEATS THE CODE ACTUALLY SETS. Both /crew rows said "four"
// for as long as [config.CrewModels] set five, and the seat they left out was
// the worker — the one that pays most of a task's bill. A hand-written number
// beside a table that owns it is a claim that drifts, so this reads the table.
func TestTheCrewRowsCountTheSeatsConfigSets(t *testing.T) {
	seats, ok := config.CrewModels(config.DefaultCrew)
	if !ok {
		t.Fatalf("config has no crew preset named %q", config.DefaultCrew)
	}
	counted := map[int]string{4: "four", 5: "five", 6: "six"}[len(seats)]
	if counted == "" {
		t.Fatalf("the crew now has %d seats and nothing here can spell that", len(seats))
	}
	for _, row := range commands {
		if row.name != "crew" {
			continue
		}
		if !strings.Contains(row.desc, counted) {
			t.Errorf("/crew %s says %q; the crew is %s models (%v)", row.args, row.desc, counted, seats)
		}
	}
}
