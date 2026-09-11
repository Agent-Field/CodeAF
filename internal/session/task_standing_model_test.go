package session

import "testing"

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

// TestARescueMovesToTheModelThePersonPickedAndNotTheChain is the law itself.
func TestARescueMovesToTheModelThePersonPickedAndNotTheChain(t *testing.T) {
	agent, node := cascadeAgent(t, nil, taskSpec{title: "the sweep", model: "vendor/flash"})

	// The rescue has already moved this node once, the way a spent transport
	// budget does ([TaskNode.runOn]), so the node is not on the admitted model.
	node.runOn("vendor/other", "")
	if got := node.runModel(); got != "vendor/other" {
		t.Fatalf("the node runs on %q, want the model the rescue moved it to", got)
	}

	// Nobody has picked anything, so the chain is still the answer.
	if standing := node.standingModel(); standing != "" {
		t.Fatalf("a node nobody chose for reports a standing pick of %q", standing)
	}

	// And then the person chooses, in the node's own room.
	node.retarget("vendor/chosen")
	// A pick clears the rescue's swap, so the node is already there and there is
	// nothing standing to move to.
	if standing := node.standingModel(); standing != "" {
		t.Fatalf("a node already on the model that was picked reports %q to move to", standing)
	}
	// A LATER RESCUE IS THE CASE THAT MATTERS: the model the person chose stops
	// answering too, the rescue swaps it out, and what is standing is still what
	// they said.
	node.runOn("vendor/whatever-the-chain-said", "")
	if standing := node.standingModel(); standing != "vendor/chosen" {
		t.Fatalf("the standing pick reads %q, want the model chosen in the room", standing)
	}
	next, moved := agent.nextNodeModel(node)
	if !moved {
		t.Fatal("a node with a model somebody chose has somewhere to go")
	}
	if next != "vendor/chosen" {
		t.Fatalf("the rescue moves to %q, want the model the person picked", next)
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
