package chat

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The work record's laws, driven through the real app and read off real frames.
//
// THE DEFECT THESE PIN, in the reporter's own words: "when I click a task on the
// right side and go inside it, I see nothing. No tree, no actual chat, no
// what-it-did, no plan, nothing. Maybe it shows the final answer — it doesn't
// show that either. It just remains empty."
//
// It was true, and the cause was one table. A task room read `messages` anchored
// to its subtree, and a resident-run job writes almost none: measured on a live
// six-part run, the parts held 7,270 characters of journaled result in
// `nodes.summary` and NOT ONE of them was a message. Every assertion below is
// therefore about a room whose message trail is empty or nearly so, because that
// is the shape every real task has.

// recordBoard is a job with a plan, parts that have finished and said what they
// did, a part that failed, and a part still waiting — and NO node-anchored
// messages at all. It is the live shape (room-homes/room-tree/graph.db) reduced
// to a fixture.
func recordBoard() *boardBackend {
	backend := board()
	backend.nodes = append(backend.nodes,
		store.Node{ID: "job-3", Title: "wisp-parity-2", Status: store.Running,
			CreatedSeq: 20, UpdatedSeq: 20,
			Brief: "Bring the wisp browser to parity with the reference on four fronts."},
		store.Node{ID: "job-3/xhr", Parent: "job-3", Title: "XhrSyn", Status: store.Done,
			CreatedSeq: 21, UpdatedSeq: 30,
			Brief:   "Write two sentences on why synchronous XHR is deprecated.",
			Summary: "Synchronous XHR blocks the event loop.\nBrowsers warn on it and vendors are removing it."},
		store.Node{ID: "job-3/h2", Parent: "job-3", Title: "H2", Status: store.Running,
			CreatedSeq: 22, UpdatedSeq: 31},
		store.Node{ID: "job-3/probe", Parent: "job-3", Title: "H2Probe", Status: store.Failed,
			CreatedSeq: 23, UpdatedSeq: 32,
			Error: "did not finish — the provider dropped the stream"},
		store.Node{ID: "job-3/keycutter", Parent: "job-3", Title: "KeyCutter", Status: store.Pending,
			CreatedSeq: 24, UpdatedSeq: 24},
	)
	backend.edges = append(backend.edges,
		store.Edge{From: "job-3/h2", To: "job-3/keycutter", Kind: store.Blocks})
	return backend
}

// enterRoom opens the room for one task and folds the trail read back in, which
// is what the runtime does with the command the enter key returns.
func enterRoom(t *testing.T, app *App, digit string) {
	t.Helper()
	// ctrl+o is a TOGGLE, and a settled room never took the keyboard off the
	// map in the first place (its composer is disabled), so pressing it blindly
	// would hand the digit to the composer as text.
	if !app.railFocus {
		press(app, "ctrl+o")
	}
	press(app, digit)
	msg := press(app, "enter")
	if app.view == nil || app.view.kind != viewNode {
		t.Fatalf("enter did not open a task room: %+v", app.view)
	}
	if trail, ok := msg.(nodeMessagesMsg); ok {
		app.applyNodeMessages(trail)
	}
}

// THE DEFECT ITSELF: a job whose parts had all done real work, whose results
// were all in the journal, and whose room showed none of it because none of them
// was a message.
// The parts are the TREE now (user-amended 2026-08-11): a graph record's middle
// is one row per part, and a part's own words are one click down rather than
// four paragraphs of prose stacked into a page whose subject is the whole job.
func TestATaskRoomDrawsTheWorkItsPartsJournaled(t *testing.T) {
	backend := recordBoard()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5") // wisp-parity-2, newest first

	frame := ansi.Strip(app.Frame(120, 30))
	// Every part is a row, named, with its own state glyph. The rail draws the
	// same names one column over; the assertion here is that the RECORD does.
	for _, want := range []string{"XhrSyn", "H2Probe", "KeyCutter"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the room did not draw the part %q:\n%s", want, frame)
		}
	}
	// A part that has not run yet is still a row, and the reason it is sitting
	// still is on it. That is the record at this moment, and a room that drew
	// nothing for it would be hiding the plan.
	if !strings.Contains(frame, waitsWord+"H2") {
		t.Fatalf("the room did not say why KeyCutter is waiting:\n%s", frame)
	}
	// And it did all of that without a single node-anchored message.
	if len(app.view.messages) != 0 {
		t.Fatalf("the fixture leaked messages into the room: %+v", app.view.messages)
	}
}

