package plan

import (
	"context"
	"strings"
	"testing"
)

// The failure these guard against is one sentence long: the last thing a person
// sees is a plan or a pointer instead of the thing they asked for. A UX suite
// judged five journeys blind and the same note came back on four of them — the
// deliverable buried under meta-commentary — while the two competitors it was
// measured against always put the goods in-channel.
//
// The law is stated in the leaf's own contract (internal/exec), and this package
// owns the three surfaces that speak to whoever produces the finished whole: the
// instruction the deliverable owner receives, the working method written for it,
// and the merge a panel's results land in. Each one is a place a job can be told
// what to produce without ever being told where to put it, and each one used to
// be exactly that.
func TestEverySurfaceThatCommissionsTheFinishedWholeAsksForItInTheMessage(t *testing.T) {
	// Written as a map from surface to the property it must carry, because the
	// enumeration is the test: a fourth surface that can commission a final
	// deliverable and is not in this list is the next regression.
	for surface, text := range map[string]string{
		"the deliverable owner's instruction": deliverableLineFor(1, "the last node", 1),
		"the deliverable owner's method":      contractDeliverableLine,
		"the panel's merge":                   ensembleMergeBrief(3, "one merged review"),
	} {
		t.Run(surface, func(t *testing.T) {
			for name, required := range map[string][]string{
				"the finished thing goes in the reply itself": {
					"in its own reply, written out in full",
					"write that whole thing out in its own final message",
					"Write that merged result out in your own final message",
				},
				"a pointer to it is not it": {
					"rather than describing it or saying where it can be found",
					"never to file it somewhere and name the place",
					"neither is a file path with a sentence saying\nthe merged result is in there",
				},
				"an account of the work is not it": {
					"rather than describing it",
					"describe how the material was combined",
					"An account of how you\ncombined the passes is not it",
				},
			} {
				if !containsAny(text, required) {
					t.Errorf("%s no longer states that %s", surface, name)
				}
			}
		})
	}
}

func containsAny(text string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.Contains(text, candidate) {
			return true
		}
	}
	return false
}

// The multi-leaf case, which is where a pointer is most tempting: the parts are
// real results sitting in real files, and the honest-looking thing for the node
// that gathers them is to write the merge to disk and reply with the path. Both
// passes that speak to that node — the instruction and the working method — have
// to ask for the merged artifact itself, and neither may ask any other node for
// it, or the parts get written over the top of each other again.
func TestTheSinkIsToldToEmitTheMergedArtifactAndTheOthersAreNot(t *testing.T) {
	graph := &Graph{Goal: "compare the two filings and report the revenue figures", NextID: 1}
	first := graph.Add(Node{Stage: 1, Title: "Filing A", Summary: "read filing A", Brief: "Read filing A."})
	second := graph.Add(Node{Stage: 1, Title: "Filing B", Summary: "read filing B", Brief: "Read filing B."})
	graph.addSynthesis()
	sink := graph.Nodes[len(graph.Nodes)-1].ID

	// The instruction pass. Briefs skips nodes that already have one, so the two
	// parts are pre-briefed above and only the sink is written for here.
	briefs := &briefFanoutClient{}
	if _, err := Briefs(context.Background(), briefs, graph); err != nil {
		t.Fatal(err)
	}
	target := briefs.targetFor(sink)
	if target == "" {
		t.Fatalf("no instruction was written for the deliverable owner %d", sink)
	}
	if !strings.Contains(target, "in its own reply, written out in full") {
		t.Errorf("the sink's instruction never asks for the artifact itself:\n%s", target)
	}

	// The working method pass, over the same graph. Every leaf gets one; exactly
	// one of them is told the job IS the deliverable.
	contracts := &contractFanoutClient{}
	if _, err := Contracts(context.Background(), contracts, graph, nil); err != nil {
		t.Fatal(err)
	}
	told := 0
	for _, call := range contracts.snapshot() {
		body := textOf(call[len(call)-1])
		if !strings.Contains(body, contractDeliverableLine) {
			continue
		}
		told++
		if !strings.Contains(body, "write that whole thing out in its own final message") {
			t.Errorf("the sink's method was told it owns the deliverable and not where to put it:\n%s", body)
		}
	}
	if told != 1 {
		t.Fatalf("%d methods were told to emit the merged artifact, want exactly the sink", told)
	}

	// And the parts are still told to hand over rather than produce it, which is
	// the older law this must not have loosened.
	for _, part := range []int{first, second} {
		line := graph.deliverableLine(part)
		if strings.Contains(line, "owns the final deliverable") {
			t.Errorf("part %d was told it owns the deliverable: %q", part, line)
		}
		if !strings.Contains(line, "produces its own result and hands it over") {
			t.Errorf("part %d is no longer told to hand over: %q", part, line)
		}
	}
}
