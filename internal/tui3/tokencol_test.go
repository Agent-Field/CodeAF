package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// tokenColApp is the live-steps fixture with BOOKS on it: a turn that has sent
// 14.2k and had 512 back, folded in through the same door a real reading takes
// ([app.take]) and snapped rather than walking, because a test about what is
// DRAWN should not also be a test about how fast it gets there.
func tokenColApp(t *testing.T) *app {
	t.Helper()
	a := liveStepsApp(t)
	a.width = 100
	a.turnBegan = liveStepsBase
	a.take(session.Usage{Input: 14200, Output: 512})
	a.tickTokenCol(0, true)
	a.touch()
	return a
}

func hasColumn(page string) bool {
	return strings.Contains(page, "↑") || strings.Contains(page, "↓")
}

func TestTheRunningTurnSaysWhatWentUpAndWhatCameBack(t *testing.T) {
	a := tokenColApp(t)
	page := livePage(a)
	if !strings.Contains(page, "↑ 14.2k") {
		t.Fatalf("what went up is not on the page:\n%s", page)
	}
	if !strings.Contains(page, "↓ 512") {
		t.Fatalf("what came back is not on the page:\n%s", page)
	}
}

// THE COLUMN IS A SIGN OF MOTION, so exactly one row of the block carries it —
// the one that stands for the turn. A finished step is work that is over.
func TestOnlyTheLiveRowOfTheBlockCarriesTheColumn(t *testing.T) {
	a := tokenColApp(t)
	carrying := 0
	for _, line := range plainRows(a) {
		if hasColumn(line) {
			carrying++
		}
	}
	if carrying != 1 {
		t.Fatalf("expected one row with the column, got %d:\n%s", carrying, livePage(a))
	}
}

// AND IT GOES WITH THE TURN. Nothing is moving once the turn has settled, so
// there is nothing for the column to say — the session's own totals are the
// status line's, and they never leave it.
func TestTheColumnLeavesWithTheTurn(t *testing.T) {
	a := tokenColApp(t)
	a.state = stateIdle
	a.tickTokenCol(0, true)
	a.touch()
	if page := livePage(a); hasColumn(page) {
		t.Fatalf("the column outlived the turn:\n%s", page)
	}
}

// THE WORDS WIN ON A FRAME THAT CANNOT HOLD BOTH. The column is spare cells and
// never a reservation, so a narrow terminal loses the figures and keeps every
// word of the step it is reading.
func TestANarrowFrameKeepsTheWordsAndDropsTheColumn(t *testing.T) {
	a := tokenColApp(t)
	a.width = 34
	a.touch()
	page := livePage(a)
	if strings.Contains(page, "↑ 14.2k") {
		t.Fatalf("the column crowded a narrow frame:\n%s", page)
	}
	if strings.Contains(page, "…") {
		t.Fatalf("a sentence was cut for the column:\n%s", page)
	}
	if !strings.Contains(page, "Checking what changed") {
		t.Fatalf("the newest step lost its words:\n%s", page)
	}
}

// WHAT CAME BACK IS THE FIGURE THE COLUMN EXISTS FOR, so a column with room for
// one of the two keeps ↓ and sheds ↑.
func TestTheColumnShedsWhatWentUpFirst(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	both, cells := a.tokenColumn(14200, 512, 40)
	if cells == 0 || !strings.Contains(plain(both), "↑") || !strings.Contains(plain(both), "↓") {
		t.Fatalf("a wide budget drew %q", plain(both))
	}
	one, cells := a.tokenColumn(14200, 512, 8)
	if cells == 0 || strings.Contains(plain(one), "↑") || !strings.Contains(plain(one), "↓ 512") {
		t.Fatalf("a tight budget kept the wrong half: %q", plain(one))
	}
	if none, cells := a.tokenColumn(14200, 512, 3); cells != 0 || none != "" {
		t.Fatalf("a budget with room for neither drew %q", plain(none))
	}
}