// THE TREE IS THE MIDDLE OF A GRAPH RECORD, on §20's ladder, with the connector
// guides §3 spells. The rows are read off the document rather than off the
// frame, because in a frame the rail is drawn beside the transcript.
func TestAGraphRecordDrawsItsPartsAsATreeOnTheGrid(t *testing.T) {
	backend := recordBoard()
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	enterRoom(t, app, "5")

	tree, ok := recordTree(app)
	if !ok {
		t.Fatal("a job with four parts drew no tree")
	}
	rows := tree.Rows(100)
	if len(rows) < 4 {
		t.Fatalf("the tree drew %d rows for four parts:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	// Guides: every part of this job is a child of the root, so the last one
	// wears the elbow and the rest the tee. Both are two cells, so the state
	// glyph lands at column 2 and the name at column 4 (§20's ladder).
	branch := tokens.GlyphTreeBranch + tokens.GlyphTreeDash
	elbow := tokens.GlyphTreeLast + tokens.GlyphTreeDash
	if !strings.HasPrefix(ansi.Strip(rows[0]), branch) {
		t.Fatalf("the first part wears no connector: %q", ansi.Strip(rows[0]))
	}
	if !strings.HasPrefix(ansi.Strip(rows[3]), elbow) {
		t.Fatalf("the last part wears no elbow: %q", ansi.Strip(rows[3]))
	}
	// And the whole tree clears §20 at every measure the product is read at.
	for _, width := range gridWidths {
		assertGrid(t, gridLabel("record tree", width, 0), strings.Join(tree.Rows(width), "\n"))
	}
}

// recordTree is the open record's tree block, or none.
func recordTree(app *App) (*recordTreeBlock, bool) {
	if app.view == nil || app.view.transcript == nil {
		return nil, false
	}
	for i := 0; i < app.view.transcript.Len(); i++ {
		if tree, ok := app.view.transcript.Block(i).(*recordTreeBlock); ok {
			return tree, true
		}
	}
	return nil, false
}

// 5.9's "no plan" half of the report. A task's brief is what it was asked to do;
// it is journaled on the node, it is the only plan an ATOMIC job has, and the
// room it belongs to never showed it.
func TestATaskRoomOpensOnTheChargeItWasGiven(t *testing.T) {
	backend := recordBoard()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5")

	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "Bring the wisp browser to parity") {
		t.Fatalf("the room did not say what the task was asked to do:\n%s", frame)
	}
	// The charge leads. Everything else in the room happened after it.
	if got := app.view.transcript.Block(0).ID(); got != chargeBlockID {
		t.Fatalf("the room does not open on the charge: first block is %q", got)
	}
}

// 13.10's card is born of this task, so it belongs in this task's room too — and
// it must not be doubled by the root's own summary, which is the same fact
// without the money or the artifact rows.
func TestTheDeliveryIsInTheTaskRoomExactlyOnce(t *testing.T) {
	backend := recordBoard()
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-3" {
			backend.nodes[i].Status = store.Done
			backend.nodes[i].Summary = "rivers.txt is written with three haiku."
			backend.nodes[i].UpdatedSeq = 40
		}
	}
	backend.node["job-3"] = []store.Message{{
		Seq: 41, SessionID: testSession, Role: store.RoleSystem, NodeID: "job-3",
		Body: "rivers.txt is written with three haiku.",
	}}
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5")

	frame := ansi.Strip(app.Frame(120, 30))
	if n := strings.Count(frame, "rivers.txt is written with three haiku."); n != 1 {
		t.Fatalf("the delivery is drawn %d times, want exactly 1:\n%s", n, frame)
	}
	// It is the delivery CARD that survived, not the bare work row: the card is
	// the one that knows the job's name off the board (13.10).
	if !strings.Contains(frame, "wisp-parity-2") {
		t.Fatalf("the delivery lost the job's name:\n%s", frame)
	}
}

// A job whose ending nobody announced still has to show it. `announceNode` posts
// a delivery only for a node parented on the spine, so a room that waited for a
// message would show a finished job as if it had said nothing.
func TestAJobsOwnEndingIsDrawnWhenNoMessageCarriesIt(t *testing.T) {
	backend := recordBoard()
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-3" {
			backend.nodes[i].Status = store.Done
			backend.nodes[i].Summary = "all four fronts are at parity now."
			backend.nodes[i].UpdatedSeq = 40
		}
	}
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5")

	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "all four fronts are at parity now.") {
		t.Fatalf("the room hid the job's own ending:\n%s", frame)
	}
}

