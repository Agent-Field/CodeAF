package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// The living tree's own laws: what moves, what does not, and what a running row
// says about itself while it runs.

// treeFixture is one tree with all four lifecycles on it and a clock behind the
// running row, built directly so the assertions are about the BLOCK rather than
// about whatever a fixture's ledger happened to hold.
func treeFixture(clock *blocks.Clock, preview string) *recordTreeBlock {
	since := fixedNow().Add(-90 * time.Second)
	return &recordTreeBlock{
		id: recordTreeID, clock: clock, live: true,
		rows: []treeRow{
			{node: "n1", name: "XhrSyn", life: rail.LifeSettled, depth: 1,
				receipt: "k3", elapsed: 12 * time.Second, hasElapsed: true},
			{node: "n2", name: "H2", life: rail.LifeWorking, depth: 1,
				receipt: "k3", since: since, preview: preview},
			{node: "n3", name: "H2Probe", life: rail.LifeFailed, depth: 1,
				elapsed: 3 * time.Second, hasElapsed: true},
			{node: "n4", name: "KeyCutter", life: rail.LifeQueued, depth: 1, last: true,
				waits: []string{"H2"}},
		},
	}
}

// A RUNNING ROW TURNS AND ITS CLOCK COUNTS, and both come off the ONE animation
// clock so every live cell in a frame agrees about what "now" is (8.1.3).
func TestARunningTreeRowSpinsAndItsClockTicks(t *testing.T) {
	clock := blocks.NewClock(0)
	tree := treeFixture(clock, "")

	clock.Latch(fixedNow())
	first := ansi.Strip(strings.Join(tree.Rows(100), "\n"))
	glyphOne := clock.Glyph()
	if !strings.Contains(first, glyphOne) {
		t.Fatalf("the running row is not wearing the spinner's frame %q:\n%s", glyphOne, first)
	}

	// One house step later the glyph has moved on and the elapsed cell has
	// counted, and NEITHER needed a journal move to do it.
	clock.Latch(fixedNow().Add(blocks.DefaultInterval * 3))
	second := ansi.Strip(strings.Join(tree.Rows(100), "\n"))
	if glyphTwo := clock.Glyph(); glyphTwo == glyphOne {
		t.Fatal("the spinner did not advance between two frames")
	} else if !strings.Contains(second, glyphTwo) {
		t.Fatalf("the running row did not take the new frame %q:\n%s", glyphTwo, second)
	}
	if first == second {
		t.Fatalf("nothing about the running row changed between frames:\n%s", second)
	}

	// A minute on, the number itself has visibly moved.
	clock.Latch(fixedNow().Add(70 * time.Second))
	later := ansi.Strip(strings.Join(tree.Rows(100), "\n"))
	if later == second {
		t.Fatalf("the elapsed cell did not count:\n%s", later)
	}

	// THE SETTLED ROWS DO NOT MOVE. Only work in flight animates (§18.2).
	for _, want := range []string{"XhrSyn", "H2Probe", "KeyCutter"} {
		if !strings.Contains(later, want) {
			t.Fatalf("the tree lost %q:\n%s", want, later)
		}
	}
	for _, frame := range blocks.Spinner {
		if strings.Count(later, frame) > 1 {
			t.Fatalf("more than one row is turning:\n%s", later)
		}
	}
}

// CALM STOPS THE GLYPH AND NOT THE NUMBER (§18, 10.1.5's linear mode): a still
// glyph beside a counting cell is still a row visibly alive.
func TestACalmWindowFreezesTheGlyphAndKeepsTheClock(t *testing.T) {
	clock := blocks.NewClock(0)
	clock.Calm = true
	tree := treeFixture(clock, "")

	clock.Latch(fixedNow())
	first := ansi.Strip(strings.Join(tree.Rows(100), "\n"))
	clock.Latch(fixedNow().Add(70 * time.Second))
	second := ansi.Strip(strings.Join(tree.Rows(100), "\n"))

	for _, frame := range blocks.Spinner {
		if strings.Contains(first, frame) || strings.Contains(second, frame) {
			t.Fatalf("a calm window drew a spinner frame %q:\n%s", frame, first)
		}
	}
	if first == second {
		t.Fatalf("a calm window stopped the clock as well as the glyph:\n%s", second)
	}
}

