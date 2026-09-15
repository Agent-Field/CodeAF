package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// tokenColApp is the live-steps fixture with a WEIGHT and BOOKS on it: a
// conversation weighing 14.2k whose turn has had 512 back, folded in through
// the same doors a real reading takes ([app.takeContext], [app.take]) and
// snapped rather than walking, because a test about what is DRAWN should not
// also be a test about how fast it gets there.
func tokenColApp(t *testing.T) *app {
	t.Helper()
	a := liveStepsApp(t)
	a.width = 100
	a.turnBegan = liveStepsBase
	a.takeContext(14200)
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
	pair := tokenPair{up: 14200, down: 512}
	both, cells := a.tokenColumn(pair, 40)
	if cells == 0 || !strings.Contains(plain(both), "↑") || !strings.Contains(plain(both), "↓") {
		t.Fatalf("a wide budget drew %q", plain(both))
	}
	one, cells := a.tokenColumn(pair, 8)
	if cells == 0 || strings.Contains(plain(one), "↑") || !strings.Contains(plain(one), "↓ 512") {
		t.Fatalf("a tight budget kept the wrong half: %q", plain(one))
	}
	if none, cells := a.tokenColumn(pair, 3); cells != 0 || none != "" {
		t.Fatalf("a budget with room for neither drew %q", plain(none))
	}
}

// AND THE RECEIPT GOES BEFORE EITHER. It is an annotation on ↑, so a row with
// room for the pair and not the note keeps the pair — and one with room for
// only ↓ keeps only ↓, receipt and all gone.
func TestTheReceiptIsTheFirstThingANarrowRowGivesUp(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	pair := tokenPair{up: 14200, down: 512, receipt: 3400}
	whole, cells := a.tokenColumn(pair, 40)
	if got := plain(whole); got != "+3.4k ↑ 14.2k  ↓ 512" || cells != len([]rune(got)) {
		t.Fatalf("a wide row drew %q (%d cells)", got, cells)
	}
	pairOnly, _ := a.tokenColumn(pair, cells-1)
	if got := plain(pairOnly); got != "↑ 14.2k  ↓ 512" {
		t.Fatalf("a row one cell short of the receipt drew %q, want the pair alone", got)
	}
	downOnly, _ := a.tokenColumn(pair, 8)
	if got := plain(downOnly); got != "↓ 512" {
		t.Fatalf("a row with room for one figure drew %q", got)
	}
}

// THE EMPTINESS LAW, in this column's own terms: a figure nobody has earned yet
// is absent, not a zero.
func TestNothingKnownDrawsNothing(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if text, cells := a.tokenColumn(tokenPair{}, 40); cells != 0 || text != "" {
		t.Fatalf("an unknown pair drew %q", plain(text))
	}
	text, cells := a.tokenColumn(tokenPair{up: 14200}, 40)
	if cells == 0 || strings.Contains(plain(text), "↓") {
		t.Fatalf("a turn with nothing back yet drew %q", plain(text))
	}
	if !strings.Contains(plain(text), "↑ 14.2k") {
		t.Fatalf("what went up was dropped with the zero beside it: %q", plain(text))
	}
}