// 12.14 finding 4 stands, but on the right question. The teaching line belongs
// to a room the journal has NOTHING to say about — not to one whose trail is
// empty, which is nearly every real task.
func TestTheTeachingLineSurvivesOnlyATrulyEmptyRoom(t *testing.T) {
	backend := recordBoard()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)

	// perf-audit has no parts, no brief, no summary and no trail: nothing is
	// journaled about it beyond its name, and the room says exactly that.
	enterRoom(t, app, "7")
	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "nothing journaled here yet") {
		t.Fatalf("a genuinely empty room drew no teaching line:\n%s", frame)
	}

	// The job with a plan is not empty and never was.
	pressThrough(app, "esc")
	enterRoom(t, app, "5")
	frame = ansi.Strip(app.Frame(120, 30))
	if strings.Contains(frame, "nothing journaled here yet") {
		t.Fatalf("a room holding a plan and four parts called itself empty:\n%s", frame)
	}
}

// The room is a lens on the BOARD, not only on the trail. A part that finishes
// journals a node event and no message at all, so a room that repainted only on
// messages would freeze its parts at the moment it was opened.
func TestTheRoomFollowsThePartsWithoutANewMessage(t *testing.T) {
	backend := recordBoard()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5")

	before, ok := recordTree(app)
	if !ok {
		t.Fatal("the room drew no tree to follow")
	}
	if got := treeLifeOf(t, before, "job-3/keycutter"); got != rail.LifeQueued {
		t.Fatalf("KeyCutter starts at %v, not queued", got)
	}

	// KeyCutter runs and finishes. Nothing is posted; only the graph moves.
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-3/keycutter" {
			backend.nodes[i].Status = store.Done
			backend.nodes[i].UpdatedSeq = 50
			backend.nodes[i].Summary = "Session keys need rotation every 90 days."
		}
	}
	backend.edges = nil
	backend.journal++
	poll(t, app)

	after, ok := recordTree(app)
	if !ok {
		t.Fatal("the repaint lost the tree")
	}
	if got := treeLifeOf(t, after, "job-3/keycutter"); got != rail.LifeSettled {
		t.Fatalf("the room did not follow the part that finished: KeyCutter is %v", got)
	}
	if strings.Contains(ansi.Strip(app.Frame(120, 30)), waitsWord+"H2") {
		t.Fatal("the row that stopped waiting still says what it was waiting for")
	}
}

// treeLifeOf is one part's lifecycle as the tree drew it.
func treeLifeOf(t *testing.T, tree *recordTreeBlock, node string) rail.Lifecycle {
	t.Helper()
	for _, row := range tree.rows {
		if row.node == node {
			return row.life
		}
	}
	t.Fatalf("the tree has no row for %q", node)
	return rail.LifeQueued
}

// The rebuild may not move the reader. A room repaints whenever any part of its
// job moves, and a reader who had scrolled back to read an early part must stay
// where they were reading.
func TestRepaintingTheRoomKeepsTheReadersPlace(t *testing.T) {
	backend := recordBoard()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5")

	app.view.transcript.SetSize(80, 6)
	app.view.transcript.Frame(fixedNow())
	app.view.transcript.GotoTop()
	if app.view.transcript.Following() {
		t.Fatal("the transcript is still following after a scroll to the top")
	}
	offset := app.view.transcript.YOffset()

	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-3/keycutter" {
			backend.nodes[i].Status = store.Done
			backend.nodes[i].UpdatedSeq = 50
			backend.nodes[i].Summary = "Session keys need rotation every 90 days."
		}
	}
	backend.journal++
	poll(t, app)

	app.view.transcript.Frame(fixedNow())
	if got := app.view.transcript.YOffset(); got != offset {
		t.Fatalf("the repaint moved the reader from row %d to row %d", offset, got)
	}
}

// An unrelated journal move costs a comparison and not a rebuild. The stamp is
// the whole of that, and it is asserted on the record rather than on a counter
// so it stays true if the caller changes.
func TestAnUnrelatedJournalMoveDoesNotRestampTheRoom(t *testing.T) {
	backend := recordBoard()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5")

	stamp := app.view.stamp
	if stamp == "" {
		t.Fatal("the room recorded no stamp")
	}
	backend.journal++
	poll(t, app)
	if app.view.stamp != stamp {
		t.Fatalf("a move that touched nothing in this task restamped it:\n%q\n%q",
			stamp, app.view.stamp)
	}
}

