package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// tokenColApp is the live-steps fixture with BOOKS on it: a turn that has sent
// 14.2k and had 512 back, snapped rather than walking, because a test about
// what is DRAWN should not also be a test about how fast it gets there.
func tokenColApp(t *testing.T) *app {
	t.Helper()
	a := liveStepsApp(t)
	a.width = 100
	a.inputTokens, a.turnInStart = 14200, 0
	a.outputTokens, a.turnOutStart = 512, 0
	a.tickTokenCol(0, true)
	a.touch()
	return a
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
		if strings.Contains(line, "↑") || strings.Contains(line, "↓") {
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
	if page := livePage(a); strings.Contains(page, "↑") || strings.Contains(page, "↓") {
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
	both, cells := a.tokenColumn(14200, 512, 40, true)
	if cells == 0 || !strings.Contains(plain(both), "↑") || !strings.Contains(plain(both), "↓") {
		t.Fatalf("a wide budget drew %q", plain(both))
	}
	one, cells := a.tokenColumn(14200, 512, 8, true)
	if cells == 0 || strings.Contains(plain(one), "↑") || !strings.Contains(plain(one), "↓ 512") {
		t.Fatalf("a tight budget kept the wrong half: %q", plain(one))
	}
	if none, cells := a.tokenColumn(14200, 512, 3, true); cells != 0 || none != "" {
		t.Fatalf("a budget with room for neither drew %q", plain(none))
	}
}

// THE EMPTINESS LAW, in this column's own terms: a figure nobody has earned yet
// is absent, not a zero.
func TestNothingKnownDrawsNothing(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if text, cells := a.tokenColumn(0, 0, 40, true); cells != 0 || text != "" {
		t.Fatalf("an unknown pair drew %q", plain(text))
	}
	text, cells := a.tokenColumn(14200, 0, 40, true)
	if cells == 0 || strings.Contains(plain(text), "↓") {
		t.Fatalf("a turn with nothing back yet drew %q", plain(text))
	}
	if !strings.Contains(plain(text), "↑ 14.2k") {
		t.Fatalf("what went up was dropped with the zero beside it: %q", plain(text))
	}
}

// THE BOOKS MOVE ONCE A STEP AND THE PAGE MOVES CONTINUOUSLY, so the bytes
// already drawn are the floor under what the provider has not yet reported.
func TestWhatIsOnThePageFloorsWhatTheBooksHaveNotSeen(t *testing.T) {
	a := liveStepsApp(t)
	a.outputTokens, a.turnOutStart = 0, 0
	a.entries = append(a.entries, entry{kind: entryAssistant, turn: 1,
		text: strings.Repeat("x", 4000)})
	_, down := a.turnTokens()
	if want := session.EstimateTokens(4000); down < want {
		t.Fatalf("streamed bytes did not floor the figure: got %d, want at least %d", down, want)
	}
	// And the provider's own count takes over the moment it lands.
	a.outputTokens, a.turnOutStart = 9000, 0
	if _, down = a.turnTokens(); down != 9000 {
		t.Fatalf("the books did not win once they had spoken: %d", down)
	}
}

// A REQUEST THAT IS OUT AND UNANSWERED HAS STILL SENT SOMETHING, and the weight
// of the conversation is what it sent.
func TestWhatOneRequestWeighsFloorsWhatWentUp(t *testing.T) {
	a := liveStepsApp(t)
	a.inputTokens, a.turnInStart = 0, 0
	a.ctxTokens = 63600
	if up, _ := a.turnTokens(); up != 63600 {
		t.Fatalf("the request's weight was not the floor: %d", up)
	}
	a.inputTokens = 90000
	if up, _ := a.turnTokens(); up != 90000 {
		t.Fatalf("the books did not win once they had spoken: %d", up)
	}
}

// THE ARRIVAL SHAPE IS NOT THE DRAWING SHAPE: a figure that jumped by fourteen
// thousand between two frames reads as a glitch, so it walks.
func TestTheFiguresWalkTowardTheBooks(t *testing.T) {
	a := liveStepsApp(t)
	a.inputTokens, a.turnInStart = 14200, 0
	a.outputTokens, a.turnOutStart = 512, 0
	a.tickTokenCol(1, false)
	up, down := a.turnTokensDrawn()
	if up <= 0 || up >= 14200 {
		t.Fatalf("what went up popped rather than walked: %d", up)
	}
	if down <= 0 || down >= 512 {
		t.Fatalf("what came back popped rather than walked: %d", down)
	}
	for range 20 {
		a.tickTokenCol(1, false)
	}
	if up, down = a.turnTokensDrawn(); up != 14200 || down != 512 {
		t.Fatalf("the walk never arrived: ↑%d ↓%d", up, down)
	}
}

// AND A TURN OPENS AT NOTHING. A pair left standing at the last turn's totals
// would spend the first second of this one counting DOWN.
func TestEveryTurnCountsUpFromNothing(t *testing.T) {
	a := liveStepsApp(t)
	a.shownUp, a.shownDown = 14200, 512
	a.turnBegan = time.Time{}
	a.startClock()
	if a.shownUp != 0 || a.shownDown != 0 {
		t.Fatalf("a new turn inherited the last one's pair: ↑%d ↓%d", a.shownUp, a.shownDown)
	}
}

// THE SCREEN-READER TIER NEVER PACES A FIGURE.
func TestTheLinearTierDrawsTheExactPair(t *testing.T) {
	a := liveStepsApp(t)
	a.linear = true
	a.inputTokens, a.turnInStart = 14200, 0
	a.outputTokens, a.turnOutStart = 512, 0
	if up, down := a.turnTokensDrawn(); up != 14200 || down != 512 {
		t.Fatalf("the linear tier walked a figure: ↑%d ↓%d", up, down)
	}
}

// THE SESSION'S FIGURES ARE THE SESSION'S: a page drawing somebody else's work
// may not quote this conversation's totals over it.
func TestOnlyThePageThatOwnsTheReceiptsDrawsTheColumn(t *testing.T) {
	a := tokenColApp(t)
	mine := a.conversation()
	if !a.tokenColumnOn(mine) {
		t.Fatal("the conversation refused its own figures")
	}
	theirs := mine
	theirs.lens = overseerLens
	if a.tokenColumnOn(theirs) {
		t.Fatal("a room quoted the conversation's figures")
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
