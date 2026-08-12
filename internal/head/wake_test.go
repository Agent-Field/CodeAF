package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// journaledCommand puts one command through the belt exactly as a head turn
// does, so the head is left owing a sentence for its settlement.
func journaledCommand(t *testing.T, head *Head, graph *store.Store, session, ask string) store.Command {
	t.Helper()
	user := postUser(t, graph, session, ask)
	run := &beltRun{head: head, user: user}
	if message, failed := run.execute(beltToolSpawn, beltArguments(t,
		map[string]any{"instruction": ask})); failed {
		t.Fatalf("spawn refused: %s", message)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v, want exactly one", commands, err)
	}
	return commands[0]
}

// settleWith resolves one command and posts the receipt row the reconciler
// posts for it — the applied shape (filed on the job's card, no session) or the
// refused one (spoken in the room).
func settleWith(t *testing.T, graph *store.Store, command store.Command,
	status store.CommandStatus, receipt string) store.Message {
	t.Helper()
	if err := graph.ResolveCommand(command.Seq, status, receipt); err != nil {
		t.Fatal(err)
	}
	spliceSurgeryJob(t, graph, "task-settled", "The work it made of it", command.Instruction)
	row := store.Message{
		Role: store.RoleSystem, NodeID: "task-settled",
		CommandSeq: command.Seq, Body: receipt,
	}
	if status == store.CommandRejected {
		row = store.Message{
			SessionID: command.SessionID, Role: store.RoleAgent,
			CommandSeq: command.Seq, Body: "I couldn't apply that request: " + receipt,
		}
	}
	posted, err := graph.PostMessage(row)
	if err != nil {
		t.Fatal(err)
	}
	return posted
}

// The commit-before-shape defect, closed.
//
// The head says "handing this over" while the compiler has not yet read the ask.
// One tick later the receipt carries the reading it settled on and the shape it
// chose, in the tasker's own register, on a card. This is the head reading that
// and saying what it means, in its own voice, in the room where the person is
// actually looking.
func TestASettledCommandWakesTheHeadToSayWhatWasUnderstood(t *testing.T) {
	graph := openHeadStore(t)
	client := &fakeClient{model: "test/model", responses: []string{
		"It's underway — read as a competitor sweep across the three names, in three steps.",
	}}
	head := New(client, graph)
	command := journaledCommand(t, head, graph, "room", "look at what our competitors are doing")
	receipt := settleWith(t, graph, command, store.CommandApplied,
		"Here's my reading: sweep the three named competitors for product changes since June\n"+
			"Assumed: the three named in the last message\nCorrect me anytime — changing course costs nothing.")

	cursors := newSessionCursors(receipt.Seq - 1)
	cursors.mark("room", receipt.Seq-1)
	if err := head.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}

	if calls := client.callCount(); calls != 1 {
		t.Fatalf("one settled command bought %d turns, want exactly one", calls)
	}
	if prompt := client.systemPrompt(); prompt != receiptWakePrompt {
		t.Fatalf("the wake ran on another prompt: %q", firstLine(prompt))
	}
	said := client.userPrompt()
	for name, want := range map[string]string{
		"the reading the tasker settled on": "sweep the three named competitors",
		"their own ask":                     "look at what our competitors are doing",
	} {
		if !strings.Contains(said, want) {
			t.Fatalf("the wake was not given %s: %q", name, said)
		}
	}

	reply := waitForAgentReply(t, graph, "room", receipt.Seq)
	if !strings.Contains(reply.Body, "read as a competitor sweep") {
		t.Fatalf("the head never said what the workforce made of it: %q", reply.Body)
	}
	// Not wearing the command's number: the head's ORIGINAL line carries that,
	// and it is what the surfaces draw the card's reading from.
	if reply.CommandSeq != 0 {
		t.Fatalf("the wake claimed the command's own seq: %+v", reply)
	}

	// And it fires once. A second poll over the same journal has nothing left,
	// because the claim was taken with the first.
	if err := head.poll(context.Background(), cursors); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("the same settlement was spoken %d times", calls)
	}
}

// The stale correction, closed. The head said "cancelling that job" in the same
// breath that it journaled the command; the job had finished a second earlier
// and the command was refused. The refusal used to land as the reconciler's own
// sentence underneath a line of the head's that was now false, with nobody
// owning the contradiction.
func TestARefusedCommandWakesTheHeadToCorrectItself(t *testing.T) {
	graph := openHeadStore(t)
	client := &fakeClient{model: "test/model", responses: []string{
		"That one had already finished a moment before I got to it, so nothing was stopped.",
	}}
	head := New(client, graph)
	command := journaledCommand(t, head, graph, "room", "stop the line scan")
	receipt := settleWith(t, graph, command, store.CommandRejected, "that job has already finished")

	cursors := newSessionCursors(receipt.Seq - 1)
	cursors.mark("room", receipt.Seq-1)
	if err := head.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("a refusal bought %d turns, want exactly one", calls)
	}
	said := client.userPrompt()
	if !strings.Contains(said, "REFUSED") || !strings.Contains(said, "that job has already finished") {
		t.Fatalf("the wake was not told what was refused or why: %q", said)
	}
	replies := agentReplies(t, graph, "room")
	last := replies[len(replies)-1]
	if !strings.Contains(last.Body, "nothing was stopped") {
		t.Fatalf("the head never took back what it had said: %q", last.Body)
	}
}

