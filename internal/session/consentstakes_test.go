package session

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"
	"time"

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

	// AND THE WHOLE OBJECT IS WRITTEN DOWN WHERE THE SURFACE CAN RAISE IT.
	//
	// The surface rule this protects lives in internal/tui3, which cannot call
	// into this package's unexported gate, and a hand-built copy of this question
	// over there is exactly the fixture that let the regression through: it
	// claimed `irreversible` where the engine says `costly`, so it tested a frame
	// nobody ever meets. So the genuine article is kept on disk, THIS test is what
	// keeps it current, and tui3's
	// TestEnterOnTheEnginesOwnPermissionDeniesTheCall raises what is in the file
	// rather than anything it made up. A golden that drifts is worse than none, so
	// the drift is what fails here.
	assertConsentGolden(t, ask)
}

// consentGolden is where the engine's own answer to `rm -rf *` is kept for the
// surface's tests to raise. It sits beside the code that produces it, because
// the producer is what owes it accuracy.
const consentGolden = "testdata/consentask-rm-rf.json"

// assertConsentGolden holds the file and the gate to each other, and rewrites the
// file under -update rather than making somebody transcribe a diff by hand.
func assertConsentGolden(t *testing.T, ask Question) {
	t.Helper()
	// THE STAMP IS NOT PART OF THE QUESTION'S SHAPE. `Asked` is a clock reading
	// and would make this file differ on every run, so it is cleared before the
	// comparison and the surface stamps its own.
	ask.Asked = time.Time{}
	fresh, err := json.MarshalIndent(ask, "", "  ")
	if err != nil {
		t.Fatalf("encode the consent question: %v", err)
	}
	fresh = append(fresh, '\n')
	if *updateGolden {
		if err := os.WriteFile(consentGolden, fresh, 0o644); err != nil {
			t.Fatalf("write %s: %v", consentGolden, err)
		}
		return
	}
	held, err := os.ReadFile(consentGolden)
	if err != nil {
		t.Fatalf("read %s: %v (run `go test ./internal/session -run %s -update`)",
			consentGolden, err, t.Name())
	}
	if !bytes.Equal(bytes.TrimSpace(held), bytes.TrimSpace(fresh)) {
		t.Fatalf("the gate no longer produces what %s holds, so tui3 is raising a "+
			"question this engine does not ask.\n\nnow:\n%s\nheld:\n%s\n"+
			"If the change is intended, re-run with -update AND re-read tui3's "+
			"TestEnterOnTheEnginesOwnPermissionDeniesTheCall: the pointer rule is "+
			"read off this object.", consentGolden, fresh, held)
	}
}

// updateGolden rewrites the file above instead of comparing against it.
var updateGolden = flag.Bool("update", false, "rewrite the consent golden in testdata")
