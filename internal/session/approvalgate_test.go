package session

import (
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the gate that can be replaced while the session runs ────────────────────

// gateCall is one call to put in front of the gate. The arguments are real JSON
// because internal/approval reads them for bash and reads them for the shape of
// a raw account call, and a test that handed it nothing would be testing a
// narrower gate than the product runs.
func gateCall(tool, arguments string) ai.ToolCall {
	call := ai.ToolCall{ID: "call-1"}
	call.Function.Name = tool
	call.Function.Arguments = arguments
	return call
}

func gateAgent(t *testing.T, policy *approval.Policy) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalPolicy = policy
	})
	return agent
}

// A rule banked in the middle of a session has to answer the NEXT call, not the
// next launch. That is the whole complaint the push seam exists for.
func TestAPushedGateAnswersTheNextCall(t *testing.T) {
	agent := gateAgent(t, promptAll())
	call := gateCall("edit", `{"path":"x"}`)

	decision, governed := agent.decide(call)
	if !governed || decision.Action != approval.ActionPrompt {
		t.Fatalf("the launch gate answered %s (governed=%v), want prompt", decision, governed)
	}

	agent.SetApprovalPolicy(&approval.Policy{
		Default: approval.ActionPrompt,
		Tools:   map[string]approval.Action{"edit": approval.ActionAllow},
	})

	decision, governed = agent.decide(call)
	if !governed || decision.Action != approval.ActionAllow {
		t.Fatalf("the banked rule answered %s (governed=%v), want allow", decision, governed)
	}
	// The rest of the pushed policy lands too, not only the row that moved.
	if decision, _ := agent.decide(gateCall("write", `{"path":"x"}`)); decision.Action != approval.ActionPrompt {
		t.Fatalf("a tool the pushed rules do not name answered %s, want prompt", decision)
	}
}

// A session that launched ungated is ungated until somebody says otherwise, and
// a push is a thing that can say otherwise.
func TestAnUngatedSessionStaysUngatedUntilSomethingIsPushed(t *testing.T) {
	agent := gateAgent(t, nil)
	call := gateCall("bash", `{"command":"rm -rf /"}`)
	if _, governed := agent.decide(call); governed {
		t.Fatal("a session with no gate answered as though it had one")
	}
	agent.SetApprovalPolicy(&approval.Policy{Default: approval.ActionAllow})
	decision, governed := agent.decide(call)
	if !governed {
		t.Fatal("a pushed gate did not take effect on a session that launched with none")
	}
	// AND THE FLOOR CAME WITH IT. A push buys exactly what a relaunch buys: the
	// critical-command table still stands over a blanket allow.
	if decision.Action != approval.ActionPrompt {
		t.Fatalf("a pushed blanket allow ran a critical command: %s", decision)
	}
}

// A rebuild that failed must never be the thing that opens the gate. Nil is the
// configured-nothing case, which means allow everything, and a caller who could
// not read the rules is the last caller who should be able to say that.
func TestANilNeverReplacesTheStandingGate(t *testing.T) {
	agent := gateAgent(t, promptAll())
	agent.SetApprovalPolicy(nil)
	if decision, governed := agent.decide(gateCall("edit", `{"path":"x"}`)); !governed || decision.Action != approval.ActionPrompt {
		t.Fatalf("a nil push answered %s (governed=%v), want the standing prompt", decision, governed)
	}

	agent.SetApprovalPolicy(&approval.Policy{Default: approval.ActionAllow})
	agent.SetApprovalPolicy(nil)
	if decision, _ := agent.decide(gateCall("edit", `{"path":"x"}`)); decision.Action != approval.ActionAllow {
		t.Fatalf("a nil push undid the one before it: %s", decision)
	}
}

// The pointer is read on the tool path and written from whatever goroutine a
// surface answers a card on. Run under -race, this is the test that says the
// discipline is one lock and not two habits.
func TestTheGateIsSafeToReplaceWhileItIsBeingRead(t *testing.T) {
	agent := gateAgent(t, promptAll())
	allow := &approval.Policy{Default: approval.ActionAllow}
	ask := &approval.Policy{Default: approval.ActionPrompt}

	var group sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		group.Add(1)
		go func(nth int) {
			defer group.Done()
			for round := 0; round < 200; round++ {
				if (nth+round)%2 == 0 {
					agent.SetApprovalPolicy(allow)
				} else {
					agent.SetApprovalPolicy(ask)
				}
			}
		}(worker)
	}
	for worker := 0; worker < 4; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for round := 0; round < 200; round++ {
				agent.decide(gateCall("edit", `{"path":"x"}`))
				agent.approvalGate()
			}
		}()
	}
	group.Wait()
}
