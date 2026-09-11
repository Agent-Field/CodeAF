package session

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// TestTheGateGradesNoCallAsIrreversibleAndMarksDenyTheSafeAnswer is the fact a
// SURFACE is not allowed to guess at, written down where it is produced.
//
// A surface deciding where a permission's pointer opens has exactly two things
// to read: the stakes, and which answer the lane marked as losing nothing. The
// 2026-09-11 design ruling was that an ordinary call should open on `allow once`
// and only a grave one on deny — and the reason that rule cannot be written from
// the stakes today is asserted here rather than described: THIS GATE STAMPS
// `costly` ON EVERY CALL ALIKE, `rm -rf *` included, on purpose and with its own
// account of why (consent.go). Nothing in internal/approval produces
// [StakesIrreversible] at all; approval's judgement is expressed as WHETHER TO
// ASK and never as how much is at stake.
//
// So a surface rule keyed on the stakes would read "ordinary" for a recursive
// delete exactly as loudly as for a `git status`, and its grave branch would be
// reachable only from a fixture. The surface's own rule is deny-first for every
// permission until that changes (tui3's [questionPointerStart]), and THIS TEST
// IS WHAT TELLS THE NEXT PERSON THE GROUND MOVED: grade a call irreversible
// here and this fails, which is the moment the pointer rule may be split by
// stakes and the moment that surface test needs re-reading.
func TestTheGateGradesNoCallAsIrreversibleAndMarksDenyTheSafeAnswer(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	call := ai.ToolCall{
		ID:       "c1",
		Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"rm -rf *"}`},
	}
	decision := approval.Policy{}.Check("bash", json.RawMessage(call.Function.Arguments))
	if decision.Action != approval.ActionPrompt {
		t.Fatalf("the default rules do not even ask about `rm -rf *`: %+v", decision)
	}

	ask := agent.consentAsk(9, call, decision, true, "needs your ok to run bash")

	// THE STAKES, WHICH ARE THE HALF A SURFACE MUST NOT KEY A SAFETY DEFAULT ON.
	if ask.Stakes == StakesIrreversible {
		t.Fatal("the gate now grades a call irreversible: the surface's deny-first " +
			"rule may be split by stakes, and tui3's " +
			"TestEnterOnAPermissionDeniesUntilTheEngineGradesTheCall must be re-read")
	}
	if ask.Stakes != StakesCostly {
		t.Fatalf("the gate stamped something other than costly on `rm -rf *`: %q", ask.Stakes)
	}
	if ask.Ask != AskPermission {
		t.Fatalf("a consent is not asking for permission: %q", ask.Ask)
	}

	// AND THE HALF A SURFACE MUST READ: which answer loses nothing. A pointer
	// placed on `Safe` is only as good as the lane marking one.
	safe := ""
	for _, option := range ask.Options {
		if option.Safe {
			if safe != "" {
				t.Fatalf("two answers claim to lose nothing: %q and %q", safe, option.Key)
			}
			safe = option.Key
		}
	}
	if safe == "" {
		t.Fatal("no answer on a permission is marked as the one that loses nothing")
	}
	for _, option := range ask.Options {
		if option.Key == safe && option.Label != "deny" {
			t.Fatalf("the answer that loses nothing is not the refusal: %q", option.Label)
		}
	}
}
