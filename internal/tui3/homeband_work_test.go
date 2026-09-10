package tui3

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// workLab is one conversation with `tasks` pieces of work behind it, the newest
// first, each with a name and an outcome — which is the shape the work band is
// about. The cursor is left on that conversation.
//
// THE FRAME IS [homeCardMin] WIDE BECAUSE THERE IS NO CARD BELOW IT. Home is one
// flat list at every ordinary width and the row's own note carries the fact the
// card was for (SCREEN 1a); the card exists only where the width is genuinely
// spare (homebridge.go's ladder). A test whose subject is the CARD therefore has
// to be asked at a width where a card is drawn at all, and a hundred and sixty
// cells is where the list has everything it wants AND one still fits.
//
// homeCardWidest, just below, is the other end of the same ladder.
//
// AND THE ≥160 CARD NO LONGER COMPOSES THE REGISTERED `work` BAND. The design's
// five bands spell the card's work rows themselves — `✓ label   $1.63`, with the
// fold naming the tasks place (place_home.go's [app.homeCardWork], FIDELITY.md
// item 8) — so a test whose subject is the BAND asks [workSheet] instead, and a
// test whose subject is the CARD asks this.
func workLab(t *testing.T, tasks int) *app { return workLabMade(t, tasks, 0) }

// workLabMade is [workLab] with `files` deliverables recorded against the same
// conversation, for the tests whose subject is a FOLD ON THE CARD.
//
// THE CARD'S WORK FOLD IS NOT A CONTROL ANY MORE. `▸ N more tasks` names the
// tasks place in the right margin and there is nothing to click open (SCREEN
// 1d), so the one band left folding on that card is `made for you` — which is
// [app.homeCardMade] drawing the registered `deliverables` band whole, fold and
// all. The fold MECHANISM tests therefore aim at files rather than at tasks; the
// gesture, the code path ([app.bandFoldAt]) and the laws around them are the
// ones they always were.
func workLabMade(t *testing.T, tasks, files int) *app {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the working one", "/tmp/alpha", now)
	for i := 0; i < tasks; i++ {
		lab.task("-tmp-alpha", session.TaskIndexEntry{
			ID: strconv.Itoa(i), Name: "task-" + strconv.Itoa(i),
			Label: "Task " + strconv.Itoa(i), Title: "Task " + strconv.Itoa(i),
			Status: string(session.TaskDone), Outcome: "outcome " + strconv.Itoa(i),
			FilesChanged: 14, EndedAt: now.Add(-time.Duration(i+1) * time.Hour),
			SessionID: "aaaa000000000001",
		})
	}
	a := lab.app(mine)
	if files > 0 {
		// The index is written through [session.RecordArtifact], which is the only
		// thing that ever writes one, so the band reads what a real run leaves.
		dir := t.TempDir()
		index := filepath.Join(dir, session.ArtifactsIndexName)
		for i := 0; i < files; i++ {
			path := filepath.Join(dir, "file-"+strconv.Itoa(i)+".md")
			if err := os.WriteFile(path, []byte("made"), 0o600); err != nil {
				t.Fatal(err)
			}
			session.RecordArtifact(index, session.Artifact{
				Path: path, Session: "aaaa000000000001", Title: "file " + strconv.Itoa(i),
				Created: now.Add(-time.Duration(i+1) * time.Hour),
			})
		}
		a.artifacts = index
	}
	a.width, a.height = homeCardMin, 40
	a.openHome()
	a.home.point(mine)
	return a
}

// workSheet is [workLab]'s conversation on a PHONE, with its sheet open — which
// is where the registered `work` band still draws whole
// ([app.homeSheetBands]). The rows come back with the sheet's own one-cell
// margin taken off, so the band's own indent law reads exactly as it does on a
// card.
//
// THE BAND IS UNCHANGED AND STILL REGISTERED; only the ≥160 conversation card
// stopped asking for it. A test about the band's SHAPE therefore belongs on the
// surface that still draws that shape, and this is it.
func workSheet(t *testing.T, tasks int) []string {
	t.Helper()
	a := workLab(t, tasks)
	a.width, a.height = 50, 40
	a.openHome()
	a.home.point(a.file)
	a.openHomeSheet()
	if !a.homeSheetShowing() {
		t.Fatalf("the sheet is not up on a %d-column frame:\n%s", a.width, phoneText(a))
	}
	var rows []string
	for _, row := range phoneRows(a) {
		rows = append(rows, strings.TrimRight(strings.TrimPrefix(row, " "), " "))
	}
	return rows
}

// homeCardWidest is a frame wide enough for the card to reach [homeCardCap] —
// the width past which its sentences have all the room they will ever ask for.
// The card takes half of every cell past [homeCardMin] ([homeColumns]), so twice
// the distance between the floor and the cap is what buys the last of them.
const homeCardWidest = homeCardMin + 2*(homeCardCap-homeCardCol)

