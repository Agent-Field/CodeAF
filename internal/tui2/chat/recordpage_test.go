package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// THE CHRONOLOGICAL RECORD (user-amended 2026-08-11), driven through the real
// app and read off the page's own block list.
//
// The order under test is the reader's own sketch: the prompt card at the top,
// the work in the middle, the result at the bottom — and the answer-first
// reading it replaced is paid back by the SCROLL POSITION, which is the second
// half of these tests.

// openRecord opens one node's record page and folds its trail back in, which is
// what the runtime does with the commands the enter key returns. It goes through
// the node id rather than through a rail digit, because these tests are about
// pages whose fixtures do not all sit on the home rail.
func openRecord(t *testing.T, app *App, node, name string) {
	t.Helper()
	app.openTaskRoom(rail.Row{ID: node, Name: name, Life: app.source.rowLife(node)}, node)
	if app.view == nil || app.view.kind != viewNode || app.view.node != node {
		t.Fatalf("the record did not open on %q: %+v", node, app.view)
	}
	if cmd := app.readNodeCmd(node, 0); cmd != nil {
		if trail, ok := cmd().(nodeMessagesMsg); ok {
			app.applyNodeMessages(trail)
		}
	}
}

// atomicBoard is ONE WORKER and no parts: the shape variant A is about. It has
// settled, it wrote a summary, and nobody announced its ending — which is the
// ordinary case for a sub-job (`announceNode` only posts for a node parented on
// the spine).
func atomicBoard() *boardBackend {
	backend := board()
	backend.nodes = append(backend.nodes,
		store.Node{ID: "job-4", Title: "haiku", Status: store.Done,
			CreatedSeq: 30, UpdatedSeq: 40,
			Brief:   "Write three haiku about rivers and put them in rivers.txt.",
			Summary: "rivers.txt is written with three haiku, each in 5-7-5."},
	)
	return backend
}

// deepBoard is a job whose plan has a plan: a branch with parts of its own, so
// the fold door and the drill door are both reachable on one page.
func deepBoard() *boardBackend {
	backend := board()
	backend.nodes = append(backend.nodes,
		store.Node{ID: "job-5", Title: "importer", Status: store.Running,
			CreatedSeq: 50, UpdatedSeq: 50,
			Brief: "Bring the importer up to the reference."},
		store.Node{ID: "job-5/read", Parent: "job-5", Title: "ReadSpec", Status: store.Done,
			CreatedSeq: 51, UpdatedSeq: 52,
			Summary: "the spec pins the column order."},
		store.Node{ID: "job-5/port", Parent: "job-5", Title: "PortRows", Status: store.Running,
			CreatedSeq: 52, UpdatedSeq: 53,
			Brief: "Port the row reader."},
		store.Node{ID: "job-5/port/a", Parent: "job-5/port", Title: "HeaderPass", Status: store.Done,
			CreatedSeq: 53, UpdatedSeq: 54, Summary: "headers line up."},
		store.Node{ID: "job-5/port/b", Parent: "job-5/port", Title: "BodyPass", Status: store.Running,
			CreatedSeq: 54, UpdatedSeq: 55},
	)
	return backend
}

// blockIDs is the page's block list as ids, which is what an order is.
func blockIDs(app *App) []string {
	out := make([]string, 0, app.view.transcript.Len())
	for i := 0; i < app.view.transcript.Len(); i++ {
		out = append(out, app.view.transcript.Block(i).ID())
	}
	return out
}

