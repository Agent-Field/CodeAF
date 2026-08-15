package resident

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The compiler's choice becomes the splice's provenance, and every node of the
// subtree inherits it. A forced worker outranks the compiler entirely, which is
// what a measurement run buys with the flag.
func TestCompiledSubharnessRidesTheSpliceAndCanBeForced(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		compiled string
		forced   string
		want     string
	}{
		{name: "the compiler chose", compiled: "swe", want: "swe"},
		{name: "the compiler chose nothing", want: ""},
		{name: "the flag outranks the compiler", compiled: "swe", forced: "reviewer", want: "reviewer"},
		{name: "the flag alone", forced: "swe", want: "swe"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			graph := openStore(t)
			if _, err := graph.RequestCommand(store.Command{
				SessionID: "s", Kind: store.CommandSplice, Instruction: "fix the failing tests",
			}); err != nil {
				t.Fatal(err)
			}
			compile := func(context.Context, string, string) (Compiled, error) {
				return Compiled{Goal: "fix the failing tests", Scale: "task", Subharness: testCase.compiled}, nil
			}
			plan := func(context.Context, Compiled) (store.Subtree, error) {
				return store.Subtree{Nodes: []store.NodeSpec{
					{ID: "root-leaf", Brief: "fix them", Stage: 1},
					{ID: "child", Parent: "root-leaf", Brief: "and check", Stage: 2},
				}}, nil
			}
			reconciler := New(graph, compile, plan)
			if testCase.forced != "" {
				reconciler = reconciler.WithSubharness(testCase.forced)
			}
			if err := reconciler.Tick(context.Background()); err != nil {
				t.Fatalf("tick: %v", err)
			}
			for _, id := range []string{"root-leaf", "child"} {
				node, ok, err := graph.Node(id)
				if err != nil || !ok {
					t.Fatalf("node %q: ok=%t err=%v", id, ok, err)
				}
				if node.Provenance.Subharness != testCase.want || node.Subharness != testCase.want {
					t.Fatalf("node %q = %q (provenance %q), want %q",
						id, node.Subharness, node.Provenance.Subharness, testCase.want)
				}
			}
		})
	}
}

// Forcing a worker has to reach the nodes, not only the splice. A node the
// planner sized for a specialist carries that name into admission, and the
// node's own name outranks the job's — so a provenance-only force was a force
// over exactly the nodes that had no opinion. The arm of a measurement that
// forces the generalist is the arm that proves it, because that is the arm
// whose nodes disagree with the flag.
func TestForcedWorkerOutranksTheNodesOwnChoice(t *testing.T) {
	graph := openStore(t)
	if _, err := graph.RequestCommand(store.Command{
		SessionID: "s", Kind: store.CommandSplice, Instruction: "fix the failing tests",
	}); err != nil {
		t.Fatal(err)
	}
	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{Goal: "fix the failing tests", Scale: "task", Subharness: "swe"}, nil
	}
	plan := func(context.Context, Compiled) (store.Subtree, error) {
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: "root-leaf", Brief: "fix them", Stage: 1, Subharness: "swe"},
			{ID: "child", Brief: "and write the note", Parent: "root-leaf", Stage: 2, Subharness: "linear"},
		}}, nil
	}
	if err := New(graph, compile, plan).WithSubharness("linear").Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	for _, id := range []string{"root-leaf", "child"} {
		node, ok, err := graph.Node(id)
		if err != nil || !ok {
			t.Fatalf("node %q: ok=%t err=%v", id, ok, err)
		}
		if node.Subharness != "linear" || node.Provenance.Subharness != "linear" {
			t.Fatalf("node %q = %q (provenance %q), want the forced generalist",
				id, node.Subharness, node.Provenance.Subharness)
		}
	}
}
