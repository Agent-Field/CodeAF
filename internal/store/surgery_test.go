package store

import (
	"errors"
	"strings"
	"testing"
)

func TestSurgeryEventsTransitionsAndRebuildRoundTrip(t *testing.T) {
	graph := openThreadStore(t)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "ship the release", Title: "Release", Stage: 2},
		{ID: "readme", Parent: "job", Brief: "update README", Title: "README", Stage: 1},
		{ID: "tests", Parent: "job", Brief: "run tests", Title: "Tests", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "surgery", Intent: "ship the release"}); err != nil {
		t.Fatal(err)
	}

	active, err := graph.AttachAmendment("readme", "surgery", "also update the changelog")
	if err != nil || active {
		t.Fatalf("pending amendment active=%t err=%v", active, err)
	}
	readme, _, _ := graph.Node("readme")
	if !strings.Contains(readme.Brief, "Amendment: also update the changelog") {
		t.Fatalf("amended brief = %q", readme.Brief)
	}

	if err := graph.SetNodeHold("readme", true, "pause while I think"); err != nil {
		t.Fatal(err)
	}
	held, _, _ := graph.Node("readme")
	if held.Status != Pending || !held.Held {
		t.Fatalf("held node = %+v", held)
	}
	ready, err := graph.Ready(20)
	if err != nil {
		t.Fatal(err)
	}
	if nodeListed(ready, "readme") {
		t.Fatalf("held node remained ready: %+v", ready)
	}
	if err := graph.SetNodeHold("readme", false, "resume"); err != nil {
		t.Fatal(err)
	}

	priority, err := graph.NextSiblingPriority("tests")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.SetNodePriority("tests", priority, "tests first"); err != nil {
		t.Fatal(err)
	}
	ready, err = graph.Ready(20)
	if err != nil {
		t.Fatal(err)
	}
	if indexOfNode(ready, "tests") >= indexOfNode(ready, "readme") {
		t.Fatalf("priority order = %+v", ready)
	}

	claim, won, err := graph.Claim("readme", "worker")
	if err != nil || !won {
		t.Fatalf("claim readme won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	active, err = graph.AttachAmendment("readme", "surgery", "mention upgrade notes")
	if err != nil || !active {
		t.Fatalf("running amendment active=%t err=%v", active, err)
	}
	messages, err := graph.NodeMessages("readme", 0, 20)
	if err != nil || len(messages) != 1 || messages[0].Role != RoleUser || messages[0].Body != "mention upgrade notes" {
		t.Fatalf("steering messages = %+v err=%v", messages, err)
	}
	if err := graph.RequestNodeCancel("readme", "cancelled by user"); err != nil {
		t.Fatal(err)
	}
	control, err := graph.Control("readme")
	if err != nil || !control.CancelRequested {
		t.Fatalf("control = %+v err=%v", control, err)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, found, err := graph.Node("readme")
	if err != nil || !found || rebuilt.Status != Running || !rebuilt.CancelRequested || rebuilt.Held ||
		rebuilt.Priority != 0 || !strings.Contains(rebuilt.Brief, "mention upgrade notes") {
		t.Fatalf("rebuilt readme = %+v found=%t err=%v", rebuilt, found, err)
	}
	rebuiltTests, _, _ := graph.Node("tests")
	if rebuiltTests.Priority != priority {
		t.Fatalf("rebuilt priority = %d, want %d", rebuiltTests.Priority, priority)
	}
	if err := graph.Release(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.CancelPending("readme", "cancelled by user"); err != nil {
		t.Fatal(err)
	}
	cancelled, _, _ := graph.Node("readme")
	if cancelled.Status != Cancelled || cancelled.Owner != "" || cancelled.CancelRequested {
		t.Fatalf("cooperatively cancelled node = %+v", cancelled)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	cancelled, _, _ = graph.Node("readme")
	if cancelled.Status != Cancelled || cancelled.Owner != "" || cancelled.CancelRequested {
		t.Fatalf("rebuilt cancellation = %+v", cancelled)
	}

	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []EventKind{EventNodeAmended, EventNodeHeld, EventNodeResumed,
		EventNodePriorityChanged, EventNodeCancelRequested, EventNodeReleased, EventNodeCancelled} {
		if !eventKindListed(events, kind) {
			t.Errorf("journal omitted %s", kind)
		}
	}
}

func TestSurgeryCommandValidationProtectsFutureEmitters(t *testing.T) {
	graph := openThreadStore(t)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "work", Brief: "work", Stage: 1}}},
		Provenance{Origin: OriginUser, Intent: "work"}); err != nil {
		t.Fatal(err)
	}
	for _, command := range []Command{
		{Kind: CommandResume, Target: "work", Instruction: "resume"},
		{Kind: CommandRestart, Target: "work", Instruction: "retry"},
	} {
		if _, err := graph.RequestCommand(command); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid command %+v error = %v", command, err)
		}
	}
	if err := graph.SetNodeHold("work", true, "pause"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RequestCommand(Command{Kind: CommandResume, Target: "work", Instruction: "resume"}); err != nil {
		t.Fatalf("valid resume: %v", err)
	}
	if _, err := graph.RequestCommand(Command{Kind: CommandPause, Target: "work", Instruction: "pause"}); err != nil {
		t.Fatalf("valid pause: %v", err)
	}
	claim, won, err := graph.Claim("work", "worker")
	if err != nil || won {
		t.Fatalf("held direct claim won=%t err=%v", won, err)
	}
	if err := graph.SetNodeHold("work", false, "resume"); err != nil {
		t.Fatal(err)
	}
	claim, won, err = graph.Claim("work", "worker")
	if err != nil || !won {
		t.Fatalf("claim after resume won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RequestCommand(Command{Kind: CommandReprioritize, Target: "work", Instruction: "first"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reprioritize running error = %v", err)
	}
}