// 5.9's progressive disclosure, and the one shape splitHeadline cannot see. A
// worker that wrote a single long paragraph has no line break to fold at, and
// its row took six rows of a live frame and pushed the job's charge and three
// sibling parts off the screen.
func TestALongResultFoldsEvenWithoutALineBreak(t *testing.T) {
	long := "Synchronous XHR is deprecated because it blocks the main thread. " +
		"While a synchronous request is in flight the browser cannot render, " +
		"run scripts, or respond to user input, which freezes the page. " +
		"The standard now requires asynchronous requests instead."
	if strings.Contains(long, "\n") {
		t.Fatal("the fixture is supposed to be one unbroken paragraph")
	}
	gist, rest := splitGist(long, gistCap)
	if gist == long || rest == "" {
		t.Fatalf("an unbroken paragraph did not fold:\ngist=%q\nrest=%q", gist, rest)
	}
	if len(gist) > gistCap {
		t.Fatalf("the gist is %d bytes, over the %d cap: %q", len(gist), gistCap, gist)
	}
	// It splits the record and never shortens it: every word survives the cut.
	if joined := strings.Join(strings.Fields(gist+" "+rest), " "); joined != long {
		t.Fatalf("the fold lost or changed words:\nwant %q\ngot  %q", long, joined)
	}
	// And the boundary is a sentence, not a guillotine through a word.
	if !strings.HasSuffix(gist, ".") {
		t.Fatalf("the gist did not end at a sentence: %q", gist)
	}
}

// 13.8 finding 6: "Work speaks under its raw node id." A narrator's line in a
// real room was headed `job-wisp` while the card beside it said `wisp-parity` —
// 5.14's never-shown tier on screen, and two names for one thing on one frame.
func TestANodeAnchoredRowSpeaksUnderTheJobsNameNotItsID(t *testing.T) {
	backend := recordBoard()
	backend.node["job-3"] = []store.Message{{
		Seq: 41, SessionID: testSession, Role: store.RoleAgent, NodeID: "job-3",
		Body: "picked the transport work back up",
	}}
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5")

	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "picked the transport work back up") {
		t.Fatalf("the room dropped the narrator's line:\n%s", frame)
	}
	if strings.Contains(frame, "job-3") {
		t.Fatalf("a node id reached a cell (5.14):\n%s", frame)
	}
}

// The other half of the same finding: a steer is the READER's sentence, and it
// was drawn as a work card titled with the node it was aimed at, under a `$—`,
// "as if the user's sentence had a price".
func TestASteerReadsBackAsSpeechAndNotAsAPricedCard(t *testing.T) {
	backend := recordBoard()
	backend.node["job-3"] = []store.Message{{
		Seq: 41, SessionID: testSession, Role: store.RoleUser, NodeID: "job-3",
		Body: "skip the H2 part",
	}}
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5")

	block, ok := app.view.transcript.Block(app.view.transcript.Len() - 1).(*messageBlock)
	if !ok {
		t.Fatal("the steer is not a message block")
	}
	// Attributed by its gutter rather than by a name, since §3b took the speaker
	// rows out: what makes it the reader's is the prompt glyph they typed at and
	// the absence of a card's anatomy around it.
	if !block.user || block.lead.glyph != tokens.GlyphPromptChat || block.lead.name != "" {
		t.Fatalf("the reader's own steer is not attributed to them: %+v", block.lead)
	}
	if strings.Contains(ansi.Strip(strings.Join(block.Rows(80), "\n")), "$") {
		t.Fatalf("the reader's sentence was drawn with a price: %q", block.Rows(80))
	}
	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "skip the H2 part") {
		t.Fatalf("the room dropped the steer:\n%s", frame)
	}
}

// THE SPECIALIST CHIP, and the silence that gives it meaning.
//
// Work executes on atomic harnesses and the store settles which one on the node
// itself (store.Node.Subharness, with the splice's choice behind it), so the
// record READS the attribution rather than inventing one. A default job leaves
// the field empty and wears nothing: silence is what makes the chip mean
// something on the job that has one.
func TestOnlyASpecialistHarnessWearsAChip(t *testing.T) {
	ordinary := recordBoard()
	app := newTestApp(ordinary, &fakeCommander{}, nil)
	poll(t, app)
	enterRoom(t, app, "5")
	if got := chargeMeta(t, app); strings.Contains(got, "swe") {
		t.Fatalf("an ordinary job named a harness: %q", got)
	}

	special := recordBoard()
	for i := range special.nodes {
		if special.nodes[i].ID == "job-3" {
			special.nodes[i].Subharness = "swe"
		}
	}
	app = newTestApp(special, &fakeCommander{}, nil)
	poll(t, app)
	enterRoom(t, app, "5")
	rule, _ := app.view.transcript.Block(0).(*ruleBlock)
	if rule == nil || len(rule.rule.Meta) == 0 || rule.rule.Meta[0] != "swe" {
		t.Fatalf("the specialist's name does not lead the record's receipt: %+v", rule)
	}
	// Dim, and never a hue: a harness is provenance, not a claim about how the
	// work went (5.16 spends its hues on state and money). On a rule that is
	// structural rather than a field — [blocks.Ruled] paints every meta cell at
	// StateChrome/HueNone and only the title carries a tier — so what the test
	// can assert is that the word rides the receipt and not the title.
	if rule.rule.Title == "swe" {
		t.Fatalf("the harness word reached the rule's title: %+v", rule.rule)
	}
	if !strings.Contains(ansi.Strip(app.Frame(120, 30)), "swe") {
		t.Fatal("the chip is not on screen")
	}
}

