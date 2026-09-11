package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// liveStepsBase is the fixture's zero so a test can name a moment in it.
var liveStepsBase = time.Unix(100, 0)

// liveStepsFixture is FOUR STEPS OF A TURN THAT IS STILL RUNNING, each one a
// narrating line and the calls under it, and a reasoning block above all of them.
//
// The calls are deliberately SEQUENTIAL — every batch begins after the one
// before it ended — because that is what makes them four steps rather than one
// (caption.go's parallel-calls law). Every argument in it is a word no caption
// contains, so a test can ask whether the machinery leaked by asking for the
// argument.
func liveStepsFixture() []entry {
	base := liveStepsBase
	at := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
	return []entry{
		{kind: entryUser, text: "why is the loader slow?", turn: 1},
		{kind: entryThinking, text: "it is probably reading the whole tree up front", turn: 1,
			settled: true, began: at(0), ended: at(1)},
		{kind: entryAssistant, text: "Reading the loader first.", turn: 1, settled: true},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK,
			detail: toolDetail{Args: `{"path":"internal/loader/prefix.go"}`}, began: at(1), ended: at(2)},
		{kind: entryAssistant, text: "Searching the tree for the caller.", turn: 1, settled: true},
		{kind: entryTool, tool: "grep", turn: 1, status: toolOK,
			detail: toolDetail{Args: `{"pattern":"needle-pattern"}`}, began: at(3), ended: at(4)},
		{kind: entryAssistant, text: "Running the suite.", turn: 1, settled: true},
		{kind: entryTool, tool: "bash", turn: 1, status: toolOK,
			detail: toolDetail{Args: `{"command":"go test ./internal/loader/"}`}, began: at(5), ended: at(6)},
		{kind: entryAssistant, text: "Checking what changed.", turn: 1, settled: true},
		{kind: entryTool, tool: "bash", turn: 1, status: toolRunning,
			detail: toolDetail{Args: `{"command":"git status --porcelain"}`}, began: at(7)},
	}
}

func liveStepsApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = liveStepsFixture()
	a.turn, a.state = 1, stateWorking
	a.clock = func() time.Time { return liveStepsBase.Add(9 * time.Second) }
	a.touch()
	return a
}

func livePage(a *app) string { return strings.Join(plainRows(a), "\n") }

// showLiveWork opens the running turn's compact window.
//
// IT IS THE ONE LINE A TEST ABOUT A LIVE TOOL ROW NOW OWES, and it is a helper
// rather than a copied pair of statements so that the reason is written once: a
// running turn's machinery stands behind three lines until somebody asks for it
// (livesteps.go), so a test whose subject is the machinery makes the gesture a
// reader makes. It asserts there is a window to open, which keeps it from
// quietly passing on a page where the work never appeared at all.
// It is written as the turn's own key rather than as a window it has to find,
// so that it can be said BEFORE the calls it is about arrive: the block is keyed
// by the turn ([liveWork.turn]) exactly so that opening the work and watching it
// fill is one control.
func showLiveWork(t *testing.T, a *app) {
	t.Helper()
	if a.turn == 0 || a.state != stateWorking {
		t.Fatalf("no running turn to open the work of (turn %d, state %v)", a.turn, a.state)
	}
	a.setWorkOpen(a.bodyDeck(), a.turn, true)
	a.touch()
}

// liveRowTexts is the PAINTED rows, because the shimmer moves colour and not
// letters: a test that stripped the escapes would see one still photograph.
func liveRowTexts(a *app) []string {
	out := make([]string, 0, len(rows(a)))
	for _, r := range rows(a) {
		out = append(out, r.text)
	}
	return out
}

// ── THE COMPACT WINDOW ──────────────────────────────────────────────────────

