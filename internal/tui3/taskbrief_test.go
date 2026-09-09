package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// startedBrief is the door's own answer for a `/task` the person typed, stamped
// with the conversation it was typed in — the shape taskcommand.go builds.
func startedBrief(a *app, id, brief string) taskStartedMsg {
	return taskStartedMsg{
		kind: "single", id: id, title: "Widen the pipe",
		brief: brief, conv: a.taskBriefConv(),
	}
}

// mintNode is the update lane minting a node, the way roomApp's own fixture
// does: the notice arriving is what puts a row on the roster.
func mintNode(t *testing.T, a *app, id uint64) {
	t.Helper()
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(id, "Widen the pipe",
		session.TaskRunning, session.TaskNotice{})})
}

// takeStartedCmd runs the started message through the real update loop and
// hands back whatever it asked for next.
func takeStartedCmd(a *app, msg taskStartedMsg) tea.Cmd {
	_, cmd := a.Update(msg)
	return cmd
}

// THE NODE IS ALREADY THERE — the ordinary order — and the words land on it.
func TestATypedBriefLandsOnTheNodeTheDoorStarted(t *testing.T) {
	a, _, _ := roomApp(t)
	if a.tasks[7] == nil {
		t.Fatal("the fixture minted no node")
	}
	if got := strings.TrimSpace(a.tasks[7].brief); got != "" {
		t.Fatalf("a typed task started life with a brief: %q", got)
	}
	if cmd := takeStartedCmd(a, startedBrief(a, "7", "Widen the import pipe.")); cmd != nil {
		t.Fatal("the brief asked again for a node that was already there")
	}
	if got := a.tasks[7].brief; got != "Widen the import pipe." {
		t.Fatalf("the node did not keep the instruction: %q", got)
	}
}

// THE NOTICE HAS NOT ARRIVED YET — the other order — and the brief waits for it
// rather than being written into a roster that has never heard of the id.
func TestATypedBriefWaitsForANodeThatHasNotArrived(t *testing.T) {
	a, _, _ := roomApp(t)
	takeStartedCmd(a, startedBrief(a, "9", "Widen the import pipe."))
	if a.tasks[9] != nil {
		t.Fatal("a receipt invented a node before its notice")
	}
	if a.typedTaskBriefs[9] == "" {
		t.Fatal("the brief was not retained")
	}
	before := len(a.entries)
	mintNode(t, a, 9)
	if a.tasks[9].brief != "Widen the import pipe." || len(a.typedTaskBriefs) != 0 {
		t.Fatal("the arriving node did not consume its brief")
	}
	// Only the notice may add its normal state entry; no second start receipt.
	for _, e := range a.entries[before:] {
		if strings.Contains(e.text, "single task 9 started") {
			t.Fatal("repeated start receipt")
		}
	}
}

func TestAPendingTypedBriefLeavesWithItsConversation(t *testing.T) {
	a, _, _ := roomApp(t)
	takeStartedCmd(a, startedBrief(a, "9", "Only this conversation"))
	a.dropTasks()
	if len(a.typedTaskBriefs) != 0 {
		t.Fatal("pending brief survived the conversation reset")
	}
	mintNode(t, a, 9)
	if a.tasks[9].brief != "" {
		t.Fatal("another conversation inherited the prompt")
	}
}

func TestATypedBriefNeverLandsInAnotherConversation(t *testing.T) {
	a, _, _ := roomApp(t)
	msg := startedBrief(a, "7", "Widen the import pipe.")
	msg.conv = msg.conv + "-somewhere-else"
	if cmd := takeStartedCmd(a, msg); cmd != nil {
		t.Fatal("a brief from another conversation kept asking")
	}
	if got := strings.TrimSpace(a.tasks[7].brief); got != "" {
		t.Fatalf("another conversation's words landed on this node: %q", got)
	}
}

// A CONTRACT THE ENGINE PUBLISHED OUTRANKS THIS ROAD. A node that came in off a
// proposal already knows what it is for.
func TestATypedBriefNeverOverwritesAProposalsOwn(t *testing.T) {
	a, _, _ := roomApp(t)
	a.tasks[7].brief = "the card's own brief"
	if cmd := takeStartedCmd(a, startedBrief(a, "7", "Widen the import pipe.")); cmd != nil {
		t.Fatal("the brief asked again for a node that was already there")
	}
	if got := a.tasks[7].brief; got != "the card's own brief" {
		t.Fatalf("the typed road argued with the engine's contract: %q", got)
	}
}

// A DOOR THAT REFUSED IS NOT A TASK, and neither is an empty brief.
func TestNothingIsAdoptedForARefusedStart(t *testing.T) {
	a, _, _ := roomApp(t)
	blank := startedBrief(a, "7", "   ")
	if cmd := takeStartedCmd(a, blank); cmd != nil {
		t.Fatal("an empty brief asked for a node")
	}
	zero := startedBrief(a, "0", "Widen the import pipe.")
	if cmd := takeStartedCmd(a, zero); cmd != nil {
		t.Fatal("a refusal's zero id asked for a node")
	}
	if got := strings.TrimSpace(a.tasks[7].brief); got != "" {
		t.Fatalf("a refusal put words on a node: %q", got)
	}
}
