package plandb

// The live step is the store's one present-tense reading of a task: the step
// whose command is running right now. These tests hold it to the law it is
// written under — A LIVE STEP IS TRUE ONLY WHILE ITS COMMAND RUNS — so the
// round-trip, the one-row-per-task rule, the clear, and the fact that the row
// is IN THE FILE rather than in a handle's memory are all asserted here.

import (
	"testing"
	"time"
)

// A live step is written, read back whole — number, command and moment — and
// cleared by a task that is no longer running. Live answers one task and
// LiveSteps answers them all keyed by the store's bare id.
func TestPlandbLiveStepRoundTripsAndClears(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, planSpec("alpha", "Alpha"), planSpec("beta", "Beta"))
	clock := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }

	if err := store.SetLive("t-alpha", 4, "$ go test ./internal/api"); err != nil {
		t.Fatalf("set the live step: %v", err)
	}
	got := store.Live("t-alpha")
	if got.Step != 4 || got.Command != "$ go test ./internal/api" {
		t.Fatalf("Live(alpha) = %#v, want step 4 with its command", got)
	}
	if got.Since.IsZero() || !got.Since.Equal(clock) {
		t.Fatalf("live moment = %v, want the store's own clock %v", got.Since, clock)
	}
	if got.Empty() {
		t.Fatal("a step is running and the reading says empty")
	}

	all := store.LiveSteps()
	if len(all) != 1 {
		t.Fatalf("LiveSteps = %#v, want the one running task", all)
	}
	if beta := store.Live("beta"); !beta.Empty() {
		t.Fatalf("a task running nothing answers %#v, want the zero value", beta)
	}
	// The `t-` spelling a surface hands back meets the bare id the store keeps.
	if store.Live("alpha").Step != 4 {
		t.Fatal("the bare id did not meet the row the t- id wrote")
	}

	// A second begin for one task REPLACES the first: there is one live step
	// per task, never a queue of them.
	if err := store.SetLive("t-alpha", 5, "$ go vet ./..."); err != nil {
		t.Fatalf("replace the live step: %v", err)
	}
	if replaced := store.Live("alpha"); replaced.Step != 5 || replaced.Command != "$ go vet ./..." {
		t.Fatalf("a second begin left %#v, want the newest step alone", replaced)
	}

	if err := store.ClearLive("t-alpha"); err != nil {
		t.Fatalf("clear the live step: %v", err)
	}
	if after := store.Live("alpha"); !after.Empty() {
		t.Fatalf("a cleared task answers %#v, want the zero value", after)
	}
	if len(store.LiveSteps()) != 0 {
		t.Fatal("a cleared live step is still in the read")
	}
	// Clearing a task that has no live step is not an error: the honest answer
	// to "clear it" is the same whether the row was there or already gone.
	if err := store.ClearLive("t-beta"); err != nil {
		t.Fatalf("clearing an empty task refused: %v", err)
	}
}

// THE LIVE STEP IS IN THE FILE, NOT IN THE HANDLE. A second process — another
// chat's store handle — reads the same row the worker's handle wrote, and a
// clear by one is the absence the other reads. This is what makes the reading
// cross the process boundary a run actually spans.
func TestPlandbLiveStepCrossesHandles(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/plan.json"
	writer := planOpen(t, path)
	planAdd(t, writer, planSpec("alpha", "Alpha"))

	if err := writer.SetLive("t-alpha", 2, "$ sleep 5"); err != nil {
		t.Fatalf("set the live step: %v", err)
	}
	reader := planReopen(t, path)
	if got := reader.Live("t-alpha"); got.Step != 2 || got.Command != "$ sleep 5" {
		t.Fatalf("the second handle reads %#v, want the row the first wrote", got)
	}

	if err := writer.ClearLive("t-alpha"); err != nil {
		t.Fatalf("clear the live step: %v", err)
	}
	if got := reader.Live("t-alpha"); !got.Empty() {
		t.Fatalf("the second handle still reads %#v, want the cleared absence", got)
	}
}