// THE RUNNING TURN IS THREE LINES OF WHAT IT IS DOING. The newest steps, in the
// model's own words, under the question — and no reasoning, no tool name, no
// argument and no output anywhere near them.
func TestARunningTurnDrawsThreeStepsAndNoMachinery(t *testing.T) {
	a := liveStepsApp(t)
	page := livePage(a)

	for _, want := range []string{
		"why is the loader slow?",
		"Searching the tree for the caller",
		"Running the suite",
		"Checking what changed",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the running turn does not say %q:\n%s", want, page)
		}
	}
	// The fourth step back has rolled out of the window: three is the budget.
	if strings.Contains(page, "Reading the loader first") {
		t.Fatalf("the window kept a fourth step:\n%s", page)
	}
	// AND NOT ONE FACT ABOUT THE MACHINERY.
	for _, leak := range []string{
		"probably reading the whole tree", // the reasoning
		"prefix.go", "needle-pattern",     // the arguments
		"go test ./internal/loader/", "git status --porcelain",
		"grep", "2 calls", "1 call",
	} {
		if strings.Contains(page, leak) {
			t.Fatalf("the compact window leaked %q:\n%s", leak, page)
		}
	}
	// AND THE BUDGET IS ROWS. Three step titles, three rows, at this width.
	compact := 0
	for _, r := range rows(a) {
		if r.hit == hitWorkFold && r.turn == 1 {
			compact++
		}
	}
	if compact != liveStepRows {
		t.Fatalf("the window drew %d rows, want %d:\n%s", compact, liveStepRows, page)
	}
}

// THE OLDER LINES FADE AND THE LIVE ONE MOVES. Three stops of one ladder, and
// exactly one of the three carries the shimmer.
func TestOnlyTheStepThatIsRunningShimmers(t *testing.T) {
	a := liveStepsApp(t)
	a.pal = newPalette(tokens.TrueColor, false)
	captionTimeAt(a, 0)
	before := liveRowTexts(a)
	captionTimeAt(a, shimmerPeriod/2)
	a.touch()
	after := liveRowTexts(a)

	if len(before) != len(after) {
		t.Fatalf("the frame changed height between paints: %d then %d", len(before), len(after))
	}
	moved := []int{}
	for i := range before {
		if before[i] != after[i] {
			moved = append(moved, i)
		}
	}
	if len(moved) != 1 {
		t.Fatalf("want exactly one moving row, got %d: %v\n%s", len(moved), moved, livePage(a))
	}
	if got := plain(after[moved[0]]); !strings.Contains(got, "Checking what changed") {
		t.Fatalf("the moving row is not the running step: %q", got)
	}
	// AND THE THREE ROWS ARE THREE TIERS. A window that painted them all alike
	// would be saying nothing about which end is new.
	var painted []string
	for _, r := range rows(a) {
		if r.hit == hitWorkFold && r.turn == 1 {
			painted = append(painted, r.text)
		}
	}
	if len(painted) != 3 {
		t.Fatalf("want three compact rows, got %d", len(painted))
	}
	if tierOf(t, painted[0]) == tierOf(t, painted[1]) {
		t.Fatalf("the oldest two steps share a tier:\n%q\n%q", painted[0], painted[1])
	}
}

// tierOf is the row's first colour escape — which stop of the ladder it was
// painted at. The row opens with the indent law's two cells, so the sequence is
// looked for rather than assumed to be at the front.
func tierOf(t *testing.T, s string) string {
	t.Helper()
	from := strings.Index(s, "\x1b[")
	if from < 0 {
		t.Fatalf("row carries no colour at all: %q", s)
	}
	to := strings.IndexByte(s[from:], 'm')
	if to < 0 {
		t.Fatalf("row carries a malformed escape: %q", s)
	}
	return s[from : from+to+1]
}

// A TURN THAT HAS ONLY THOUGHT INVENTS NOTHING. It has a truthful Working
// state and a visible door onto hidden reasoning, but no invented step.
func TestATurnWithNoStepsYetKeepsAVisibleWorkDoor(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = []entry{
		{kind: entryUser, text: "why is the loader slow?", turn: 1},
		{kind: entryThinking, text: "it is probably reading the whole tree", turn: 1,
			began: liveStepsBase, ended: liveStepsBase.Add(time.Second)},
	}
	a.turn, a.state = 1, stateWorking
	a.touch()

	page := livePage(a)
	if strings.Contains(page, "probably reading") {
		t.Fatalf("the reasoning was drawn under the compact window:\n%s", page)
	}
	for _, invented := range []string{"step 1", "working on it", "step"} {
		if strings.Contains(strings.ToLower(page), invented) {
			t.Fatalf("the surface invented %q with no step to report:\n%s", invented, page)
		}
	}
	doors := 0
	for _, r := range rows(a) {
		if r.hit == hitWorkFold {
			doors++
			if !strings.Contains(plain(r.text), "Working · ctrl+e") || !r.activity {
				t.Fatalf("hidden work has no truthful activity door: %q", plain(r.text))
			}
		}
	}
	if doors != 1 {
		t.Fatalf("want one visible work door, got %d: %s", doors, page)
	}
	if !a.toggleLatestWorkfold() {
		t.Fatal("the visible work door could not open")
	}
	if !strings.Contains(livePage(a), "probably reading") {
		t.Fatal("opening work lost reasoning")
	}
}