// THE EMPTINESS LAW, in this column's own terms: a figure nobody has earned yet
// is absent, not a zero.
func TestNothingKnownDrawsNothing(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if text, cells := a.tokenColumn(0, 0, 40); cells != 0 || text != "" {
		t.Fatalf("an unknown pair drew %q", plain(text))
	}
	text, cells := a.tokenColumn(14200, 0, 40)
	if cells == 0 || strings.Contains(plain(text), "↓") {
		t.Fatalf("a turn with nothing back yet drew %q", plain(text))
	}
	if !strings.Contains(plain(text), "↑ 14.2k") {
		t.Fatalf("what went up was dropped with the zero beside it: %q", plain(text))
	}
}

// THE COLUMN IS NEVER BRIGHTER THAN THE SENTENCE IT SITS BESIDE. The figure
// wears the margin's tier and the arrow one stop under it — the datum hue that
// the first cut used made a number at the right edge outrank the words.
func TestTheColumnWearsTheMarginsInkAndNotThePayloads(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	text, _ := a.tokenColumn(14200, 512, 40)
	if strings.Contains(text, a.pal.data("512")) || strings.Contains(text, a.pal.data("14.2k")) {
		t.Fatalf("a figure took the payload ink: %q", text)
	}
	if !strings.Contains(text, a.pal.dim(" 512")) {
		t.Fatalf("the figure is not on the dim tier: %q", text)
	}
	if !strings.Contains(text, a.pal.fade(tokenDownGlyph, 0)) {
		t.Fatalf("the arrow is not one stop under its figure: %q", text)
	}
}

// THE BOOKS MOVE ONCE A STEP AND THE PAGE MOVES CONTINUOUSLY, so the bytes
// already drawn are the floor under what the provider has not yet reported.
func TestWhatIsOnThePageFloorsWhatTheBooksHaveNotSeen(t *testing.T) {
	a := liveStepsApp(t)
	a.entries = append(a.entries, entry{kind: entryAssistant, turn: 1,
		text: strings.Repeat("x", 4000)})
	_, down := a.col.reading(a.turnWritten())
	if want := session.EstimateTokens(4000); down < want {
		t.Fatalf("streamed bytes did not floor the figure: got %d, want at least %d", down, want)
	}
	// And the provider's own count takes over the moment it lands.
	a.col.down = 9000
	if _, down = a.col.reading(a.turnWritten()); down != 9000 {
		t.Fatalf("the books did not win once they had spoken: %d", down)
	}
}

// A REQUEST THAT IS OUT AND UNANSWERED HAS STILL SENT SOMETHING, and the weight
// of the conversation is what it sent.
func TestWhatOneRequestWeighsFloorsWhatWentUp(t *testing.T) {
	a := liveStepsApp(t)
	a.ctxTokens = 63600
	a.tickTokenCol(0, true)
	if up, _ := a.col.reading(0); up != 63600 {
		t.Fatalf("the request's weight was not the floor: %d", up)
	}
	a.col.up = 90000
	if up, _ := a.col.reading(0); up != 90000 {
		t.Fatalf("the books did not win once they had spoken: %d", up)
	}
}

// THE ARRIVAL SHAPE IS NOT THE DRAWING SHAPE: a figure that jumped by fourteen
// thousand between two frames reads as a glitch, so it walks.
func TestTheFiguresWalkTowardTheBooks(t *testing.T) {
	a := liveStepsApp(t)
	a.turnBegan = liveStepsBase
	a.take(session.Usage{Input: 14200, Output: 512})
	a.tickTokenCol(1, false)
	up, down := a.col.drawn(a.turnWritten(), true, false)
	if up <= 0 || up >= 14200 {
		t.Fatalf("what went up popped rather than walked: %d", up)
	}
	if down <= 0 || down >= 512 {
		t.Fatalf("what came back popped rather than walked: %d", down)
	}
	for range 20 {
		a.tickTokenCol(1, false)
	}
	if up, down = a.col.drawn(a.turnWritten(), true, false); up != 14200 || down != 512 {
		t.Fatalf("the walk never arrived: ↑%d ↓%d", up, down)
	}
}

// AND A TURN OPENS AT NOTHING. A pair left standing at the last turn's totals
// would spend the first second of this one counting DOWN.
func TestEveryTurnCountsUpFromNothing(t *testing.T) {
	a := liveStepsApp(t)
	a.col = tokenCol{up: 14200, down: 512, shownUp: 14200, shownDown: 512}
	a.turnBegan = time.Time{}
	a.startClock()
	if a.col != (tokenCol{}) {
		t.Fatalf("a new turn inherited the last one's column: %+v", a.col)
	}
}

