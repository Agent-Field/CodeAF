package session

import (
	"io"
	"testing"
)

// ── A PERSON'S PICK OUTRANKS THE FALLBACK CHAIN ─────────────────────────────
//
// THE MEASURED TURN (2026-09-11 14:39–14:41). A step was stuck on a machine that
// answered `temporarily rate-limited upstream` eight times in ninety seconds.
// The person walked into the task's room and chose another model, and was told
// the change was taken. The step then ran out of transport budget, the rescue
// asked the adapter's fallback chain what to try next, and the work carried on
// — on a model nobody had named, while the room went on showing the one they had
// picked.
//
// THE LAW: a chain answers "what should this try next when NOBODY has said". The
// moment somebody has said, the question is closed.
//
// AND IT HAS TO HOLD ON THE FIRST MOVE AFTER THE PICK, which is the only move
// the person is actually watching. The first spelling of this could not: the
// pick was a bool beside the spec's id, and whether it was still owed was
// derived by comparing the spec against the model the node was running on —
// which the pick had just rewritten. So it read as already carried out from the
// instant it was made, and only a LATER rescue, after something else had moved
// the node off it, could see it. It is a FACT on the node now, written by the
// retarget and spent when a worker is actually built on it.

// TestTheFirstMoveAfterAPickGoesToThePick is that law, on the road the run loop
// walks: the gate on a failure, and then the move that answers it.
func TestTheFirstMoveAfterAPickGoesToThePick(t *testing.T) {
	agent, node, _ := boundaryGateAgent(t, nil)
	// The node is on the model it was admitted with and has never moved.
	ranOn := node.runModel()
	node.startedOn(ranOn)
	if standing := node.standingModel(); standing != "" {
		t.Fatalf("a node nobody chose for owes a pick of %q", standing)
	}

	// THE PERSON CHOOSES, while that run is still going.
	node.retarget("vendor/chosen")
	if standing := node.standingModel(); standing != "vendor/chosen" {
		t.Fatalf("the pick reads %q the moment it is made, want the model the person named", standing)
	}

	// AND THE RUN THEN DIES ON A MACHINE THAT WOULD NOT SERVE IT — the owner's
	// own shape: a 429 relayed from one upstream, which is TRANSPORT by this
	// build's own classifier and is exactly what the gate used to refuse to move
	// for.
	paced := refusalOf(429, "Provider returned error", "Wafer",
		`{"error":{"message":"deepseek/deepseek-v4.1-flash is temporarily rate-limited upstream."}}`)
	if !agent.movesForFailure(node, ranOn, paced, io.Discard) {
		t.Fatal("a step being paced stayed on the machine pacing it after the person chose another model")
	}
	next, moved := agent.escalateNodeModel(node, ranOn)
	if !moved {
		t.Fatal("a node with a model somebody chose has somewhere to go")
	}
	if next != "vendor/chosen" {
		t.Fatalf("the first move after the pick went to %q, want the model the person named", next)
	}

	// AND THE PICK IS SPENT BY BEING CARRIED OUT, exactly once: the next worker
	// is built on it, and a failure after that walks the chain like any other.
	node.runOn(next, "")
	node.startedOn(next)
	if standing := node.standingModel(); standing != "" {
		t.Fatalf("the pick still reads %q after the node ran on it", standing)
	}
}

// AND A PACED STEP NOBODY CHOSE FOR STAYS WHERE IT IS. The gate's own rule is
// unchanged and this is the half that proves the pick did not simply disable it:
// moving a node to a dearer model to answer for somebody else's bad minute is
// the purchase the boundary exists to refuse.
func TestAPacedStepNobodyChoseForStaysOnItsModel(t *testing.T) {
	agent, node, _ := boundaryGateAgent(t, nil)
	ranOn := node.runModel()
	paced := refusalOf(429, "Provider returned error", "Wafer",
		`{"error":{"message":"deepseek/deepseek-v4.1-flash is temporarily rate-limited upstream."}}`)
	if agent.movesForFailure(node, ranOn, paced, io.Discard) {
		t.Fatal("a machine's bad minute bought a whole node a dearer model with nobody asking")
	}
}

// AND A PICK NAMING THE MODEL THE RUN JUST DIED ON IS NOT A MOVE. The person
// asked for nothing to change; spending a second worker to arrive where the last
// one died would be this build inventing a move out of their agreement.
func TestAPickNamingTheModelThatFailedIsNotAMove(t *testing.T) {
	agent, node, _ := boundaryGateAgent(t, nil)
	ranOn := node.runModel()
	node.retarget(ranOn)
	if standing := node.standingModel(); standing != ranOn {
		t.Fatalf("the pick reads %q, want the model named", standing)
	}
	paced := refusalOf(429, "Provider returned error", "Wafer", "")
	if agent.movesForFailure(node, ranOn, paced, io.Discard) {
		t.Fatalf("a pick naming %s, the model that just failed, was read as somewhere to go", ranOn)
	}
}

// AND A MODEL THE PLANNER WROTE IS NOT A PERSON'S PICK. `propose_task` carries a
// `model` argument and the proposing model fills it in; that is a default, not a
// standing instruction, and a rescue is free to walk past it — which is the
// whole reason this is a fact of its own rather than "the spec has a model".
func TestAModelTheProposalNamedIsNotAStandingPick(t *testing.T) {
	_, node := cascadeAgent(t, nil,
		taskSpec{title: "the sweep", model: "vendor/flash", modelWord: "vendor/flash"})
	node.runOn("vendor/other", "")
	if standing := node.standingModel(); standing != "" {
		t.Fatalf("a model the proposal named reads as a standing pick of %q", standing)
	}
}

// AND A RESCUE THAT HAS ALREADY MOVED THE NODE STILL COMES BACK TO THE PICK.
// This is the case the first spelling of the law could answer, and it must keep
// working: the model the person chose stopped answering too, the rescue swapped
// it out, and what is standing is still what they said.
func TestARescueMovesToTheModelThePersonPickedAndNotTheChain(t *testing.T) {
	agent, node := cascadeAgent(t, nil, taskSpec{title: "the sweep", model: "vendor/flash"})

	node.runOn("vendor/other", "")
	node.startedOn("vendor/other")
	if got := node.runModel(); got != "vendor/other" {
		t.Fatalf("the node runs on %q, want the model the rescue moved it to", got)
	}
	node.retarget("vendor/chosen")
	node.runOn("vendor/whatever-the-chain-said", "")

	next, moved := agent.nextNodeModel(node, "vendor/whatever-the-chain-said")
	if !moved {
		t.Fatal("a node with a model somebody chose has somewhere to go")
	}
	if next != "vendor/chosen" {
		t.Fatalf("the rescue moves to %q, want the model the person picked", next)
	}
}
