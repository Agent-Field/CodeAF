package tui3

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// workLab is one conversation with `tasks` pieces of work behind it, the newest
// first, each with a name and an outcome — which is the shape the work band is
// about. The cursor is left on that conversation.
//
// THE FRAME IS [homeCardMin] WIDE BECAUSE THERE IS NO CARD BELOW IT. Home is one
// flat list at every ordinary width and the row's own note carries the fact the
// card was for (SCREEN 1a); the card exists only where the width is genuinely
// spare (homebridge.go's ladder). A test whose subject is a BAND therefore has
// to be asked at a width where a card is drawn at all, and a hundred and sixty
// cells is where the list has everything it wants AND one still fits.
//
// homeCardWidest, just below, is the other end of the same ladder.
func workLab(t *testing.T, tasks int) *app {
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
	a.width, a.height = homeCardMin, 40
	a.openHome()
	a.home.point(mine)
	return a
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
func TestTheWorkBandIsTwoLinesAndABlank(t *testing.T) {
	a := workLab(t, 2)
	rows := workCard(t, a)
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

// DONE IS THE ABSENCE OF A MARK (D11). No tick, and no `done` either — the word
// was the loudest thing on every row and it never said anything.
func TestADoneTaskWearsNoTickAndNoWord(t *testing.T) {
	a := workLab(t, 2)
	card := strings.Join(workCard(t, a), "\n")
	for _, banned := range []string{glyphDone, doneWord + " Task", "done  "} {
		if strings.Contains(card, banned) {
			t.Fatalf("the card draws %q on a landed task:\n%s", banned, card)
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
	// exactly [homeCardCol] cells and `▲ needs your look · nobody could judge it`
	// is longer than that — which would be a test about clipping wearing the
	// clothes of a test about wording.
	a.width, a.height = homeCardWidest, 40
	a.openHome()
	a.home.point(mine)
	rows := workCard(t, a)
	card := strings.Join(rows, "\n")
	for _, want := range []string{
		homeAskGlyph + " " + taskUnverifiedWord + " · nobody could judge it",
		glyphBad + " " + doneFailWord + " · the build would not run",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card does not say %q:\n%s", want, card)
		}
	}
	// The name is still the first line of each — the state is UNDER it.
	at := workRowAt(t, rows, "Look At This")
	if strings.Contains(rows[at], taskUnverifiedWord) {
		t.Fatalf("the state landed on the name's line:\n%s", card)
	}
}

// THE FOLD CUTS AT A TASK BOUNDARY AND NEVER THROUGH ONE. Three tasks is three
// names with three sentences under them, and the line that says so.
func TestTheWorkBandFoldsAtThreeTasks(t *testing.T) {
	a := workLab(t, 6)
	rows := workCard(t, a)
	card := strings.Join(rows, "\n")
	for i := 0; i < homeWorkTasks; i++ {
		if !strings.Contains(card, "Task "+strconv.Itoa(i)) {
			t.Fatalf("the band dropped task %d:\n%s", i, card)
		}
	}
	if strings.Contains(card, "Task "+strconv.Itoa(homeWorkTasks)) {
		t.Fatalf("the band drew a fourth task:\n%s", card)
	}
	fold := workRowAt(t, rows, "…3 more tasks")
	if !strings.HasPrefix(strings.TrimSpace(rows[fold]), bandFoldGlyph) {
		t.Fatalf("the fold line wears no fold mark:\n%s", card)
	}
	// THE CUT IS AT THE BOUNDARY: the row above the fold line is the third
	// task's own sentence, not a name left hanging with nothing under it.
	if !strings.Contains(rows[fold-1], "outcome "+strconv.Itoa(homeWorkTasks-1)) {
		t.Fatalf("the fold cut through a task:\n%s", card)
	}
}

// A CLICK ON THE FOLD LINE OPENS THAT BAND, and a second folds it again. It is
// the one gesture that acts on ONE band — `m` acts on the whole card.
func TestClickingTheWorkFoldLineTogglesIt(t *testing.T) {
	a := workLab(t, 6)
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	left, _ := homeColumns(width)
	row := -1
	for y, line := range lines {
		if strings.Contains(ansi.Strip(line), "…3 more tasks") {
			row = y
		}
	}
	if row < 0 {
		t.Fatalf("the fold line is not on the frame:\n%s", homeText(a))
	}
	a.homePress(left+homeGutter+2, row)
	if strings.Contains(homeText(a), "…3 more tasks") {
		t.Fatalf("the click did not open the band:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), "Task 5") {
		t.Fatalf("the opened band does not draw what it was hiding:\n%s", homeText(a))
	}
	lines, _, _, _ = a.homeFrame(width, height)
	for y, line := range lines {
		if strings.Contains(ansi.Strip(line), "…3 fewer") {
			row = y
		}
	}
	a.homePress(left+homeGutter+2, row)
	if !strings.Contains(homeText(a), "…3 more tasks") {
		t.Fatalf("the second click did not fold it back:\n%s", homeText(a))
	}
	// AND THE CURSOR NEVER MOVED. A press in the right pane acts on the card,
	// not on the list across the gutter.
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeSession {
		t.Fatalf("a click on the card moved the list's cursor (kind %v)", line.kind)
	}
}

// `→` IS THE VERB STRIP'S FIRST, AND THE CARD'S FOLD LADDER ONLY WHERE THE ROW
// HAS NO VERBS.
//
// The arrow used to be the card's outright — it took over from the bare `m` when
// every letter went back to the box for good ([app.homeKey]'s always-types law).
// The strip has the stronger claim on it now (verbstrip.go, SCREEN 3c): a row
// with verbs draws them, because the one law that makes a bare letter safe is
// that the line naming it is on screen. So on a conversation row `→` opens the
// strip and the card does not move, and the card's own folds are reached by the
// pointer instead ([TestClickingTheWorkFoldLineTogglesIt] — the gesture that did
// not change).
//
// WHAT SURVIVES OF THE ARROW IS EVERY ROW THE STRIP HAS NOTHING TO OFFER, and
// [TestTheRightArrowStillOpensACardNoRowIsOn] pins that end of it.
func TestTheRightArrowOpensTheVerbStripAndLeavesTheCardAlone(t *testing.T) {
	a := workLab(t, 6)
	drive(t, a, key("down"))
	drive(t, a, key("right"))
	if !a.strip.open {
		t.Fatalf("→ on a row with verbs did not draw them:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), "…3 more tasks") {
		t.Fatalf("→ opened the card's folds out from under the strip:\n%s", homeText(a))
	}
	// AND THE WAY OUT LEAVES THE CARD WHERE IT WAS. `←` closes the strip rather
	// than folding a band, which is the same one-layer-at-a-time rule esc keeps.
	drive(t, a, key("left"))
	if a.strip.open {
		t.Fatalf("← did not leave the strip:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), "…3 more tasks") {
		t.Fatalf("leaving the strip moved the card's folds:\n%s", homeText(a))
	}
	// AND ONLY WITH NOTHING TYPED: in a draft the arrows belong to the caret, so
	// neither the strip nor a fold moves while something is in the box.
	subject, ok := a.homeSubject()
	if !ok {
		t.Fatal("the cursor's row has no subject")
	}
	a.homeKey(key("x"))
	drive(t, a, key("right"))
	if a.strip.open || a.anyBandFoldOpen(subject) {
		t.Fatal("→ acted on the row while something was typed")
	}
	// And an m, the letter that used to carry this, is just an m in the box.
	a.homeKey(key("m"))
	if !strings.Contains(a.home.box.String(), "m") {
		t.Fatalf("m was eaten as a key: box is %q", a.home.box.String())
	}
}

// AND THE ARROW IS STILL THE CARD'S WHERE THE STRIP HAS NOTHING TO SAY.
//
// This is the other half of the law above, and it is the half that kept the
// gesture alive: the strip only claims `→` on a row that HAS verbs
// ([app.openStrip] answers false otherwise and the arrow keeps every meaning it
// already had). The card nobody's row is on — the machine's own, at rest — folds
// its news past [machineNewsShown], and `→` still opens all of it while `←`
// folds it back.
func TestTheRightArrowStillOpensACardNoRowIsOn(t *testing.T) {
	lab := newHomeLab(t)
	now := middayNow()
	work := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "Pricing Research", work, now.Add(-time.Hour))
	// One more piece of news than the band draws, so there is something folded.
	for i := 0; i < machineNewsShown+1; i++ {
		if err := standing.Deliver(filepath.Dir(mine), standing.Note{
			At:    now.Add(-time.Duration(i+1) * time.Minute),
			Words: "keep an eye on thing " + strconv.Itoa(i), Text: "it moved",
		}); err != nil {
			t.Fatal(err)
		}
	}
	a := lab.app(mine)
	a.width, a.height = homeCardWidest, 40
	a.openHome()
	// THE CURSOR ON NOTHING IS WHAT MAKES THIS CARD THE MACHINE'S ([app.homeDetail]),
	// and a row nobody is on is a row with no verbs for the strip to take.
	a.home.cursor, a.home.picked = homeRest, false
	a.home.build()
	subject, ok := a.homeSubject()
	if !ok || subject.kind != bandKindMachine {
		t.Fatalf("the cursor at rest is not on the machine's card: %+v", subject)
	}

	drive(t, a, key("right"))
	if a.strip.open {
		t.Fatalf("a row with no verbs still drew a strip:\n%s", homeText(a))
	}
	if !a.anyBandFoldOpen(subject) {
		t.Fatalf("→ did not open the machine card's folds:\n%s", machineCardText(a, 48))
	}
	drive(t, a, key("left"))
	if a.anyBandFoldOpen(subject) {
		t.Fatalf("← did not fold the machine card back:\n%s", machineCardText(a, 48))
	}
}