// THE ORDER, VARIANT A. Prompt card, execution, result — and nothing after the
// result, because a record ends where the work ended.
func TestAnAtomicRecordReadsChronologically(t *testing.T) {
	backend := atomicBoard()
	commander := &tracingCommander{traces: map[string]string{"job-4": traceFixture}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	openRecord(t, app, "job-4", "haiku")
	if cmd := app.readTraceCmd("job-4"); cmd != nil {
		if msg, ok := cmd().(traceReadMsg); ok {
			app.applyTraceRead(msg)
		}
	}

	ids := blockIDs(app)
	if len(ids) < 3 {
		t.Fatalf("the record drew %d blocks: %v", len(ids), ids)
	}
	if ids[0] != chargeBlockID {
		t.Fatalf("the page does not open on its prompt card: %v", ids)
	}
	if ids[len(ids)-1] != resultBlockID {
		t.Fatalf("the page does not end on its result card: %v", ids)
	}
	// The middle is the trace, and it sits between the two — after the seam that
	// names it and before the answer it produced.
	seam, trace := -1, -1
	for i, id := range ids {
		if id == traceSeamID {
			seam = i
		}
		if isTraceBlockID(id) && trace < 0 {
			trace = i
		}
	}
	if seam < 0 || trace != seam+1 || trace >= len(ids)-1 {
		t.Fatalf("the execution does not sit in the middle: seam %d, first row %d of %v", seam, trace, ids)
	}
	// And the answer is the answer, drawn once.
	frame := ansi.Strip(app.Frame(120, 40))
	if n := strings.Count(frame, "rivers.txt is written with three haiku"); n != 1 {
		t.Fatalf("the result is drawn %d times, want one:\n%s", n, frame)
	}
}

// THE ORDER, VARIANT B. Same top card, the living tree in the middle, the result
// at the bottom once the whole job has settled.
func TestAGraphRecordReadsChronologically(t *testing.T) {
	backend := recordBoard()
	for i := range backend.nodes {
		switch backend.nodes[i].ID {
		case "job-3":
			backend.nodes[i].Status = store.Done
			backend.nodes[i].UpdatedSeq = 60
			backend.nodes[i].Summary = "all four fronts are at parity now."
		case "job-3/h2", "job-3/keycutter":
			backend.nodes[i].Status = store.Done
			backend.nodes[i].UpdatedSeq = 59
		}
	}
	backend.edges = nil
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	openRecord(t, app, "job-3", "wisp-parity-2")

	ids := blockIDs(app)
	if ids[0] != chargeBlockID {
		t.Fatalf("the page does not open on its prompt card: %v", ids)
	}
	if ids[len(ids)-1] != resultBlockID {
		t.Fatalf("the page does not end on its result card: %v", ids)
	}
	tree := -1
	for i, id := range ids {
		if id == recordTreeID {
			tree = i
		}
	}
	if tree <= 0 || tree >= len(ids)-1 {
		t.Fatalf("the tree is not the middle of the page: %v", ids)
	}
}

// A RUNNING JOB HAS NO RESULT AND MUST NOT WEAR ONE (§4: the ground and the `▎`
// edge mean a finished answer and nothing else may wear them).
func TestARunningRecordDrawsNoResultCard(t *testing.T) {
	backend := recordBoard()
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	openRecord(t, app, "job-3", "wisp-parity-2")

	for _, id := range blockIDs(app) {
		if id == resultBlockID {
			t.Fatal("a job still running drew a result card")
		}
	}
}

// A SETTLED RECORD OPENS SCROLLED TO ITS RESULT. That is the whole of what the
// chronological order traded the answer-first reading for.
func TestASettledRecordOpensOnItsResult(t *testing.T) {
	backend := atomicBoard()
	commander := &tracingCommander{traces: map[string]string{"job-4": traceFixture}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	openRecord(t, app, "job-4", "haiku")
	if cmd := app.readTraceCmd("job-4"); cmd != nil {
		if msg, ok := cmd().(traceReadMsg); ok {
			app.applyTraceRead(msg)
		}
	}

	transcript := app.view.transcript
	transcript.SetSize(90, 8)
	transcript.Frame(fixedNow())
	app.view.anchored = false
	app.anchorRecord(app.view)
	frame := transcript.Frame(fixedNow())

	if frame.YOffset == 0 {
		t.Fatalf("a settled record opened at the top of its own trace:\n%s", frame.String())
	}
	index, ok := transcript.IndexOf(resultBlockID)
	if !ok {
		t.Fatal("the settled record built no result card to open on")
	}
	// THE CARD'S TOP IS THE VIEWPORT'S TOP (user review, 2026-08-11): "let's go
	// to the opened final result with the final result STARTING point opening
	// there instead of end", so the whole face of the answer is on screen and
	// reads downward. The one bound on it is the document: a page cannot scroll
	// past its own end, and a card with less than a screenful under it is fully
	// visible either way.
	card := rowOfBlock(transcript, index)
	want := card
	if max := frame.Total - transcript.Height(); want > max {
		want = max
	}
	if frame.YOffset != want {
		t.Fatalf("the record opened at row %d, want the result card's own row %d", frame.YOffset, want)
	}
	if card < frame.YOffset || card >= frame.YOffset+transcript.Height() {
		t.Fatalf("the result card's first row (%d) is off the first screen (%d..%d)",
			card, frame.YOffset, frame.YOffset+transcript.Height())
	}
	// The work above it is one scroll away rather than gone: the page is still
	// the whole record, read from wherever the reader was going.
	if frame.YOffset == 0 || frame.AtTop {
		t.Fatal("the record opened at the top of its own trace")
	}
	if !strings.Contains(ansi.Strip(frame.String()), "rivers.txt is written") {
		t.Fatalf("the answer is not on the first screen:\n%s", frame.String())
	}
}

// A RUNNING RECORD OPENS AT ITS LIVE TAIL, which is where the thing worth
// watching is.
func TestARunningRecordOpensAtItsLiveTail(t *testing.T) {
	backend := recordBoard()
	commander := &tracingCommander{traces: map[string]string{"job-3": traceFixture}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	openRecord(t, app, "job-3", "wisp-parity-2")
	if cmd := app.readTraceCmd("job-3"); cmd != nil {
		if msg, ok := cmd().(traceReadMsg); ok {
			app.applyTraceRead(msg)
		}
	}

	transcript := app.view.transcript
	transcript.SetSize(90, 8)
	frame := transcript.Frame(fixedNow())
	if !frame.AtBottom {
		t.Fatalf("a running record did not open at its live tail (row %d of %d)",
			frame.YOffset, frame.Total)
	}
}

// MANUAL SCROLLING IS NEVER FOUGHT. The anchor is spent once, on entry; a job
// that settles under a reader who has scrolled back does not yank them.
func TestTheRecordNeverYanksAReaderWhoHasScrolled(t *testing.T) {
	backend := atomicBoard()
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	openRecord(t, app, "job-4", "haiku")

	transcript := app.view.transcript
	transcript.SetSize(90, 4)
	transcript.Frame(fixedNow())
	transcript.GotoTop()
	if transcript.Following() {
		t.Fatal("the transcript is still following after a scroll to the top")
	}
	offset := transcript.YOffset()

	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-4" {
			backend.nodes[i].UpdatedSeq = 70
			backend.nodes[i].Summary += "\nEach one names a river."
		}
	}
	backend.journal++
	poll(t, app)
	transcript.Frame(fixedNow())

	if got := transcript.YOffset(); got != offset {
		t.Fatalf("the repaint moved the reader from row %d to row %d", offset, got)
	}
}

// A FAILURE IS A RESULT (§4's coral variant), and its reason is what the reader
// opened the record for.
func TestAFailedRecordEndsOnItsReason(t *testing.T) {
	backend := atomicBoard()
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-4" {
			backend.nodes[i].Status = store.Failed
			backend.nodes[i].Summary = ""
			backend.nodes[i].Error = "the provider dropped the stream"
		}
	}
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	openRecord(t, app, "job-4", "haiku")

	ids := blockIDs(app)
	if ids[len(ids)-1] != resultBlockID {
		t.Fatalf("the failed record does not end on its result card: %v", ids)
	}
	card, ok := app.view.transcript.Block(app.view.transcript.Len() - 1).(*messageBlock)
	if !ok {
		t.Fatal("the result is not a card")
	}
	// The card wears the delivery dress — §4's ground plus edge — and the BROKEN
	// hue, because a failure drawn green is the one mistake this row can make.
	if card.card != dressDelivery {
		t.Fatalf("the failure is not dressed as a result: %v", card.card)
	}
	if card.head.GlyphHue != blocks.HueBroken {
		t.Fatalf("the failure is not coral: %+v", card.head)
	}
	if !strings.Contains(ansi.Strip(app.Frame(120, 30)), "the provider dropped the stream") {
		t.Fatal("the record does not say why it failed")
	}
}

