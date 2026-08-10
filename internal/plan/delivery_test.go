package plan

import (
	"context"
	"strings"
	"testing"
)

// The law had five authors and they contradicted each other, so the test that
// matters is not what any one of them says — it is that the two shapes exist,
// that they are stated once, and that a caller who has not made the judgment
// gets exactly the prompt this system has always sent.
func TestTheDeliveryLawIsOneStatementWithTwoShapes(t *testing.T) {
	if got := DeliveryLaw(false); got != DeliverInMessage {
		t.Errorf("the default shape is not the in-message law:\n%s", got)
	}
	if got := DeliveryLaw(true); got != DeliverToNamedFile {
		t.Errorf("the file-shaped ask does not get the carve-out:\n%s", got)
	}
	// The in-message half must state the split rather than a length, because a
	// length is what put it at war with the leaf's own budget: the answer is
	// never what gets filed, and the working always may be.
	for name, phrase := range map[string]string{
		"a pointer is not delivery": "says where the answer lives instead of carrying it has\ndelivered nothing",
		"the split is named":        "between the answer and its working, never between the answer and a pointer to\nthe answer",
	} {
		if !strings.Contains(DeliverInMessage, phrase) {
			t.Errorf("the in-message law no longer states that %s", name)
		}
	}
	// And the carve-out must say the two things the gate judges by, or a worker
	// held to it is being set up to fail a rule it was never given.
	for name, phrase := range map[string]string{
		"the file is the deliverable":              "so the file IS the deliverable",
		"the message carries the answer beside it": "carries the answer itself, what was run and what came back, and the name of the\nfile",
		"shortness is not thinness":                "its shortness convicts\nnothing",
	} {
		if !strings.Contains(DeliverToNamedFile, phrase) {
			t.Errorf("the carve-out no longer states that %s", name)
		}
	}
}

// Byte-for-byte where the caller does not know. Every existing caller passes
// false today, and false has to render the exact lines both passes have always
// rendered — otherwise the fix that was meant to save one cache invalidation
// spends several.
func TestTheUnknownAskRendersExactlyTheLinesItAlwaysDid(t *testing.T) {
	const brief = "This node owns the final deliverable the goal asks for: it is the only " +
		"one that produces it, and the other results arrive here as inputs. Tell it to put that " +
		"finished deliverable in its own reply, written out in full, rather than describing it or " +
		"saying where it can be found.\n"
	if got := deliverableLineFor(7, "the last node", 7, false); got != brief {
		t.Errorf("the deliverable owner's instruction changed:\ngot:  %q\nwant: %q", got, brief)
	}
	const method = "This job IS the deliverable: every other result arrives here as material, and what " +
		"this agent produces is the whole of what the person who asked will read. The method must therefore end by " +
		"telling the agent to write that whole thing out in its own final message — the merged result, the figures, " +
		"the verdict, in full — and never to file it somewhere and name the place, describe how the material was " +
		"combined, or report that the assembly is finished.\n"
	if got := contractDeliverableLineFor(false); got != method {
		t.Errorf("the deliverable owner's method changed:\ngot:  %q\nwant: %q", got, method)
	}
	// A non-owner is told the same thing whatever the shape of the ask: the
	// carve-out is about where the finished thing lands, not about who makes it.
	if deliverableLineFor(7, "the last node", 3, true) != deliverableLineFor(7, "the last node", 3, false) {
		t.Error("the file-shaped ask changed what a non-owner is told")
	}
}

// The failure this closes cost four gate rounds and a whole task: the round
// that filed a 37KB document was failed for obeying the sane rule, because the
// only place that knew file-shaped asks are the exception was the gate itself.
// Both surfaces that commission the deliverable now carry the same carve-out,
// and they carry the identical bytes of it.
func TestTheFileShapedCarveOutReachesBothSurfacesThatCommissionTheDeliverable(t *testing.T) {
	for name, line := range map[string]string{
		"the deliverable owner's instruction": deliverableLineFor(7, "the last node", 7, true),
		"the deliverable owner's method":      contractDeliverableLineFor(true),
	} {
		if !strings.Contains(line, DeliverToNamedFile) {
			t.Errorf("%s states the carve-out in its own words instead of the shared one:\n%s", name, line)
		}
		if strings.Contains(line, "never to file it somewhere and name the place") {
			t.Errorf("%s still forbids the file the person asked for:\n%s", name, line)
		}
	}
}

// The bit is one fact about the goal, so it rides the graph: whoever holds the
// request sets it once and every pass that writes for the deliverable owner
// reads the same answer. This is the wiring test — set it, and both writing
// passes change what they ask for.
func TestTheFileShapedBitOnTheGraphReachesBothWritingPasses(t *testing.T) {
	build := func(fileShaped bool) *Graph {
		graph := &Graph{Goal: "update REPORT.md with the new figures", NextID: 1, FileShaped: fileShaped}
		graph.Add(Node{Stage: 1, Title: "Figures", Summary: "gather the figures", Brief: "Gather them."})
		graph.Add(Node{Stage: 1, Title: "Prose", Summary: "draft the prose", Brief: "Draft it."})
		graph.addSynthesis()
		return graph
	}
	for _, fileShaped := range []bool{false, true} {
		graph := build(fileShaped)
		sink := graph.Nodes[len(graph.Nodes)-1].ID

		briefs := &briefFanoutClient{}
		if _, err := Briefs(context.Background(), briefs, graph); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(briefs.targetFor(sink), DeliverToNamedFile); got != fileShaped {
			t.Errorf("FileShaped=%v: the instruction carries the carve-out = %v", fileShaped, got)
		}

		contracts := &contractFanoutClient{}
		if _, err := Contracts(context.Background(), contracts, build(fileShaped), nil); err != nil {
			t.Fatal(err)
		}
		carved := 0
		for _, call := range contracts.snapshot() {
			if strings.Contains(textOf(call[len(call)-1]), DeliverToNamedFile) {
				carved++
			}
		}
		want := 0
		if fileShaped {
			want = 1
		}
		if carved != want {
			t.Errorf("FileShaped=%v: %d methods carry the carve-out, want %d", fileShaped, carved, want)
		}
	}
}

// The build carries the bit too, because a caller that turns briefs on never
// sees the graph before they are written — the instructions are already out by
// the time Build returns.
func TestBuildCarriesTheFileShapedBitOntoTheGraph(t *testing.T) {
	options := Options{Ensemble: EnsembleNever, SpineSamples: 1, FileShaped: true}
	graph, err := Build(context.Background(), &passClient{}, "update REVIEW.md with the new findings", options)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !graph.FileShaped {
		t.Fatal("Options.FileShaped never reached the graph, so no pass over it can see the ask's shape")
	}
}
