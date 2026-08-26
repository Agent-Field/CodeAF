package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The deliverables picker.
//
// Everything here drives the real surface against a real index and real files
// on a real disk: the whole job of this list is to say what was made and where
// it went, and to move one of them, so a test that stubbed the filesystem would
// be a test of the stub.

// madeIndex writes an index holding rows and answers its path. The rows are
// written through [session.RecordArtifact], which is the only thing that ever
// writes this file in the product.
func madeIndex(t *testing.T, rows ...session.Artifact) string {
	t.Helper()
	index := filepath.Join(t.TempDir(), session.ArtifactsIndexName)
	for _, row := range rows {
		session.RecordArtifact(index, row)
	}
	return index
}

// madeFile puts a real file on the disk and answers its path.
func madeFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

// madeApp is a surface over an index.
func madeApp(t *testing.T, index string) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 100, 24
	a.artifacts = index
	return a
}

// madeScreen is the open list as a reader sees it, blank lines dropped.
func madeScreen(a *app) string {
	out := make([]string, 0, filesRows)
	for _, line := range plainOverlay(a) {
		if strings.TrimSpace(line) != "" {
			out = append(out, strings.TrimRight(line, " "))
		}
	}
	return strings.Join(out, "\n")
}

// watchOpener takes the platform handoff over for one test and answers what it
// was asked to open (opener.go's seam, the same one the sign-in tests take).
func watchOpener(t *testing.T) *[]string {
	t.Helper()
	opened := make([]string, 0, 2)
	was := processOpener
	processOpener = func(target string) error {
		opened = append(opened, target)
		return nil
	}
	t.Cleanup(func() { processOpener = was })
	return &opened
}

// ── 1. the command ──────────────────────────────────────────────────────────

// /files IS ON THE LIST AND IN /help, which is one table read twice
// (commands.go).
func TestFilesIsOnTheCommandListAndInHelp(t *testing.T) {
	found := false
	for _, c := range commands {
		found = found || c.name == "files"
	}
	if !found {
		t.Fatal("/files is not on the command list")
	}
	if !strings.Contains(helpText("", chordSpelling{}), "/files") {
		t.Fatal("/files is not in /help")
	}
	if err := checkCommands(commands); err != nil {
		t.Fatalf("the table stopped being a table: %v", err)
	}
}

// POSITION IN THE TABLE IS A CLAIM ABOUT FREQUENCY, and the new row must not
// have pushed the command people reach for daily behind a scroll — the law the
// permissions row was placed under, restated for this one.
func TestTheFilesRowDidNotPushCompactIntoAScroll(t *testing.T) {
	for index, c := range commands {
		if c.name == "compact" {
			if index >= menuRows {
				t.Fatalf("/compact is row %d of the first %d — it is now behind a scroll", index+1, menuRows)
			}
			return
		}
	}
	t.Fatal("/compact left the table")
}

// ── 2. what the list draws ──────────────────────────────────────────────────

// A ROW IS A MARK, A TITLE, WHERE IT WENT AND HOW LONG AGO — and a picture is
// marked differently from everything else.
func TestTheFilesListDrawsWhatWasMade(t *testing.T) {
	report := madeFile(t, "report.md", "# tuesday")
	chart := madeFile(t, "chart.png", "\x89PNG")
	index := madeIndex(t,
		session.Artifact{Path: chart, Title: "the sales chart", Kind: "image", Created: time.Now().Add(-3 * time.Hour)},
		session.Artifact{Path: report, Title: "the tuesday report", Kind: "export", Created: time.Now().Add(-2 * time.Hour)},
	)
	a := madeApp(t, index)
	typeLine(t, a, "/files")
	if !a.shelf.open {
		t.Fatal("/files opened nothing")
	}
	screen := madeScreen(a)
	for _, want := range []string{
		glyphMadeFile + " the tuesday report",
		glyphMadePicture + " the sales chart",
		"report.md",
		"2h ago",
		"3h ago",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the list does not say %q:\n%s", want, screen)
		}
	}
	// NEWEST FIRST, and the cursor opens on it.
	row, ok := a.shelf.choice()
	if !ok || row.title != "the tuesday report" {
		t.Fatalf("the cursor opened on %q, want the newest", row.title)
	}
}

