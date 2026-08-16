package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE THREE-CLASS LAW, held over a whole cycle (13.18).
//
// A thread message is a COMMITMENT, a DELIVERY or a QUESTION. Nothing else may
// be in a conversation: not a compile phase, not a stage advance, not the
// receipt for a change the head already said out loud. Those are the work
// RECORD's, and the record is the room, read by node.
//
// The measurement this exists to make impossible: thirty of the forty-three
// messages under one real job were `setting working standards · N of 4`, and
// five consecutive copies were the first screen of a fresh task room (13.17
// item 2, H13). Nothing filtered them, because nothing had ever said which
// sentences a thread is FOR.
//
// So the assertion is the whole thread, row by row, with the class each row
// belongs to named. A producer that grows a fourth kind of sentence fails here
// with the sentence printed, which is the only failure mode worth having.
func TestAWholeCycleSaysOnlyCommitmentDeliveryAndQuestion(t *testing.T) {
	graph := openHeadStore(t)

	// 1. The person asks, and the head commissions. Its reply is the COMMITMENT
	//    — the one sentence a person hears when prose becomes work.
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("s1", beltToolTask, map[string]any{
			"instruction": "compare the three cities"})}},
		{text: "On it — comparing the three cities now."},
	}}
	user, err := graph.PostMessage(store.Message{
		SessionID: "cycle", Role: store.RoleUser, Body: "compare the three cities",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	// 2. The compile lands and the splice is applied. Its receipt is the second
	//    half of the same COMMITMENT and stays in the thread on purpose: it
	//    carries the reading and the assumptions the head's sentence cannot, and
	//    5.20.1 requires that a turn whose words fail still says what it
	//    commissioned. Two rows, one commitment, and neither is droppable.
	compile := func(_ context.Context, instruction, _ string) (resident.Compiled, error) {
		return resident.Compiled{Goal: instruction, Assumptions: []string{"today's weather, not the averages"}}, nil
	}
	planner := func(_ context.Context, compiled resident.Compiled) (store.Subtree, error) {
		return store.Subtree{Nodes: []store.NodeSpec{{ID: "task-1", Brief: compiled.Goal, Stage: 1}}}, nil
	}
	reconciler := resident.New(graph, compile, planner)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 3. Planning narrates itself. This is the shape cmd/aforge's plan-progress
	//    poster writes — that package cannot be imported from here, and its own
	//    test pins the poster; what is pinned HERE is that the shape lands on
	//    the record and never in the room's conversation.
	for phase := 1; phase <= 5; phase++ {
		if _, err := thread.Record(graph, store.Message{
			Role: store.RoleSystem, NodeID: "task-1",
			Body: "setting working standards · " + string(rune('0'+phase)) + " of 5",
			Progress: &store.MessageProgress{
				Phase: "setting working standards", Done: phase, Total: 5,
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	// 4. The person changes the work mid-flight. The head says so in its own
	//    voice; the applied receipt is filed on the job, not said again.
	amend, err := graph.RequestCommand(store.Command{
		SessionID: "cycle", Kind: store.CommandAmend, Target: "task-1",
		Instruction: "include the wind too",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 5. The work runs, asks one thing, and lands. The QUESTION and the
	//    DELIVERY are the two rows a person is owed for all of it.
	asked, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: "cycle", Text: "Celsius or Fahrenheit?", Urgency: store.QuestionWhenever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(asked.Seq); err != nil {
		t.Fatal(err)
	}
	runner := resident.NewRunner(graph, func(context.Context, store.Node) (resident.ExecResult, error) {
		return resident.ExecResult{Summary: "London is warmest."}, nil
	}, "cycle-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	// THE THREAD. Every row, in order, and the class it belongs to.
	type row struct {
		class string
		role  store.Role
		body  string
	}
	want := []row{
		{class: "the person", role: store.RoleUser, body: "compare the three cities"},
		{class: "COMMITMENT", role: store.RoleAgent, body: "On it — comparing the three cities now."},
		{class: "COMMITMENT", role: store.RoleSystem, body: "Here's my reading: compare the three cities"},
		{class: "QUESTION", role: store.RoleAgent, body: "Celsius or Fahrenheit?"},
		{class: "DELIVERY", role: store.RoleSystem, body: "London is warmest."},
	}
	spoken, err := graph.Messages("cycle", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(spoken) != len(want) {
		for _, message := range spoken {
			t.Logf("thread: role=%s node=%q command=%d question=%d body=%q",
				message.Role, message.NodeID, message.CommandSeq, message.QuestionSeq, message.Body)
		}
		t.Fatalf("thread rows = %d, want %d — a fourth class of sentence got in", len(spoken), len(want))
	}
	for index, expected := range want {
		got := spoken[index]
		if got.Role != expected.role || !strings.Contains(got.Body, expected.body) {
			t.Fatalf("thread row %d (%s) = role %s body %q", index, expected.class, got.Role, got.Body)
		}
		if got.Progress != nil {
			t.Fatalf("thread row %d (%s) carries structured progress: %+v", index, expected.class, got.Progress)
		}
	}

	// NOTHING WAS LOST. Everything the thread stopped carrying is on the job's
	// own record, in order, where a reader who opens the room finds it.
	filed, err := graph.NodeMessages("task-1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	phases, receipts := 0, 0
	for _, message := range filed {
		switch {
		case message.Progress != nil:
			phases++
			if message.SessionID != "" {
				t.Fatalf("a compile phase kept its session: %+v", message)
			}
		case message.CommandSeq == amend.Seq:
			receipts++
			if message.SessionID != "" {
				t.Fatalf("an applied surgery receipt kept its session: %+v", message)
			}
		}
	}
	if phases != 5 {
		t.Fatalf("compile phases on the record = %d, want all five", phases)
	}
	if receipts != 1 {
		t.Fatalf("applied amendment receipts on the record = %d, want one", receipts)
	}
}