// ── THE DOORS ───────────────────────────────────────────────────────────────

// A CLICK ANYWHERE ON THE WINDOW OPENS THE OUTLINE, through the one resolver a
// real click goes through.
func TestClickingTheCompactWindowOpensTheOutline(t *testing.T) {
	a := liveStepsApp(t)
	a.height = 40
	a.touch()
	page := livePage(a)

	top := a.bodyTop()
	pressed := false
	for y := top; y < top+a.viewHeight(); y++ {
		if r, ok := a.rowAt(y); ok && r.hit == hitWorkFold && r.turn == 1 {
			drive(t, a, tea.MouseClickMsg{Y: y, Button: tea.MouseLeft})
			drive(t, a, tea.MouseReleaseMsg{Y: y, Button: tea.MouseLeft})
			pressed = true
			break
		}
	}
	if !pressed {
		t.Fatalf("no visible row is the running turn's window:\n%s", page)
	}
	opened := livePage(a)
	for _, want := range []string{
		"Reading the loader first", // every step, not only the last three
		"Checking what changed",    // including the live one
		"git status --porcelain",   // the running step's own calls
		liveWorkWord + " · ctrl+e", // and the way back out
	} {
		if !strings.Contains(opened, want) {
			t.Fatalf("the opened outline does not carry %q:\n%s", want, opened)
		}
	}
	// A PAST STEP STAYS SHUT UNDER THE OUTLINE ([app.captionCallsOpen]).
	if strings.Contains(opened, "needle-pattern") {
		t.Fatalf("opening the window dumped every call:\n%s", opened)
	}
}

// AND ctrl+e IS THE SAME DOOR, AND THE WAY BACK. It is the key the open window
// names, and the key the finished chip names, because it is the same chip.
func TestTheDisclosureKeyOpensAndShutsTheWholeWindow(t *testing.T) {
	a := liveStepsApp(t)
	if !a.toggleLatestWorkfold() {
		t.Fatal("ctrl+e found no running work to open")
	}
	if !strings.Contains(livePage(a), "git status --porcelain") {
		t.Fatalf("the key did not open the outline:\n%s", livePage(a))
	}
	if !a.toggleLatestWorkfold() {
		t.Fatal("ctrl+e found nothing to shut again")
	}
	shut := livePage(a)
	if strings.Contains(shut, "git status --porcelain") {
		t.Fatalf("the key did not shut the window again:\n%s", shut)
	}
	if !strings.Contains(shut, "Checking what changed") {
		t.Fatalf("the compact window did not come back:\n%s", shut)
	}
}

// AND THE KEY THAT MEANS "SHOW ME THE REST OF THIS" STILL MEANS IT. ctrl+o over
// a running turn drops the window entirely rather than pressing against a door
// it has already opened — and the second press brings the compact block back.
func TestAskingForEverythingDropsTheWindowAndPuttingItBackRestoresIt(t *testing.T) {
	a := liveStepsApp(t)
	a.unfold(1)
	opened := livePage(a)
	for _, want := range []string{"needle-pattern", "git status --porcelain"} {
		if !strings.Contains(opened, want) {
			t.Fatalf("ctrl+o did not show the machinery — %q missing:\n%s", want, opened)
		}
	}
	a.unfold(1)
	shut := livePage(a)
	if strings.Contains(shut, "needle-pattern") {
		t.Fatalf("the second press did not fold the machinery back:\n%s", shut)
	}
	if !strings.Contains(shut, "Checking what changed") {
		t.Fatalf("the compact window did not come back:\n%s", shut)
	}
}