// workCard is the right pane as plain rows.
func workCard(t *testing.T, a *app) []string {
	t.Helper()
	width, _ := a.size()
	_, right := homeColumns(width)
	if right <= 0 {
		t.Fatalf("a %d-column frame lent the card nothing", width)
	}
	var rows []string
	for _, line := range a.homeDetail(right, 30, a.pal) {
		rows = append(rows, strings.TrimRight(ansi.Strip(line), " "))
	}
	return rows
}

func workRowAt(t *testing.T, rows []string, want string) int {
	t.Helper()
	for at, row := range rows {
		if strings.Contains(row, want) {
			return at
		}
	}
	t.Fatalf("the card has no row holding %q:\n%s", want, strings.Join(rows, "\n"))
	return -1
}

// THE SHAPE ITSELF: the name on its own line, what it came to indented under it,
// and a blank before the next task.
//
// It is asked of the phone sheet because that is where the band draws now
// ([workSheet] says why). Nothing about the claim changed — the band was not
// touched — only the surface it is asked on.
func TestTheWorkBandIsTwoLinesAndABlank(t *testing.T) {
	rows := workSheet(t, 2)
	first := workRowAt(t, rows, "Task 0")
	if !strings.HasPrefix(rows[first+1], strings.Repeat(" ", homeWorkIndent)+"outcome 0") {
		t.Fatalf("the outcome does not hang under the name:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[first+1], "14 files") {
		t.Fatalf("the outcome line lost the file count:\n%s", strings.Join(rows, "\n"))
	}
	if rows[first+2] != "" {
		t.Fatalf("there is no blank between two tasks:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[first+3], "Task 1") {
		t.Fatalf("the second task does not follow the blank:\n%s", strings.Join(rows, "\n"))
	}
	// AND NONE AFTER THE LAST. The band ends on the second task's own sentence,
	// and whatever comes next is another band with the card's own blank between.
	last := workRowAt(t, rows, "Task 1")
	if rows[last+1] == "" {
		t.Fatalf("the band drew a blank after its last task:\n%s", strings.Join(rows, "\n"))
	}
}

func TestTheNarrowWorkBandKeepsFilesAndCost(t *testing.T) {
	a := newTestApp(nil)
	now := time.Now()
	row := session.SessionRow{Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{{
		Label: "A long named task", Status: string(session.TaskDone), Outcome: "the migration landed cleanly",
		FilesChanged: 14, Cost: .12, EndedAt: now.Add(-time.Hour),
	}}}}
	ctx := ambientBandContext(a, row, now, 30)
	rows := drawWorkBand(a, ctx)
	assertNarrowRows(t, "work", rows, 30, "$0.12")
	assertNarrowRows(t, "work", rows, 30, "14 files")
}

// THE RIGHT EDGE IS A LANDING AGE AND NOTHING ELSE. A run still going has no
// ending to measure from, while the same run draws the age once its closing row
// supplies one.
func TestARunningRunLeavesTheWorkBandsRightEdgeEmpty(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	a := newTestApp(nil)
	entry := session.TaskIndexEntry{
		Label: "Audit the pricing code", Kind: session.TaskKindAdaptive,
		Status: string(session.TaskRunning),
	}
	if line := plain(strings.Join(homeWorkName(entry, 40, now, a.pal), "\n")); line != entry.Label {
		t.Fatalf("the running run's name row is %q, want no right-edge ending", line)
	}

	entry.Status = string(session.TaskDone)
	entry.EndedAt = now.Add(-2 * time.Hour)
	if line := plain(strings.Join(homeWorkName(entry, 40, now, a.pal), "\n")); !strings.HasSuffix(line, "2h") {
		t.Fatalf("the landed run's name row is %q, want its age at the right edge", line)
	}
}

// DONE IS THE ABSENCE OF A MARK (D11), ON THE BAND. No tick, and no `done`
// either — the word was the loudest thing on every row and it never said
// anything.
//
// This is the half of the old law that survived. It held for every surface until
// the fidelity wave; it holds now for the band, which the phone sheet draws
// unchanged, and [TestADoneTaskOnTheCardWearsTheDesignsTick] holds the half that
// did not.
func TestADoneTaskOnTheWorkBandWearsNoMark(t *testing.T) {
	band := strings.Join(workSheet(t, 2), "\n")
	for _, banned := range []string{glyphDone, doneWord + " Task", "done  "} {
		if strings.Contains(band, banned) {
			t.Fatalf("the band draws %q on a landed task:\n%s", banned, band)
		}
	}
}

// EVERY OTHER STATE LEADS THE SENTENCE, with home's own glyph in front of it.
func TestATaskThatIsNotDoneLeadsWithItsState(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the working one", "/tmp/alpha", now)
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "look", Label: "Look At This", Title: "Look At This",
		Status: string(session.TaskUnverified), Outcome: "nobody could judge it",
		EndedAt: now.Add(-time.Hour), SessionID: "aaaa000000000001",
	})
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "2", Name: "broke", Label: "Broke It", Title: "Broke It",
		Status: string(session.TaskFailed), Outcome: "the build would not run",
		EndedAt: now.Add(-2 * time.Hour), SessionID: "aaaa000000000001",
	})
	a := lab.app(mine)
	// THE SUBJECT HERE IS A WHOLE SENTENCE, so it is asked at the width where the
	// card has all the room it will ever ask for. At [homeCardMin] the card is
	// exactly [homeCardCol] cells and `▲ your call · nobody could judge it`
	// is longer than that — which would be a test about clipping wearing the
	// clothes of a test about wording.
	a.width, a.height = homeCardWidest, 40
	a.openHome()
	a.home.point(mine)
	rows := workCard(t, a)
	card := strings.Join(rows, "\n")
	for _, want := range []string{
		homeAskGlyph + " " + tierYourCallWord + " · nobody could judge it",
		glyphBad + " " + taskRecordStoppedWord + " · the build would not run",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card does not say %q:\n%s", want, card)
		}
	}
	// The name is still the first line of each — the state is UNDER it.
	at := workRowAt(t, rows, "Look At This")
	if strings.Contains(rows[at], tierYourCallWord) {
		t.Fatalf("the state landed on the name's line:\n%s", card)
	}
}

