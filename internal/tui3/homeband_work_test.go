package tui3

import (
	"os"
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

// AND ON THE ≥160 CARD A DONE TASK WEARS `✓`, WHICH IS THE OPPOSITE LAW.
//
// THE LAW THAT DIED IS "DONE WEARS NO MARK" — on this surface only. It was
// argued from the ink: a tick on every finished row spends the loudest cell on
// the rows that want nothing. SCREEN 1d simply draws it —
// `✓ toy-scale validation of decomposition   $1.63` — and the owner ordered the
// design followed exactly (FIDELITY.md item 8), so the design overruled the
// argument. The mark is [app.homeTaskGlyph]'s, which is the same function the
// task page's own rows already used, so there is one spelling of `done` and not
// two.
//
// WHAT THE CARD DOES NOT DRAW IS THE OUTCOME. A landed task is the mark, the
// name and the figure — one row — and the sentence about how it went belongs to
// the tasks place and to the band above. A task that is NOT done keeps its
// sentence, which is [TestATaskThatIsNotDoneLeadsWithItsState].
func TestADoneTaskOnTheCardWearsTheDesignsTick(t *testing.T) {
	a := workLab(t, 2)
	rows := workCard(t, a)
	card := strings.Join(rows, "\n")
	at := workRowAt(t, rows, "Task 0")
	if !strings.HasPrefix(strings.TrimSpace(rows[at]), glyphDone+" Task 0") {
		t.Fatalf("a landed task on the card does not lead with the design's mark:\n%s", card)
	}
	// The word never came back with the mark: `done` was banned for saying
	// nothing, and the glyph is what replaced it rather than a second spelling.
	for _, banned := range []string{doneWord + " Task", "done  "} {
		if strings.Contains(card, banned) {
			t.Fatalf("the card draws %q beside the mark:\n%s", banned, card)
		}
	}
	if strings.Contains(card, "outcome 0") {
		t.Fatalf("a landed task on the card kept its outcome sentence:\n%s", card)
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

// AND THE CARD'S OWN CUT NAMES THE PLACE THAT HOLDS THE REST — it does not open.
//
// THE LAW THAT DIED IS "THE WORK FOLD IS A TOGGLE", on this surface. SCREEN 1d
// draws `▸ 3 more tasks` with `tasks` hard against the right margin, which is
// the grammar every row of this surface uses to say where it goes in: anything
// that grows says how much it is hiding and NAMES ITS PAGE, because a card is
// not the tasks place and must not pretend to be one. So the line is a pointer
// and not a door, and what is pinned here is what a pointer owes a reader — an
// honest count, and the name of the place that has the rest.
//
// The fold MECHANISM did not die with it; it moved to the one band on this card
// that still holds something back ([TestClickingACardsFoldLineTogglesIt]).
func TestTheCardsWorkFoldNamesTheTasksPlace(t *testing.T) {
	a := workLab(t, 6)
	rows := workCard(t, a)
	card := strings.Join(rows, "\n")
	for i := 0; i < homeCardTasks; i++ {
		if !strings.Contains(card, "Task "+strconv.Itoa(i)) {
			t.Fatalf("the card dropped task %d:\n%s", i, card)
		}
	}
	if strings.Contains(card, "Task "+strconv.Itoa(homeCardTasks)) {
		t.Fatalf("the card drew a fourth task:\n%s", card)
	}
	// THE COUNT IS HONEST: six tasks behind a card that shows three is three more.
	fold := workRowAt(t, rows, "3 more tasks")
	if !strings.HasPrefix(strings.TrimSpace(rows[fold]), bandFoldGlyph) {
		t.Fatalf("the fold line wears no fold mark:\n%s", card)
	}
	// AND THE PLACE IS NAMED, at the right margin, in the tab bar's own word for
	// it — one spelling, so the line and the place it opens cannot drift apart.
	if !strings.HasSuffix(strings.TrimRight(rows[fold], " "), pageTasks.word()) {
		t.Fatalf("the fold line does not name the tasks place at its margin: %q", rows[fold])
	}
	// A POINTER IS NOT A CONTROL. Nothing recorded a fold line for it, so a press
	// aimed at it opens nothing and the card is exactly where it was.
	subject, ok := a.homeSubject()
	if !ok {
		t.Fatal("the cursor's row has no subject")
	}
	if _, found := a.bandFoldAt(rows[fold]); found {
		t.Fatalf("the card's work fold registered itself as a control: %q", rows[fold])
	}
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	left, _ := homeColumns(width)
	at := -1
	for y, line := range lines {
		if strings.Contains(ansi.Strip(line), "3 more tasks") {
			at = y
		}
	}
	if at < 0 {
		t.Fatalf("the fold line is not on the frame:\n%s", homeText(a))
	}
	a.homePress(left+homeGutter+2, at)
	if a.anyBandFoldOpen(subject) {
		t.Fatalf("a press on the pointer opened a band:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), "3 more tasks") {
		t.Fatalf("a press on the pointer changed the card:\n%s", homeText(a))
	}
}

// A CLICK ON THE FOLD LINE OPENS THAT BAND, and a second folds it again. It is
// the one gesture that acts on ONE band — `m` acts on the whole card.
//
// IT AIMS AT FILES RATHER THAN AT TASKS NOW, and the gesture is the one it
// always was. The card's work rows stopped folding when the design's five bands
// landed: `▸ N more tasks` names the tasks place and opens nothing
// ([TestTheCardsWorkFoldNamesTheTasksPlace] pins that half). `made for you` is
// the band on the same card that still holds something back — it is the
// registered `deliverables` band drawn whole — so the mechanism this test is
// about is asked there, over the same [app.bandFoldAt] road, in the same column,
// with the same law about the cursor.
func TestClickingACardsFoldLineTogglesIt(t *testing.T) {
	a := workLabMade(t, 1, 5)
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	left, _ := homeColumns(width)
	row := -1
	for y, line := range lines {
		if strings.Contains(ansi.Strip(line), "…2 more files") {
			row = y
		}
	}
	if row < 0 {
		t.Fatalf("the fold line is not on the frame:\n%s", homeText(a))
	}
	a.homePress(left+homeGutter+2, row)
	if strings.Contains(homeText(a), "…2 more files") {
		t.Fatalf("the click did not open the band:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), "file-4.md") {
		t.Fatalf("the opened band does not draw what it was hiding:\n%s", homeText(a))
	}
	lines, _, _, _ = a.homeFrame(width, height)
	for y, line := range lines {
		if strings.Contains(ansi.Strip(line), "…2 fewer") {
			row = y
		}
	}
	a.homePress(left+homeGutter+2, row)
	if !strings.Contains(homeText(a), "…2 more files") {
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
// pointer instead ([TestClickingACardsFoldLineTogglesIt] — the gesture that did
// not change).
//
// THE FOLD IT WATCHES IS `made for you`, for that test's reason: the card's work
// rows stopped folding when the design's five bands landed, so the fold left
// standing on this card is the deliverables band's. What is asserted is
// unchanged — a fold that was shut before the arrow is shut after it.
//
// WHAT SURVIVES OF THE ARROW IS EVERY ROW THE STRIP HAS NOTHING TO OFFER, and
// [TestTheRightArrowStillOpensACardNoRowIsOn] pins that end of it.
func TestTheRightArrowOpensTheVerbStripAndLeavesTheCardAlone(t *testing.T) {
	a := workLabMade(t, 6, 5)
	drive(t, a, key("down"))
	drive(t, a, key("right"))
	if !a.strip.open {
		t.Fatalf("→ on a row with verbs did not draw them:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), "…2 more files") {
		t.Fatalf("→ opened the card's folds out from under the strip:\n%s", homeText(a))
	}
	// AND THE WAY OUT LEAVES THE CARD WHERE IT WAS. `←` closes the strip rather
	// than folding a band, which is the same one-layer-at-a-time rule esc keeps.
	drive(t, a, key("left"))
	if a.strip.open {
		t.Fatalf("← did not leave the strip:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), "…2 more files") {
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