// EVERY STEP KEEPS ITS OWN DOOR ONTO ITS OWN CALLS once the outline is open.
func TestEachStepInTheOpenWindowStillOpensItsOwnCalls(t *testing.T) {
	a := liveStepsApp(t)
	a.toggleLatestWorkfold()

	d := a.bodyDeck()
	stampHierarchy(d.entries, a.deckFolds(d))
	caps := deriveCaptions(d.entries, d.runningTurn)
	var searching caption
	for _, c := range caps {
		if strings.Contains(captionText(c), "Searching the tree") {
			searching = c
		}
	}
	if searching.text == "" {
		t.Fatalf("the outline has no searching step: %#v", caps)
	}
	a.toggleCap(searching.start)
	if !strings.Contains(livePage(a), "needle-pattern") {
		t.Fatalf("opening one step did not show its call:\n%s", livePage(a))
	}
}

// ── COMPLETION ALWAYS COLLAPSES THE WORK ────────────────────────────────────

// The owner's ruling, and the one place a reader's expansion does not outlive
// what it was about: a turn that is over is an exchange again.
func TestCompletionCollapsesWorkTheReaderOpenedWhileItRan(t *testing.T) {
	a := liveStepsApp(t)
	a.toggleLatestWorkfold()
	if !a.workOpen[1] {
		t.Fatal("the reader's expansion was not recorded")
	}

	a.entries[9].status, a.entries[9].ended = toolOK, liveStepsBase.Add(8*time.Second)
	a.entries = append(a.entries, entry{kind: entryAssistant, turn: 1, settled: true,
		text: "The loader reads the whole tree up front."})
	a.settle()
	a.touch()

	page := livePage(a)
	if !strings.Contains(page, "The loader reads the whole tree up front.") {
		t.Fatalf("the answer did not survive the collapse:\n%s", page)
	}
	if !strings.Contains(page, "worked") {
		t.Fatalf("the finished turn drew no chip:\n%s", page)
	}
	for _, gone := range []string{"git status --porcelain", "Checking what changed", "Running the suite"} {
		if strings.Contains(page, gone) {
			t.Fatalf("the work stayed open after the turn finished — %q:\n%s", gone, page)
		}
	}
	if _, kept := a.workOpen[1]; kept {
		t.Fatalf("the expansion outlived the turn: %#v", a.workOpen)
	}
}

// ── WHAT THE WINDOW MAY NEVER COVER ─────────────────────────────────────────

// The person's own words, a failed call and this surface's own notice all stand
// outside the window, and the step that failed is kept WHOLE rather than half
// compacted.
func TestTheWindowNeverCoversWordsFailuresOrNotices(t *testing.T) {
	base := liveStepsBase
	at := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = []entry{
		{kind: entryUser, text: "why is the loader slow?", turn: 1},
		{kind: entryAssistant, text: "Reading the loader first.", turn: 1, settled: true},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK,
			detail: toolDetail{Args: `{"path":"internal/loader/prefix.go"}`}, began: at(1), ended: at(2)},
		{kind: entrySteer, text: "check the parser instead", turn: 1,
			steer: &steerElbow{words: "check the parser instead", at: at(2)}},
		{kind: entryNote, text: "context is 84% full", turn: 1},
		{kind: entryAssistant, text: "Running the suite.", turn: 1, settled: true},
		{kind: entryTool, tool: "bash", turn: 1, status: toolFailed,
			text: "go test ./internal/loader/", detail: toolDetail{Args: `{"command":"go test ./internal/loader/"}`,
				Output: "loader_test.go:12: prefix mismatch"}, began: at(3), ended: at(4)},
		{kind: entryAssistant, text: "Checking what changed.", turn: 1, settled: true},
		{kind: entryTool, tool: "bash", turn: 1, status: toolRunning,
			detail: toolDetail{Args: `{"command":"git status --porcelain"}`}, began: at(5)},
	}
	a.turn, a.state = 1, stateWorking
	a.clock = func() time.Time { return at(6) }
	a.touch()

	page := livePage(a)
	for _, want := range []string{
		"check the parser instead", // their correction
		"context is 84% full",      // this surface's own voice
		"Running the suite",        // the failed step's own heading, kept whole
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the window swallowed %q:\n%s", want, page)
		}
	}
	// AND THE FAILURE IS DRAWN WHERE IT HAPPENED, AS MACHINERY. The step that
	// failed keeps the outline row it has always had — its own door and its own
	// count — rather than becoming a faded line in a window that says nothing
	// went wrong. Whether a PAST step shows its calls unasked is
	// [app.captionCallsOpen]'s answer and this block does not change it.
	failed := false
	for _, r := range rows(a) {
		if r.hit == hitCaption && strings.Contains(plain(r.text), "Running the suite") {
			failed = true
		}
		if r.hit == hitWorkFold && strings.Contains(plain(r.text), "Running the suite") {
			t.Fatalf("a failed step was compacted into the window: %q", plain(r.text))
		}
	}
	if !failed {
		t.Fatalf("the failed step lost its outline row:\n%s", page)
	}
	// The step still running is compact all the same.
	if !strings.Contains(page, "Checking what changed") || strings.Contains(page, "git status --porcelain") {
		t.Fatalf("the live step is not compact:\n%s", page)
	}
}