// The user's words are as durable as any other command: request, resolution,
// and the verbatim instruction all survive a replay from the journal alone.
func TestRedirectCommandRoundTripsThroughRebuild(t *testing.T) {
	graph := openThreadStore(t)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "api", Brief: "write a v1 client", Stage: 1}}},
		Provenance{Origin: OriginUser, SessionID: "redirect", Intent: "write a v1 client"}); err != nil {
		t.Fatal(err)
	}
	command, err := graph.RequestCommand(Command{
		SessionID: "redirect", Kind: CommandRedirect, Target: "api",
		Instruction: "no, use the v2 API not v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.ResolveCommand(command.Seq, CommandApplied, "redirected api: 1 added"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, found, err := graph.CommandBySeq(command.Seq)
	if err != nil || !found || rebuilt.Kind != CommandRedirect || rebuilt.Target != "api" ||
		rebuilt.Status != CommandApplied || rebuilt.Instruction != "no, use the v2 API not v1" ||
		rebuilt.Result != "redirected api: 1 added" {
		t.Fatalf("rebuilt redirect command = %+v found=%t err=%v", rebuilt, found, err)
	}
	if err := graph.CancelPending("api", "done here"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RequestCommand(Command{
		SessionID: "redirect", Kind: CommandRedirect, Target: "api", Instruction: "too late",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("redirect onto settled work error = %v", err)
	}
}

// Impatience is journaled as its own verb, so replay stays a switch on the
// kind and a rebuilt graph still knows the user asked for this one sooner.
func TestExpediteCommandRoundTripsThroughRebuild(t *testing.T) {
	graph := openThreadStore(t)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "finance", Brief: "research the finance question", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "hurry", Intent: "research the finance question"}); err != nil {
		t.Fatal(err)
	}
	instruction := "please complete the dinance research fast and give me result immediatly"
	command, err := graph.RequestCommand(Command{
		SessionID: "hurry", Kind: CommandExpedite, Target: "finance", Instruction: instruction,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.ResolveCommand(command.Seq, CommandApplied, "expedited finance: moved=true"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, found, err := graph.CommandBySeq(command.Seq)
	if err != nil || !found || rebuilt.Kind != CommandExpedite || rebuilt.Target != "finance" ||
		rebuilt.Status != CommandApplied || rebuilt.Instruction != instruction ||
		rebuilt.Result != "expedited finance: moved=true" {
		t.Fatalf("rebuilt expedite command = %+v found=%t err=%v", rebuilt, found, err)
	}
	if err := graph.CancelPending("finance", "done here"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RequestCommand(Command{
		SessionID: "hurry", Kind: CommandExpedite, Target: "finance", Instruction: "too late",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expedite onto settled work error = %v", err)
	}
}

func nodeListed(nodes []Node, id string) bool { return indexOfNode(nodes, id) >= 0 }

func indexOfNode(nodes []Node, id string) int {
	for index, node := range nodes {
		if node.ID == id {
			return index
		}
	}
	return -1
}

func eventKindListed(events []Event, kind EventKind) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}