// AT REST THE COLUMN IS THE MARGIN'S INK. The figure wears the margin's tier and
// the arrow one stop under it — the datum hue that the first cut used made a
// number at the right edge outrank the words.
func TestTheColumnWearsTheMarginsInkAndNotThePayloads(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	text, _ := a.tokenColumn(tokenPair{up: 14200, down: 512}, 40)
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

// THE BOOKS MOVE ONCE A STEP AND THE PAGE MOVES CONTINUOUSLY, so before the
// books have said anything the bytes already drawn are the figure.
func TestWhatIsOnThePageIsTheFigureBeforeTheBooksSpeak(t *testing.T) {
	a := liveStepsApp(t)
	a.entries = append(a.entries, entry{kind: entryAssistant, turn: 1,
		text: strings.Repeat("x", 4000)})
	_, down := a.col.reading(a.turnWritten(), a.turn)
	if want := session.EstimateTokens(a.turnWritten()); down != want {
		t.Fatalf("streamed bytes did not make the figure: got %d, want %d", down, want)
	}
}

// ↓ KEEPS MOVING AFTER A BILL. The bill counted tokens the page never showed —
// hidden reasoning, a call's arguments, the error in the guess — so it lands
// ABOVE the page's estimate, and the first cut's max(books, estimate) stood
// still from there until the prose caught up. The figure is the books plus
// what arrived since they moved.
func TestTheDownFigureKeepsMovingAfterABill(t *testing.T) {
	a := liveStepsApp(t)
	a.turnBegan = liveStepsBase
	a.take(session.Usage{Output: 9000})
	if _, down := a.col.reading(a.turnWritten(), a.turn); down != 9000 {
		t.Fatalf("the books did not take over once they had spoken: %d", down)
	}
	a.entries = append(a.entries, entry{kind: entryAssistant, turn: 1,
		text: strings.Repeat("x", 4000)})
	want := 9000 + session.EstimateTokens(4000)
	if _, down := a.col.reading(a.turnWritten(), a.turn); down != want {
		t.Fatalf("what arrived after the bill did not move the figure: got %d, want %d", down, want)
	}
	// The same books read again are not a new bill, and must not swallow the
	// bytes that arrived after the real one.
	a.take(session.Usage{Output: 9000})
	if _, down := a.col.reading(a.turnWritten(), a.turn); down != want {
		t.Fatalf("a repeated reading of the same books moved the mark: got %d, want %d", down, want)
	}
	// And the next real bill takes the whole of it over.
	a.take(session.Usage{Output: 11000})
	if _, down := a.col.reading(a.turnWritten(), a.turn); down != 11000 {
		t.Fatalf("the next bill did not take over: %d", down)
	}
}

// A CALL'S ARGUMENTS ARE THE MODEL'S WRITING. A `write` streaming its body is
// billed as output byte for byte, and a count of prose alone stood still for
// exactly as long as the model was writing hardest.
func TestTheDownFigureCountsACallStillArriving(t *testing.T) {
	a := liveStepsApp(t)
	before := a.turnWritten()
	_, was := a.col.reading(before, a.turn)
	a.formTool(session.Event{Kind: session.EventToolForming, Tool: "write", CallID: "w1", Bytes: 8000})
	if got := a.turnWritten() - before; got != 8000 {
		t.Fatalf("the forming call added %d bytes of writing, want the 8000 that have arrived", got)
	}
	if _, now := a.col.reading(a.turnWritten(), a.turn); now <= was {
		t.Fatalf("↓ stood still while a call's body streamed: %d then %d", was, now)
	}
}

// ↑ IS THE WEIGHT OF THE REQUEST, NOT THE BILL. The bill is every request's
// input added together — three steps on a 50k conversation read 150k — and the
// column's ↑ is the one request that is going out now.
func TestWhatWentUpIsTheWeightOfTheRequestAndNotTheBill(t *testing.T) {
	a := liveStepsApp(t)
	a.turnBegan = liveStepsBase
	a.takeContext(63600)
	a.take(session.Usage{Input: 190800, Output: 900})
	a.tickTokenCol(0, true)
	if up, _ := a.col.reading(0, a.turn); up != 63600 {
		t.Fatalf("↑ read %d, want the conversation's weight (63600) and not the summed bill", up)
	}
}

// ↑ IS RE-READ WHERE A TOOL RESULT LANDS. A tool's end is the step's usage in
// the books and its result joining the conversation, so the beat is asked on
// the very next frame — off the loop, like every other ask of the agent — and
// its answer is the conversation's own weight.
func TestAToolEndReReadsWhatTheRequestWeighs(t *testing.T) {
	fake := &fakeAgent{model: "m", weight: 40000}
	a := newTestApp(fake)
	a.entries = liveStepsFixture()
	a.turn, a.state = 1, stateWorking
	a.clock = func() time.Time { return liveStepsBase.Add(9 * time.Second) }
	a.turnBegan = liveStepsBase
	a.takeContext(31000)
	fake.weight = 52400
	a.event(session.Event{Kind: session.EventToolEnd, Tool: "bash", CallID: "b9"})
	if !a.usageOwed {
		t.Fatal("a tool's end did not ask the beat for the next frame")
	}
	cmd := a.usageKick()
	if cmd == nil {
		t.Fatal("the beat asked nothing")
	}
	msg, ok := cmd().(usageMsg)
	if !ok {
		t.Fatal("the beat's answer is not a usage reading")
	}
	a.usageBack(msg)
	a.tickTokenCol(0, true)
	if a.ctxTokens != fake.ContextTokens() {
		t.Fatalf("the weight read %d, want the agent's ContextTokens (%d)", a.ctxTokens, fake.ContextTokens())
	}
	if up, _ := a.col.reading(0, a.turn); up != 52400 {
		t.Fatalf("↑ read %d after the re-read, want 52400", up)
	}
}

// AND A READING TAKEN ON THE LOOP OUTRANKS ONE THE BEAT STARTED BEFORE IT. A
// compaction is measured at once; a beat that left before it and lands after
// it carries the old weight, and must not put it back.
func TestABeatOvertakenByAMeasureLeavesTheWeightAlone(t *testing.T) {
	fake := &fakeAgent{model: "m", weight: 168000}
	a := newTestApp(fake)
	cmd := a.usageKick()
	fake.weight = 41000
	a.measureContext()
	a.usageBack(cmd().(usageMsg))
	if a.ctxTokens != 41000 {
		t.Fatalf("a stale beat put the old weight back: %d", a.ctxTokens)
	}
}

// THE ARRIVAL SHAPE IS NOT THE DRAWING SHAPE: a figure that jumped by fourteen
// thousand between two frames reads as a glitch, so it walks.
func TestTheFiguresWalkTowardTheBooks(t *testing.T) {
	a := liveStepsApp(t)
	a.turnBegan = liveStepsBase
	// The first reading ARRIVES ([chaseFigure]); only what follows it walks.
	a.takeContext(14200)
	a.take(session.Usage{Output: 256})
	a.tickTokenCol(1, false)
	a.takeContext(18000)
	a.take(session.Usage{Output: 512})
	a.tickTokenCol(1, false)
	up, down := a.col.drawn(a.turnWritten(), a.turn, true, false)
	if up <= 14200 || up >= 18000 {
		t.Fatalf("what went up popped rather than walked: %d", up)
	}
	if down <= 256 || down >= 512 {
		t.Fatalf("what came back popped rather than walked: %d", down)
	}
	for range 20 {
		a.tickTokenCol(1, false)
	}
	if up, down = a.col.drawn(a.turnWritten(), a.turn, true, false); up != 18000 || down != 512 {
		t.Fatalf("the walk never arrived: ↑%d ↓%d", up, down)
	}
}

// A FIGURE THAT WAS NOT ON THE SCREEN ARRIVES WHOLE AND UNLIT. Taking a
// conversation up from a tab opens its column at nothing, and the weight and
// the books it then reads existed before anybody looked: counting them up from
// zero and lighting them on the way drew a page being redrawn as work moving.
func TestTakingUpATabShowsItsFiguresWholeAndUnlit(t *testing.T) {
	a := liveStepsApp(t)
	a.turnBegan = liveStepsBase
	a.takeContext(9000)
	a.take(session.Usage{Output: 128})
	for range 40 {
		a.tickTokenCol(1, false)
	}
	// The switch: every meter about the conversation being left goes, and the
	// one being taken up is read afresh.
	a.resetMeters()
	a.turnBegan = liveStepsBase
	a.takeContext(79600)
	a.take(session.Usage{Output: 37600})
	a.tickTokenCol(1, false)
	up, down := a.col.drawn(a.turnWritten(), a.turn, true, false)
	if up != 79600 || down != 37600 {
		t.Fatalf("the conversation's figures counted up instead of arriving: ↑%d ↓%d", up, down)
	}
	if p := a.tokenPairOf(a.conversation()); p.upLit != 0 || p.downLit != 0 {
		t.Fatalf("figures that only arrived are lit as though they moved: ↑%d ↓%d", p.upLit, p.downLit)
	}
}

// AND A TURN OPENS AT NOTHING. A pair left standing at the last turn's totals
// would spend the first second of this one counting DOWN; from nothing, the new
// turn's first reading arrives whole ([chaseFigure]).
func TestEveryTurnOpensItsColumnAtNothing(t *testing.T) {
	a := liveStepsApp(t)
	a.col = tokenCol{down: 512, weight: 14200, shownUp: 14200, shownDown: 512}
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
	a.takeContext(14200)
	a.take(session.Usage{Output: 512})
	a.tickTokenCol(0, true)
	if up, down := a.col.drawn(a.turnWritten(), a.turn, true, a.linear); up != 14200 || down != 512 {
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

// A STEP CARRIES ONLY WHAT IT WROTE. A step that has only called a tool wrote
// the call — its arguments are output the provider bills like any sentence —
// so it carries that; a call with nothing written for it yet says nothing.
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
	args := len(d.entries[9].detail.Args)
	if word, want := a.stepTokenWord(called, d), "↓ "+tokenColWord(session.EstimateTokens(args)); word != want {
		t.Fatalf("a step that only called a tool said %q, want what it wrote to call it (%q)", word, want)
	}
	d.entries[9].detail.Args = ""
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
// column — off its OWN sources, never the conversation's: ↓ from the steps its
// lane has heard, ↑ from its worker's own weight.
func TestATaskRoomCarriesItsOwnColumn(t *testing.T) {
	a, fake, _ := roomApp(t)
	weighed := &weighedRoomFake{roomFake: fake, weight: 4200}
	a.agent = weighed
	fake.journal = midFlightJournal(t)
	a.openRoom(7, "Fix the nil-map crash")
	a.width = 100
	a.touch()
	// The conversation's own figures are loud, and must not reach the node's page.
	a.turnBegan = liveStepsBase
	a.takeContext(999000)
	a.take(session.Usage{Input: 999000, Output: 999000})

	// The node's lane reports one finished step, and the beat asks its worker.
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventTurnDone, Usage: session.Usage{Input: 4200, Output: 310},
	}})
	a.usageAsking = false
	a.usageBack(a.usageKick()().(usageMsg))
	if a.room.col.weight != 4200 || a.room.col.down != 310 {
		t.Fatalf("the room's sources did not feed its column: %+v", a.room.col)
	}
	a.tickTokenCol(0, true)
	a.room.dirty = true
	page := roomText(a)
	if !strings.Contains(page, "↑ 4,200") || !strings.Contains(page, "↓ 310") {
		t.Fatalf("the node's page does not carry its own column:\n%s", page)
	}
	if strings.Contains(page, "999") {
		t.Fatalf("the conversation's figures leaked onto the node's page:\n%s", page)
	}
	// And the column leaves with the node's run.
	a.room.done = true
	a.tickTokenCol(0, true)
	a.room.dirty = true
	if page := roomText(a); hasColumn(page) {
		t.Fatalf("the column outlived the node's run:\n%s", page)
	}
}