// THE SAME PATH RECORDED TWICE IS ONE ROW, and the newest of the two is the one
// that is drawn: the index is append-only, so a document exported again is two
// rows about one file.
func TestAFileRecordedTwiceIsOneRow(t *testing.T) {
	report := madeFile(t, "report.md", "# tuesday")
	index := madeIndex(t,
		session.Artifact{Path: report, Title: "the first name", Created: time.Now().Add(-time.Hour)},
		session.Artifact{Path: report, Title: "the second name", Created: time.Now()},
	)
	list := readDeliverables(index)
	if len(list) != 1 {
		t.Fatalf("one file made %d rows", len(list))
	}
	if list[0].title != "the second name" {
		t.Fatalf("the row says %q, want the newest record", list[0].title)
	}
}

// TYPING NARROWS THE LIST BY TITLE, on the picker's own ladder — every token
// must match, and a subsequence is a match.
func TestTheFilterNarrowsTheFilesList(t *testing.T) {
	first := madeFile(t, "one.md", "one")
	second := madeFile(t, "two.md", "two")
	index := madeIndex(t,
		session.Artifact{Path: first, Title: "the migration notes", Created: time.Now().Add(-time.Hour)},
		session.Artifact{Path: second, Title: "the tuesday report", Created: time.Now()},
	)
	a := madeApp(t, index)
	typeLine(t, a, "/files")
	for _, r := range "migr" {
		drive(t, a, key(string(r)))
	}
	if got := len(a.shelf.hits); got != 1 {
		t.Fatalf("%d rows matched \"migr\", want 1:\n%s", got, madeScreen(a))
	}
	row, _ := a.shelf.choice()
	if row.title != "the migration notes" {
		t.Fatalf("the filter left %q standing", row.title)
	}
	// A subsequence of the title is a match, the way it is on every other list.
	drive(t, a, key("ctrl+u"))
	for _, r := range "tsdy" {
		drive(t, a, key(string(r)))
	}
	row, ok := a.shelf.choice()
	if !ok || row.title != "the tuesday report" {
		t.Fatalf("a subsequence found %q:\n%s", row.title, madeScreen(a))
	}
	// AND ESC UNDOES THE QUERY BEFORE IT UNDOES THE LIST.
	drive(t, a, key("esc"))
	if !a.shelf.open {
		t.Fatal("esc closed the list while a filter was still on it")
	}
	if got := len(a.shelf.hits); got != 2 {
		t.Fatalf("the cleared filter left %d rows, want 2", got)
	}
	drive(t, a, key("esc"))
	if a.shelf.open {
		t.Fatal("the second esc did not close the list")
	}
}

// ── 3. the three verbs ──────────────────────────────────────────────────────

// ENTER OPENS THE FILE AND ctrl+r OPENS THE FOLDER IT IS IN, both handed to the
// platform the way a sign-in link is (opener.go).
func TestEnterOpensAFileAndTheChordOpensItsFolder(t *testing.T) {
	report := madeFile(t, "report.md", "# tuesday")
	index := madeIndex(t, session.Artifact{Path: report, Title: "the tuesday report", Created: time.Now()})
	opened := watchOpener(t)

	a := madeApp(t, index)
	typeLine(t, a, "/files")
	drive(t, a, key(filesRevealKey))
	// The list stays open under a verb that did not close it, so the second
	// press is on the same row rather than on a list opened again.
	drive(t, a, key("enter"))

	want := []string{filepath.Dir(report), report}
	if got := *opened; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("the platform was asked to open %v, want %v", got, want)
	}
}

// A ROW WHOSE FILE IS GONE SAYS SO AND OFFERS NOTHING. The row keeps its place —
// "I did make that, and it is not there any more" is the answer somebody came
// for — and all three verbs step over it.
func TestAGoneFileIsInert(t *testing.T) {
	report := madeFile(t, "report.md", "# tuesday")
	index := madeIndex(t, session.Artifact{Path: report, Title: "the tuesday report", Created: time.Now()})
	if err := os.Remove(report); err != nil {
		t.Fatalf("removing the file: %v", err)
	}
	opened := watchOpener(t)

	a := madeApp(t, index)
	typeLine(t, a, "/files")
	if !a.shelf.open {
		t.Fatal("a list of gone files did not open — the row is the answer")
	}
	if screen := madeScreen(a); !strings.Contains(screen, filesGoneWord) {
		t.Fatalf("the row does not say it is gone:\n%s", screen)
	}
	// The path and the age were promises about a file somebody can open.
	if screen := madeScreen(a); strings.Contains(screen, "report.md") {
		t.Fatalf("a gone row still points at a path:\n%s", screen)
	}
	drive(t, a, key("enter"))
	drive(t, a, key(filesRevealKey))
	if got := *opened; len(got) != 0 {
		t.Fatalf("a gone row opened %v", got)
	}
	drive(t, a, key(filesCopyKey))
	if a.shelf.dest != nil {
		t.Fatal("a gone row offered to be copied")
	}
}

