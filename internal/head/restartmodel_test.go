package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// "rerun that with the better model" matched the restart cue, was handled
// deterministically, and re-ran the failure on the default slot — the model
// words were never parsed on any path but a fresh compile. Now the restart
// carries the reading, in the payload the store already has room for.
//
// The reading is read at the journaling door rather than in a recognizer, which
// is why it survived the recognizers being demoted: every route to a restart —
// the change tool's words, a set, a confirmed question replayed later — goes
// through the same funnel and picks up the same mark.
func TestRestartCarriesTheModelWordsItWasGiven(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "market-scan", "Market scan", "scan the market")
	failNode(t, graph, "market-scan")

	user := postUser(t, graph, "escalate", "rerun the market scan with the better model")
	head, _ := beltHead(graph, beltTurn{calls: []ai.ToolCall{
		beltCall("c1", beltToolChange, map[string]any{
			"target": "market-scan", "words": user.Body})}}, beltTurn{})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 || commands[0].Kind != store.CommandRestart || commands[0].Target != "market-scan" {
		t.Fatalf("commands = %+v, want one restart of market-scan", commands)
	}
	words, wanted := RestartModel(commands[0].Instruction)
	if !wanted || !words.Boost {
		t.Fatalf("the restart lost the model words:\n%s", commands[0].Instruction)
	}
	// The user's own sentence is still the whole of the instruction's front.
	if !strings.HasPrefix(commands[0].Instruction, "rerun the market scan with the better model") {
		t.Fatalf("the restart instruction is no longer the user's words:\n%s", commands[0].Instruction)
	}
	// And the receipt vocabulary still names the model, because it is read off
	// the journaled instruction: a wrong reading costs one word to fix rather
	// than a whole re-run on the slot the user was trying to leave.
	if !strings.Contains(restartModelReceipt(commands[0].Instruction), "stronger model") {
		t.Fatalf("the receipt cannot say which model it asked for: %q",
			restartModelReceipt(commands[0].Instruction))
	}
	if reply := waitForAgentReply(t, graph, "escalate", user.Seq); reply.CommandSeq != commands[0].Seq {
		t.Fatalf("the reply is not the receipt for the restart it journaled: %+v", reply)
	}
}

// A named model rides the same seam, and an ordinary restart is unchanged to
// the byte — the mark only appears when the sentence asked for a model.
func TestRestartModelMarkIsNamedAndSilentByTurns(t *testing.T) {
	plain := "restart the market scan"
	if got := MarkRestartModel(plain); got != plain {
		t.Fatalf("an ordinary restart gained a model mark: %q", got)
	}
	if _, wanted := RestartModel(plain); wanted {
		t.Fatal("an unmarked restart reads as carrying a model")
	}
	named := MarkRestartModel("rerun it with the opus model")
	words, wanted := RestartModel(named)
	if !wanted || words.Boost || len(words.Names) != 1 || words.Names[0] != "opus" {
		t.Fatalf("a named model did not survive the mark: %+v from\n%s", words, named)
	}
	// The mark is written once. A gated restart is journaled from the same
	// instruction the confirmation question carried.
	if twice := MarkRestartModel(named); twice != named {
		t.Fatalf("marking a marked instruction wrote a second mark:\n%s", twice)
	}
}
