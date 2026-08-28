package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// The permissions panel.
//
// Everything here drives the real surface against a real profile directory: the
// panel's whole job is to say what is written in two settings rows and to write
// one of them back, and a test that stubbed the config would be a test of the
// stub.

// permApp is a surface over a profile carrying the rows it was given.
func permApp(t *testing.T, rows map[string]string) *app {
	t.Helper()
	dir := t.TempDir()
	body, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("building the profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), body, 0o600); err != nil {
		t.Fatalf("writing the profile: %v", err)
	}
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 100, 24
	a.profileDir = dir
	return a
}

// permScreen is the open panel as a reader sees it, blank lines dropped — the
// block is padded to the height the frame reserved, and the padding is not
// something a person reads.
func permScreen(a *app) string {
	out := make([]string, 0, permRowsMax)
	for _, line := range plainOverlay(a) {
		if strings.TrimSpace(line) != "" {
			out = append(out, strings.TrimRight(line, " "))
		}
	}
	return strings.Join(out, "\n")
}

// ── 1. the command ──────────────────────────────────────────────────────────

// /permissions IS ON THE LIST AND IN /help, which is one table read twice
// (commands.go).
func TestPermissionsIsOnTheCommandListAndInHelp(t *testing.T) {
	found := false
	for _, c := range commands {
		found = found || c.name == "permissions"
	}
	if !found {
		t.Fatal("/permissions is not on the command list")
	}
	help := helpText("", chordSpelling{})
	for _, want := range []string{"/permissions", permHeading, "also /perms"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help is missing %q:\n%s", want, help)
		}
	}
	// The word fingers type reaches the row, and it does not shadow anything.
	if got := canonicalCommand("perms"); got != "permissions" {
		t.Fatalf("/perms ran as /%s", got)
	}
	if err := checkCommands(commands); err != nil {
		t.Fatalf("the table stopped being a table: %v", err)
	}
}

// POSITION IN THE TABLE IS A CLAIM ABOUT FREQUENCY, and [menuRows] is what makes
// it cost something: a row added above /compact would push a command people
// reach for daily into a scroll. This is the law the new row was placed under,
// so it is the law that has to fail if somebody moves it back up.
func TestTheNewRowDidNotPushCompactIntoAScroll(t *testing.T) {
	at := -1
	for index, c := range commands {
		if c.name == "compact" {
			at = index
			break
		}
	}
	if at < 0 {
		t.Fatal("/compact left the table")
	}
	if at >= menuRows {
		t.Fatalf("/compact is row %d of the first %d — it is now behind a scroll", at+1, menuRows)
	}
}

// ── 2. what the panel draws ─────────────────────────────────────────────────

// TWO GLYPHS AND A NAME, under the one sentence this panel says.
func TestThePermissionsPanelDrawsWhatIsBanked(t *testing.T) {
	a := permApp(t, map[string]string{
		config.KeyBashApprovals: "allow git status*",
		config.KeyToolApprovals: "read:allow",
	})
	typeLine(t, a, "/permissions")
	if !a.permPanel.open {
		t.Fatal("/permissions opened nothing")
	}
	screen := permScreen(a)
	for _, want := range []string{
		permHeading,
		glyphPermBash + " git status*",
		glyphPermTool + " read",
		// The breadth of a tool row is the one fact the heading did not say.
		permToolWord,
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the panel does not say %q:\n%s", want, screen)
		}
	}
	// AN ALLOW ON A SHELL COMMAND CARRIES NO TAIL. The heading already said it,
	// and a column of rows all saying "allowed" is the heading once per line.
	for _, banned := range []string{"allowed", "allow "} {
		if strings.Contains(screen, banned) {
			t.Fatalf("the shell row repeats the heading with %q:\n%s", banned, screen)
		}
	}
}

// A ROW THE HEADING IS WRONG FOR SAYS SO ITSELF, rather than being filed under a
// word that does not fit it.
func TestARuleThatIsNotAnAllowCarriesItsOwnAnswer(t *testing.T) {
	a := permApp(t, map[string]string{
		config.KeyBashApprovals: "deny rm -rf *",
		config.KeyToolApprovals: "write:prompt",
	})
	typeLine(t, a, "/permissions")
	screen := permScreen(a)
	for _, want := range []string{permDenyWord, permPromptWord} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the panel does not say %q:\n%s", want, screen)
		}
	}
}