// AND A FAILURE AT THE FRONTIER SPEAKS AT ONCE. The newest step is the one whose
// calls are open on a running turn ([app.captionCallsOpen]), so a call that has
// just failed is on the page with its row — never behind a rolling window.
func TestAFailureAtTheFrontierIsDrawnWithItsRow(t *testing.T) {
	base := liveStepsBase
	at := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = []entry{
		{kind: entryUser, text: "why is the loader slow?", turn: 1},
		{kind: entryAssistant, text: "Reading the loader first.", turn: 1, settled: true},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK,
			detail: toolDetail{Args: `{"path":"internal/loader/prefix.go"}`}, began: at(1), ended: at(2)},
		{kind: entryAssistant, text: "Running the suite.", turn: 1, settled: true},
		{kind: entryTool, tool: "bash", turn: 1, status: toolFailed, text: "go test ./internal/loader/",
			detail: toolDetail{Args: `{"command":"go test ./internal/loader/"}`,
				Output: "loader_test.go:12: prefix mismatch"}, began: at(3), ended: at(4)},
	}
	a.turn, a.state = 1, stateWorking
	a.clock = func() time.Time { return at(5) }
	a.touch()

	page := livePage(a)
	if !strings.Contains(page, "go test") {
		t.Fatalf("the failed call is not on the page:\n%s", page)
	}
	if !strings.Contains(page, "Reading the loader first") {
		t.Fatalf("the step before the failure was swallowed:\n%s", page)
	}
}

// ── NARROW FRAMES AND STILL TERMINALS ───────────────────────────────────────

// A NARROW FRAME NEVER CHOPS A STEP TITLE. The oldest step leaves the window
// before a word leaves a sentence, and nothing ever ends in an ellipsis.
func TestANarrowFrameWrapsTheWindowAndKeepsWholeTitles(t *testing.T) {
	a := liveStepsApp(t)
	a.width = 34
	a.touch()

	page := livePage(a)
	if strings.Contains(page, "…") || strings.Contains(page, "...") {
		t.Fatalf("a step title was chopped:\n%s", page)
	}
	for _, word := range strings.Fields("Checking what changed") {
		if !strings.Contains(page, word) {
			t.Fatalf("the running step lost %q on a narrow frame:\n%s", word, page)
		}
	}
	// THE BUDGET IS ROWS, so a wrapped title costs a step rather than the frame.
	compact := 0
	for _, r := range rows(a) {
		if r.hit == hitWorkFold && r.turn == 1 {
			compact++
		}
	}
	if compact > liveStepRows {
		t.Fatalf("the window spent %d rows on a narrow frame:\n%s", compact, page)
	}
}

// A TERMINAL THAT WILL NOT ANIMATE READS THE SAME WORDS, STILL. Motion is the
// one thing the linear tier drops, and the sentence is never the motion.
func TestTheLinearTierDrawsTheWindowStill(t *testing.T) {
	a := liveStepsApp(t)
	a.linear = true
	a.touch()
	captionTimeAt(a, 0)
	before := liveRowTexts(a)
	captionTimeAt(a, shimmerPeriod/2)
	a.touch()
	after := liveRowTexts(a)

	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("the linear tier animated row %d: %q then %q", i, before[i], after[i])
		}
	}
	page := livePage(a)
	for _, want := range []string{"Running the suite", "Checking what changed"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the still window lost %q:\n%s", want, page)
		}
	}
}

// ── ONE SURFACE ONLY ────────────────────────────────────────────────────────

