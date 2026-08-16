package blocks

import (
	"strings"
	"testing"
)

// A live list folds from the TOP: running rows are at the live edge and the
// live edge is what the reader came for.
func TestLiveListFoldsFromTheTop(t *testing.T) {
	states := make([]ItemState, 24)
	for i := range states {
		states[i] = ItemPending
	}
	states[21], states[22], states[23] = ItemRunning, ItemRunning, ItemRunning
	states[20] = ItemDone

	var f Folder
	plan := f.Live(states, 4)
	if !plan.FoldAtTop {
		t.Fatal("a live list folded from the bottom")
	}
	want := []int{21, 22, 23}
	if len(plan.Shown) != len(want) {
		t.Fatalf("shown %v, want %v", plan.Shown, want)
	}
	for i := range want {
		if plan.Shown[i] != want[i] {
			t.Fatalf("shown %v, want %v", plan.Shown, want)
		}
	}
	if plan.Hidden != 21 {
		t.Fatalf("hidden %d, want 21", plan.Hidden)
	}
	if plan.Fold != "… 21 more (20 pending · 1 done)" {
		t.Fatalf("fold line %q", plan.Fold)
	}
}

// A finalized list gives its slots to failures first: a settled list is read to
// find out what went wrong.
func TestFinalizedListPrioritisesFailures(t *testing.T) {
	states := make([]ItemState, 20)
	for i := range states {
		states[i] = ItemDone
	}
	states[3] = ItemFailed
	states[17] = ItemAborted

	var f Folder
	plan := f.Finalized(states, 4)
	if plan.FoldAtTop {
		t.Fatal("a finalized list folded from the top")
	}
	if len(plan.Shown) != 3 {
		t.Fatalf("shown %v, want three rows", plan.Shown)
	}
	seen := map[int]bool{}
	for _, i := range plan.Shown {
		seen[i] = true
	}
	if !seen[3] || !seen[17] {
		t.Fatalf("the failed and aborted rows lost their slots: %v", plan.Shown)
	}
	// And the shown rows stay in document order.
	for i := 1; i < len(plan.Shown); i++ {
		if plan.Shown[i] <= plan.Shown[i-1] {
			t.Fatalf("shown rows out of order: %v", plan.Shown)
		}
	}
	if !strings.Contains(plan.Fold, "17 more") || !strings.Contains(plan.Fold, "done") {
		t.Fatalf("fold line %q", plan.Fold)
	}
	if strings.Contains(plan.Fold, "failed") || strings.Contains(plan.Fold, "aborted") {
		t.Fatalf("the fold line accounts for rows that are actually shown: %q", plan.Fold)
	}
}

func TestFoldLineCarriesTheBreakdown(t *testing.T) {
	got := FoldLine(21, map[ItemState]int{ItemPending: 18, ItemDone: 3})
	if got != "… 21 more (18 pending · 3 done)" {
		t.Fatalf("fold line %q", got)
	}
	if FoldLine(0, nil) != "" {
		t.Fatal("a fold line appeared with nothing hidden")
	}
	got = FoldLine(5, map[ItemState]int{ItemFailed: 2, ItemAborted: 1, ItemRunning: 2})
	if got != "… 5 more (2 running · 2 failed · 1 aborted)" {
		t.Fatalf("fold line %q", got)
	}
}

func TestFoldsThatFit(t *testing.T) {
	var f Folder
	states := []ItemState{ItemDone, ItemDone, ItemRunning}
	for _, plan := range []Plan{f.Live(states, 3), f.Finalized(states, 3), f.Live(states, 9)} {
		if plan.Hidden != 0 || plan.Fold != "" {
			t.Fatalf("a list that fits was folded: %+v", plan)
		}
		if len(plan.Shown) != 3 {
			t.Fatalf("shown %v", plan.Shown)
		}
	}
}

func TestFoldAtZeroBudget(t *testing.T) {
	var f Folder
	states := []ItemState{ItemRunning, ItemFailed}
	plan := f.Live(states, 0)
	if len(plan.Shown) != 0 || plan.Hidden != 2 {
		t.Fatalf("zero budget: %+v", plan)
	}
	if !strings.Contains(plan.Fold, "1 running") || !strings.Contains(plan.Fold, "1 failed") {
		t.Fatalf("fold line %q", plan.Fold)
	}
}

// A live list re-folds on every frame, so the folder must not allocate once its
// buffers are warm and the breakdown has stopped changing.
func TestFolderDoesNotAllocateInTheSteadyState(t *testing.T) {
	states := make([]ItemState, 200)
	for i := range states {
		states[i] = ItemPending
	}
	for i := 190; i < 200; i++ {
		states[i] = ItemRunning
	}
	var f Folder
	f.Live(states, 8)
	if got := allocs(func() { f.Live(states, 8) }); got != 0 {
		t.Fatalf("a steady-state live fold allocated %.0f times per call", got)
	}
	if got := allocs(func() { f.Finalized(states, 8) }); got != 0 {
		t.Fatalf("a steady-state finalized fold allocated %.0f times per call", got)
	}
}