// COPYING IS THE PROMOTION: the file lands where it was asked for, under the
// name it already had, and the original stays exactly where it was.
func TestCopyingLandsWhereItWasAskedAndLeavesTheOriginal(t *testing.T) {
	report := madeFile(t, "report.md", "# tuesday")
	index := madeIndex(t, session.Artifact{Path: report, Title: "the tuesday report", Created: time.Now()})
	keep := t.TempDir()

	a := madeApp(t, index)
	typeLine(t, a, "/files")
	drive(t, a, key(filesCopyKey))
	if a.shelf.dest == nil {
		t.Fatal("the copy key opened no box")
	}
	typeLine(t, a, keep)

	landed := filepath.Join(keep, "report.md")
	body, err := os.ReadFile(landed)
	if err != nil {
		t.Fatalf("the copy is not at %s: %v", landed, err)
	}
	if string(body) != "# tuesday" {
		t.Fatalf("the copy reads %q", body)
	}
	if _, err := os.Stat(report); err != nil {
		t.Fatalf("the original moved: %v", err)
	}
	if note := lastNote(t, a); !strings.Contains(note, filesCopiedWord) {
		t.Fatalf("the copy said %q", note)
	}
	// The list is put away by the copy: the errand it was opened for is done.
	if a.shelf.open {
		t.Fatal("the list stayed open after the copy")
	}
}

// A COPY NEVER OVERWRITES. What is already there was put there by somebody, and
// the filesystem decides — O_EXCL — rather than a check that could go stale.
func TestCopyingRefusesToOverwrite(t *testing.T) {
	report := madeFile(t, "report.md", "# tuesday")
	index := madeIndex(t, session.Artifact{Path: report, Title: "the tuesday report", Created: time.Now()})
	keep := t.TempDir()
	standing := filepath.Join(keep, "report.md")
	if err := os.WriteFile(standing, []byte("something else"), 0o600); err != nil {
		t.Fatalf("writing the file already there: %v", err)
	}

	a := madeApp(t, index)
	typeLine(t, a, "/files")
	drive(t, a, key(filesCopyKey))
	typeLine(t, a, keep)

	if note := lastNote(t, a); !strings.Contains(note, filesThereWord) {
		t.Fatalf("the refusal reads %q", note)
	}
	body, err := os.ReadFile(standing)
	if err != nil || string(body) != "something else" {
		t.Fatalf("the file already there was written over: %q, %v", body, err)
	}
}

// ESC BACKS OUT OF THE BOX AND NOT OUT OF THE LIST: nothing is waiting on that
// box, so declining it declines nothing.
func TestEscapingTheCopyBoxKeepsTheList(t *testing.T) {
	report := madeFile(t, "report.md", "# tuesday")
	index := madeIndex(t, session.Artifact{Path: report, Title: "the tuesday report", Created: time.Now()})
	a := madeApp(t, index)
	typeLine(t, a, "/files")
	drive(t, a, key(filesCopyKey))
	drive(t, a, key("esc"))
	if a.shelf.dest != nil {
		t.Fatal("esc left the box open")
	}
	if !a.shelf.open {
		t.Fatal("esc closed the list under the box")
	}
}

// ── 4. the empty answer ─────────────────────────────────────────────────────

// A PERSON WHO HAS MADE NOTHING GETS ONE SENTENCE AND NO OVERLAY. A modal list
// with no rows is a trap that has to be dismissed before it can be told it was
// useless.
func TestFilesOnAnEmptyIndexAnswersOneQuietLine(t *testing.T) {
	a := madeApp(t, filepath.Join(t.TempDir(), session.ArtifactsIndexName))
	typeLine(t, a, "/files")
	if a.shelf.open {
		t.Fatal("/files opened a list with nothing in it")
	}
	if note := lastNote(t, a); note != filesNothingWord {
		t.Fatalf("the answer reads %q, want %q", note, filesNothingWord)
	}
}