// Not everything settled is news. A pause did exactly what the verb says and the
// head already said it, in the person's own words, a tick earlier; waking to say
// it again is the duplication the three-class law exists to prevent. What earns
// the turn is a command something INTERPRETED on the way through.
func TestATrivialReceiptBuysNoTurn(t *testing.T) {
	graph := openHeadStore(t)
	client := &fakeClient{responses: []string{"should never be asked for"}}
	head := New(client, graph)
	spliceSurgeryJob(t, graph, "task-3", "Line scans", "run the line scans")
	user := postUser(t, graph, "room", "hold the scans")
	run := &beltRun{head: head, user: user}
	if message, failed := run.execute(beltToolControl, beltArguments(t, map[string]any{
		"verb": "pause", "ids": []string{"task-3"}})); failed {
		t.Fatalf("control refused: %s", message)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	receipt := settleWith(t, graph, commands[0], store.CommandApplied, "held 3 steps")

	cursors := newSessionCursors(receipt.Seq - 1)
	cursors.mark("room", receipt.Seq-1)
	if err := head.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if calls := client.callCount(); calls != 0 {
		t.Fatalf("a receipt that said what the head already said bought %d paid turns", calls)
	}
}

// A command nobody in a head turn journaled — a cancel button on a task page, a
// craft fired from a card — is answered where it was pressed. The head appearing
// in the conversation to narrate it would be an unbidden second voice.
func TestACommandTheHeadNeverJournaledDoesNotWakeIt(t *testing.T) {
	graph := openHeadStore(t)
	client := &fakeClient{responses: []string{"should never be asked for"}}
	head := New(client, graph)
	command, err := graph.RequestCommand(store.Command{
		SessionID: "room", Kind: store.CommandSplice, Instruction: "fired from a page",
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt := settleWith(t, graph, command, store.CommandApplied, "Here's my reading: something")

	cursors := newSessionCursors(receipt.Seq - 1)
	cursors.mark("room", receipt.Seq-1)
	if err := head.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if calls := client.callCount(); calls != 0 {
		t.Fatalf("a page's own command bought %d turns in the conversation", calls)
	}
}

// The two halves of the trigger, stated where they can be read together: the
// head wakes for the kinds it SPOKE for (so a receipt that is itself the answer
// is left alone), and among those only for the ones something interpreted.
func TestTheWakeOnlyCoversKindsTheHeadAlreadySpokeFor(t *testing.T) {
	for _, kind := range []store.CommandKind{
		store.CommandSplice, store.CommandAmend, store.CommandRedirect,
		store.CommandExpedite, store.CommandCraftRun,
	} {
		if !receiptInterprets(kind) {
			t.Fatalf("%s comes back with a reading of its own and does not wake the head", kind)
		}
		if !resident.HeadSpeaksFor(kind) {
			t.Fatalf("%s wakes the head over a receipt that is already the answer", kind)
		}
	}
	for _, kind := range []store.CommandKind{
		store.CommandPause, store.CommandResume, store.CommandCancel,
		store.CommandReprioritize, store.CommandCharterRetire,
	} {
		if receiptInterprets(kind) {
			t.Fatalf("%s did exactly what the verb says and still buys a turn", kind)
		}
	}
	// And the row predicate: only rows that settle a command, and never the
	// person's own words.
	if !receiptRow(store.Message{Role: store.RoleSystem, CommandSeq: 4, Body: "applied"}) {
		t.Fatal("a filed receipt is not read as one")
	}
	if !receiptRow(store.Message{Role: store.RoleAgent, SessionID: "room", CommandSeq: 4, Body: "refused"}) {
		t.Fatal("a spoken refusal is not read as a receipt")
	}
	if receiptRow(store.Message{Role: store.RoleUser, SessionID: "room", CommandSeq: 4, Body: "hi"}) {
		t.Fatal("the person's own row reads as a receipt")
	}
	if receiptRow(store.Message{Role: store.RoleSystem, NodeID: "task-1", SessionID: "room", Body: "delivered"}) {
		t.Fatal("a delivery reads as a receipt — it would be absorbed and woken for")
	}
}