// -- the models that did the work ----------------------------------------------

// modelledBoard is a board that can also answer [Models] — the optional read
// *store.Store and *command.Commander both satisfy, faked here so the record's
// header can be driven without a journal.
type modelledBoard struct {
	*boardBackend
	models map[string][]string
	err    error
	asked  int
}

func (b *modelledBoard) NodeModels(node string) ([]string, error) {
	b.asked++
	if b.err != nil {
		return nil, b.err
	}
	return b.models[node], nil
}

// §5's record-header law: "the full receipt — elapsed · $cost · Nk tok plus THE
// MODELS THAT DID THE WORK (deduped, dim)". Until the seam existed the header
// could only name the model the RAIL resolved, which on an escalating job names
// the rung the work started on and not the one that finished it.
func TestTheRecordHeaderNamesEveryModelThatDidTheWork(t *testing.T) {
	backend := &modelledBoard{
		boardBackend: recordBoard(),
		// The store answers deduped already; the duplicate here is the host that
		// records one row per RUN, which this seam must survive.
		models: map[string][]string{
			"job-3": {"anthropic/claude-k3", "openai/gpt-mini", "anthropic/claude-k3"},
		},
	}
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5")

	cells := chargeMeta(t, app)
	// HUMANE WORDS AND NEVER SLUGS (user-directed 2026-08-11). A provider id is
	// 5.14's never-shown tier, so what reaches the cell is what [modelWord]
	// makes of it — and the receipt is asserted to hold the WORD and not the id.
	first, second := modelWord("anthropic/claude-k3"), modelWord("openai/gpt-mini")
	for _, want := range []string{first, second} {
		if !strings.Contains(cells, want) {
			t.Fatalf("the header does not name %q: %q", want, cells)
		}
	}
	if strings.Contains(cells, "anthropic/") || strings.Contains(cells, "openai/") {
		t.Fatalf("a provider slug reached the receipt: %q", cells)
	}
	// DEDUPED, and in the order the read gave them — the store's own "most
	// expensive first" must survive the pass.
	if n := strings.Count(cells, first); n != 1 {
		t.Fatalf("the model word is drawn %d times: %q", n, cells)
	}
	if strings.Index(cells, first) > strings.Index(cells, second) {
		t.Fatalf("the read's order did not survive: %q", cells)
	}
	// Dim, like every other cell of the receipt: meta is chrome by grammar.
	if !strings.Contains(ansi.Strip(app.Frame(140, 30)), second) {
		t.Fatal("the second model is not on screen")
	}
}

// ABSENT IS THE RAIL'S ONE WORD, never nothing and never both. A backend with
// no usage read draws exactly what the record drew before this landed, and a
// read that FAILS is drawn as absence rather than as an error row — the models
// are the least of a record's facts and must not cost it the room.
func TestWithoutTheModelReadTheRecordIsUnchanged(t *testing.T) {
	cells := func(t *testing.T, backend Backend) string {
		t.Helper()
		app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
		poll(t, app)
		enterRoom(t, app, "5")
		return chargeMeta(t, app)
	}
	plain := cells(t, recordBoard())
	broken := &modelledBoard{boardBackend: recordBoard(), err: errNoModels}
	if got := cells(t, broken); got != plain {
		t.Fatalf("a failed model read moved the header:\n got %q\nwant %q", got, plain)
	}
	if broken.asked == 0 {
		t.Fatal("the record never asked for the models at all")
	}
	empty := &modelledBoard{boardBackend: recordBoard()}
	if got := cells(t, empty); got != plain {
		t.Fatalf("a job nothing has billed yet moved the header:\n got %q\nwant %q", got, plain)
	}
}

