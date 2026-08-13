package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// The ask, and the room it came out of. The ask is one sentence; the
// conversation is a morning, and in the incident it was 157 lines of one.
const (
	forkedAsk  = "Rework the SIGNAL game into a tabbed multi-game launcher."
	forkedRoom = `them: launch a multi level dag task on agentfield using qwen max
you, earlier: the dag is on agentfield now, running on qwen max
them: add another level to the dag
you, earlier: done — every level is on qwen max
them: okay, different thing now`
)

// forkedRoomJournal is one forked job on a real journal: the command carries
// the two halves apart, and the node carries only the ask.
func forkedRoomJournal(t *testing.T) (*store.Store, string) {
	t.Helper()
	graph := openJournal(t)
	command, err := graph.RequestCommand(store.Command{
		SessionID:   testSession,
		Kind:        store.CommandSplice,
		Instruction: forkedAsk,
		Context:     forkedRoom,
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}
	root := commandJobIDs(command.Seq)[0]
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: root, Title: "SIGNAL launcher", Brief: "Build the tabbed launcher.", Stage: 1},
	}}, store.Provenance{
		Origin: store.OriginUser, SessionID: testSession, Intent: forkedAsk,
	}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	return graph, root
}

// THE ROOM OPENS ON THE ASK, AND THE CONVERSATION IS BEHIND THE DOOR.
//
// The defect (owner's journal, 2026-08-12): a forked task's room led with the
// whole instruction, which was the person's sentence with a room's worth of
// transcript pasted under it — a hundred and fifty lines of an earlier subject,
// dressed as what this work is. The ask was the first 76 bytes and the rest was
// somebody else's morning.
func TestATaskRoomLeadsWithTheAskAndFoldsTheConversation(t *testing.T) {
	graph, root := forkedRoomJournal(t)
	app := newJournalApp(t, graph, &fakeCommander{model: "z-ai/glm-5.2"}, nil)
	poll(t, app)
	enterNodeRoom(t, app, root)

	card := chargeRows(t, app)
	if !strings.Contains(card, forkedAsk) {
		t.Fatalf("the room does not open on the ask:\n%s", card)
	}
	for _, buried := range []string{"qwen max", "multi level dag", "them:"} {
		if strings.Contains(strings.ToLower(card), buried) {
			t.Fatalf("the conversation is above the fold (%q):\n%s", buried, card)
		}
	}
	// And it is on the frame, which is where the reader meets it.
	if !strings.Contains(ansi.Strip(app.Frame(120, 30)), forkedAsk) {
		t.Fatal("the ask is not on screen")
	}
}

// NOTHING IS HIDDEN (13.1 item 3). The conversation is folded, not dropped: it
// is the record of what this work was told, and one keypress opens it.
func TestATaskRoomsFoldOpensOntoTheConversation(t *testing.T) {
	graph, root := forkedRoomJournal(t)
	app := newJournalApp(t, graph, &fakeCommander{model: "z-ai/glm-5.2"}, nil)
	poll(t, app)
	enterNodeRoom(t, app, root)

	ask, ok := app.view.transcript.Block(1).(*messageBlock)
	if !ok || ask.ID() != chargeAskID {
		t.Fatalf("block 1 is not the ask: %T", app.view.transcript.Block(1))
	}
	if !ask.collapsible {
		t.Fatal("the conversation went behind no door at all")
	}
	runCmd(t, app, app.toggleFold(ask), 0)
	if !ask.Expanded() {
		t.Fatal("the door did not open")
	}
	opened := ansi.Strip(strings.Join(ask.Rows(110), "\n"))
	for _, want := range []string{forkedAsk, "qwen max", "okay, different thing now"} {
		if !strings.Contains(opened, want) {
			t.Fatalf("the opened card is missing %q:\n%s", want, opened)
		}
	}
}

// A job that inherited no conversation draws exactly the page it always drew.
// The separation is not allowed to cost the overwhelming majority of work a
// door onto nothing, which 5.20 rule 3 forbids naming.
func TestAJobWithNoConversationGrowsNoExtraDoor(t *testing.T) {
	graph := openJournal(t)
	command, err := graph.RequestCommand(store.Command{
		SessionID: testSession, Kind: store.CommandSplice, Instruction: forkedAsk,
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}
	root := commandJobIDs(command.Seq)[0]
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: root, Title: "SIGNAL launcher", Brief: forkedAsk, Stage: 1},
	}}, store.Provenance{
		Origin: store.OriginUser, SessionID: testSession, Intent: forkedAsk,
	}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	app := newJournalApp(t, graph, &fakeCommander{model: "z-ai/glm-5.2"}, nil)
	poll(t, app)
	enterNodeRoom(t, app, root)

	ask, ok := app.view.transcript.Block(1).(*messageBlock)
	if !ok || ask.ID() != chargeAskID {
		t.Fatalf("block 1 is not the ask: %T", app.view.transcript.Block(1))
	}
	if ask.collapsible {
		t.Fatal("a job with nothing folded was given a door onto nothing")
	}
}

// A PART'S PAGE IS NOT ITS JOB'S, and that rule is stated where the READ is as
// well as where the card is: a part's id is not a command's, so the room over
// one never asks for the whole job's conversation.
func TestOnlyAJobRootNamesTheCommandItsAskCameFrom(t *testing.T) {
	for _, c := range []struct {
		id   string
		seq  int64
		want bool
	}{
		{"task-12", 12, true},
		{"craft-12", 12, true},
		{"task-12-3", 0, false},
		{"craft/scaffold", 0, false},
		{"spine", 0, false},
		{"task-", 0, false},
		{"task-0", 0, false},
	} {
		seq, ok := commandSeqOf(c.id)
		if ok != c.want || seq != c.seq {
			t.Fatalf("commandSeqOf(%q) = %d,%t want %d,%t", c.id, seq, ok, c.seq, c.want)
		}
	}
}