// A node transcript is an explicitly detailed view. Its lens does not opt
// into compact live work; task rooms are covered by roomcompact_test.go.
func TestTheNodeTranscriptKeepsItsMachinery(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 80, 40
	for _, l := range []lens{transcriptLens} {
		d := deck{entries: liveStepsFixture(), unfolded: map[int]bool{},
			workOpen: map[int]bool{}, capOpen: map[int]bool{}, lens: l, runningTurn: 1}
		d.captions = deriveCaptions(d.entries, d.runningTurn)
		if got := deriveLiveWork(d); len(got) != 0 {
			t.Fatalf("a page that is not the conversation derived a window: %#v", got)
		}
		drawn, _ := a.deckRows(d, 60)
		var page strings.Builder
		for _, r := range drawn {
			page.WriteString(plain(r.text) + "\n")
		}
		if !strings.Contains(page.String(), "git status --porcelain") {
			t.Fatalf("the page lost its live machinery:\n%s", page.String())
		}
	}
}

// AND A COMPLETED TURN IS UNTOUCHED. With no turn running there is no window at
// all, and the chip is what it always was.
func TestACompletedTurnDerivesNoWindow(t *testing.T) {
	a := liveStepsApp(t)
	a.state, a.turn = stateIdle, 0
	d := a.conversation()
	d.captions = deriveCaptions(d.entries, d.runningTurn)
	if got := deriveLiveWork(d); len(got) != 0 {
		t.Fatalf("a finished conversation derived a window: %#v", got)
	}
}

// A completed caption must never be relit as ongoing just because the model
// has not answered yet. The real frame handler advances only its separate inline mark.
func TestBetweenCallsThePaintClockMovesOnlyCurrentActivity(t *testing.T) {
	a := liveStepsApp(t)
	a.pal = newPalette(tokens.TrueColor, false)
	last := &a.entries[len(a.entries)-1]
	last.status, last.ended = toolOK, liveStepsBase.Add(8*time.Second)
	captionTimeAt(a, 500*time.Millisecond)
	a.touch()
	before := rows(a)
	activity, captions := 0, 0
	for _, r := range before {
		if r.hit == hitWorkFold {
			captions++
			if r.activity {
				activity++
				if !strings.Contains(plain(r.text), "Checking what changed  ·") || strings.Contains(plain(r.text), "Working") {
					t.Fatalf("waiting replaced the useful caption: %s", plain(r.text))
				}
			}
		}
	}
	if activity != 1 || captions != liveStepRows {
		t.Fatalf("activity=%d rows=%d", activity, captions)
	}
	captionTimeAt(a, 800*time.Millisecond)
	a.paint()
	after := rows(a)
	if len(before) != len(after) {
		t.Fatal("activity changed the page height")
	}
	moved := 0
	for i, r := range before {
		if r.text != after[i].text {
			moved++
			if !r.activity {
				t.Fatalf("finished work moves at row %d: %q", i, plain(r.text))
			}
		}
	}
	if moved != 1 {
		t.Fatalf("real paint clock moved %d rows, want only the current status", moved)
	}
	for _, r := range after {
		if r.hit != hitWorkFold && strings.Contains(plain(r.text), a.pulse()) {
			t.Fatal("compact activity kept a second pulse")
		}
	}
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "The loader reads the tree once.", turn: 1})
	a.touch()
	if hasCompactActivity(rows(a)) {
		t.Fatal("old compact work claimed activity while the answer writes")
	}
}

