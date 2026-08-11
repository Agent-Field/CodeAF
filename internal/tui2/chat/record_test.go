package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
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
func TestATaskRoomDrawsTheWorkItsPartsJournaled(t *testing.T) {
	backend := recordBoard()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	enterRoom(t, app, "5") // wisp-parity-2, newest first

	frame := ansi.Strip(app.Frame(120, 30))
	// The parts are named, and each one says what it said. The rail draws the
	// same names one column over; these assertions are about the TRANSCRIPT, so
	// they are made on the prose only a transcript carries.
	for _, want := range []string{
		"Synchronous XHR blocks the event loop.",
		"did not finish — the provider dropped the stream",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the room did not draw %q:\n%s", want, frame)
		}
	}
	// A part that has not run yet is still a row, and the reason it is sitting
	// still is on it. That is the record at this moment, and a room that drew
	// nothing for it would be hiding the plan.
	if !strings.Contains(frame, "waits on H2") {
		t.Fatalf("the room did not say why KeyCutter is waiting:\n%s", frame)
	}
	// And it did all of that without a single node-anchored message.
	if len(app.view.messages) != 0 {
		t.Fatalf("the fixture leaked messages into the room: %+v", app.view.messages)
	}
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

	if frame := ansi.Strip(app.Frame(120, 30)); strings.Contains(frame, "rotation every 90 days") {
		t.Fatalf("the fixture already said what the test is waiting for:\n%s", frame)
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

	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "rotation every 90 days") {
		t.Fatalf("the room did not follow the part that finished:\n%s", frame)
	}
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
