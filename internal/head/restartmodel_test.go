package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// "rerun that with the better model" matched the restart cue, was handled
// deterministically, and re-ran the failure on the default slot — the model
// words were never parsed on any path but a fresh compile. Now the restart
// carries the reading, in the payload the store already has room for.
func TestRestartCarriesTheModelWordsItWasGiven(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "market-scan", "Market scan", "scan the market")
	failNode(t, graph, "market-scan")

	user := postUser(t, graph, "escalate", "rerun the market scan with the better model")
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
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
	reply := waitForAgentReply(t, graph, "escalate", user.Seq)
	if !strings.Contains(reply.Body, "stronger model") {
		t.Fatalf("the receipt does not say which model it asked for: %q", reply.Body)
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
