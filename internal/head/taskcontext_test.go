package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Carrying the conversation was never a mechanism, only a thing that travels
// beside one — which is why it is an argument now (chat-simplify §2.3) rather
// than a second commissioning door with its own copy of every guard. The two
// things to prove are unchanged: it carries the conversation, and it is still
// one ordinary splice through the one funnel with the person's words leading.
func TestTaskWithContextCommissionsWorkCarryingTheConversation(t *testing.T) {
	graph := openHeadStore(t)
	postUser(t, graph, "design", "the export needs to keep the column order from the source")
	if _, err := graph.PostMessage(store.Message{
		SessionID: "design", Role: store.RoleAgent,
		Body: "Understood — source order, and the header row stays.",
	}); err != nil {
		t.Fatal(err)
	}

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("f1", beltToolTask, map[string]any{
			"instruction": "go and build the exporter", "context": true})}},
		{text: "Building it now."},
	}}
	user := postUser(t, graph, "design", "okay, go and do it")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 {
		t.Fatalf("the task journaled %+v err=%v, want one splice", commands, err)
	}
	command := commands[0]
	if command.Kind != store.CommandSplice {
		t.Fatalf("the task journaled a %q, want an ordinary splice", command.Kind)
	}
	// The ask leads, because everything downstream names the work off the first
	// line: a job whose row reads as the middle of somebody's chat is a job
	// nobody can find again.
	if !strings.HasPrefix(command.Instruction, "go and build the exporter") {
		t.Fatalf("the brief does not open with the ask: %q", command.Instruction)
	}
	if !strings.Contains(command.Instruction, ForkedContextPrefix) {
		t.Fatalf("the conversation was not fenced as context: %q", command.Instruction)
	}
	for _, want := range []string{
		"them: the export needs to keep the column order",
		"you, earlier: Understood — source order",
		// The message being answered right now is part of what was discussed,
		// and the window it comes from reads everything BEFORE it.
		"them: okay, go and do it",
	} {
		if !strings.Contains(command.Instruction, want) {
			t.Fatalf("the inherited conversation is missing %q:\n%s", want, command.Instruction)
		}
	}
	// It is a reflex under no circumstances: work that needed a conversation to
	// specify is not a reversible seconds-scale action.
	if command.Reflex {
		t.Fatal("work that needed a conversation to specify rode in as a reflex")
	}
}

// There is one set of commissioning guards now because there is one
// commissioning tool, and the proof they apply to an inherited brief is that
// they still fire on it: a brief that crosses the consequence gate is journaled
// as ordinary work the person sees coming, with their sentence intact.
func TestTaskWithContextRidesTheSameGuards(t *testing.T) {
	graph := openHeadStore(t)
	postUser(t, graph, "spend", "the vendor invoice is the one from March")
	head := New(nil, graph)
	user := postUser(t, graph, "spend", "go ahead and pay it")
	run := &beltRun{head: head, user: user}

	result, failed := run.execute(beltToolTask,
		`{"instruction":"pay the vendor invoice","context":true,"reflex":true}`)
	if failed {
		t.Fatalf("task refused honest work: %s", result)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if commands[0].Reflex {
		t.Fatal("money words rode in on a reflex — the consequence gate did not reach the brief")
	}
	// And the receipt is written from what was journaled, as it is for every task.
	if len(run.did) != 1 || !strings.HasPrefix(run.did[0], "Queued: pay the vendor invoice") {
		t.Fatalf("the receipt is not assembled from the journaled work: %+v", run.did)
	}
}

// A room with nothing in it has nothing to inherit, and saying so is better
// than commissioning a job whose context block is empty scaffolding.
func TestTaskWithContextRefusesAnEmptyConversation(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph)
	run := &beltRun{head: head, user: store.Message{SessionID: "fresh", Role: store.RoleUser}}

	result, failed := run.execute(beltToolTask, `{"instruction":"go do the thing","context":true}`)
	if !failed || !strings.Contains(result, "nothing discussed in this conversation yet") {
		t.Fatalf("an empty room was inherited: %q failed=%t", result, failed)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a refused task journaled work: %+v", commands)
	}
}

// The inherited block is bounded, because an instruction is a brief and a brief
// that is mostly transcript has stopped being one.
func TestTheInheritedConversationIsBounded(t *testing.T) {
	graph := openHeadStore(t)
	for index := 0; index < forkContextTurns*3; index++ {
		postUser(t, graph, "long", strings.Repeat("word ", 400))
	}
	head := New(nil, graph)
	user := postUser(t, graph, "long", "go")

	context, err := head.forkContext(user)
	if err != nil {
		t.Fatal(err)
	}
	if len(context) > forkContextBytes+forkTurnBytes+64 {
		t.Fatalf("the inherited block is %d bytes, over its %d ceiling", len(context), forkContextBytes)
	}
	if turns := strings.Count(context, "them: "); turns > forkContextTurns+1 {
		t.Fatalf("the brief inherited %d turns, over its cap of %d", turns, forkContextTurns)
	}
}
