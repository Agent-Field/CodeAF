package tui3

// ── WALKING INTO WORK THAT IS ALREADY UNDER WAY ─────────────────────────────
//
// The report these tests hold shut: "when I click on a task it seems like it is
// always starting from the start — streaming from the start — and moving between
// tasks, or from the chat to a task, the work is not displayed properly".
//
// A room has two lanes and they meet at ONE INSTANT: the journal is every
// message the node has COMPLETED, and the live lane is what happens FROM NOW.
// Everything the node is in the middle of falls between them — the answer it is
// streaming, the reasoning it is spilling, the tool that is running — and a page
// that draws neither of those is a page showing the last thing that finished,
// which is exactly "it is not displaying the work".
//
// The tests below drive the reproduction from both ends: what the FIRST FRAME
// shows on a node caught mid-flight, and what it shows again after walking out
// to the conversation and back, and after walking to another node and back.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// midFlightJournal is a node's session file at the instant a person walks in:
// two settled turns behind it, and a third whose assistant message has been
// journaled — the model finished spelling the call out — while the call it made
// is STILL RUNNING, so no tool result line exists for it yet.
//
// That last shape is the whole reproduction. internal/session journals the
// assistant message BEFORE the tool batch runs (loop.go), so the file a room
// reads names a call whose answer is minutes away.
func midFlightJournal(t *testing.T) string {
	t.Helper()
	lines := []string{
		msgLine(`{"type":"message","role":"user","content":"Fix the nil-map crash"}`),
		msgLine(`{"type":"message","role":"assistant","content":"Looking at the loader first.","toolCalls":[{"id":"c1","function":{"name":"read","arguments":"{\"path\":\"internal/config/load.go\"}"}}]}`),
		msgLine(`{"type":"message","role":"tool","toolCallId":"c1","content":"189 lines"}`),
		msgLine(`{"type":"message","role":"assistant","content":"Found it — the map is never made."}`),
		msgLine(`{"type":"message","role":"user","content":"good, fix it"}`),
		msgLine(`{"type":"message","role":"assistant","content":"Running the tests now.","toolCalls":[{"id":"c2","function":{"name":"bash","arguments":"{\"command\":\"go test ./internal/config/\"}"}}]}`),
	}
	return roomJournal(t, lines...)
}

// msgLine is one journal line, verbatim: these tests are about what a real file
// says, so they write the shape internal/session appends rather than a helper's
// idea of it.
func msgLine(line string) string { return line }

// ── 1. THE CALL THAT HAS NOT COME BACK ──────────────────────────────────────

// A NODE'S RUNNING CALL IS DRAWN AS RUNNING. The journal names a call with no
// result under it, which is the file's own way of saying "this has not returned",
// and a page that drew it as a finished call would be telling a person the work
// is further along than it is — and would leave the row inert when the real end
// arrives on the live lane.
func TestARoomOpenedMidCallDrawsThatCallAsRunning(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = midFlightJournal(t)

	a.openRoom(7, "Fix the nil-map crash")
	a.touch()

	running := 0
	for _, e := range a.room.entries {
		if e.kind == entryTool && e.status.live() {
			running++
		}
	}
	if running != 1 {
		t.Fatalf("the room drew %d running calls, want the one the journal left open:\n%s",
			running, roomText(a))
	}
	// And the call that DID come back is settled: a page that marked everything
	// running would be the same lie in the other direction.
	for _, e := range a.room.entries {
		if e.kind == entryTool && e.tool == "read" && e.status.live() {
			t.Fatalf("a call with its result in the journal is still drawn as running:\n%s",
				roomText(a))
		}
	}
}

// AND THE LIVE END RESOLVES THE ROW THE JOURNAL DREW. The node's `bash` finishes
// a second after the person walked in; the row it lands on is the one that was
// already on the page, not a second row for the same call.
func TestTheLiveEndResolvesTheCallTheJournalLeftRunning(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = midFlightJournal(t)
	a.openRoom(7, "Fix the nil-map crash")
	a.touch()

	before := countTool(a, "bash")
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolEnd, Tool: "bash", Output: "ok  internal/config",
	}})

	if got := countTool(a, "bash"); got != before {
		t.Fatalf("the live end drew a second row for the same call: %d rows, want %d\n%s",
			got, before, roomText(a))
	}
	for _, e := range a.room.entries {
		if e.kind == entryTool && e.tool == "bash" && e.status.live() {
			t.Fatalf("the call the journal left running never settled:\n%s", roomText(a))
		}
	}
}

