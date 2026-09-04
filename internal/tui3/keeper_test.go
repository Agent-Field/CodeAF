package tui3

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE KEEPER: conversations this process holds and is not drawing.

// stowOne detaches whatever is in front and puts it in the keeper, then puts a
// fresh conversation on the surface — which is what every door that opens a
// second project does, said once for the tests.
func stowOne(t *testing.T, a *app, agent *switchAgent, file string) {
	t.Helper()
	leaving, side := a.front(), a.detachConversation()
	a.stow(leaving, side)
	drain(t, a, a.attachConversation(Conversation{Agent: agent, SessionFile: file}, nil))
}

func TestAConversationInTheKeeperIsStillOpenAndNeverAnotherWindow(t *testing.T) {
	first := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(first)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan string, stirDepth)

	second := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	stowOne(t, a, second, "/tmp/lab/two/transcript.jsonl")

	if first.closed {
		t.Fatal("the conversation left behind was closed — it is open, not gone")
	}
	if a.openCount() != 2 {
		t.Fatalf("this process holds %d conversations, and two are open", a.openCount())
	}
	// IDENTITY BEFORE FLOCK. A transcript this process holds answers InUse TRUE
	// about itself, so every door has to ask the keeper first.
	if !a.holding("/tmp/lab/one/transcript.jsonl") {
		t.Fatal("the keeper does not recognise a transcript this process is holding")
	}
	if !a.holding("/tmp/lab/two/transcript.jsonl") {
		t.Fatal("the conversation in front is not recognised as ours")
	}
	if a.holding("/tmp/lab/three/transcript.jsonl") {
		t.Fatal("a transcript nobody here has open was claimed as ours")
	}
}

// Going back is a switch and not an open: the same agent comes forward, and the
// one that was in front takes its place in the keeper.
func TestGoingBackToAConversationSwapsThemRatherThanOpeningOne(t *testing.T) {
	first := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(first)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan string, stirDepth)
	second := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	stowOne(t, a, second, "/tmp/lab/two/transcript.jsonl")

	cmd, ours := a.bringForward("/tmp/lab/one/transcript.jsonl")
	if !ours {
		t.Fatal("the keeper did not recognise its own conversation")
	}
	drain(t, a, cmd)

	if a.agent != Agent(first) {
		t.Fatal("the conversation that came forward is not the one that was asked for")
	}
	if first.closed || second.closed {
		t.Fatal("a switch closed an agent")
	}
	if a.openCount() != 2 {
		t.Fatalf("this process holds %d conversations after a switch", a.openCount())
	}
	// AND THE WAY BACK IS THE ONE THAT WAS JUST LEFT, not the one before it.
	last, ok := a.lastBehind()
	if !ok || last != convKey("/tmp/lab/two/transcript.jsonl") {
		t.Fatalf("the way back points at %q", last)
	}
}

// Being asked for the conversation already on screen is answered by staying in
// it: reopening would drop the lock, replay the journal and land exactly here.
func TestBringingForwardTheConversationInFrontStaysInIt(t *testing.T) {
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.file = "/tmp/lab/one/transcript.jsonl"
	cmd, ours := a.bringForward("/tmp/lab/one/transcript.jsonl")
	if !ours || cmd != nil {
		t.Fatalf("asking for the conversation in front did %v", cmd)
	}
	if a.openCount() != 1 {
		t.Fatalf("this process holds %d conversations", a.openCount())
	}
}

// The canonical key is what identity is compared on, so two spellings of one
// transcript are one conversation rather than two agents on one journal.
func TestTwoSpellingsOfOneTranscriptAreOneConversation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if convKey(file) != convKey(filepath.Join(dir, ".", "transcript.jsonl")) {
		t.Fatal("two spellings of one transcript came out as two keys")
	}
	if convKey("") != "" {
		t.Fatal("an unnamed transcript was given a key")
	}
}

// A window holds as many conversations as somebody opens: the keeper counts them
// and never refuses one, and the count on the status line keeps counting past
// the eight that used to be the cap.
func TestTheKeeperRefusesNothingHoweverManyAreOpen(t *testing.T) {
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.stirs = make(chan string, stirDepth)
	a.behind = map[string]*kept{}
	for i := 0; i < 20; i++ {
		key := "/tmp/lab/" + itoa(i) + "/transcript.jsonl"
		a.behind[key] = &kept{conv: Conversation{SessionFile: key}, side: &aside{}}
	}
	// Twenty in the keeper plus the one in front, and every one of them is still
	// held and still counted.
	if got := a.openCount(); got != 21 {
		t.Fatalf("this process holds %d conversations", got)
	}
	if got := a.waitingCount(); got != 0 {
		t.Fatalf("%d of them said they were waiting on somebody", got)
	}
}

// A conversation this process holds says how long ago it was left, which is what
// home measures "since you last looked" from.
func TestTheKeeperRecordsWhenAConversationWasLeft(t *testing.T) {
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan string, stirDepth)
	stowOne(t, a, &switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "/tmp/lab/two/transcript.jsonl")

	since := a.behindSince("/tmp/lab/one/transcript.jsonl")
	if since.IsZero() || time.Since(since) > time.Minute {
		t.Fatalf("the conversation was left at %v", since)
	}
	if !a.behindSince("/tmp/lab/nowhere/transcript.jsonl").IsZero() {
		t.Fatal("a conversation this process does not hold was given a leaving time")
	}
}