// The rail's word is the FALLBACK and not a second opinion: where the usage
// read answers, the header names what RAN, and the binding on the job's own row
// — which on an escalating job is the rung the work started on — stands down
// rather than being drawn beside it.
func TestTheSubtreesModelsOutrankTheRowsBinding(t *testing.T) {
	item := workRow{row: rail.Row{Meta: rail.Telemetry{Model: "claude-k3"}}}
	alone := strings.Join(chargeCells(item, spend{}, nil), " ")
	if !strings.Contains(alone, "claude-k3") {
		t.Fatalf("with no usage read the row's own word did not stand: %q", alone)
	}
	both := strings.Join(chargeCells(item, spend{}, []string{"claude-k5", "gpt-mini"}), " ")
	for _, want := range []string{"claude-k5", "gpt-mini"} {
		if !strings.Contains(both, want) {
			t.Fatalf("the header does not name %q: %q", want, both)
		}
	}
	if strings.Contains(both, "claude-k3") {
		t.Fatalf("the header names the same fact twice: %q", both)
	}
}

var errNoModels = errors.New("usage table unavailable")

// -- what the room leads with, and what its receipt says -------------------------

// THE TWO DEFECTS THIS SECTION PINS came off one screenshot of one live run
// (2026-08-11, the SIGNAL launcher craft):
//
//  1. the room opened on "Deliver the result of build a single-file precision
//     arcade game… Every step of the craft arrives as one of your inputs…
//     Assemble them into the one answer…", which is the ASSEMBLING WORKER's
//     errand (internal/resident/craftadapter.go's craftRootBrief) and not
//     anything the reader asked for, while the card in the conversation led with
//     the task's own ask;
//  2. its header said "· 4m · $—" after four minutes of visible execution, with
//     no burn beside it and no money on any row of the tree.
//
// The store is the REAL one here for the reason [TestEveryPartOfAnEnteredJobCarriesItsOwnReceipt]
// gives: the ledger's index is the store's own, a `usage_recorded` event is the
// exact journal move that leaves `nodes.updated_seq` untouched, and a fake that
// cannot reproduce that cannot show either half of the dead receipt.

// The ask, in the reader's own words, and the errand a worker was handed. The
// errand is quoted from craftRootBrief because the point of the fixture is that
// the two are DIFFERENT SENTENCES about one job.
const (
	roomAsk    = "Rework the SIGNAL game into a tabbed multi-game launcher with 3-4 games."
	roomErrand = "Deliver the result of build a single-file precision arcade game.\n\n" +
		"Every step of the craft arrives as one of your inputs, including any that " +
		"were fanned out or repaired. Assemble them into the one answer the person " +
		"who asked is waiting for."
)

// roomJournal is that job on a real journal: the ask in provenance, the errand
// on the root's brief, three parts, and the first of them running.
func roomJournal(t *testing.T) *store.Store {
	t.Helper()
	graph := openJournal(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "craft", Title: "SIGNAL tabbed multi-game launcher", Brief: roomErrand, Stage: 2},
		{ID: "craft/scaffold", Parent: "craft", Title: "scaffold", Brief: "scaffold the page", Stage: 1},
		{ID: "craft/implement", Parent: "craft", Title: "implement", Brief: "implement the games", Stage: 1},
		{ID: "craft/verify", Parent: "craft", Title: "verify", Brief: "check it runs", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: testSession,
		Intent: roomAsk}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"craft", "craft/scaffold"} {
		claim, taken, err := graph.Claim(id, "tester")
		if err != nil || !taken {
			t.Fatalf("claim %q: taken=%v err=%v", id, taken, err)
		}
		if err := graph.Start(claim); err != nil {
			t.Fatalf("start %q: %v", id, err)
		}
	}
	return graph
}

// enterNodeRoom walks into one task's room through the door the palette, the
// board and an `@job` mention all use (App.jumpTo), and folds back everything
// the entry read — the trail, the plan and the recorders.
func enterNodeRoom(t *testing.T, app *App, node string) {
	t.Helper()
	runCmd(t, app, app.drain(app.jumpTo(rowTaskPrefix+node)), 0)
	if app.view == nil || app.view.kind != viewNode || app.view.node != node {
		t.Fatalf("the room for %q did not open: %+v", node, app.view)
	}
}

// runCmd is the Bubble Tea runtime's own loop, small enough to test with: run
// the command, fold whatever it answers with back into the app, and follow the
// commands that produces. The depth bound is a belt against a ticker.
func runCmd(t *testing.T, app *App, cmd tea.Cmd, depth int) {
	t.Helper()
	if cmd == nil || depth > 8 {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			runCmd(t, app, sub, depth+1)
		}
		return
	}
	_, next := app.update(msg)
	runCmd(t, app, next, depth+1)
}