// NO SLUG REACHES A FRAME (5.14's never-shown tier). Provider ids are what the
// usage table records and humane words are what a receipt says.
func TestNoModelSlugReachesTheRecord(t *testing.T) {
	backend := &modelledBoard{
		boardBackend: recordBoard(),
		models: map[string][]string{
			"job-3":       {"anthropic/claude-k3-20260114", "openai/gpt-5-mini"},
			"job-3/xhr":   {"anthropic/claude-k3-20260114"},
			"job-3/h2":    {"openai/gpt-5-mini"},
			"job-3/probe": {"anthropic/claude-haiku-4-5"},
		},
	}
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	openRecord(t, app, "job-3", "wisp-parity-2")

	frame := ansi.Strip(app.Frame(160, 40))
	for _, slug := range []string{"anthropic/", "openai/", "-20260114"} {
		if strings.Contains(frame, slug) {
			t.Fatalf("a provider slug reached the frame (%q):\n%s", slug, frame)
		}
	}
	// And the word itself is there, so the cell was shortened rather than
	// dropped — an absent model and a slug are both wrong, differently.
	if word := modelWord("anthropic/claude-k3-20260114"); !strings.Contains(frame, word) {
		t.Fatalf("the model word %q is not on screen:\n%s", word, frame)
	}
}