// THE EMPTINESS LAW: the heading, and nothing under it. Not "none", not "0
// rules", not a paragraph about what could be here.
func TestAnEmptyPermissionsPanelDrawsTheHeadingAndNothingElse(t *testing.T) {
	a := permApp(t, map[string]string{})
	typeLine(t, a, "/permissions")
	if !a.permPanel.open {
		t.Fatal("/permissions opened nothing with an empty profile")
	}
	screen := permScreen(a)
	if strings.TrimSpace(screen) != permHeading {
		t.Fatalf("an empty panel drew more than its heading:\n%q", screen)
	}
	if a.permPanel.height(a.width) != 1 {
		t.Fatalf("an empty panel wants %d lines, not one", a.permPanel.height(a.width))
	}
}

// THE SHELL ROW'S ORDER IS THE AUTHOR'S PRIORITY STATEMENT — first match wins —
// so the panel draws it in the order it was written and never sorted.
func TestTheShellRulesKeepTheOrderTheyWereWrittenIn(t *testing.T) {
	a := permApp(t, map[string]string{
		config.KeyBashApprovals: "deny git push*, allow git status*, allow ls*",
	})
	typeLine(t, a, "/permissions")
	screen := permScreen(a)
	first, second := strings.Index(screen, "git push*"), strings.Index(screen, "git status*")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("the rules were reordered:\n%s", screen)
	}
}

// ── 3. taking one back ──────────────────────────────────────────────────────

// ONE KEYSTROKE IS NOT ENOUGH OF A DECISION to throw away something a person
// deliberately banked, so the first press asks and the second one does it.
func TestDroppingARuleTakesTwoPresses(t *testing.T) {
	a := permApp(t, map[string]string{config.KeyBashApprovals: "allow git status*"})
	typeLine(t, a, "/permissions")

	drive(t, a, key("enter"))
	if !strings.Contains(permScreen(a), permArmWord) {
		t.Fatalf("the first press did not ask:\n%s", permScreen(a))
	}
	if got := config.BashApprovalsAt(a.profileDir); got != "allow git status*" {
		t.Fatalf("the first press already wrote the row: %q", got)
	}

	drive(t, a, key("enter"))
	if got := config.BashApprovalsAt(a.profileDir); got != "" {
		t.Fatalf("the second press left %q behind", got)
	}
	if len(a.permPanel.rows) != 0 {
		t.Fatalf("the panel still lists %d rules", len(a.permPanel.rows))
	}
	if !strings.Contains(lastNote(t, a), droppedWord+"git status*") {
		t.Fatalf("no receipt for the drop: %q", lastNote(t, a))
	}
}

// ESC UNDOES ONE THING AT A TIME, nearest first: the question standing on a row,
// and only then the panel.
func TestEscTakesBackTheQuestionBeforeItTakesThePanel(t *testing.T) {
	a := permApp(t, map[string]string{config.KeyBashApprovals: "allow git status*"})
	typeLine(t, a, "/permissions")
	drive(t, a, key("enter"))
	drive(t, a, key("esc"))
	if !a.permPanel.open {
		t.Fatal("esc closed the panel while a question was still standing on a row")
	}
	if strings.Contains(permScreen(a), permArmWord) {
		t.Fatal("esc left the question standing")
	}
	drive(t, a, key("esc"))
	if a.permPanel.open {
		t.Fatal("the second esc did not close the panel")
	}
}

// d IS THE WORD FOR THE SAME ACT, and it is free here because nothing on this
// panel is typed into.
func TestDDropsTheLineUnderTheCursor(t *testing.T) {
	a := permApp(t, map[string]string{config.KeyToolApprovals: "read:allow, write:allow"})
	typeLine(t, a, "/permissions")
	drive(t, a, key("down")) // read, write — sorted, so this is write
	drive(t, a, key("d"), key("d"))
	if got := config.ToolApprovalsAt(a.profileDir); got != "read:allow" {
		t.Fatalf("dropping write left %q", got)
	}
}