// OPENING A TASK PAGE DOES NOT REPLAY THE CLIMB. The owner's report: every
// time a task was taken from the rail, its page counted `↑ 79.6k  ↓ 37.6k` up
// from zero and lit both figures, then did the same climb to the same figures
// on the next visit — a page being built, drawn as a task being busy. A room is
// a new page with its column at nothing, so its first reading ARRIVES: whole,
// on the first frame, and at rest. The next visit is the same.
func TestOpeningATaskPageShowsItsFiguresWholeAndUnlit(t *testing.T) {
	a, fake, _ := roomApp(t)
	a.agent = &weighedRoomFake{roomFake: fake, weight: 79600}
	fake.journal = midFlightJournal(t)
	for visit := 1; visit <= 2; visit++ {
		a.openRoom(7, "Build site/product.html")
		a.width = 100
		a.touch()
		drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
			Kind: session.EventTurnDone, Usage: session.Usage{Input: 79600, Output: 37600},
		}})
		a.usageAsking = false
		a.usageBack(a.usageKick()().(usageMsg))
		// One slot of the ordinary clock — not a snap — is all it takes.
		a.tickTokenCol(1, false)
		p := a.tokenPairOf(a.room.deck())
		if p.up != 79600 || p.down != 37600 {
			t.Fatalf("visit %d: the page counted its figures up instead of showing them: ↑%d ↓%d", visit, p.up, p.down)
		}
		if p.upLit != 0 || p.downLit != 0 {
			t.Fatalf("visit %d: figures that only arrived are lit as though the task moved: ↑%d ↓%d", visit, p.upLit, p.downLit)
		}
		a.closeRoom()
	}
}