// THE FOLD CUTS AT A TASK BOUNDARY AND NEVER THROUGH ONE. Three tasks is three
// names with three sentences under them, and the line that says so.
//
// Asked of the sheet for [workSheet]'s reason: the band is where this claim
// lives, and the card's own cut is [TestTheCardsWorkFoldNamesTheTasksPlace].
func TestTheWorkBandFoldsAtThreeTasks(t *testing.T) {
	rows := workSheet(t, 6)
	band := strings.Join(rows, "\n")
	for i := 0; i < homeWorkTasks; i++ {
		if !strings.Contains(band, "Task "+strconv.Itoa(i)) {
			t.Fatalf("the band dropped task %d:\n%s", i, band)
		}
	}
	if strings.Contains(band, "Task "+strconv.Itoa(homeWorkTasks)) {
		t.Fatalf("the band drew a fourth task:\n%s", band)
	}
	fold := workRowAt(t, rows, "…3 more tasks")
	if !strings.HasPrefix(strings.TrimSpace(rows[fold]), bandFoldGlyph) {
		t.Fatalf("the fold line wears no fold mark:\n%s", band)
	}
	// THE CUT IS AT THE BOUNDARY: the row above the fold line is the third
	// task's own sentence, not a name left hanging with nothing under it.
	if !strings.Contains(rows[fold-1], "outcome "+strconv.Itoa(homeWorkTasks-1)) {
		t.Fatalf("the fold cut through a task:\n%s", band)
	}
}

// ── THE POINTER ON THE CARD ─────────────────────────────────────────────────
//
// A DOOR THAT LOOKS EXACTLY LIKE THE PROSE AROUND IT IS A DOOR NOBODY FINDS. The
// card's work rows opened a record from the day above, and the card lit nothing
// at all under the pointer — neither its task rows nor its fold lines — so the
// only way to learn the gesture was to be told it. These pin the other half:
// what a press acts on is what LIGHTS, in the app's own hover ink, and what
// answers nothing stays plain (carddoors.go).

// cardHoverInk is what a row wears when the pointer is on it: THE GROUND
// LADDER's cursor step ([palette.cursor]), on the PLACE ladder home paints its
// own frame from (home.go's [app.homeFrame] swaps to it before it draws). The
// suite's terminal is ANSI256, which is the branch of [palette.background] that
// spells the step as one indexed background.
func cardHoverInk(a *app) string {
	return "\x1b[48;5;" + strconv.Itoa(int(a.pal.onPlaces().ramp.cursor.idx)) + "m"
}

// cardRowY is the screen row the frame drew a card line holding `want` on.
func cardRowY(t *testing.T, a *app, want string) int {
	t.Helper()
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	at := -1
	for y, line := range lines {
		if strings.Contains(ansi.Strip(line), want) {
			at = y
		}
	}
	if at < 0 {
		t.Fatalf("the frame holds no line saying %q:\n%s", want, homeText(a))
	}
	return at
}

// framePaint is one screen row exactly as it is painted, colour and all — which
// is the only reading that can say whether a row is lit.
func framePaint(t *testing.T, a *app, y int) string {
	t.Helper()
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	if y < 0 || y >= len(lines) {
		t.Fatalf("row %d is not on a %d-row frame", y, len(lines))
	}
	return lines[y]
}

// pointAtCard moves the pointer to a screen row of the right column, through the
// real message the terminal sends — folded by the coalescer exactly as a live
// sweep is, and spent at the frame boundary [pointerMsg] stands for
// (coalesce.go).
func pointAtCard(t *testing.T, a *app, y int) {
	t.Helper()
	width, _ := a.size()
	left, right := homeColumns(width)
	if right <= 0 {
		t.Fatalf("a %d-column frame lent the card nothing", width)
	}
	drive(t, a, tea.MouseMotionMsg{X: left + homeGutter + 2, Y: y}, pointerMsg{})
}
