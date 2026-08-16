package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// settledJob puts one finished piece of the person's own work on the board and
// posts the delivery row the resident posts for it.
func settledJob(t *testing.T, graph *store.Store, id, title, intent, result string) store.Message {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: id, Title: title, Brief: intent, Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "room", Intent: intent}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim(id, "worker")
	if err != nil || !won {
		t.Fatalf("claim %s: won=%v err=%v", id, won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, result); err != nil {
		t.Fatal(err)
	}
	delivered, err := graph.PostMessage(store.Message{
		SessionID: "room", Role: store.RoleSystem, NodeID: id, Body: result,
	})
	if err != nil {
		t.Fatal(err)
	}
	return delivered
}

// The turn the person's own sketch asked for: when the work they commissioned
// lands, the head speaks the result back against the ask that started it —
// their past words, their request, the job, and what it delivered, all in one
// context.
func TestASettledJobIsSpokenBackAgainstTheAskThatStartedIt(t *testing.T) {
	graph := openHeadStore(t)
	ask := postUser(t, graph, "room", "which of the three regions grew fastest last quarter?")
	delivered := settledJob(t, graph, "task-9", "Regional growth read",
		"which of the three regions grew fastest last quarter?",
		"North grew 14%, South 9%, East 3%. Written to regions.md.")

	client := &fakeClient{model: "test/model", responses: []string{
		"North grew fastest at 14%, roughly half again what South managed; the full read is in regions.md.",
	}}
	head := New(client, graph)

	cursors := newSessionCursors(ask.Seq)
	cursors.mark("room", ask.Seq)
	if err := head.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}

	if calls := client.callCount(); calls != 1 {
		t.Fatalf("the delivery cost %d turns, want exactly one", calls)
	}
	if prompt := client.systemPrompt(); prompt != absorbPrompt {
		t.Fatalf("the absorption turn ran on another prompt: %q", firstLine(prompt))
	}
	said := client.userPrompt()
	for name, want := range map[string]string{
		"their original ask": "which of the three regions grew fastest last quarter?",
		"what came back":     "North grew 14%, South 9%, East 3%",
		"the job's own name": "Regional growth read",
	} {
		if !strings.Contains(said, want) {
			t.Fatalf("the turn was not given %s: %q", name, said)
		}
	}

	reply := waitForAgentReply(t, graph, "room", delivered.Seq)
	if !strings.Contains(reply.Body, "North grew fastest") {
		t.Fatalf("the head did not speak the result back: %q", reply.Body)
	}
	// Unannotated like any ordinary reply: a model chip in the thread is for a
	// model the person named, not for every line the head speaks.
	if reply.Model != "" {
		t.Fatalf("the absorption line wore a model chip: %+v", reply)
	}

	// And it fires once. A second poll over the same journal has nothing left to
	// absorb, because the row was walked past.
	if err := head.poll(context.Background(), cursors); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("the same delivery was absorbed %d times", calls)
	}
}

// Work nobody asked for buys no sentence. The resident practising on its own
// time settles the same way a commissioned job does, and paying a turn to
// narrate it speaks to a room where nobody is waiting.
func TestSelfDirectedWorkIsNotSpokenBack(t *testing.T) {
	graph := openHeadStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "practice-1", Title: "How flaky is the suite", Brief: "run it ten times",
			Group: store.PracticeGroup, Stage: 1},
	}}, store.Provenance{Origin: store.OriginSelf, SessionID: "room",
		Intent: "find out how flaky the suite is"}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("practice-1", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%v err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "two failures in ten"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "room", Role: store.RoleSystem, NodeID: "practice-1", Body: "two failures in ten",
	}); err != nil {
		t.Fatal(err)
	}

	client := &fakeClient{responses: []string{"should never be asked for"}}
	if err := New(client, graph).poll(context.Background(), newSessionCursors(0)); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if calls := client.callCount(); calls != 0 {
		t.Fatalf("self-directed work bought %d paid turns", calls)
	}
}