// weighedRoomFake is a room fake whose engine can say what its node's worker
// weighs — the door a local engine has and a hosted one does not.
type weighedRoomFake struct {
	*roomFake
	weight int
}

func (f *weighedRoomFake) TaskContextTokens(uint64) int { return f.weight }

// IN A ROOM ↑ IS ONE REQUEST, NEVER A SUM. Each step's usage is every request
// of that step added together; two steps on a 5k worker used to read 9.4k and
// climbing. The figure is the worker's newest weight, and a worker between
// hands (who answers nothing) leaves the last one standing.
func TestARoomsUpFigureIsTheLatestRequestAndNotASum(t *testing.T) {
	a, fake, _ := roomApp(t)
	weighed := &weighedRoomFake{roomFake: fake, weight: 5100}
	a.agent = weighed
	fake.journal = midFlightJournal(t)
	a.openRoom(7, "Fix the nil-map crash")
	for _, input := range []int{4200, 5200} {
		drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
			Kind: session.EventTurnDone, Usage: session.Usage{Input: input, Output: 100},
		}})
	}
	a.usageAsking = false
	a.usageBack(a.usageKick()().(usageMsg))
	if up, _ := a.room.col.reading(0, a.room.turn); up != 5100 {
		t.Fatalf("the room's ↑ read %d, want its worker's newest weight (5100), not a sum of steps", up)
	}
	weighed.weight = 0
	a.usageAsking = false
	a.usageBack(a.usageKick()().(usageMsg))
	if up, _ := a.room.col.reading(0, a.room.turn); up != 5100 {
		t.Fatalf("a worker between hands took ↑ away: %d", up)
	}
}