// THE SCREEN-READER TIER NEVER PACES A FIGURE.
func TestTheLinearTierDrawsTheExactPair(t *testing.T) {
	a := liveStepsApp(t)
	a.linear = true
	a.turnBegan = liveStepsBase
	a.take(session.Usage{Input: 14200, Output: 512})
	if up, down := a.col.drawn(a.turnWritten(), true, a.linear); up != 14200 || down != 512 {
		t.Fatalf("the linear tier walked a figure: ↑%d ↓%d", up, down)
	}
}

// A PAGE WITH NO COLUMN TO CARRY DRAWS NONE: a run's read-only transcript has
// no lane and no books, and the deck says so by carrying nothing.
func TestAPageWithNoColumnDrawsNone(t *testing.T) {
	a := tokenColApp(t)
	mine := a.conversation()
	if !a.tokenColumnOn(mine) {
		t.Fatal("the conversation refused its own column")
	}
	bare := mine
	bare.col = nil
	if a.tokenColumnOn(bare) {
		t.Fatal("a page carrying no column drew one")
	}
}

// A STEP CARRIES ONLY WHAT IT WROTE, and a step that has only called a tool
// wrote nothing — so it says nothing.
func TestARunningStepCarriesOnlyWhatItWrote(t *testing.T) {
	a := tokenColApp(t)
	d := a.conversation()
	wrote := caption{text: "writing the answer", start: 8, end: 10, began: liveStepsBase}
	if word := a.stepTokenWord(wrote, d); !strings.HasPrefix(word, "↓") {
		t.Fatalf("a writing step said %q", word)
	}
	if strings.Contains(a.stepTokenWord(wrote, d), "↑") {
		t.Fatal("a step claimed a share of what went up")
	}
	called := caption{text: "running the suite", start: 9, end: 10, began: liveStepsBase}
	if word := a.stepTokenWord(called, d); word != "" {
		t.Fatalf("a step that wrote nothing said %q", word)
	}
	settled := wrote
	settled.ended = liveStepsBase.Add(time.Second)
	if word := a.stepTokenWord(settled, d); word != "" {
		t.Fatalf("a finished step kept its figure: %q", word)
	}
}

// ONE COLUMN, EVERY PAGE THAT STREAMS. A task room grows its transcript with
// the same reducer as the conversation, so its live work carries the same
// column — off its OWN lane's books, never the conversation's.
func TestATaskRoomCarriesItsOwnColumn(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = midFlightJournal(t)
	a.openRoom(7, "Fix the nil-map crash")
	a.width = 100
	a.touch()
	// The conversation's own books are loud, and must not reach the node's page.
	a.turnBegan = liveStepsBase
	a.take(session.Usage{Input: 999000, Output: 999000})

	// The node's lane reports one finished step.
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventTurnDone, Usage: session.Usage{Input: 4200, Output: 310},
	}})
	if a.room.col.up != 4200 || a.room.col.down != 310 {
		t.Fatalf("the room's lane did not feed its column: %+v", a.room.col)
	}
	a.tickTokenCol(0, true)
	a.room.dirty = true
	page := roomText(a)
	if !strings.Contains(page, "↑ 4.2k") || !strings.Contains(page, "↓ 310") {
		t.Fatalf("the node's page does not carry its own column:\n%s", page)
	}
	if strings.Contains(page, "999k") {
		t.Fatalf("the conversation's books leaked onto the node's page:\n%s", page)
	}
	// And the column leaves with the node's run.
	a.room.done = true
	a.tickTokenCol(0, true)
	a.room.dirty = true
	if page := roomText(a); hasColumn(page) {
		t.Fatalf("the column outlived the node's run:\n%s", page)
	}
}