// -- the doors -----------------------------------------------------------------

// CLICKING A BRANCH FOLDS ITS SUBTREE (§10: the whole line is the door, the
// chevron is the witness).
func TestClickingABranchFoldsItsSubtree(t *testing.T) {
	app := newTestApp(deepBoard(), &fakeCommander{}, nil)
	poll(t, app)
	openRecord(t, app, "job-5", "importer")

	tree, ok := recordTree(app)
	if !ok {
		t.Fatal("the deep job drew no tree")
	}
	if len(tree.rows) != 4 {
		t.Fatalf("the open tree has %d rows, want four: %+v", len(tree.rows), tree.rows)
	}
	branch, line := -1, -1
	for i, row := range tree.rows {
		if row.branch {
			branch = i
		}
	}
	if branch < 0 {
		t.Fatal("no row in the tree is a branch")
	}
	// Which document line the branch is on, so the click is aimed the way a
	// pointer aims: at a row on screen.
	rows := tree.Rows(100)
	if len(rows) == 0 {
		t.Fatal("the tree drew no rows")
	}
	for i := range tree.lines {
		if tree.lines[i] == branch {
			line = i
			break
		}
	}
	if line < 0 {
		t.Fatal("the branch is on no line")
	}
	app.toggleTreeFold(tree.rows[branch])

	folded, ok := recordTree(app)
	if !ok {
		t.Fatal("the fold lost the tree")
	}
	if len(folded.rows) != 2 {
		t.Fatalf("the collapsed tree has %d rows, want two: %+v", len(folded.rows), folded.rows)
	}
	var closed treeRow
	for _, row := range folded.rows {
		if row.branch {
			closed = row
		}
	}
	if closed.open || closed.hidden != 2 {
		t.Fatalf("the collapsed branch does not say what it is holding: %+v", closed)
	}
	// The witness says which way the door faces, and the count is in PARTS.
	frame := ansi.Strip(strings.Join(folded.Rows(100), "\n"))
	if !strings.Contains(frame, blocks.CollapsedMark+" 2 parts") {
		t.Fatalf("the collapsed branch wears no witness:\n%s", frame)
	}
	// And it opens again on the same door.
	app.toggleTreeFold(closed)
	reopened, _ := recordTree(app)
	if len(reopened.rows) != 4 {
		t.Fatalf("the branch did not reopen: %d rows", len(reopened.rows))
	}
}

// CLICKING AN ATOMIC LEAF DRILLS INTO ITS OWN RECORD, and esc walks back one
// level with the page it left restored — transcript, scroll offset and all.
func TestClickingALeafDrillsAndEscWalksBack(t *testing.T) {
	app := newTestApp(deepBoard(), &fakeCommander{}, nil)
	poll(t, app)
	openRecord(t, app, "job-5", "importer")

	parent := app.view
	parent.transcript.SetSize(90, 6)
	parent.transcript.Frame(fixedNow())
	parent.transcript.ScrollTo(1)
	offset := parent.transcript.YOffset()

	tree, ok := recordTree(app)
	if !ok {
		t.Fatal("the deep job drew no tree")
	}
	var leaf treeRow
	for _, row := range tree.rows {
		if !row.branch && row.node == "job-5/read" {
			leaf = row
		}
	}
	if leaf.node == "" {
		t.Fatal("the tree has no atomic leaf to drill into")
	}
	app.drillInto(leaf.node, leaf.name)

	if app.view == parent || app.view.node != "job-5/read" {
		t.Fatalf("the leaf did not open its own record: %+v", app.view)
	}
	if app.view.parent != parent {
		t.Fatal("the nested record does not remember where it came from")
	}
	// It is variant A of the same page: the leaf's own charge, its own words.
	if got := app.view.transcript.Block(0).ID(); got != chargeBlockID {
		t.Fatalf("the nested record does not open on its charge: %q", got)
	}
	if !strings.Contains(ansi.Strip(app.Frame(120, 30)), "the spec pins the column order") {
		t.Fatal("the nested record does not say what the part did")
	}

	if !app.drillBack() {
		t.Fatal("esc did not walk back out of the nested record")
	}
	if app.view != parent {
		t.Fatalf("the walk back landed somewhere else: %+v", app.view)
	}
	parent.transcript.Frame(fixedNow())
	if got := parent.transcript.YOffset(); got != offset {
		t.Fatalf("the walk back moved the reader from row %d to row %d", offset, got)
	}
	// And there is no second level to leave.
	if app.drillBack() {
		t.Fatal("the top-level record thought it had a parent")
	}
}

