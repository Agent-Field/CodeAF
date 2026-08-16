package plan

import (
	"strings"
	"testing"
)

// VERIFIED MEANS DONE, AND THE REVISER HAS TO BE ABLE TO SEE IT.
//
// The pass that reads a job's remainder was shown a title, a state and a
// result, and a result reads exactly the same whether or not anything proved
// it. So it kept proposing children to re-run the tests and re-investigate work
// that was finished and green — measured at 48% to 57% of a run's cost. This is
// not a rule about what to conclude: it is the one fact the reviser was
// reasoning without, put where it can read it.
func TestTheStateBlockShowsWhatANodeCheckedForItself(t *testing.T) {
	graph := &Graph{Goal: "port the parser", NextID: 4}
	graph.Nodes = append(graph.Nodes,
		Node{
			ID: 1, Stage: 1, Kind: KindWork, Title: "Port the parser", Summary: "write it",
			State: StateDone, Result: "the trailing-comma case parses",
			Checked: "2 files changed, +65 -3 lines; its own checks passed: go build ./..., go test ./...",
		},
		Node{
			ID: 2, Stage: 1, Kind: KindWork, Title: "Port the lexer", Summary: "port it",
			State: StateDone, Result: "the lexer is ported",
		},
	)
	block := graph.stateBlock()
	if !strings.Contains(block, "checked: 2 files changed, +65 -3 lines; its own checks passed: go build ./..., go test ./...") {
		t.Fatalf("the reviser cannot see that the work was proved:\n%s", block)
	}
	// A node whose worker checked nothing claims nothing. "Nothing was checked"
	// and "everything passed" are the two answers a silence used to collapse
	// into, and only one of them may be manufactured here — neither.
	if strings.Count(block, "checked:") != 1 {
		t.Fatalf("a node with no verifier of its own reported one:\n%s", block)
	}
}
