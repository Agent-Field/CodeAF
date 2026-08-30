package resident

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// assertRecordOnly is the noise law as one assertion: the line is on the work's
// record, where a reader who opens that work finds it, and nowhere in the
// conversation.
func assertRecordOnly(t *testing.T, graph *store.Store, sessionID, nodeID, phrase string) {
	t.Helper()
	recorded, err := graph.NodeMessages(nodeID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range recorded {
		if strings.Contains(message.Body, phrase) {
			found = true
			if message.SessionID != "" {
				t.Fatalf("%q is on the record with a room attached: %+v", phrase, message)
			}
		}
	}
	if !found {
		t.Fatalf("%q was not written to %s's record at all", phrase, nodeID)
	}
	said, err := graph.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range said {
		if strings.Contains(message.Body, phrase) {
			t.Fatalf("%q reached the conversation: %q", phrase, message.Body)
		}
	}
}

func mustMessages(t *testing.T, graph *store.Store, sessionID string) []store.Message {
	t.Helper()
	messages, err := graph.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

// recordRunNote writes one of the surface's own run notes the way the surface
// writes it: to the work's record, with no room attached.
func recordRunNote(t *testing.T, graph *store.Store, nodeID, body string) {
	t.Helper()
	if _, err := thread.Record(graph, store.Message{
		Role: store.RoleSystem, NodeID: nodeID, Body: body,
	}); err != nil {
		t.Fatal(err)
	}
}

// A steer is delivered to the workers, not said again to the person who just
// said it. The mailbox is read off the node, so the copy needs no room — and
// with one attached, a person steering a four-leaf job watched their own
// sentence appear four more times under their own name.
func TestSteeringRunningWorkNeverEchoesIntoTheConversation(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "ship the thing", Stage: 2},
		{ID: "job-n1", Parent: "job", Brief: "first part", Stage: 1},
		{ID: "job-n2", Parent: "job", Brief: "second part", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "room", Intent: "ship the thing"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"job-n1", "job-n2"} {
		if _, won, err := graph.Claim(id, "worker"); err != nil || !won {
			t.Fatalf("claim %s: won=%v err=%v", id, won, err)
		}
	}

	informed, err := BroadcastRedirection(graph, "job", "room", "keep the tone plain")
	if err != nil || informed != 2 {
		t.Fatalf("broadcast informed %d err=%v", informed, err)
	}
	for _, id := range []string{"job-n1", "job-n2"} {
		assertRecordOnly(t, graph, "room", id, "keep the tone plain")
	}
	// And the mailbox still delivers: the executor's steering poll reads a
	// node's own messages, which is the read that made the room unnecessary.
	mailbox, err := graph.NodeMessages("job-n1", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(mailbox) != 1 || mailbox[0].Role != store.RoleUser ||
		!strings.HasPrefix(mailbox[0].Body, redirectSteerPrefix) {
		t.Fatalf("the steer is no longer readable as steering: %+v", mailbox)
	}
}

// The whole law in one settled task: parts finish, a part fails, the machinery
// narrates itself all the way through, and what the conversation is left
// holding is the task's own delivery and nothing else. Asserted after a rebuild
// too, because the journal is the truth and a view that agreed only until the
// next replay would be no law at all.
func TestASettledMultiPartTaskLeavesOnlyItsDeliveryInTheConversation(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "ship the thing", Stage: 2},
		{ID: "job-n1", Parent: "job", Brief: "first part", Stage: 1},
		{ID: "job-n2", Parent: "job", Brief: "second part", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "room", Intent: "ship the thing"}); err != nil {
		t.Fatal(err)
	}
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Everything the machinery says about itself while the parts run.
	first, _, err := graph.Node("job-n1")
	if err != nil {
		t.Fatal(err)
	}
	postGovernorNotice(graph, first, CauseRounds, RefusedRounds)
	// The two the surface writes on the same node, through the same door: the
	// ruler's verdict about itself, and the arithmetic of a split.
	recordRunNote(t, graph, "job-n1", "ruler: median 19 turns, 4 of 18 overran — the ruler holds")
	recordRunNote(t, graph, "job-n1", OverrunContinuationMessage(1))

	// A part lands, and an intermediate landing is progress rather than news.
	claim, won, err := graph.Claim("job-n1", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%v err=%v", won, err)
	}
	if err := graph.Complete(claim, "the first part is done"); err != nil {
		t.Fatal(err)
	}
	claim, won, err = graph.Claim("job-n2", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%v err=%v", won, err)
	}
	if err := graph.Complete(claim, "the second part is done"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	// The task itself settles, and that is the sentence the person is owed.
	claim, won, err = graph.Claim("job", "worker")
	if err != nil || !won {
		t.Fatalf("claim job: won=%v err=%v", won, err)
	}
	if err := graph.Complete(claim, "Shipped — the notes are in /tmp/notes.md"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	assertConversation := func(where string) {
		t.Helper()
		messages := mustMessages(t, graph, "room")
		bodies := make([]string, 0, len(messages))
		for _, message := range messages {
			bodies = append(bodies, message.Body)
		}
		if len(bodies) != 1 || !strings.Contains(bodies[0], "Shipped") {
			t.Fatalf("%s: the conversation holds %d lines, want the delivery alone: %q", where, len(bodies), bodies)
		}
	}
	assertConversation("as journaled")
	assertRecordOnly(t, graph, "room", "job-n1", "split as many times")
	assertRecordOnly(t, graph, "room", "job-n1", "the ruler holds")
	assertRecordOnly(t, graph, "room", "job-n1", "pieces queued")
	// An intermediate part's own landing is not announced anywhere: the graph
	// already shows it live, and its summary flows into the delivery above.
	for _, message := range mustMessages(t, graph, "room") {
		if strings.Contains(message.Body, "the second part is done") {
			t.Fatalf("an intermediate landing was spoken: %q", message.Body)
		}
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assertConversation("after a rebuild")
	assertRecordOnly(t, graph, "room", "job-n1", "split as many times")
}
