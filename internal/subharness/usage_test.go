package subharness

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// billed is the accounting every reply carries in these tests. The figures are
// small and distinct so that a sum can only come out right one way.
func billed() *ai.Usage {
	cost := 0.25
	return &ai.Usage{
		PromptTokens:             30,
		CompletionTokens:         10,
		CacheReadInputTokens:     5,
		CacheCreationInputTokens: 2,
		Cost:                     &cost,
	}
}

// TestModelExecBillsEveryCallARunMakes is the ledger's whole claim: what a run
// cost is the sum over every request it made, INCLUDING the intermediate rounds
// of a tool loop, and each of them exactly once.
//
// The count is asserted against the endpoint's own tally rather than a literal,
// because that is the property that matters: the bridge cannot bill for a call
// nobody made, and it cannot miss one. A loop's final call is the one at risk —
// it lands in the trace AND comes back as the response — so a run of two
// requests reading as three is the regression this pins.
func TestModelExecBillsEveryCallARunMakes(t *testing.T) {
	server := newModelServer(t, modelRule{
		when: "inspect it", say: "the suite is green", tool: "bash",
		args: `{"command":"go test ./..."}`, toolCalls: 1,
	})
	server.bill = billed()
	h := Harness{Id: Id{Name: "worker", Version: 1}, Whitelist: []string{"bash"}, Verify: Verify{Ladder: VerifyAccept}}
	var spent Usage
	exec := ModelExec(server.client(t), ModelExecOpts{Harness: h, Usage: &spent, Toolbelt: Toolbelt{
		Defs: []ai.ToolDefinition{testTool("bash")},
		Call: func(context.Context, string, map[string]any) (string, error) { return "PASS from the tool", nil },
	}})
	if _, err := exec(context.Background(), Node{Id: "work", Kind: KindAgentLoop, Fields: Fields{
		"brief": "inspect it", "tools": "bash",
	}}); err != nil {
		t.Fatalf("loop: %v", err)
	}

	calls := server.calls()
	if calls < 2 {
		t.Fatalf("the endpoint saw %d requests, want the tool round and the answer", calls)
	}
	if spent.Calls != calls {
		t.Fatalf("the ledger counted %d calls, the endpoint served %d", spent.Calls, calls)
	}
	if spent.Input != 30*calls || spent.Output != 10*calls {
		t.Fatalf("the ledger holds %+v, want %d calls of 30 in and 10 out", spent, calls)
	}
	if spent.CacheRead != 5*calls || spent.CacheWrite != 2*calls {
		t.Fatalf("the ledger's cache figures are %+v, want %d calls of 5 read and 2 written", spent, calls)
	}
	if spent.CostUSD != 0.25*float64(calls) {
		t.Fatalf("the ledger's cost is %v, want %d calls at $0.25", spent.CostUSD, calls)
	}
	if spent.Model != "test/model" {
		t.Fatalf("the ledger says the run rode %q, want what the endpoint reported", spent.Model)
	}
	if !spent.Reported() {
		t.Fatal("a run the provider priced reads as unreported")
	}
}

// TestModelExecBillsACalledHarnessToItsCaller: a harness that calls another
// spends the same person's money on the same client, so the child's calls land
// in the ledger the RUN was given rather than in one of the child's own — which
// nobody outside this package would ever see.
func TestModelExecBillsACalledHarnessToItsCaller(t *testing.T) {
	store := At(t.TempDir())
	saveHarness(t, store, Harness{
		Id: Id{Name: "thinker", Desc: "thinks about it", Version: 1},
		Program: Program{Nodes: []Node{
			{Id: "think", Kind: KindAgentLoop, Fields: Fields{"brief": "think about it"}},
		}},
		Verify: Verify{Ladder: VerifyAccept},
	})
	outer := callerHarness("outer", 2, "thinker")

	server := newModelServer(t, modelRule{when: "think about it", say: "it is fine"})
	server.bill = billed()
	var spent Usage
	if _, err := Run(context.Background(), outer, ModelExec(server.client(t), ModelExecOpts{
		Harness: outer,
		Store:   store,
		Usage:   &spent,
	})); err != nil {
		t.Fatalf("run: %v", err)
	}

	calls := server.calls()
	if calls == 0 {
		t.Fatal("the child never reached the endpoint")
	}
	if spent.Calls != calls {
		t.Fatalf("the caller's ledger counted %d calls, the endpoint served %d", spent.Calls, calls)
	}
	if spent.Input != 30*calls {
		t.Fatalf("the caller's ledger holds %+v, want the child's tokens", spent)
	}
}

// TestModelExecWithoutALedgerRunsUnchanged: the ledger is opt-in, and a caller
// that hands over none is the caller every existing surface already is. A nil
// [ModelExecOpts.Usage] must be a run that behaves exactly as it did before the
// ledger existed — not a nil dereference on the first call it makes.
func TestModelExecWithoutALedgerRunsUnchanged(t *testing.T) {
	server := newModelServer(t, modelRule{when: "think about it", say: "it is fine"})
	server.bill = billed()
	h := Harness{Id: Id{Name: "worker", Version: 1}, Verify: Verify{Ladder: VerifyAccept}}
	exec := ModelExec(server.client(t), ModelExecOpts{Harness: h})
	result, err := exec(context.Background(), Node{Id: "work", Kind: KindAgentLoop, Fields: Fields{
		"brief": "think about it",
	}})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if result.Out != "it is fine" {
		t.Fatalf("the node said %q", result.Out)
	}
}

// TestUsageForgetsTheModelWhenCallsDisagree: a run whose nodes pinned models of
// their own is not a run one name describes, so the ledger reports none rather
// than the first one it happened to see. A call that reported no model at all is
// no evidence either way and must not clear a name the run has.
func TestUsageForgetsTheModelWhenCallsDisagree(t *testing.T) {
	var one Usage
	one.add("anthropic/claude", nil)
	one.add("", nil)
	one.add("anthropic/claude", nil)
	if one.Model != "anthropic/claude" {
		t.Fatalf("a run on one model reports %q", one.Model)
	}
	if one.Calls != 3 {
		t.Fatalf("the ledger counted %d calls; a call the provider was quiet about still happened", one.Calls)
	}
	if one.Reported() {
		t.Fatal("a run nobody priced reads as reported")
	}

	var two Usage
	two.add("anthropic/claude", nil)
	two.add("openai/gpt", nil)
	two.add("anthropic/claude", nil)
	if two.Model != "" {
		t.Fatalf("a run across two models reports %q, want no name at all", two.Model)
	}
}
