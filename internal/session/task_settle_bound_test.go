package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE SETTLE TURN RUNS UNDER THE CHECKER'S OWN BOUND ──────────────────────
//
// A landed-but-unverified task under `task.settle = auto` wakes the
// conversation's own turn and tells it to read the work and settle it. That turn
// used to be an ordinary full-belt turn with no bound of its own — no call
// ceiling, no dollar bound, no window — and it was measured running 28 minutes
// of 49 tool-call rounds on the high-tier model, stopped only by the run's wall,
// over a tree that was already clean.
//
// So the wake is marked as a SETTLE wake where the note is minted, and the turn
// it starts runs under the checker's own contract: a call window it is told, a
// ceiling on the calls it may spend, and a share of the run's own money. When one
// of those trips, the node comes back to the person with the reason and the count
// on its report.

// settleScribe is the completer a settle-turn fixture rides: every step the turn
// takes is answered with one more tool call, so a turn with no bound runs until
// its script does and a turn with a bound ends at the bound. The path varies per
// round so the loop detector has nothing to say about the repeating shape.
type settleScribe struct {
	mu       sync.Mutex
	calls    int
	deadline bool
	cost     float64
}

func (s *settleScribe) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	// THE SESSION'S OWN ERRANDS DO NOT SPEND A STEP, for scriptedCompleter's
	// reason: a namer or a narrator that arrived in the middle of the turn would
	// otherwise take the slot a step was scripted for.
	if isCaptionCall(messages) {
		return textResponse(""), nil
	}
	if isTitleCall(messages) {
		return textResponse(""), nil
	}
	s.mu.Lock()
	s.calls++
	n := s.calls
	if _, ok := ctx.Deadline(); ok {
		s.deadline = true
	}
	s.mu.Unlock()
	response := toolResponse(fmt.Sprintf("settle-call-%d", n), "ls", fmt.Sprintf(`{"path":"./%d"}`, n))
	if s.cost > 0 {
		cost := s.cost
		response.Usage.Cost = &cost
	}
	return response, nil
}

func (s *settleScribe) callsMade() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *settleScribe) sawDeadline() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deadline
}

// toolStep is one scripted step that answers with a bare tool call.
func toolStep(name, arguments string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("step-call", name, arguments), nil
	}
}

// settleLanding lands ONE unverified node in the agent's own graph, so the
// landing note wakes the settle turn under test. prepare runs on the node before
// it lands, which is where a fixture states what the node's check saw.
func settleLanding(t *testing.T, agent *Agent, prepare func(*TaskNode)) *TaskNode {
	t.Helper()
	graph := stubbedGraph(agent, func(node *TaskNode) {
		if prepare != nil {
			prepare(node)
		}
		node.finish("the checker answered neither way", nil, "", "")
		node.graph.complete(node, TaskUnverified)
	})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Port the parser", named: true, brief: "b", acceptance: "a"})
	waitDoneNode(t, graph.node(id))
	return graph.node(id)
}

// waitForSettleHandback waits until the woken settle turn has ended and given the
// node's question back to the person, then hands the node's own notice.
func waitForSettleHandback(t *testing.T, node *TaskNode) TaskNotice {
	t.Helper()
	waitFor(t, "the settle turn to be bounded and hand the decision back", func() bool {
		return node.decidedBy() == TaskAskOwnerPerson
	})
	return node.notice()
}

// (a) A settle turn ends at its call ceiling, not at the run's wall.
func TestASettleTurnEndsAtItsCallCeilingRatherThanTheRunsWall(t *testing.T) {
	scribe := &settleScribe{}
	agent, _ := newTestAgent(t, scribe, func(config *Config) {
		config.AskConsent = false
	})
	node := settleLanding(t, agent, nil)
	waitForSettleHandback(t, node)

	if calls := scribe.callsMade(); calls != settleCallCeiling {
		t.Fatalf("the settle turn spent %d provider calls, want the ceiling of %d", calls, settleCallCeiling)
	}
	if !scribe.sawDeadline() {
		t.Fatal("the settle turn's calls were made under no window the model is told")
	}
}

// (c) Either trip hands the node back with the bound and the count on its
// report — never a silent stop.
func TestASettleTurnHandsBackWithTheBoundAndTheCount(t *testing.T) {
	scribe := &settleScribe{}
	agent, _ := newTestAgent(t, scribe, func(config *Config) {
		config.AskConsent = false
	})
	node := settleLanding(t, agent, nil)
	notice := waitForSettleHandback(t, node)

	if !strings.Contains(notice.Report, taskAskSettleReason) {
		t.Fatalf("the hand-back does not say the bound stopped it:\n%s", notice.Report)
	}
	if !strings.Contains(notice.Report, fmt.Sprintf("%d calls", settleCallCeiling)) {
		t.Fatalf("the hand-back does not carry the count:\n%s", notice.Report)
	}
	// AND THE CARD CARRIES THE SAME SENTENCE, read off the same report.
	if reason := ProjectTask(notice.StatusFacts()).Ask.Reason; reason != taskAskSettleReason {
		t.Fatalf("the card asks %q, want the settle bound's own reason", reason)
	}
}

// (b) With a Steward armed, the settle turn also ends when its own spend passes
// its share of the run's money.
func TestASettleTurnEndsWhenItsOwnSpendPassesItsShareOfTheBudget(t *testing.T) {
	scribe := &settleScribe{cost: 0.25}
	agent, _ := newTestAgent(t, scribe, func(config *Config) {
		config.Unattended = true
		config.Budget = Budget{USD: 1}
	})
	node := settleLanding(t, agent, nil)
	notice := waitForSettleHandback(t, node)

	if calls := scribe.callsMade(); calls >= settleCallCeiling {
		t.Fatalf("the money bound did not stop the turn before the call ceiling: %d calls", calls)
	}
	if !strings.Contains(notice.Report, taskAskSettleReason) || !strings.Contains(notice.Report, "$") {
		t.Fatalf("the money hand-back does not carry the bound and the spend:\n%s", notice.Report)
	}
}

// (d) A node whose check saw a clean tree and declared no check settles after ONE
// call: the two facts ride from the audit onto the node, and the turn's ceiling
// for such a node is one.
func TestACleanTreeWithNoDeclaredCheckSettlesAfterOneCall(t *testing.T) {
	scribe := &settleScribe{}
	agent, _ := newTestAgent(t, scribe, func(config *Config) {
		config.AskConsent = false
	})
	node := settleLanding(t, agent, func(n *TaskNode) {
		n.sawNoDeclaredCheck()
		n.sawCleanGround()
	})
	waitForSettleHandback(t, node)

	if calls := scribe.callsMade(); calls != 1 {
		t.Fatalf("a clean tree with no declared check settled after %d calls, want 1", calls)
	}
}

// AND AN ORDINARY TURN KEEPS NO CEILING. Nobody wakes it with a landing note, so
// nothing bounds it but the ladder every turn already runs under.
func TestAnOrdinaryTurnIsNotBoundedByTheSettleCeiling(t *testing.T) {
	steps := make([]step, 0, settleCallCeiling+3)
	for round := 0; round < settleCallCeiling+2; round++ {
		steps = append(steps, toolStep("ls", fmt.Sprintf(`{"path":"./%d"}`, round)))
	}
	steps = append(steps, finalText("read it"))
	completer := &scriptedCompleter{steps: steps}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
	})

	collect(t, mustSubmit(t, agent, "read the parser and tell me what you see"))
	if calls := completer.requests(); calls <= settleCallCeiling {
		t.Fatalf("an ordinary turn was bounded by the settle ceiling: %d calls", calls)
	}
}