// The pointer reaches both doors through one hit test, resolved against the
// layout the last frame produced.
func TestTheRecordPointerFindsTheRowItWasAimedAt(t *testing.T) {
	app := newTestApp(deepBoard(), &fakeCommander{}, nil)
	poll(t, app)
	openRecord(t, app, "job-5", "importer")

	transcript := app.view.transcript
	transcript.SetSize(100, 40)
	transcript.Frame(fixedNow())
	transcript.GotoTop()
	transcript.Frame(fixedNow())

	tree, ok := recordTree(app)
	if !ok {
		t.Fatal("the deep job drew no tree")
	}
	index, found := transcript.IndexOf(recordTreeID)
	if !found {
		t.Fatal("the tree is not in the page")
	}
	base := rowOfBlock(transcript, index) - transcript.YOffset()
	for line, row := range tree.lines {
		if row < 0 {
			continue
		}
		_, got, ok := app.recordTreeAt(base + line)
		if !ok {
			t.Fatalf("line %d of the tree resolved to no row", line)
		}
		if got.node != tree.rows[row].node {
			t.Fatalf("line %d resolved to %q, want %q", line, got.node, tree.rows[row].node)
		}
	}
	// A click above the tree is not a click on it.
	if _, _, ok := app.recordTreeAt(0); ok {
		t.Fatal("the prompt card resolved to a tree row")
	}
}

// ONE POINTER, TWO DOORS, and the row itself says which. This drives the seam
// the pane calls into ([App.recordPointer]) rather than the two acts behind it,
// so a click on a branch and a click on a leaf are told apart by the same code
// the terminal reaches.
func TestOnePointerReachesBothDoorsOfTheTree(t *testing.T) {
	app := newTestApp(deepBoard(), &fakeCommander{}, nil)
	poll(t, app)
	openRecord(t, app, "job-5", "importer")

	transcript := app.view.transcript
	transcript.SetSize(100, 40)
	transcript.Frame(fixedNow())
	transcript.GotoTop()
	transcript.Frame(fixedNow())

	lineOf := func(t *testing.T, node string) int {
		t.Helper()
		tree, ok := recordTree(app)
		if !ok {
			t.Fatal("the page has no tree")
		}
		index, found := transcript.IndexOf(recordTreeID)
		if !found {
			t.Fatal("the tree is not in the page")
		}
		base := rowOfBlock(transcript, index) - transcript.YOffset()
		for line, row := range tree.lines {
			if row >= 0 && tree.rows[row].node == node {
				return base + line
			}
		}
		t.Fatalf("the tree draws no row for %q", node)
		return -1
	}

	// A BRANCH: the click folds, and the page keeps its subject.
	before, _ := recordTree(app)
	if cmd := app.recordPointer(lineOf(t, "job-5/port")); cmd != nil {
		t.Fatal("folding a branch produced a command; nothing is read or spent")
	}
	after, _ := recordTree(app)
	if len(after.rows) >= len(before.rows) {
		t.Fatalf("the branch did not fold: %d rows before, %d after",
			len(before.rows), len(after.rows))
	}
	if app.view.node != "job-5" {
		t.Fatalf("a fold moved the page to %q", app.view.node)
	}

	// AN ATOMIC LEAF: the click opens that part's own page.
	transcript.Frame(fixedNow())
	if cmd := app.recordPointer(lineOf(t, "job-5/read")); cmd == nil {
		t.Fatal("drilling into a leaf asked for nothing; a nested page needs its reads")
	}
	if app.view.node != "job-5/read" {
		t.Fatalf("the leaf did not open its own page: %q", app.view.node)
	}
	if !app.drillBack() {
		t.Fatal("there was no level to walk back out of")
	}
}