// chargeRows is the ASK block as it is drawn, one row per line.
//
// It is block ONE and not block zero: the page's top is a titled rule carrying
// the job's name and receipt ([chargeRule]), and the ask stands under it in its
// own block. Both halves are asserted here — the rule by [chargeMeta] — because
// they used to be one card and the split must not lose either.
func chargeRows(t *testing.T, app *App) string {
	t.Helper()
	ask, ok := app.view.transcript.Block(1).(*messageBlock)
	if !ok {
		t.Fatalf("the room does not carry its ask: %T", app.view.transcript.Block(1))
	}
	if ask.ID() != chargeAskID {
		t.Fatalf("block 1 is %q, not the ask", ask.ID())
	}
	return ansi.Strip(strings.Join(ask.Rows(110), "\n"))
}

// chargeMeta is the top rule's receipt, as cells.
func chargeMeta(t *testing.T, app *App) string {
	t.Helper()
	rule, ok := app.view.transcript.Block(0).(*ruleBlock)
	if !ok {
		t.Fatalf("the room does not open on its rule: %T", app.view.transcript.Block(0))
	}
	return strings.Join(rule.rule.Meta, " ")
}

// DEFECT 1. The room leads with the ASK, and the worker's errand is still all
// there — under it, where a record of what was handed down belongs.
func TestTheRoomLeadsWithTheAskAndNotTheWorkersErrand(t *testing.T) {
	app := newJournalApp(t, roomJournal(t), &fakeCommander{model: "z-ai/glm-5.2"}, nil)
	poll(t, app)
	enterNodeRoom(t, app, "craft")

	card := chargeRows(t, app)
	ask := strings.Index(card, roomAsk)
	errand := strings.Index(card, "Every step of the craft arrives")
	if ask < 0 {
		t.Fatalf("the room does not say what was asked for:\n%s", card)
	}
	// NOTHING IS HIDDEN (13.1 item 3): the errand is still on the card.
	if errand < 0 {
		t.Fatalf("the room dropped the brief its worker was handed:\n%s", card)
	}
	if ask > errand {
		t.Fatalf("the worker's errand still leads the room:\n%s", card)
	}
	// And on the frame, which is where the reader meets it.
	if !strings.Contains(ansi.Strip(app.Frame(120, 30)), roomAsk) {
		t.Fatal("the ask is not on screen")
	}
}

// A PART'S PAGE IS NOT ITS JOB'S. Provenance rides the splice, so every part
// carries the job's ask; a drilled-into part that led with it would say the
// job's sentence over work that is one leaf of it. A part's ask is its brief.
func TestAPartsPageLeadsWithItsOwnBrief(t *testing.T) {
	app := newJournalApp(t, roomJournal(t), &fakeCommander{model: "z-ai/glm-5.2"}, nil)
	poll(t, app)
	enterNodeRoom(t, app, "craft")
	runCmd(t, app, app.drillInto("craft/scaffold", "scaffold"), 0)

	card := chargeRows(t, app)
	if !strings.Contains(card, "scaffold the page") {
		t.Fatalf("the part's page does not say what the part was asked to do:\n%s", card)
	}
	if strings.Contains(card, roomAsk) {
		t.Fatalf("the part's page led with the whole job's ask:\n%s", card)
	}
}

