package plan

import (
	"strings"
	"testing"
)

// The requests the ask was read as containing reach the planner as structure —
// the person's own words, listed — rather than as prose somebody rewrote on the
// way in.
//
// This is the whole of what replaced a second route out of the tasker. That
// route took the same reading and turned it straight into a layout, which is a
// planner with one shape in it and no way to say that one request waits for the
// others. Here the reading arrives as evidence and the shape stays with the
// passes whose job it is, so what has to be pinned is that the words survive
// the trip intact and that they are framed as a reading rather than as an
// order.
func TestTheAskedRequestsReachThePlannerInThePersonsOwnWords(t *testing.T) {
	requests := []string{
		"Write me a haiku about the first cold morning of autumn.",
		"Find out which three vendors ship the parser we are on and what each charges.",
	}
	block := goalBlock("Two unrelated things.", "", requests, "")

	for _, request := range requests {
		if !strings.Contains(block, request) {
			t.Fatalf("the block does not carry %q verbatim:\n%s", request, block)
		}
	}
	// Numbered, because the order they were spoken in is the only order the
	// reading actually holds, and a delivery is owed back in it.
	for index, request := range requests {
		if !strings.Contains(block, "  "+string(rune('1'+index))+". "+request) {
			t.Fatalf("request %d is not listed in the order it was spoken:\n%s", index+1, block)
		}
	}
	// Evidence, not instruction. The failure this replaced was a layout
	// committed to before anyone asked whether one request reads the others, so
	// the block has to leave that question open in as many words.
	if !strings.Contains(block, "not a decision about") {
		t.Fatalf("the block reads as an instruction about the plan's shape:\n%s", block)
	}
}

// The ordinary ask — one thing comes back, however large — sends the bytes it
// always sent, down to the newline. That is the whole of the compatibility
// story for every goal that has no separable requests in it, and one request is
// the same case as none: there is nothing for it to stand apart from.
func TestAnAskWithFewerThanTwoRequestsChangesNoPromptByte(t *testing.T) {
	const goal = "summarise the responses"
	want := goalBlock(goal, "", nil, "")
	if want != "Goal:\n"+goal {
		t.Fatalf("the bare goal block has moved: %q", want)
	}
	for name, asked := range map[string][]string{
		"one request":   {goal},
		"blank entries": {"", "   "},
		"one and blank": {goal, "  "},
	} {
		if got := goalBlock(goal, "", asked, ""); got != want {
			t.Fatalf("%s changed the prompt\n got: %q\nwant: %q", name, got, want)
		}
	}
}

// The block joins the one preamble every pass over the whole graph shares, so
// the passes that decide what waits for what read the same words the spine did
// rather than a second rendering of them.
func TestTheAskedRequestsJoinTheSharedPreamble(t *testing.T) {
	const request = "Write the three answers up as one comparison."
	graph := &Graph{Goal: "Compare three countries.", Asked: []string{
		"How does France measure road distance?", request,
	}}
	if !strings.Contains(graph.context(), request) {
		t.Fatalf("the shared preamble lost the requests:\n%s", graph.context())
	}
	if !strings.Contains(graph.planBlock(), request) {
		t.Fatalf("bind, size and audit read a preamble without the requests:\n%s", graph.planBlock())
	}
}

// A sub-planner divides one node, not the whole ask, so it must not be handed
// the person's division of an ask it is no longer planning. Expansion builds
// its sub-graph by hand and inherits what a sub-planner genuinely needs; this
// pins that the requests are not on that list.
func TestASubPlannerDoesNotInheritTheWholeAsksRequests(t *testing.T) {
	graph := &Graph{Goal: "Compare three countries.", Terrain: "answers/  3 files",
		Asked:   []string{"How does France measure road distance?", "Write them up as one comparison."},
		Settled: []Settlement{{Variable: "The three countries are France, the UK and Japan."}}}
	sub := &Graph{
		Goal:     "Look up France",
		Settled:  graph.Settled,
		Open:     graph.Open,
		Evidence: graph.Evidence,
		Terrain:  graph.Terrain,
		Invoice:  graph.Invoice,
		NextID:   1,
	}
	if strings.Contains(sub.context(), "Write them up as one comparison.") {
		t.Fatalf("a sub-planner dividing one part was handed the whole ask's division:\n%s", sub.context())
	}
	// The things it does inherit are still there — this is a statement about
	// one field, not about the sub-graph having lost its premises.
	if !strings.Contains(sub.context(), "France, the UK and Japan") {
		t.Fatalf("the sub-planner lost the settled points:\n%s", sub.context())
	}
}
