package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

func TestGateDecisions(t *testing.T) {
	files, _ := filepath.Glob("../../bench/swarm/tasks/*.txt")
	for _, f := range files {
		b, _ := os.ReadFile(f)
		t.Logf("%-30s enumerated=%d divide=%v", filepath.Base(f),
			enumeratedItems(string(b)), divisionWorthIt(string(b)))
	}
}

// THE PLAN DOOR IS WHERE MOST DIVISIONS ARE BORN, so it is where the mode has
// to be readable. These rows walk one graph — three sittings that owe each
// other nothing, under a brief with no digit anywhere in it, which is #418's
// replication — through every value the pin takes.
func TestThePlanDoorFoldsOrKeepsAccordingToTheModePinned(t *testing.T) {
	const narrow = "HANDBOOK.md is one file. Deliver lanes that share no lines: rewrite the headings, link the cross-references, insert a contents section."
	for _, probe := range []struct {
		pin    string
		folded int
		why    string
	}{
		{"", 3, "the shipped gate reads no digit beside a plural and folds, which is the bug #418 reports"},
		{"1", 3, "the same gate, named"},
		{"0", 0, "the rollback switch has always taken the gate away entirely"},
		{"lanes", 3, "the wider counting reads three named parts and three is still under the six-item floor"},
		{"judgment", 0, "three sittings that owe each other nothing are a real division whatever the brief counted"},
	} {
		t.Run(probe.pin, func(t *testing.T) {
			t.Setenv("AFORGE_SPLITGATE", probe.pin)
			graph := threeIndependentSittings(narrow)
			if got := gatePlanDivision(graph, narrow); got != probe.folded {
				t.Fatalf("AFORGE_SPLITGATE=%q folded %d leaves, want %d — %s", probe.pin, got, probe.folded, probe.why)
			}
			want := 3
			if probe.folded > 0 {
				want = 1
			}
			if got := len(graph.Leaves()); got != want {
				t.Fatalf("AFORGE_SPLITGATE=%q left %d work nodes, want %d", probe.pin, got, want)
			}
		})
	}
}

// AND THE SENTENCE A FOLDED NODE CARRIES DOES NOT MOVE. It is what a person
// reads afterwards to learn why their plan is one node, and #418 quotes it
// verbatim; an experiment that changed the wording would make every report of
// this fault, before and after, unsearchable against each other.
func TestTheFoldedNodeStillSaysWhatItAlwaysSaid(t *testing.T) {
	t.Setenv("AFORGE_SPLITGATE", "")
	const narrow = "rewrite the handbook in lanes that share no lines"
	graph := threeIndependentSittings(narrow)
	if folded := gatePlanDivision(graph, narrow); folded != 3 {
		t.Fatalf("the unpinned gate folded %d leaves, want 3", folded)
	}
	const want = "split gate: goal enumerates 0 items, under the 6-item floor — one sitting"
	if got := graph.Nodes[0].Undivided; got != want {
		t.Errorf("the folded node says %q, want %q", got, want)
	}
}

// threeIndependentSittings is a two-stage plan whose three work nodes are each
// sized a sitting and wait on nothing but the stage before them.
func threeIndependentSittings(goal string) *plan.Graph {
	graph := &plan.Graph{Goal: goal, Stages: []plan.Stage{{Title: "the lanes"}}}
	for _, title := range []string{"headings", "cross-references", "contents"} {
		graph.Add(plan.Node{
			Kind:  plan.KindWork,
			Stage: 1,
			Title: title,
			Size:  plan.SizeAtomic,
			Brief: title,
		})
	}
	return graph
}
