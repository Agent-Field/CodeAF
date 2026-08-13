package plan

import "testing"

// A graph nobody could tell the window of is shown exactly what it was always
// shown; a graph that knows its reviser holds 200k tokens is shown far more.
// Both halves matter: the second is the defect, the first is the rollback.
func TestTheStateBlockBudgetsFromTheWindowItIsReadThrough(t *testing.T) {
	unknown := &Graph{}
	pot, perNode := unknown.stateResultBudget()
	if pot != stateResultsBytes || perNode != stateResultBytes {
		t.Fatalf("an unknown window gave (%d, %d), want exactly the old pair (%d, %d)",
			pot, perNode, stateResultsBytes, stateResultBytes)
	}

	wide := &Graph{ContextTokens: 200_000}
	widePot, widePerNode := wide.stateResultBudget()
	if widePot <= pot*8 {
		t.Fatalf("a 200k-token reviser is shown %d bytes of what happened, "+
			"barely more than the %d a 4 KiB literal gave it", widePot, pot)
	}
	if widePerNode <= perNode {
		t.Fatalf("per-node room = %d, no better than the literal %d", widePerNode, perNode)
	}

	// A window so small the law has nothing left to hand out falls back rather
	// than handing out nothing: being wrong downward here means a reviser shown
	// none of what it is being asked to judge.
	narrow := &Graph{ContextTokens: 8_000}
	if narrowPot, narrowPerNode := narrow.stateResultBudget(); narrowPot < stateResultsBytes || narrowPerNode < stateResultBytes {
		t.Fatalf("a small window starved the reviser: (%d, %d)", narrowPot, narrowPerNode)
	}
}

// The completion gate's two tables are clipped by one number, computed once per
// ask so the cache-shaped prefix they form does not move between asks.
func TestTheSatisfactionTablesBudgetFromTheWindowTheyAreAskedThrough(t *testing.T) {
	if room := satisfiedBudget(0); room != satisfiedResultBytes {
		t.Fatalf("an unknown window clipped rows at %d, want the old %d", room, satisfiedResultBytes)
	}
	wide := satisfiedBudget(200_000)
	if wide <= satisfiedResultBytes*4 {
		t.Fatalf("a 200k-token gate clips a landed result at %d bytes", wide)
	}
	if again := satisfiedBudget(200_000); again != wide {
		t.Fatalf("the row budget moved between two asks: %d then %d", wide, again)
	}
}

// The window is a fact about the job and is persisted with it, because the
// passes that read this document afterwards are handed the document and nothing
// else. A graph read back off disk that had lost it would quietly go back to
// showing a large reviser 4 KiB.
func TestTheWindowSurvivesTheJournal(t *testing.T) {
	graph := &Graph{Goal: "ship it", NextID: 2, ContextTokens: 200_000,
		Stages: []Stage{{Title: "Do it", Summary: "the whole of it"}},
		Nodes:  []Node{{ID: 1, Title: "Do it", Summary: "the whole of it", Stage: 1}}}
	encoded, err := graph.JSON()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Window() != 200_000 {
		t.Fatalf("the window came back as %d", reloaded.Window())
	}
	// And a caller holding no graph at all asks safely: nil is unknown, which
	// is the same answer as a graph nobody told.
	var missing *Graph
	if missing.Window() != 0 {
		t.Fatalf("a nil graph claimed a window of %d", missing.Window())
	}
}