// A TURN SPLIT INTO TWO RUNS IS STILL ONE TURN. A kept row — here the first
// call failed, and its step is kept whole because only failure speaks — splits
// the running turn's machinery into a run holding only the reasoning above it
// and a run below. The pair rides the frontier alone: the same figures drawn
// twice read as the turn running twice, which is what the owner's screenshot
// showed.
func TestASplitTurnCarriesTheColumnOnItsFrontierAlone(t *testing.T) {
	a := tokenColApp(t)
	a.entries[3].status = toolFailed
	a.touch()
	d := a.conversation()
	stampHierarchy(d.entries, a.deckFolds(d))
	d.captions = deriveCaptions(d.entries, d.runningTurn)
	if runs := liveWorkRuns(d); len(runs) < 2 {
		t.Fatalf("the fixture did not split into two runs: %d", len(runs))
	}
	page := plainRows(a)
	carrying := 0
	for _, line := range page {
		if hasColumn(line) {
			carrying++
		}
	}
	if carrying != 1 {
		t.Fatalf("a split turn drew the column %d times, want once, on the frontier:\n%s",
			carrying, strings.Join(page, "\n"))
	}
	// And the run that carries it is the lowest one on the page.
	at := -1
	for i, line := range page {
		if hasColumn(line) {
			at = i
		}
	}
	for i := at + 1; i < len(page); i++ {
		if strings.Contains(page[i], "ctrl+e") {
			t.Fatalf("a run below the column has its own door — the column is not on the frontier:\n%s",
				strings.Join(page, "\n"))
		}
	}
}

// AND THE RUN ABOVE THE SPLIT DRAWS NO DOOR OF ITS OWN. It holds nothing but
// reasoning that is over — no step — and a window with no steps in it draws
// itself as `▸ Work · ctrl+e` (livesteps.go). Two of those on one page, keyed
// alike, is one turn claiming to be two pieces of running work; the owner read
// it as "things opening that don't need to". The reasoning goes back under the
// ordinary `thought for …` row, which is the chip a finished turn draws and
// which carries its own door onto the working.
//
// Every `ctrl+e` on the page is counted and named, because the defect was not a
// missing row but a second one that looked exactly like the right one.
func TestASplitTurnDrawsNoSecondWorkDoorAboveTheKeptStep(t *testing.T) {
	a := tokenColApp(t)
	a.entries[3].status = toolFailed
	a.touch()
	d := a.conversation()
	stampHierarchy(d.entries, a.deckFolds(d))
	d.captions = deriveCaptions(d.entries, d.runningTurn)
	if runs := liveWorkRuns(d); len(runs) < 2 {
		t.Fatalf("the fixture did not split into two runs: %d", len(runs))
	}

	doors := func(page []string) (thought, work int) {
		for _, line := range page {
			if !strings.Contains(line, "ctrl+e") {
				continue
			}
			switch {
			case strings.Contains(line, "thought for"):
				thought++
			case strings.Contains(line, "Work · ctrl+e"),
				strings.Contains(line, "Working · ctrl+e"),
				strings.Contains(line, liveWorkWord+" · ctrl+e"):
				work++
			default:
				t.Fatalf("an unaccounted ctrl+e row on the page: %q\n%s", line, strings.Join(page, "\n"))
			}
		}
		return thought, work
	}

	page := plainRows(a)
	thought, work := doors(page)
	if work != 0 {
		t.Fatalf("the shut page drew %d work doors, want none — the compact block IS the shut state:\n%s",
			work, strings.Join(page, "\n"))
	}
	if thought != 1 {
		t.Fatalf("the reasoning above the kept step is not behind exactly one thought row (%d):\n%s",
			thought, strings.Join(page, "\n"))
	}
	// The step the failure kept still stands between the two, whole and above
	// the working block, exactly where it happened.
	if !strings.Contains(strings.Join(page, "\n"), "Reading the loader first") {
		t.Fatalf("the kept step is not on the page:\n%s", strings.Join(page, "\n"))
	}

	// And the work is still one window with one way in and out of it: opening it
	// draws exactly one `working · ctrl+e`, and the thought row is still its own.
	showLiveWork(t, a)
	page = plainRows(a)
	thought, work = doors(page)
	if work != 1 {
		t.Fatalf("the opened work drew %d doors, want exactly one:\n%s", work, strings.Join(page, "\n"))
	}
	if thought != 1 {
		t.Fatalf("the thought row is not on the opened page exactly once (%d):\n%s",
			thought, strings.Join(page, "\n"))
	}
}