// The reading is chosen off TYPED COLUMNS and never from prose (13.3.1), and it
// never says one sentence twice (§19).
func TestTheChargeReadingChoosesBetweenTheAskAndTheBrief(t *testing.T) {
	job := func(node store.Node) store.Node {
		node.Parent = store.RootID
		return node
	}
	for _, c := range []struct {
		name string
		node store.Node
		want string
	}{
		{"the ask leads and the errand follows",
			job(store.Node{Brief: roomErrand, Provenance: store.Provenance{Intent: roomAsk}}),
			roomAsk + "\n\n" + roomErrand},
		{"a brief that already carries the ask is not made to say it twice",
			job(store.Node{Brief: roomAsk + " Write the file.",
				Provenance: store.Provenance{Intent: roomAsk}}),
			roomAsk + " Write the file."},
		{"no ask recorded leaves the brief exactly as it was",
			job(store.Node{Brief: roomErrand}), roomErrand},
		{"a part keeps its own brief",
			store.Node{Parent: "craft", Brief: "scaffold the page",
				Provenance: store.Provenance{Intent: roomAsk}}, "scaffold the page"},
	} {
		if got := chargeReading(c.node); got != c.want {
			t.Fatalf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// DEFECT 2. The header's receipt is LIVE: absent while nothing has billed, and
// the subtree's own rollup — money and burn — the moment a call settles.
//
// The move that lands it is the one the old fingerprint could not see. A
// `usage_recorded` event advances the journal and touches no node: the part is
// still the same part, still running. So this test changes NOTHING else.
func TestTheRoomHeaderRollsUpTheSubtreesUsageAsCallsSettle(t *testing.T) {
	graph := roomJournal(t)
	app := newJournalApp(t, graph, &fakeCommander{model: "z-ai/glm-5.2"}, nil)
	poll(t, app)
	enterNodeRoom(t, app, "craft")

	// ABSENT IS NOT ZERO. Nothing has billed, so money draws its own absence and
	// the burn draws nothing at all (§16, 8.2.20).
	before := chargeMeta(t, app)
	if !strings.Contains(before, tokens.GlyphSpend+tokens.GlyphMissing) {
		t.Fatalf("an unbilled job did not draw its money as absent: %q", before)
	}
	for _, banned := range []string{"$0.00", "tok"} {
		if strings.Contains(before, banned) {
			t.Fatalf("the header invented %q before anything billed: %q", banned, before)
		}
	}

	if err := graph.RecordUsage(store.NodeUsage{NodeID: "craft/scaffold", Cost: 0.31,
		PromptTokens: 12000, CompletionTokens: 900, Model: "deepseek/deepseek-v4-flash"}); err != nil {
		t.Fatal(err)
	}
	poll(t, app)

	after := chargeMeta(t, app)
	if !strings.Contains(after, "$0.31") {
		t.Fatalf("the bill landed and the room's header did not move: %q", after)
	}
	if !strings.Contains(after, "tok") {
		t.Fatalf("the header says what the job cost and not what it burned: %q", after)
	}
	if strings.Contains(after, tokens.GlyphSpend+tokens.GlyphMissing) {
		t.Fatalf("a billed job still draws its money as absent: %q", after)
	}
	// The receipt is the CARD's grammar, one surface over (jobcard.go's
	// taskReceipt): elapsed, money, burn, then the models that did the work.
	if !strings.Contains(after, modelWord("deepseek/deepseek-v4-flash")) {
		t.Fatalf("the header does not name what ran: %q", after)
	}
	if strings.Contains(after, "deepseek/") {
		t.Fatalf("a provider slug reached the receipt: %q", after)
	}
	// And it is on the frame, not only in the block.
	if !strings.Contains(ansi.Strip(app.Frame(140, 30)), "$0.31") {
		t.Fatal("the money is not on screen")
	}
}

// A ROOM STANDING OVER A NODE THE RAIL NEVER PRICED STILL DRAWS ITS RECEIPT, and
// it pays for exactly one read to do it.
//
// The rail's ledger map is filled for the rooms whose subtree read has come back
// and for a bounded few cards on the home; a page can be over neither — a job
// further down the board, or a PART, which is never a ledger root because a
// ledger is read per job. [scopeSource.roomSpend] is that page's own read, and a
// part's page must NOT take it: its job's ledger already holds its receipt, and
// a second read would be two opinions about one subtree taken at two moments.
func TestAPageOverAnUnpricedNodeReadsItsOwnLedgerOnce(t *testing.T) {
	graph := roomJournal(t)
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "craft/scaffold", Cost: 0.31,
		PromptTokens: 12000, CompletionTokens: 900, Model: "deepseek/deepseek-v4-flash"}); err != nil {
		t.Fatal(err)
	}
	counted := &countingReceipts{Store: graph}
	app := New(Options{Backend: counted, Commander: &fakeCommander{model: "z-ai/glm-5.2"},
		Session: testSession, Profile: tokens.NoColor})
	poll(t, app)
	enterNodeRoom(t, app, "craft")
	if !strings.Contains(chargeMeta(t, app), "$0.31") {
		t.Fatalf("the room drew no money: %q", chargeMeta(t, app))
	}

	reads := counted.reads
	runCmd(t, app, app.drillInto("craft/scaffold", "scaffold"), 0)
	if !strings.Contains(chargeMeta(t, app), "$0.31") {
		t.Fatalf("the part's own page drew no money: %q", chargeMeta(t, app))
	}
	if counted.reads != reads {
		t.Fatalf("the part's page paid for %d more ledger reads; its job's ledger already holds it",
			counted.reads-reads)
	}
}

// countingReceipts is the real store with a tally on the one read this lane
// added. Every other capability is the store's own, promoted, so the app is
// wired exactly as it is in production.
type countingReceipts struct {
	*store.Store
	reads int
}

func (c *countingReceipts) SubtreeReceipts(root string) (store.SubtreeLedger, error) {
	c.reads++
	return c.Store.SubtreeReceipts(root)
}
