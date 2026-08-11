package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// 8.2.12's whole claim is that this is a spawn VARIANT, so the two things to
// prove are that it carries the conversation and that it is still spawn: one
// ordinary splice through the one funnel, with the person's words leading.
func TestForkCommissionsWorkCarryingTheConversation(t *testing.T) {
	graph := openHeadStore(t)
	postUser(t, graph, "design", "the export needs to keep the column order from the source")
	if _, err := graph.PostMessage(store.Message{
		SessionID: "design", Role: store.RoleAgent,
		Body: "Understood — source order, and the header row stays.",
	}); err != nil {
		t.Fatal(err)
	}

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("f1", beltToolFork, map[string]any{
			"instruction": "go and build the exporter"})}},
		{text: "Building it now."},
	}}
	user := postUser(t, graph, "design", "okay, go and do it")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 {
		t.Fatalf("fork journaled %+v err=%v, want one splice", commands, err)
	}
	command := commands[0]
	if command.Kind != store.CommandSplice {
		t.Fatalf("fork journaled a %q, want an ordinary splice", command.Kind)
	}
	// The ask leads, because everything downstream names the work off the first
	// line: a job whose row reads as the middle of somebody's chat is a job
	// nobody can find again.
	if !strings.HasPrefix(command.Instruction, "go and build the exporter") {
		t.Fatalf("the fork's brief does not open with the ask: %q", command.Instruction)
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
		t.Fatal("a fork rode in as a reflex")
	}
}

// The guards are spawn's, and the proof they still apply is that they still
// fire: a fork whose brief crosses the consequence gate is journaled as
// ordinary work the person sees coming, with their sentence intact.
func TestForkRidesSpawnsGuardsRatherThanRestatingThem(t *testing.T) {
	graph := openHeadStore(t)
	postUser(t, graph, "spend", "the vendor invoice is the one from March")
	head := New(nil, graph)
	user := postUser(t, graph, "spend", "go ahead and pay it")
	run := &beltRun{head: head, user: user}

	result, failed := run.execute(beltToolFork, `{"instruction":"pay the vendor invoice","reflex":true}`)
	if failed {
		t.Fatalf("fork refused honest work: %s", result)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if commands[0].Reflex {
		t.Fatal("money words rode in on a reflex — the consequence gate did not reach the fork")
	}
	// And the receipt is the one spawn writes, from what was journaled.
	if len(run.did) != 1 || !strings.HasPrefix(run.did[0], "Queued: pay the vendor invoice") {
		t.Fatalf("the fork's receipt is not spawn's own: %+v", run.did)
	}
}

// A room with nothing in it has nothing to inherit, and saying so is better
// than commissioning a job whose context block is empty scaffolding.
func TestForkRefusesAnEmptyConversation(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph)
	run := &beltRun{head: head, user: store.Message{SessionID: "fresh", Role: store.RoleUser}}

	result, failed := run.execute(beltToolFork, `{"instruction":"go do the thing"}`)
	if !failed || !strings.Contains(result, "nothing discussed in this conversation yet") {
		t.Fatalf("an empty room was forked: %q failed=%t", result, failed)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a refused fork journaled work: %+v", commands)
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
		t.Fatalf("the fork inherited %d turns, over its cap of %d", turns, forkContextTurns)
	}
}