// A HOSTED NODE'S RAIL COUNTS ITS TOKENS FROM ITS NOTICES. A window with no
// lane to the worker never hears its steps end, so the engine's own count on
// the row it publishes is the only one it gets — and a local pilot's larger
// count is never taken back by a notice that carries less.
func TestAHostedNodesTokensComeFromItsNotices(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{})})
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{CostUSD: 0.12, Tokens: 48200})})
	if got := a.tasks[7].tokens; got != 48200 {
		t.Fatalf("the node's tokens read %d, want the notice's 48200", got)
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{CostUSD: 0.2})})
	if got := a.tasks[7].tokens; got != 48200 {
		t.Fatalf("a row without a token figure took the count away: %d", got)
	}
}

// farJournal is a hosted node's journal tail in the shape internal/session
// writes it: message lines, and one `call` line per request banked beside them
// (sessionfile.go's appendCall) — plus an errand's call, which is not the work.
func farJournal(calls ...[2]int) []byte {
	lines := []string{
		`{"type":"session","version":1,"id":"n9","cwd":"/tmp/lab"}`,
		`{"type":"message","role":"user","content":"Widen the import pipe."}`,
		`{"type":"call","call":{"model":"cheap/namer","role":"title","input":400,"output":12},"timestamp":"t"}`,
	}
	for i, c := range calls {
		lines = append(lines,
			`{"type":"call","call":{"model":"vendor/worker","input":`+itoa(c[0])+`,"output":`+itoa(c[1])+`},"timestamp":"t"}`,
			`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"c`+itoa(i)+`","function":{"name":"bash","arguments":"{\"command\":\"go test ./...\"}"}}]}`,
			`{"type":"message","role":"tool","toolCallId":"c`+itoa(i)+`","content":"ok"}`)
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// A HOSTED ROOM HAS NO LANE, AND ITS COLUMN USED TO BE EMPTY FOR IT. The page is
// rebuilt from a journal tail read four times a second, so the figures come off
// the same bytes: ↑ the newest request as it was sent, ↓ the output of the
// requests read — and a later read adds only what is new, even when the window
// has slid past the oldest ones.
func TestAHostedRoomsColumnIsFedFromItsJournal(t *testing.T) {
	a, entry := startingRoomLab(t)
	a.width = 100
	a.farRoomRecord = func(uint64, int) (session.TaskRecord, error) { return session.TaskRecord{}, nil }
	a.openRoomFor(9, entry.Title)
	if a.room == nil {
		t.Fatal("the running far row opened no room")
	}
	read := func(journal []byte) {
		a.farRoomRead(roomRecordMsg{gen: a.room.gen, record: session.TaskRecord{Journal: journal}})
	}
	read(farJournal([2]int{21000, 180}, [2]int{24600, 95}))
	if a.room.col.weight != 24600 {
		t.Fatalf("↑ read %d, want the newest request (24600)", a.room.col.weight)
	}
	if a.room.col.down != 275 {
		t.Fatalf("↓'s books read %d, want the two requests' output (275) and not the errand's", a.room.col.down)
	}
	a.tickTokenCol(0, true)
	a.room.dirty = true
	page := farRoomText(a)
	if !strings.Contains(page, "↑ 24.6k") || !strings.Contains(page, "↓ ") {
		t.Fatalf("the hosted page draws no column:\n%s", page)
	}
	// The next read holds one more request; ↓ adds its output and nothing else.
	read(farJournal([2]int{21000, 180}, [2]int{24600, 95}, [2]int{26100, 40}))
	if a.room.col.down != 315 || a.room.col.weight != 26100 {
		t.Fatalf("a later read did not add only the new request: %+v", a.room.col)
	}
	// And the window slides: the oldest request falls off the top of the tail as
	// a new one arrives. ↓ must not walk backwards over what it already counted.
	read(farJournal([2]int{24600, 95}, [2]int{26100, 40}, [2]int{27000, 60}))
	if a.room.col.down != 375 {
		t.Fatalf("a slid window moved ↓ to %d, want 375 — the new request added, nothing lost", a.room.col.down)
	}
}

// ── THE GLOW ────────────────────────────────────────────────────────────────

// glowApp is the live-steps fixture at a clock the test moves by hand, with a
// figure that has just walked to where the books are.
func glowApp(t *testing.T) (*app, func(time.Duration)) {
	t.Helper()
	a := liveStepsApp(t)
	a.width = 100
	at := liveStepsBase.Add(9 * time.Second)
	a.clock = func() time.Time { return at }
	a.turnBegan = liveStepsBase
	// The turn's first reading ARRIVES unlit ([chaseFigure]); what lights ↓ is
	// what comes back after it.
	a.takeContext(14200)
	a.take(session.Usage{Output: 256})
	a.tickTokenCol(1, false)
	a.take(session.Usage{Output: 512})
	for range 40 {
		a.tickTokenCol(1, false)
	}
	return a, func(d time.Duration) { at = at.Add(d) }
}

func glowText(a *app) string {
	return a.turnTokenSuffix(a.conversation(), 0, 100)
}

// THE SIDE THAT HAS JUST MOVED LIGHTS UP, and decays through the palette's own
// stops back to where it rests: the reading ink, then the narration rung, then
// the margin's dim — and its arrow one stop up the fade ramp until it is back.
func TestAFigureThatJustMovedGlowsAndDecaysToRest(t *testing.T) {
	a, advance := glowApp(t)
	text := glowText(a)
	if !strings.Contains(text, a.pal.fade(tokenDownGlyph, 1)+a.pal.ink(" 512")) {
		t.Fatalf("a figure that has just moved is not lit:\n%q", text)
	}
	advance(tokenGlowSpan / 2)
	text = glowText(a)
	if !strings.Contains(text, a.pal.fade(tokenDownGlyph, 1)+a.pal.narr(" 512")) {
		t.Fatalf("half way through the decay the figure is not one stop down:\n%q", text)
	}
	advance(tokenGlowSpan / 2)
	text = glowText(a)
	if !strings.Contains(text, a.pal.fade(tokenDownGlyph, 0)+a.pal.dim(" 512")) {
		t.Fatalf("the figure did not come back to rest:\n%q", text)
	}
}

// WHILE A TOOL RUNS NOTHING MOVES, AND BOTH HALVES REST. That is the reading
// the column exists to give: the shimmer says alive, the still dim figures say
// nothing is arriving right now — waiting, not dead.
func TestBothFiguresRestWhileAToolRuns(t *testing.T) {
	a, advance := glowApp(t)
	// The fixture's last step is a running `git status`; nothing arrives.
	for range 3 {
		advance(tokenGlowSpan / 2)
		a.tickTokenCol(1, false)
	}
	text := glowText(a)
	if !strings.Contains(text, a.pal.fade(tokenUpGlyph, 0)+a.pal.dim(" 14.2k")) ||
		!strings.Contains(text, a.pal.fade(tokenDownGlyph, 0)+a.pal.dim(" 512")) {
		t.Fatalf("a column with nothing moving is not at rest:\n%q", text)
	}
}

// THE TWO HALVES GLOW INDEPENDENTLY, so a person can see which way the traffic
// is going: bytes arriving light ↓ and leave ↑ at rest.
func TestOnlyTheSideThatMovedGlows(t *testing.T) {
	a, advance := glowApp(t)
	advance(tokenGlowSpan)
	a.entries = append(a.entries, entry{kind: entryAssistant, turn: 1, text: strings.Repeat("x", 4000)})
	for range 40 {
		a.tickTokenCol(1, false)
	}
	p := a.tokenPairOf(a.conversation())
	if p.downLit != tokenGlowStops || p.upLit != 0 {
		t.Fatalf("↓ moved and ↑ did not, but the glow reads ↑%d ↓%d", p.upLit, p.downLit)
	}
}

// NO GLOW WHERE FADING CANNOT BE DRAWN — the sixteen-colour profiles and the
// screen-reader tier, which is exactly where [palette.fade] gives up too.
func TestTheGlowIsAbsentWhereTheRampIs(t *testing.T) {
	a, _ := glowApp(t)
	a.pal = newPalette(tokens.ANSI16, false)
	if p := a.tokenPairOf(a.conversation()); p.downLit != 0 || p.upLit != 0 {
		t.Fatalf("a sixteen-colour terminal was handed a glow: %+v", p)
	}
	b, _ := glowApp(t)
	b.linear = true
	if p := b.tokenPairOf(b.conversation()); p.downLit != 0 || p.upLit != 0 {
		t.Fatalf("the screen-reader tier was handed a glow: %+v", p)
	}
}

// ── EXACT DIGITS AND THE RECEIPT ────────────────────────────────────────────

// UNDER TEN THOUSAND EVERY TOKEN IS VISIBLE, spelled the one way this surface
// groups thousands; from ten thousand up the one-decimal word takes over.
func TestAColumnFigureIsExactUnderTenThousand(t *testing.T) {
	for n, want := range map[int]string{
		12: "12", 486: "486", 2531: "2,531", 9999: "9,999",
		10000: "10k", 63600: "63.6k", 1_260_000: "1.3M",
	} {
		if got := tokenColWord(n); got != want {
			t.Fatalf("tokenColWord(%d) = %q, want %q", n, got, want)
		}
	}
	a := newTestApp(&fakeAgent{model: "m"})
	text, _ := a.tokenColumn(tokenPair{up: 2531, down: 486}, 40)
	if got := plain(text); got != "↑ 2,531  ↓ 486" {
		t.Fatalf("the column spelled %q", got)
	}
}

// A JUMP IN ↑ LEAVES A RECEIPT, and only a jump: the first reading of a turn is
// the figure arriving, not growing. It stands for a second and a half and goes.
func TestAJumpInWhatWentUpLeavesAReceiptThatGoes(t *testing.T) {
	a, advance := glowApp(t)
	if p := a.tokenPairOf(a.conversation()); p.receipt != 0 {
		t.Fatalf("the turn's first reading drew a receipt of %d", p.receipt)
	}
	a.takeContext(17600)
	a.tickTokenCol(1, false)
	if got := plain(glowText(a)); !strings.Contains(got, "+3.4k ↑") {
		t.Fatalf("a jump of 3.4k left no receipt beside ↑: %q", got)
	}
	advance(tokenReceiptSpan)
	if got := plain(glowText(a)); strings.Contains(got, "+") {
		t.Fatalf("the receipt outstayed its second and a half: %q", got)
	}
}

// NOT ON THE LINEAR TIER, where the figure is spoken exact each time and an
// annotation on its motion is noise.
func TestTheLinearTierDrawsNoReceipt(t *testing.T) {
	a, _ := glowApp(t)
	a.linear = true
	a.takeContext(17600)
	a.tickTokenCol(1, false)
	if got := plain(glowText(a)); strings.Contains(got, "+") {
		t.Fatalf("the linear tier drew a receipt: %q", got)
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