// ── closing, quitting, and the count ────────────────────────────────────────

// /quit closes the conversation in front and brings the previous one forward.
// aforge leaves only when it was the last one.
func TestQuitClosesOneConversationAndLeavesOnTheLast(t *testing.T) {
	first := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(first)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan string, stirDepth)
	second := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	stowOne(t, a, second, "/tmp/lab/two/transcript.jsonl")

	cmd, more := a.closeFront()
	if !more {
		t.Fatal("/quit with two open left aforge")
	}
	drain(t, a, cmd)
	if !second.closed {
		t.Fatal("the conversation in front was not closed")
	}
	if a.agent != Agent(first) {
		t.Fatal("the previous conversation did not come forward")
	}
	if a.openCount() != 1 {
		t.Fatalf("this terminal holds %d conversations", a.openCount())
	}
	// AND THE CLOSED ONE IS GONE FROM THE WAY BACK.
	for _, key := range a.prev {
		if key == convKey("/tmp/lab/two/transcript.jsonl") {
			t.Fatal("a closed conversation is still on the previous stack")
		}
	}
	if _, more := a.closeFront(); more {
		t.Fatal("/quit on the last conversation found another one to come forward")
	}
}

// ctrl+c twice ends every in-process conversation, and leaving is idempotent.
func TestQuittingClosesEveryConversation(t *testing.T) {
	first := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(first)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan string, stirDepth)
	second := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	stowOne(t, a, second, "/tmp/lab/two/transcript.jsonl")

	a.leaveEverything()
	a.leaveEverything()
	if !first.closed || !second.closed {
		t.Fatalf("closed: front=%v behind=%v", second.closed, first.closed)
	}
	if len(a.behind) != 0 || len(a.prev) != 0 {
		t.Fatal("the keeper survived the quit")
	}
}

// The armed line counts conversations before it counts work: a person who has
// forgotten they left something open in another project needs the first number
// before the second one means anything.
func TestTheArmedLineCountsAcrossEveryConversation(t *testing.T) {
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan string, stirDepth)

	// A quiet single conversation reads exactly the bare sentence.
	if got := a.quitHint(); got != quitArmWord {
		t.Fatalf("a quiet single conversation armed %q", got)
	}

	stowOne(t, a, &switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "/tmp/lab/two/transcript.jsonl")
	if got := a.quitHint(); got != quitArmWord+" · 2 conversations" {
		t.Fatalf("two open armed %q", got)
	}

	a.tasks = map[uint64]*taskNode{7: {id: 7, state: session.TaskRunning}}
	a.taskOrder = []uint64{7}
	if got := a.quitHint(); got != quitArmWord+" · 2 conversations · a task will stop" {
		t.Fatalf("two open with work armed %q", got)
	}
}

// The count segment is absent at one conversation and present at two, and the
// waiting clause is absent when nothing is waiting.
func TestTheOpenCountIsAbsentAtOneAndPresentAtTwo(t *testing.T) {
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan string, stirDepth)
	if got := a.openSegment(); got != "" {
		t.Fatalf("one conversation drew %q", got)
	}
	stowOne(t, a, &switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "/tmp/lab/two/transcript.jsonl")
	if got := a.openSegment(); got != "2 open" {
		t.Fatalf("two conversations drew %q", got)
	}
}

// tab over an empty box is the way back, and it does nothing at all when there
// is nowhere to go.
func TestTabGoesBackToTheLastConversationAndIsSilentWhenThereIsNone(t *testing.T) {
	first := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(first)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan string, stirDepth)
	a.dismissWelcome()

	if cmd := a.lastConversation(); cmd != nil {
		t.Fatal("tab did something with one conversation open")
	}
	second := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	stowOne(t, a, second, "/tmp/lab/two/transcript.jsonl")

	drive(t, a, key("tab"))
	if a.agent != Agent(first) {
		t.Fatal("tab did not go back to the conversation before this one")
	}
	drive(t, a, key("tab"))
	if a.agent != Agent(second) {
		t.Fatal("tab again did not come back")
	}
}

// AND IT IS SWALLOWED BY THE TWO CLAIMS THAT USED TO COLLIDE WITH IT: a rail
// holding the keyboard eats it, and the welcome box neither takes it nor is
// dismissed by it.
func TestTabIsEatenByTheRailAndNeverDismissesTheWelcomeBox(t *testing.T) {
	first := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(first)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan string, stirDepth)
	a.welcome = welcome{open: true, sel: -1}
	second := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	stowOne(t, a, second, "/tmp/lab/two/transcript.jsonl")
	a.welcome = welcome{open: true, sel: -1}

	drive(t, a, key("tab"))
	if !a.welcome.open || a.welcome.spent {
		t.Fatal("tab dismissed the welcome box on its way past")
	}
	if a.agent != Agent(first) {
		t.Fatal("tab did not switch with the welcome box up")
	}

	// AND THE RAIL EATS IT while it holds the keyboard: a person who arrived
	// somewhere else with a rail focus they cannot see would be one keystroke
	// doing two things. The box is put away first, because the roster stands
	// down while it is up (task.go's [app.railKey]).
	a.dismissWelcome()
	held := a.agent
	a.railHold = true
	drive(t, a, key("tab"))
	if a.agent != held {
		t.Fatal("tab switched while the rail held the keyboard")
	}
}
