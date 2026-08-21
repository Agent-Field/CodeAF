package tui3

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

// At the cap the surface refuses in home's own voice, and the count in the
// sentence is the constant rather than a second spelling of it.
func TestTheCapRefusesInHomesOwnVoice(t *testing.T) {
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.stirs = make(chan string, stirDepth)
	a.behind = map[string]*kept{}
	// Six behind plus the one in front is seven open, which leaves room for one.
	for i := 0; i < convCap-2; i++ {
		key := "/tmp/lab/" + itoa(i) + "/transcript.jsonl"
		a.behind[key] = &kept{conv: Conversation{SessionFile: key}, side: &aside{}}
	}
	if word, ok := a.roomForAnother(); !ok {
		t.Fatalf("room was refused one short of the cap: %s", word)
	}
	// And the eighth fills it.
	a.behind["/tmp/lab/last/transcript.jsonl"] = &kept{
		conv: Conversation{SessionFile: "/tmp/lab/last/transcript.jsonl"}, side: &aside{}}
	word, ok := a.roomForAnother()
	if ok {
		t.Fatal("a ninth conversation was allowed")
	}
	if want := "8 open is as many as aforge holds — /quit closes this one"; word != want {
		t.Fatalf("the refusal reads %q", word)
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
