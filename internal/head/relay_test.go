package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// §13.4, pinned.
//
// Twelve leaves spent twelve minutes and twelve dollars' worth of tokens
// verifying twelve technical profiles against live documentation. The head then
// wrote the person two messages, both opening "Here are the 12 technical
// profiles:", neither of which was the researched text and neither of which
// agreed with the other: one said Chroma keeps SQLite-backed metadata, the
// other that it stores collections as Parquet files, and the verified result
// said something different again. Two generations disagreeing on fact is proof
// they were generated; nothing relayed can disagree with itself.
//
// The closure contract says the head owns the discourse — one mouth — and this
// is what that costs it: when a job delivers, the delivered text IS the answer.
// The head may put a sentence in front of it and may not rewrite its substance.
func TestASettledJobsAnswerCarriesTheDeliverableItself(t *testing.T) {
	graph := openHeadStore(t)
	ask := postUser(t, graph, "room", "profile Milvus, Qdrant and Chroma for me")
	// Long enough that the old path could only ever have shown the head its
	// opening, which is exactly how a paraphrase became the rest.
	result := strings.Join([]string{
		"**Milvus** — a shared-storage architecture with fully disaggregated compute. " + strings.Repeat("Detail. ", 300),
		"**Qdrant** — a Rust engine with payload-aware HNSW filtering. " + strings.Repeat("Detail. ", 300),
		"**Chroma** — an embedded store that keeps collections on local disk. " + strings.Repeat("Detail. ", 300),
	}, "\n\n")
	delivered := settledJob(t, graph, "task-41", "Vector database profiles",
		"profile Milvus, Qdrant and Chroma for me", result)

	client := &fakeClient{model: "test/model", responses: []string{
		"Here are the three, in the order you named them.",
	}}
	head := New(client, graph)
	cursors := newSessionCursors(ask.Seq)
	cursors.mark("room", ask.Seq)
	if err := head.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}

	reply := waitForAgentReply(t, graph, "room", delivered.Seq)
	settled, found, err := graph.Node("task-41")
	if err != nil || !found {
		t.Fatalf("node: found=%t err=%v", found, err)
	}
	if !strings.Contains(reply.Body, strings.TrimSpace(settled.Summary)) {
		t.Fatalf("the head's answer does not carry the deliverable — it is a paraphrase of it.\n"+
			"answer (%d bytes): %.400q\ndeliverable (%d bytes): %.400q",
			len(reply.Body), reply.Body, len(settled.Summary), settled.Summary)
	}
	if !strings.HasPrefix(reply.Body, "Here are the three, in the order you named them.") {
		t.Fatalf("the head's own framing was dropped: %.200q", reply.Body)
	}
}

// The frame is optional and the deliverable is not. A provider that refused, or
// a turn with nothing worth adding, costs the person a sentence of context; it
// may never cost them the answer, because relaying is the whole job.
func TestTheDeliverableStillReachesThemWhenTheHeadHasNothingToAdd(t *testing.T) {
	if body := RelayDelivery("", "the finished answer"); body != "the finished answer" {
		t.Fatalf("an unframed delivery came out as %q", body)
	}
	if body := RelayDelivery("here it is", ""); body != "here it is" {
		t.Fatalf("a frame with nothing to relay came out as %q", body)
	}
	// Where both will not fit one message, the frame gives way rather than the
	// work: a missing courtesy costs nothing, a missing paragraph costs the ask.
	long := strings.Repeat("x", store.MaxMessageBytes-4)
	body := RelayDelivery("a sentence of context", long)
	if !strings.HasPrefix(body, "xxx") {
		t.Fatalf("the deliverable lost its place to the frame: %.80q", body)
	}
	if len(body) > store.MaxMessageBytes {
		t.Fatalf("the relay overran what a message may carry: %d bytes", len(body))
	}
}

// §13.6's duplicate, pinned. One settle, one answer.
//
// A job can put more than one row of its own into a room — the delivery, a rail
// note, a continuation line — and every one of them reads as a delivery by the
// three columns deliveredRow tests. Measured, that produced two absorption
// turns whose replies both carried answers_seq 0, so neither superseded the
// other and the person read the answer twice, in two versions that disagreed.
func TestOneAnswerPerSettleHoweverManyRowsTheJobPosts(t *testing.T) {
	graph := openHeadStore(t)
	ask := postUser(t, graph, "room", "profile the four reverse proxies")
	delivered := settledJob(t, graph, "task-519", "Reverse proxy profiles",
		"profile the four reverse proxies", "nginx, HAProxy, Envoy and Caddy, in that order.")
	// The second row: the same job, speaking again in the same room. It is a
	// delivery by every column the poll reads.
	if _, err := graph.PostMessage(store.Message{
		SessionID: "room", Role: store.RoleSystem, NodeID: "task-519",
		Body: "nginx, HAProxy, Envoy and Caddy, in that order.",
	}); err != nil {
		t.Fatal(err)
	}

	client := &fakeClient{model: "test/model", responses: []string{
		"Here are the four, in the order you named them.",
		"Here are the four technical profiles:",
	}}
	head := New(client, graph)
	cursors := newSessionCursors(ask.Seq)
	cursors.mark("room", ask.Seq)
	if err := head.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}

	if calls := client.callCount(); calls != 1 {
		t.Fatalf("one settled job bought %d absorption turns", calls)
	}
	waitForAgentReply(t, graph, "room", delivered.Seq)
	messages, err := graph.MessageTail("room", 50)
	if err != nil {
		t.Fatal(err)
	}
	answers := 0
	for _, message := range messages {
		if message.Role == store.RoleAgent && message.Seq > delivered.Seq {
			answers++
		}
	}
	if answers != 1 {
		t.Fatalf("one settle produced %d answers on screen", answers)
	}
}
