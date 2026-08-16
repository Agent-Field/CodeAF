package head

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// §13.4 and what actually answers it.
//
// Twelve leaves spent twelve minutes and twelve dollars' worth of tokens
// verifying twelve technical profiles against live documentation. The head then
// wrote the person two messages, both opening "Here are the 12 technical
// profiles:", neither of which was the researched text and neither of which
// agreed with the other: one said Chroma keeps SQLite-backed metadata, the
// other that it stores collections as Parquet files, and the verified result
// said something different again. Two generations disagreeing on fact is proof
// they were generated.
//
// For a while the answer to that was to relay: the head wrote one sentence and
// the code pasted the whole deliverable underneath it. That made generation
// impossible and made the person's answer a machine's raw output. The answer
// contract (chat-simplify §2.6) fixes the actual cause instead — the turn was
// shown a QUARTER of the deliverable and asked to speak about the whole — so
// what this pins now is that the turn is given all of it.
func TestTheDeliveryTurnIsGivenTheWholeDeliverableAndNotAClipOfIt(t *testing.T) {
	graph := openHeadStore(t)
	ask := postUser(t, graph, "room", "profile Milvus, Qdrant and Chroma for me")
	// Four times the old four-kilobyte clip, with the finding that decides the
	// answer sitting past where that clip ended.
	result := strings.Join([]string{
		"**Milvus** — a shared-storage architecture with fully disaggregated compute. " + strings.Repeat("Detail. ", 300),
		"**Qdrant** — a Rust engine with payload-aware HNSW filtering. " + strings.Repeat("Detail. ", 300),
		"**Chroma** — an embedded store that keeps collections on local disk. " + strings.Repeat("Detail. ", 300),
		"VERDICT: Qdrant for this workload.",
	}, "\n\n")
	delivered := settledJob(t, graph, "task-41", "Vector database profiles",
		"profile Milvus, Qdrant and Chroma for me", result)

	client := &fakeClient{model: "test/model", responses: []string{
		"Qdrant, for the filtering you described — the three profiles are in the card above.",
	}}
	head := New(client, graph)
	cursors := newSessionCursors(ask.Seq)
	cursors.mark("room", ask.Seq)
	if err := head.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}

	given := client.userPrompt()
	if !strings.Contains(given, "VERDICT: Qdrant for this workload.") {
		t.Fatalf("the delivery turn was shown a clip of the deliverable, not the whole of it: %d bytes given, %d bytes delivered",
			len(given), len(result))
	}
	if len(given) < len(result) {
		t.Fatalf("the prompt (%d bytes) is smaller than the result (%d bytes) it is supposed to carry",
			len(given), len(result))
	}

	// And what lands in the room is the head's composed answer — not the raw
	// deliverable a second time. The delivery row itself is the record.
	reply := waitForAgentReply(t, graph, "room", delivered.Seq)
	if !strings.HasPrefix(reply.Body, "Qdrant, for the filtering") {
		t.Fatalf("the head's answer is not what was posted: %.200q", reply.Body)
	}
	if strings.Contains(reply.Body, "shared-storage architecture") {
		t.Fatalf("the answer re-posted the deliverable that is already journaled: %d bytes", len(reply.Body))
	}
}

// The delivery turn is armed with the READS and nothing else. It is woken by an
// outcome, and a turn woken by an outcome must not be able to start another one
// — so the definitions it is offered carry no acts, and a call to one anyway is
// refused rather than dispatched.
func TestTheDeliveryTurnCarriesReadsAndNoHands(t *testing.T) {
	for _, definition := range beltReadDefinitions() {
		if !beltReadOnly(definition.Function.Name) {
			t.Fatalf("the delivery turn was offered %q, which changes something", definition.Function.Name)
		}
	}
	names := map[string]bool{}
	for _, definition := range beltReadDefinitions() {
		names[definition.Function.Name] = true
	}
	for _, read := range []string{beltToolBoard, beltToolResult, beltToolPlan, beltToolRead} {
		if !names[read] {
			t.Fatalf("the delivery turn cannot %s, so it cannot go and get what it is asked about", read)
		}
	}
	if names[beltToolTask] || names[beltToolChange] || names[beltToolStop] || names[beltToolWrite] {
		t.Fatal("the delivery turn was handed a hand")
	}
}

// It reads before it answers, and the reading is bounded. A turn that opened the
// file where the substance actually lived is the whole point of arming it; a
// turn that browses forever is a poll that stopped answering the person.
func TestTheDeliveryTurnReadsTheFileTheResultOnlyPointsAt(t *testing.T) {
	graph := openHeadStore(t)
	ask := postUser(t, graph, "room", "how fast did each region grow?")
	path := filepath.Join(t.TempDir(), "regions.md")
	if err := os.WriteFile(path, []byte("North 14%, South 9%, East 3%\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	delivered := settledJob(t, graph, "task-77", "Regional growth read",
		"how fast did each region grow?",
		"Numbers are in regions.md — see the file for the per-region figures.\nFiles:\n"+path)

	client := &toolingClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("r1", beltToolRead, map[string]any{
			"job": "task-77", "file": "regions.md"})}},
		// A hand, reached for out of habit. It must be refused rather than run.
		{calls: []ai.ToolCall{beltCall("s1", beltToolTask, map[string]any{
			"instruction": "go and check the numbers again"})}},
		{text: "North 14%, South 9%, East 3% — the full table is in regions.md."},
	}}
	head := New(client, graph)
	cursors := newSessionCursors(ask.Seq)
	cursors.mark("room", ask.Seq)
	if err := head.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}

	if !client.calledRead {
		t.Fatal("the delivery turn never opened the file the result pointed at")
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a turn woken by finished work started something: %+v", commands)
	}
	if !client.toldHandless {
		t.Fatal("the act was dispatched instead of being refused in the loop")
	}
	reply := waitForAgentReply(t, graph, "room", delivered.Seq)
	if !strings.Contains(reply.Body, "North 14%") {
		t.Fatalf("the composed answer never reached the room: %q", reply.Body)
	}
}

// toolingClient scripts a tool-calling turn and watches what came back for the
// refusals the loop is supposed to produce itself.
type toolingClient struct {
	mutex        sync.Mutex
	turns        []beltTurn
	calledRead   bool
	toldHandless bool
}

func (client *toolingClient) CompleteWithMessages(_ context.Context, messages []ai.Message,
	_ ...ai.Option) (*ai.Response, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	for _, message := range messages {
		for _, part := range message.Content {
			if strings.Contains(part.Text, absorbHandless) {
				client.toldHandless = true
			}
			if strings.Contains(part.Text, "North 14%, South 9%, East 3%") &&
				message.Role == "tool" {
				client.calledRead = true
			}
		}
	}
	if len(client.turns) == 0 {
		return nil, errors.New("no scripted turn left")
	}
	turn := client.turns[0]
	client.turns = client.turns[1:]
	response := textResponse(turn.text)
	response.Choices[0].Message.ToolCalls = turn.calls
	return response, nil
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
