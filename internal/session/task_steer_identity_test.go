package session

// A SEND HAS A NAME, AND THE NAME IS WHAT MAKES ASKING AGAIN SAFE.
//
// The failure these tests are about is not somebody typing twice. It is a
// correction that crossed to the engine, was taken, and whose ANSWER was lost —
// a link that died, a deadline that ran out. The surface holding it can neither
// report it delivered nor send it again, unless the engine can recognise the
// same send arriving a second time. [Agent.SteerTaskFrom] is that door, and the
// laws below are the whole of what it promises.

import (
	"errors"
	"testing"
	"time"
)

// nodeWithNobodyIn is a node that is RUNNING with no worker inside it: a
// stubbed runner parks in the middle of the run, which is what a real node
// looks like while its worktree is still being prepared or its work is being
// checked. Every send into it is HELD on the record — which is a receipt with a
// direction id on it, and therefore exactly the fixture these laws need without
// a provider, a repository or a worker.
//
// The returned function lands the node, so a test can ask what the record says
// after the work is over.
func nodeWithNobodyIn(t *testing.T) (*Agent, uint64, func()) {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	graph := stubbedGraph(agent, func(node *TaskNode) {
		close(started)
		<-release
		node.finish("it landed", nil, "", "")
		node.graph.complete(node, TaskDone)
	})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "a node", brief: "b", acceptance: "a"})
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the stubbed runner never started")
	}
	var once bool
	land := func() {
		if once {
			return
		}
		once = true
		close(release)
		waitDoneNode(t, graph.node(id))
	}
	t.Cleanup(land)
	return agent, id, land
}

// saidTo is every direction on one node's record, for a test that wants to
// count them rather than read them.
func saidTo(t *testing.T, agent *Agent, id uint64) []taskDirection {
	t.Helper()
	node := agent.taskNode(id)
	if node == nil {
		t.Fatalf("no node %d", id)
	}
	return node.directionsNow()
}

// THE FIRST LAW. One send asked for twice is one direction, and the second ask
// answers the receipt the first one was written down as.
func TestOneSendAskedForTwiceIsHeardOnce(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	from := SteerSource{Scope: "window-1", Seq: 1, At: time.Now()}

	first, err := agent.SteerTaskFrom(id, "the config lives under etc/", from)
	if err != nil {
		t.Fatalf("SteerTaskFrom: %v", err)
	}
	if first.Direction == 0 || first.Again {
		t.Fatalf("first send = %+v, want a direction and not a repeat", first)
	}

	// The answer to the first was lost on the way back, so the surface asks
	// again with the same name.
	second, err := agent.SteerTaskFrom(id, "the config lives under etc/", from)
	if err != nil {
		t.Fatalf("asking again for one send: %v", err)
	}
	if !second.Again {
		t.Fatalf("second ask = %+v, want the receipt already on the record", second)
	}
	if second.Direction != first.Direction {
		t.Fatalf("second ask answered direction %d, want the first one's %d", second.Direction, first.Direction)
	}
	if second.Landing != steerAgainWord {
		t.Fatalf("landing = %q, want %q", second.Landing, steerAgainWord)
	}
	if said := saidTo(t, agent, id); len(said) != 1 {
		t.Fatalf("the node holds %d directions, want one: %+v", len(said), said)
	}
}

// THE SECOND LAW, AND IT IS THE ONE THAT KEEPS THE FIRST HONEST. What is
// recognised is the SEND and never the words: pressing enter twice on the same
// sentence means it twice.
func TestTwoIntentionalSendsOfOneSentenceAreTwoDirections(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	at := time.Now()

	first, err := agent.SteerTaskFrom(id, "try it again", SteerSource{Scope: "window-1", Seq: 1, At: at})
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	second, err := agent.SteerTaskFrom(id, "try it again", SteerSource{Scope: "window-1", Seq: 2, At: at.Add(time.Second)})
	if err != nil {
		t.Fatalf("second send: %v", err)
	}
	if first.Again || second.Again {
		t.Fatalf("a second intentional send was folded into the first: %+v / %+v", first, second)
	}
	if first.Direction == second.Direction || second.Direction == 0 {
		t.Fatalf("two sends took one receipt: %d and %d", first.Direction, second.Direction)
	}
	if said := saidTo(t, agent, id); len(said) != 2 {
		t.Fatalf("the node holds %d directions, want two: %+v", len(said), said)
	}
}