// TWO CALLS OF ONE TOOL IN FLIGHT PAIR BY THEIR PAYLOAD, not by which row was
// drawn first: a page can hold one row off the file and one off the lane, and
// oldest-of-that-tool would settle the wrong one half the time.
func TestAnEndSettlesTheCallItIsActuallyAbout(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = roomJournal(t,
		`{"type":"message","role":"user","content":"Fix the nil-map crash"}`,
		`{"type":"message","role":"assistant","content":"Running both suites.","toolCalls":[`+
			`{"id":"c1","function":{"name":"bash","arguments":"{\"command\":\"go test ./internal/config/\"}"}},`+
			`{"id":"c2","function":{"name":"bash","arguments":"{\"command\":\"go test ./internal/parse/\"}"}}]}`,
	)
	a.openRoom(7, "Fix the nil-map crash")
	a.touch()

	// The SECOND call comes back first, which is what a parallel batch does.
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolEnd, Tool: "bash",
		Args: `{"command":"go test ./internal/parse/"}`, Output: "ok  internal/parse",
	}})

	for _, e := range a.room.entries {
		if e.kind != entryTool {
			continue
		}
		parse := strings.Contains(e.detail.Args, "internal/parse")
		if parse == e.status.live() {
			t.Fatalf("the end settled the wrong row: %q is %v\n%s",
				e.detail.Args, e.status, roomText(a))
		}
	}
}

// A NODE THAT DIED MID-CALL STOPS PULSING. The lane closing is the last word
// there will ever be about that call, so a row still animating at it is a page
// drawing work that ended — the same law [app.roomResolveUnfinished] states for
// a call that was still being spelled out. The row keeps its status, because
// nobody watched what became of the call; what it stops doing is claiming to be
// alive.
func TestALaneThatClosesResolvesTheCallsStillRunning(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = midFlightJournal(t)
	a.openRoom(7, "Fix the nil-map crash")
	a.touch()
	a.state = stateWorking // the conversation above the room is busy

	drive(t, a, roomClosedMsg{gen: a.room.gen})
	for i := range a.room.entries {
		e := &a.room.entries[i]
		if e.kind != entryTool || !e.status.live() {
			continue
		}
		if e.ended.IsZero() {
			t.Fatalf("a landed node left %q unresolved:\n%s", e.tool, roomText(a))
		}
		if mark := plain(a.mark(e)); mark != glyphIdle && mark != glyphIdleASCII {
			t.Fatalf("a landed node still animates %q: mark %q", e.tool, mark)
		}
	}
}

// ── 2. THE STEP THE NODE IS IN THE MIDDLE OF ────────────────────────────────

// THE CATCH-UP IS ON THE FIRST FRAME. internal/session hands a joining watcher
// the step in flight before its live events (task_room.go's [taskCatchup]), and
// the page has to draw it where it happened: under the journal's last settled
// line, at the bottom of the page, on the frame a person sees when they walk in
// — not on whatever frame the next delta happens to land on.
func TestARoomOpensShowingTheStepTheNodeIsInTheMiddleOf(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = midFlightJournal(t)
	// What the engine's catch-up carries: the reasoning, the reply so far, and
	// the call the model has finished asking for and not yet started.
	fake.catchup = map[uint64][]session.Event{7: {
		{Kind: session.EventThinking},
		{Kind: session.EventReasoning, Text: "the map is never made"},
		{Kind: session.EventTextDelta, Text: "The tests pass. Fixing the loader"},
		{Kind: session.EventToolAnnounced, CallID: "c3", Tool: "edit",
			Hint: "edit internal/config/load.go", Args: `{"path":"internal/config/load.go"}`},
	}}

	clickRail(t, a, 0)
	if !a.roomOpen() {
		t.Fatal("a rail click did not open the node's room")
	}

	// The whole page has it…
	page := roomText(a)
	for _, want := range []string{"Fixing the loader", "edit internal/config/load.go"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the room never drew the step in flight (%q):\n%s", want, page)
		}
	}
	// …and so does the frame, which is the half that was actually broken: a page
	// that had the rows but opened above them is a page showing work from an hour
	// ago.
	visible, _ := a.roomWindow(a.bodyWidth(), a.viewHeight())
	var frame []string
	for _, r := range visible {
		frame = append(frame, plain(r.text))
	}
	shown := strings.Join(frame, "\n")
	if !strings.Contains(shown, "Fixing the loader") {
		t.Fatalf("the first frame does not show the reply in flight:\n%s", shown)
	}
	// AND IT IS DRAWN ONCE. The journal's history and the catch-up meet at one
	// instant and must not overlap: a second copy of the same paragraph is the
	// failure mode a replay-everything catch-up would have had.
	if n := strings.Count(page, "Running the tests now."); n != 1 {
		t.Fatalf("the settled reply is drawn %d times:\n%s", n, page)
	}

	// AND IT SURVIVES WALKING OUT AND BACK IN, which is the other half of the
	// report: the step in flight is a fact about the node, so every joiner is
	// handed it — not just whoever opened the page first.
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("esc did not leave the room")
	}
	clickRail(t, a, 0)
	again := roomText(a)
	if !strings.Contains(again, "Fixing the loader") {
		t.Fatalf("the re-opened room lost the step in flight:\n%s", again)
	}
	if n := strings.Count(again, "Fixing the loader"); n != 1 {
		t.Fatalf("the re-opened room drew the step in flight %d times:\n%s", n, again)
	}
}