func TestPendingActivityKeepsWaitDetailsAcrossFullFrameTransitions(t *testing.T) {
	forgetPhases()
	t.Cleanup(forgetPhases)
	a := liveStepsApp(t)
	a.width, a.height = 140, 40
	a.model = phaseModel
	now := liveStepsBase.Add(20 * time.Second)
	a.clock = func() time.Time { return now }
	a.frameBody()
	last := &a.entries[len(a.entries)-1]
	last.status, last.ended = toolOK, liveStepsBase.Add(8*time.Second)
	a.awaited = now.Add(-12 * time.Second)
	a.live = -1
	a.touch()
	frame, _, _ := a.frameBody()
	if !strings.Contains(plain(frame), "awaiting response · 12s") {
		t.Fatalf("between-call wait vanished: %s", plain(frame))
	}
	PostPhaseNews(PhaseNews{Model: a.model, Role: lane.RoleTalk, Phase: provider.PhaseRetrying, At: now, Since: now.Add(-2 * time.Second), Detail: "2 of 6"})
	for _, open := range []bool{false, true, false} {
		a.setWorkOpen(a.conversation(), a.turn, open)
		a.touch()
		frame, _, _ = a.frameBody()
		if got := strings.Count(plain(frame), "2 of 6"); got != 1 {
			t.Fatalf("open=%v phase appeared %d times on transition: %s", open, got, plain(frame))
		}
	}
	a.entries = append(a.entries, entry{kind: entryThinking, turn: 1, text: "private reasoning", began: now})
	a.live = len(a.entries) - 1
	a.lastDelta = time.Now()
	a.touch()
	frame, _, _ = a.frameBody()
	if got := strings.Count(plain(frame), "2 of 6"); got != 1 {
		t.Fatalf("streaming reasoning drew phase %d times: %s", got, plain(frame))
	}
}

func TestPendingActivityPreservesAnOversizedFinishedCaption(t *testing.T) {
	a := liveStepsApp(t)
	description := "reading the complete configuration and checking every startup setting"
	w := liveWork{key: 1, turn: 1, pending: true, steps: []caption{{text: description, ended: liveStepsBase}}}
	out := a.liveStepBlock(w, 20, a.conversation())
	w.pending = false
	before := a.liveStepBlock(w, 20, a.conversation())
	if len(out) != len(before) || !hasCompactActivity(out) {
		t.Fatalf("pending changed the caption height or lost activity: %#v", out)
	}
	for i, r := range out {
		if ansi.StringWidth(r.text) > 20-workIndentCols(20) {
			t.Fatalf("waiting overflowed: %q", plain(r.text))
		}
		if !strings.Contains(plain(r.text), strings.TrimSpace(string([]rune(plain(before[i].text))[2:]))) {
			t.Fatalf("waiting lost caption words: %q -> %q", plain(before[i].text), plain(r.text))
		}
	}
}

func TestThePreCaptionDoorWrapsWithinTheMinimumWidth(t *testing.T) {
	a := liveStepsApp(t)
	for _, width := range []int{8, 9, 20} {
		out := a.liveStepBlock(liveWork{key: 1, turn: 1, pending: true}, width, deck{lens: participantLens})
		for _, r := range out {
			if ansi.StringWidth(r.text) > width-workIndentCols(width) {
				t.Fatalf("width %d overflowed: %q", width, plain(r.text))
			}
			if r.hit != hitWorkFold {
				t.Fatal("wrapped activity lost its disclosure")
			}
		}
	}
}

// A room has its own worker. The main chat's retry/wait clock cannot describe
// that worker, whether the room is before its first caption or between calls.
func TestRoomLiveActivityDoesNotBorrowTheParentPhase(t *testing.T) {
	now := time.Now()
	a := phaseApp(t, now)
	PostPhaseNews(PhaseNews{
		Model: phaseModel, Role: lane.RoleTalk, Phase: provider.PhaseRetrying,
		At: now, Since: now.Add(-2 * time.Second), Detail: "2 of 6",
	})
	for _, afterCaption := range []bool{false, true} {
		w := liveWork{key: 0, turn: 1, pending: true}
		if afterCaption {
			w.steps = []caption{{text: "Reading the loader", ended: now.Add(-time.Second)}}
		}
		for _, tc := range []struct {
			name       string
			lens       lens
			wantsPhase bool
		}{
			{"conversation", participantLens, true},
			{"task room", overseerLens, false},
		} {
			var text strings.Builder
			for _, row := range a.liveStepBlock(w, 100, deck{lens: tc.lens}) {
				text.WriteString(plain(row.text))
				text.WriteByte('\n')
			}
			if got := strings.Contains(text.String(), "2 of 6"); got != (tc.wantsPhase && !afterCaption) {
				t.Fatalf("%s afterCaption=%v inherited phase=%v: %s", tc.name, afterCaption, got, text.String())
			}
			if !tc.wantsPhase && !strings.Contains(text.String(), map[bool]string{false: "Working", true: "Reading the loader"}[afterCaption]) {
				t.Fatalf("room lost truthful working state: %s", text.String())
			}
		}
	}
}