// AND A SEND WITH NO NAME IS THE DOOR THAT WAS ALWAYS THERE. A caller that
// cannot number its sends is not made worse off, and it is not silently given a
// guarantee it has not earned: nothing about two of its sends is comparable, so
// both are delivered.
func TestAnUnnamedSendIsDeliveredEveryTime(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	for range 2 {
		receipt, err := agent.SteerTaskFrom(id, "the config lives under etc/", SteerSource{})
		if err != nil {
			t.Fatalf("SteerTaskFrom with no identity: %v", err)
		}
		if receipt.Again {
			t.Fatalf("an unnamed send was recognised as a repeat: %+v", receipt)
		}
	}
	if said := saidTo(t, agent, id); len(said) != 2 {
		t.Fatalf("the node holds %d directions, want two: %+v", len(said), said)
	}
}

// A TASK NUMBER MEANS SOMETHING ONLY INSIDE ONE CONVERSATION. A send written
// for another one is refused before the number is used for anything, because
// task 7 here is not the task 7 the person was looking at.
func TestASendWrittenForAnotherConversationIsRefused(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	agent.config.SessionFile = "/srv/one.jsonl"

	_, err := agent.SteerTaskFrom(id, "make it CSV",
		SteerSource{Scope: "window-1", Seq: 1, At: time.Now(), Conversation: "/srv/two.jsonl"})
	if !errors.Is(err, ErrNotThatConversation) {
		t.Fatalf("a send for another conversation came back as %v, want a refusal", err)
	}
	if said := saidTo(t, agent, id); len(said) != 0 {
		t.Fatalf("the node was corrected anyway: %+v", said)
	}

	// The same send named for THIS conversation lands, so the check is a check
	// and not a wall — and the spelling is compared as a path, not as bytes.
	receipt, err := agent.SteerTaskFrom(id, "make it CSV",
		SteerSource{Scope: "window-1", Seq: 1, At: time.Now(), Conversation: "/srv/./one.jsonl"})
	if err != nil {
		t.Fatalf("a send for the open conversation: %v", err)
	}
	if receipt.Direction == 0 {
		t.Fatalf("receipt = %+v, want the direction it was written down as", receipt)
	}
	if said := saidTo(t, agent, id); len(said) != 1 {
		t.Fatalf("the node holds %d directions, want one", len(said))
	}
}

// AN UNKNOWN IDENTITY IS NOT A MATCH. An engine that cannot say which
// transcript it is writing cannot establish the claim on the send, so the send
// is refused rather than delivered on the strength of a comparison nobody made.
// A send that names no conversation is unaffected, which is every older caller.
func TestAnEngineThatCannotNameItsConversationRefusesABoundSend(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	agent.config.SessionFile = ""

	_, err := agent.SteerTaskFrom(id, "make it CSV",
		SteerSource{Scope: "window-1", Seq: 1, At: time.Now(), Conversation: "/srv/two.jsonl"})
	if !errors.Is(err, ErrConversationUnchecked) {
		t.Fatalf("a bound send to an engine with no transcript came back as %v", err)
	}
	// The caller has to be able to treat it as the refusal it is.
	if !errors.Is(err, ErrNotThatConversation) {
		t.Fatalf("the refusal does not read as one: %v", err)
	}
	if said := saidTo(t, agent, id); len(said) != 0 {
		t.Fatalf("it was delivered anyway: %+v", said)
	}

	if _, err := agent.SteerTaskFrom(id, "make it CSV",
		SteerSource{Scope: "window-1", Seq: 1, At: time.Now()}); err != nil {
		t.Fatalf("a send that names no conversation: %v", err)
	}
	if said := saidTo(t, agent, id); len(said) != 1 {
		t.Fatalf("the node holds %d directions, want one", len(said))
	}
}