// A TREE WITH NOTHING RUNNING IS NOT LIVE, which is the battery law: no
// animation may run when nothing is in flight.
func TestASettledTreeIsFinalized(t *testing.T) {
	tree := treeFixture(nil, "")
	if tree.IsFinalized() {
		t.Fatal("a tree with a running part called itself settled")
	}
	for i := range tree.rows {
		tree.rows[i].life = rail.LifeSettled
	}
	tree.live = treeIsLive(tree.rows)
	if !tree.IsFinalized() {
		t.Fatal("a tree with nothing running is still live")
	}
}

// -- the recorder preview --------------------------------------------------------

// THE PREVIEW IS ONE LINE, UNDER THE RUNNING ROW, AND NOWHERE ELSE.
func TestOnlyARunningRowShowsItsRecordersLatestLine(t *testing.T) {
	clock := blocks.NewClock(0)
	clock.Latch(fixedNow())
	tree := treeFixture(clock, "reading internal/http2/frame.go")
	// A settled and a queued row are handed a preview too, and must refuse it:
	// a part that has finished says its RESULT and a part that has not started
	// has said nothing at all.
	tree.rows[0].preview = "this must not be drawn"
	tree.rows[3].preview = "nor this"

	rows := tree.Rows(100)
	joined := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(joined, "reading internal/http2/frame.go") {
		t.Fatalf("the running row shows no recorder line:\n%s", joined)
	}
	for _, forbidden := range []string{"this must not be drawn", "nor this"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("a row that is not running drew a preview (%q):\n%s", forbidden, joined)
		}
	}
	// One line, and one only: five drawn rows for four parts plus the block's
	// own trailing blank.
	if len(rows) != 6 {
		t.Fatalf("the tree drew %d rows, want four parts + one preview + one blank:\n%s",
			len(rows), joined)
	}
	// It hangs at the CHILD indent under its subject, which on this ladder is
	// the name column plus one step.
	var preview string
	for _, row := range rows {
		if strings.Contains(ansi.Strip(row), "reading internal") {
			preview = ansi.Strip(row)
		}
	}
	if got := len(preview) - len(strings.TrimLeft(preview, " │")); got == 0 {
		t.Fatalf("the preview hangs at the row's own edge: %q", preview)
	}
}