// ── 3. THE FIRST FRAME IS THE LIVE EDGE ─────────────────────────────────────

// A LONG JOURNAL OPENS AT ITS BOTTOM, not at its top. This is the "it starts
// from the start" half of the report read literally: a page whose first frame is
// row one is a page showing work from hours ago.
func TestARoomOpensAtTheLiveEdgeAndNotAtTheTop(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = longJournal(t, 60)

	a.openRoom(7, "Fix the nil-map crash")
	a.touch()

	rows := a.roomRows(a.bodyWidth())
	height := a.viewHeight()
	if len(rows) <= height {
		t.Fatalf("the journal is %d rows at %d high — too short to scroll, so this "+
			"test asserts nothing", len(rows), height)
	}
	if got, want := a.roomOffsetFor(len(rows), height), len(rows)-height; got != want {
		t.Fatalf("the room's first frame sits at row %d of %d, want the live edge at %d",
			got, len(rows), want)
	}
	// And the frame a person actually reads ends on the newest line.
	visible, _ := a.roomWindow(a.bodyWidth(), height)
	if len(visible) == 0 {
		t.Fatal("the room's first frame is empty")
	}
	if !strings.Contains(plain(visible[len(visible)-1].text), "line 59") {
		t.Fatalf("the first frame does not end on the newest line:\n%q",
			plain(visible[len(visible)-1].text))
	}
}

// longJournal is a node that has been talking for a while: one turn, then N
// assistant lines, which is more than any terminal is tall.
func longJournal(t *testing.T, n int) string {
	t.Helper()
	lines := []string{msgLine(`{"type":"message","role":"user","content":"Fix the nil-map crash"}`)}
	for i := 0; i < n; i++ {
		lines = append(lines, `{"type":"message","role":"assistant","content":"line `+
			strconv.Itoa(i)+`"}`)
	}
	return roomJournal(t, lines...)
}

// ── 4. WALKING OUT AND BACK ─────────────────────────────────────────────────

// MAIN → ROOM → MAIN → ROOM SHOWS THE SAME PAGE. The second visit re-reads the
// journal off disk, so anything the first visit could see and the second cannot
// is a page that went backwards while the person's back was turned.
func TestLeavingARoomAndComingBackShowsTheSamePage(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = midFlightJournal(t)

	a.openRoom(7, "Fix the nil-map crash")
	a.touch()
	first := roomText(a)

	a.closeRoom()
	if a.roomOpen() {
		t.Fatal("esc did not leave the room")
	}
	a.openRoom(7, "Fix the nil-map crash")
	a.touch()

	if second := roomText(a); second != first {
		t.Fatalf("the room came back different:\nfirst:\n%s\n\nsecond:\n%s", first, second)
	}
	rows := a.roomRows(a.bodyWidth())
	if got, want := a.roomOffsetFor(len(rows), a.viewHeight()), maxInt(0, len(rows)-a.viewHeight()); got != want {
		t.Fatalf("the re-opened room sits at row %d, want the live edge at %d", got, want)
	}
}

// A → B → A IS STABLE. Two nodes, walked between: each page is its own, and
// coming back to the first one lands on the first one — not on a page still
// holding the second node's rows or the second node's scroll.
func TestWalkingBetweenTwoRoomsKeepsEachPageItsOwn(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = midFlightJournal(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(8, "Write the loader test",
		session.TaskRunning, session.TaskNotice{})})

	a.openRoom(7, "Fix the nil-map crash")
	a.touch()
	seven := roomText(a)

	a.openRoom(8, "Write the loader test")
	a.touch()
	if a.room.id != 8 {
		t.Fatalf("the room is on node %d, want 8", a.room.id)
	}
	if a.room.title != "Write the loader test" {
		t.Fatalf("the room's title is %q", a.room.title)
	}

	a.openRoom(7, "Fix the nil-map crash")
	a.touch()
	if a.room.id != 7 {
		t.Fatalf("the room is on node %d, want 7", a.room.id)
	}
	if got := roomText(a); got != seven {
		t.Fatalf("walking to another node and back changed the page:\nwas:\n%s\n\nnow:\n%s",
			seven, got)
	}
}

// countTool is how many rows on the page are that call.
func countTool(a *app, tool string) int {
	n := 0
	for _, e := range a.room.entries {
		if e.kind == entryTool && e.tool == tool {
			n++
		}
	}
	return n
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