// THE THIRD LAW, AND IT IS THE ONE THE ORDER OF THE CHECKS EXISTS FOR. A
// correction whose answer was lost is asked about again LATER — after a
// reconnect, after the window came back — and by then the work may be over.
// The record outlives the run, so the ask is answered with the receipt it
// already earned rather than refused with "that task is done".
func TestARepeatIsAnsweredAfterTheTaskHasFinished(t *testing.T) {
	agent, id, land := nodeWithNobodyIn(t)
	from := SteerSource{Scope: "window-1", Seq: 1, At: time.Now()}

	first, err := agent.SteerTaskFrom(id, "make it CSV", from)
	if err != nil {
		t.Fatalf("SteerTaskFrom: %v", err)
	}
	land()

	// The link came back, and the window is asking what became of its send.
	after, err := agent.SteerTaskFrom(id, "make it CSV", from)
	if err != nil {
		t.Fatalf("asking again after the task landed: %v", err)
	}
	if !after.Again || after.Direction != first.Direction {
		t.Fatalf("after the task landed the ask answered %+v, want the receipt %d it already had", after, first.Direction)
	}
	if said := saidTo(t, agent, id); len(said) != 1 {
		t.Fatalf("the finished node holds %d directions, want one: %+v", len(said), said)
	}
	// AND A SEND IT HAS NEVER HEARD IS STILL REFUSED. The record answering first
	// must not turn a finished task into one that accepts new corrections.
	if _, err := agent.SteerTaskFrom(id, "and sort it", SteerSource{Scope: "window-1", Seq: 2}); err == nil {
		t.Fatal("a finished task accepted a correction it had never heard")
	}
}

// AND ONE NAME MAY NOT CARRY TWO SENTENCES. Answering that with the older
// words' receipt would tell somebody their new correction had arrived while the
// worker was reading a different one.
func TestOneNameCarryingDifferentWordsIsRefused(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	from := SteerSource{Scope: "window-1", Seq: 1, At: time.Now()}

	if _, err := agent.SteerTaskFrom(id, "make it CSV", from); err != nil {
		t.Fatalf("SteerTaskFrom: %v", err)
	}
	receipt, err := agent.SteerTaskFrom(id, "make it TSV", from)
	if !errors.Is(err, errSaidUnderThatName) {
		t.Fatalf("new words under an old name = %+v, %v — want a refusal", receipt, err)
	}
	if receipt.Again {
		t.Fatalf("new words under an old name were reported as already delivered: %+v", receipt)
	}
	if said := saidTo(t, agent, id); len(said) != 1 {
		t.Fatalf("the node holds %d directions, want the one it actually heard: %+v", len(said), said)
	}
}

// AND THE NAME SURVIVES THE CHECKPOINT, which is what makes asking again safe
// after the engine machine itself has restarted: the identity is written beside
// the direction (task_store.go's sourceRecord) and read back with it.
func TestARepeatIsRecognisedAfterTheRecordIsRestored(t *testing.T) {
	var before taskAssignment
	source := spokenSource{id: personSourceID{scope: "window-1", seq: 4}, spoken: time.Now()}
	id, kept, order := before.hear("make it CSV", directionFromPerson, time.Now(), source)
	if !kept || order != directionHeardNow {
		t.Fatalf("the first send was not written down: kept=%v order=%v", kept, order)
	}

	after := restoredAssignment(recordedAssignment(before))
	again, _, order := after.hear("make it CSV", directionFromPerson, time.Now(), source)
	if order != directionAgain {
		t.Fatalf("after a restart the same send was heard as %v, want %v", order, directionAgain)
	}
	if again != id {
		t.Fatalf("the restored record answered direction %d, want the original %d", again, id)
	}
	if len(after.directions) != 1 {
		t.Fatalf("the restored record holds %d directions, want one", len(after.directions))
	}
}

// AND A NAMED SEND FROM A ROOM IS STILL A LINE SAID IN THAT ROOM. The worker
// reads two different sentences under the person's words, and reading an
// identity as proof of a forward would tell it they were addressing other work.
func TestANamedRoomSendKeepsTheRoomsOwnReceiptLine(t *testing.T) {
	inRoom := receiptLineFor(3, SteerSource{Scope: "window-1", Seq: 1}.spoken())
	if inRoom != directionReceiptLine(3) {
		t.Fatalf("a named send from a room reads:\n%s\nwant the room's own line:\n%s", inRoom, directionReceiptLine(3))
	}
	forwarded := receiptLineFor(3, personSource{
		id: personSourceID{scope: "life", seq: 1}, words: "make it CSV", spoken: time.Now(),
	}.said())
	if forwarded != forwardedReceiptLine(3) {
		t.Fatalf("a forwarded line lost its own receipt line:\n%s", forwarded)
	}
}