// A LINE LONGER THAN THE ROW IS ELLIPSIZED, never wrapped: a preview that grew
// to three rows would push the rest of the tree down every time a worker wrote a
// long sentence (§19).
func TestTheRecorderPreviewIsAlwaysOneLine(t *testing.T) {
	clock := blocks.NewClock(0)
	clock.Latch(fixedNow())
	long := strings.Repeat("the worker is explaining itself at length. ", 12)
	tree := treeFixture(clock, long+"\nand then it wrote a second line")

	rows := tree.Rows(60)
	if len(rows) != 6 {
		t.Fatalf("a long preview grew the tree to %d rows:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	for _, row := range rows {
		if width := blocks.Width(ansi.Strip(row)); width > 60 {
			t.Fatalf("a row is %d cells wide at a 60-cell measure: %q", width, ansi.Strip(row))
		}
	}
}

// AND IT CHANGES AS THE RECORDER MOVES. The changing text IS the animation
// (§11 stays at three motions): the parent's spinner carries the movement and
// the line underneath it carries the news.
func TestTheRecorderPreviewFollowsTheRecorder(t *testing.T) {
	first := "── turn 1  finish=tool_calls ──\n" +
		"text: reading the spec first.\n" +
		`call sh {"cmd":"cat spec.md"}` + "\n"
	if got := recorderLine(first); got == "" {
		t.Fatal("a recorder with two rows in it previewed nothing")
	}
	before := recorderLine(first)

	grown := first + "  → 40B: ok\n" +
		"text: now porting the row reader.\n"
	after := recorderLine(grown)
	if after == before {
		t.Fatalf("the preview did not follow the recorder: still %q", before)
	}
	if !strings.Contains(after, "porting the row reader") {
		t.Fatalf("the preview is not the recorder's latest human line: %q", after)
	}
	// A machine stream is not a preview: clampStreams folds it into one bounded
	// row and the last thing a PERSON wrote is what the line says.
	machine := grown
	for i := 0; i < 6; i++ {
		machine += `{"event":"progress","pct":` + string(rune('0'+i)) + "}\n"
	}
	if got := recorderLine(machine); got != after {
		t.Fatalf("a machine feed reached the preview: %q", got)
	}
}

// A RUNNING PART IN A REAL ROOM PREVIEWS THE RECORDER ITS WORK ACTUALLY WENT
// INTO — which is the SPLICE's file and not the node's, because a planner
// splices every part in one transaction and they share one recorder.
func TestARunningPartPreviewsTheSharedRecorder(t *testing.T) {
	backend := deepBoard()
	// Every part of job-5/port shares its parent's created sequence, which is
	// the shape the executor actually writes.
	for i := range backend.nodes {
		switch backend.nodes[i].ID {
		case "job-5/port/a", "job-5/port/b":
			backend.nodes[i].CreatedSeq = 52
		}
	}
	running := "── turn 1  finish=tool_calls ──\n" +
		"text: rewriting the body pass.\n" +
		`call sh {"cmd":"go test ./importer"}` + "\n"
	commander := &tracingCommander{traces: map[string]string{"job-5/port": running}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	openRecord(t, app, "job-5", "importer")
	if cmd := app.readTraceCmd("job-5"); cmd != nil {
		if msg, ok := cmd().(traceReadMsg); ok {
			app.applyTraceRead(msg)
		}
	}

	tree, ok := recordTree(app)
	if !ok {
		t.Fatal("the deep job drew no tree")
	}
	var running0 treeRow
	for _, row := range tree.rows {
		if row.node == "job-5/port/b" {
			running0 = row
		}
	}
	if running0.node == "" {
		t.Fatal("the tree has no row for the running grandchild")
	}
	if running0.preview == "" {
		t.Fatalf("the running part previews nothing though its splice has a recorder: %+v", running0)
	}
	if !strings.Contains(ansi.Strip(app.Frame(140, 40)), running0.preview) {
		t.Fatal("the preview is not on screen")
	}
}

// -- the money on a row ----------------------------------------------------------

// §13 at the row: "every tree row carries its own `$ · elapsed`". The reader's
// screenshot had neither — the parts tree said `scaffold (4m)` and `implement
// (waits: scaffold)` and nothing else, four minutes into a job that was billing
// — because the page was handed no ledger and, once it had one, never repainted
// on a receipt landing (record.go's [recordStamp]).
//
// A ROW THAT BILLED NOTHING STILL SHOWS NOTHING. That is the same law from the
// other side (§16, 8.2.20): `$0.00` on a part that has not run is a settled
// figure over an unsettled fact, and the two must not look alike.
func TestATreeRowCarriesTheMoneyItsPartBilled(t *testing.T) {
	graph := roomJournal(t)
	app := newJournalApp(t, graph, &fakeCommander{model: "z-ai/glm-5.2"}, nil)
	poll(t, app)
	enterNodeRoom(t, app, "craft")

	if err := graph.RecordUsage(store.NodeUsage{NodeID: "craft/scaffold", Cost: 0.31,
		PromptTokens: 12000, CompletionTokens: 900, Model: "deepseek/deepseek-v4-flash"}); err != nil {
		t.Fatal(err)
	}
	poll(t, app)

	tree, ok := recordTree(app)
	if !ok {
		t.Fatal("the job drew no tree")
	}
	rows := make(map[string]string, len(tree.rows))
	for _, row := range tree.rows {
		rows[row.name] = row.receipt
	}
	billed := rows["scaffold"]
	if !strings.Contains(billed, "$0.31") {
		t.Fatalf("the part that billed carries no money: %q", billed)
	}
	if !strings.Contains(billed, "tok") {
		t.Fatalf("the part that billed carries no burn: %q", billed)
	}
	if !strings.Contains(billed, modelWord("deepseek/deepseek-v4-flash")) {
		t.Fatalf("the row does not name what ran: %q", billed)
	}
	for _, quiet := range []string{"implement", "verify"} {
		if got := rows[quiet]; got != "" {
			t.Fatalf("the unbilled part %q claimed a receipt: %q", quiet, got)
		}
	}
	// And it is drawn: right of the name, in the row's own dim column, with the
	// clock last (§16's right edge is a column of figures).
	line := ""
	for _, drawn := range tree.Rows(100) {
		if strings.Contains(ansi.Strip(drawn), "scaffold") {
			line = ansi.Strip(drawn)
		}
	}
	if !strings.Contains(line, "$0.31") {
		t.Fatalf("the money is not on the row as drawn: %q", line)
	}
	if strings.Index(line, "scaffold") > strings.Index(line, "$0.31") {
		t.Fatalf("the receipt is drawn left of the name it belongs to: %q", line)
	}
	if strings.Contains(ansi.Strip(app.Frame(140, 30)), "$0.00") {
		t.Fatal("a part that billed nothing was priced at zero")
	}
}