// THE LINE THAT GOES IS THE LINE UNDER THE CURSOR and never the one that merely
// looks like it. The shell row is an ORDERED list and two lines may carry the
// same glob, so a drop that matched on text would take whichever one it met
// first.
func TestADropTakesTheLineItWasPointedAt(t *testing.T) {
	a := permApp(t, map[string]string{
		config.KeyBashApprovals: "allow git status*, deny git push*, allow ls*",
	})
	typeLine(t, a, "/permissions")
	drive(t, a, key("down"))
	drive(t, a, key("enter"), key("enter"))
	if got := config.BashApprovalsAt(a.profileDir); got != "allow git status*, allow ls*" {
		t.Fatalf("the wrong line went: %q", got)
	}
}

// A WALK IS NOT A DECISION: moving off an armed row takes the question with it.
func TestMovingOffAnArmedRowDisarmsIt(t *testing.T) {
	a := permApp(t, map[string]string{config.KeyBashApprovals: "allow git status*, allow ls*"})
	typeLine(t, a, "/permissions")
	drive(t, a, key("enter"))
	drive(t, a, key("down"))
	drive(t, a, key("enter"))
	if got := config.BashApprovalsAt(a.profileDir); got != "allow git status*, allow ls*" {
		t.Fatalf("a walk between the two presses still dropped something: %q", got)
	}
}

// ── 4. the live seam ────────────────────────────────────────────────────────

// A SURFACE THAT CANNOT REACH THE RUNNING GATE SAYS THE NARROWER TRUTH rather
// than claiming an effect it did not deliver.
func TestADropWithNoLiveSeamPromisesOnlyTheNextSession(t *testing.T) {
	a := permApp(t, map[string]string{config.KeyBashApprovals: "allow git status*"})
	typeLine(t, a, "/permissions")
	drive(t, a, key("enter"), key("enter"))
	if !strings.Contains(lastNote(t, a), nextSessionWord) {
		t.Fatalf("a surface with no seam over-promised: %q", lastNote(t, a))
	}
}

// AND A SURFACE THAT CAN REACH IT TELLS IT, and then says the whole truth.
func TestADropTellsTheRunningGate(t *testing.T) {
	a := permApp(t, map[string]string{config.KeyBashApprovals: "allow git status*"})
	told := 0
	a.applyApprovals = func() error {
		told++
		return nil
	}
	typeLine(t, a, "/permissions")
	drive(t, a, key("enter"), key("enter"))
	if told != 1 {
		t.Fatalf("the running gate was told %d times", told)
	}
	if strings.Contains(lastNote(t, a), nextSessionWord) {
		t.Fatalf("a seam that landed still promised only the next session: %q", lastNote(t, a))
	}
}

// A SEAM THAT REFUSED IS THE NIL SEAM AGAIN: the line is out of the config
// either way, and what the receipt may not do is claim the running conversation
// heard about it.
func TestASeamThatRefusedFallsBackToTheNarrowerTruth(t *testing.T) {
	a := permApp(t, map[string]string{config.KeyBashApprovals: "allow git status*"})
	a.applyApprovals = func() error { return os.ErrPermission }
	typeLine(t, a, "/permissions")
	drive(t, a, key("enter"), key("enter"))
	if got := config.BashApprovalsAt(a.profileDir); got != "" {
		t.Fatalf("a refused seam undid the write: %q", got)
	}
	if !strings.Contains(lastNote(t, a), nextSessionWord) {
		t.Fatalf("a refused seam over-promised: %q", lastNote(t, a))
	}
}

// ── 5. a row that does not read back ────────────────────────────────────────

// "THERE IS NOTHING HERE" AND "I CANNOT READ WHAT IS HERE" ARE DIFFERENT FACTS.
// The broken row is reported in the transcript, where prose lives, and the panel
// stays a list of whatever it could read.
func TestARowThatDoesNotReadBackIsSaidOutLoud(t *testing.T) {
	a := permApp(t, map[string]string{
		config.KeyBashApprovals: "sometimes git status",
		config.KeyToolApprovals: "read:allow",
	})
	typeLine(t, a, "/permissions")
	if !strings.Contains(lastNote(t, a), badBashRowWord) {
		t.Fatalf("the broken row was swallowed: %q", lastNote(t, a))
	}
	if !strings.Contains(permScreen(a), glyphPermTool+" read") {
		t.Fatalf("the readable row went with the broken one:\n%s", permScreen(a))
	}
}
