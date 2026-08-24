package exec

// THE DEOPTIMIZATION, on the one question this package owns: may the long way be
// taken at all?
//
// Both surfaces — the task node (internal/session) and the headless command
// (cmd/aforge) — reach [Deopt], so the decision is taken here and neither can
// skip it. What they assert is the sentence a person reads; what these assert is
// that the ceiling holds.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// caged is a program approved to read files and nothing else — the ordinary
// shape of a saved program, and the one whose card reads "can use · read".
func caged() Manifest {
	return Manifest{
		SubharnessInfo: SubharnessInfo{Name: "reconcile", Purpose: "reconcile a statement"},
		Whitelist:      []string{"read"},
	}
}

func TestACeilingThatDoesNotReachAShellHoldsTheLongWay(t *testing.T) {
	for _, row := range []struct {
		name      string
		whitelist []string
		held      bool
	}{
		{"nothing declared", nil, true},
		{"an empty ceiling", []string{}, true},
		{"reading only", []string{"read"}, true},
		{"reading and writing, but no shell", []string{"read", "write"}, true},
		{"a shell in the conversation's spelling", []string{"read", "bash"}, false},
		{"a shell in a leaf's own spelling", []string{"sh"}, false},
		{"a shell spelled loudly", []string{"BASH"}, false},
		{"a shell with the padding a hand-written bundle leaves", []string{" bash "}, false},
	} {
		manifest := caged()
		manifest.Whitelist = row.whitelist
		if got := DeoptHeld(manifest); got != row.held {
			t.Errorf("%s: the long way held = %v, want %v", row.name, got, row.held)
		}
	}
}

// A HELD FALLBACK NEVER REACHES THE WORKER UNDER IT. This is the whole of the
// finding: a program approved to touch nothing became an unrestricted shell
// agent in somebody's workspace the moment a check did not pass.
func TestAHeldFallbackNeverReachesTheGeneralist(t *testing.T) {
	general := &fallbackWorker{}
	registry := NewRegistry(general)

	result, err := Deopt(context.Background(), registry, caged(),
		json.RawMessage(`{"brief":"reconcile the March statement"}`), UnwiredEnv{},
		"this one needs the statement in the repository")
	if err != nil {
		t.Fatalf("a ceiling that held is not a fault: %v", err)
	}
	if general.ran != 0 {
		t.Fatalf("the generalist ran %d times for a program that may not reach it", general.ran)
	}
	if result.Finished() {
		t.Fatal("a run that never happened cannot be finished")
	}
	if !strings.Contains(result.Incomplete, DeoptHeldWord) {
		t.Fatalf("the result does not say why nothing else was tried: %q", result.Incomplete)
	}
	// The program's own reason is carried through, exactly as the taken road
	// carries it: it was written for a person to read.
	if !strings.Contains(result.Incomplete, "needs the statement in the repository") {
		t.Fatalf("the program's own reason was dropped: %q", result.Incomplete)
	}
	if FellBack(result) {
		t.Fatalf("a run that was NOT handled the long way says it was: %q", result.FellBack)
	}
}

// AND A CEILING THAT REACHES A SHELL STILL FALLS BACK, with the original input,
// because the long way stays inside what was approved.
func TestACeilingThatReachesAShellStillFallsBack(t *testing.T) {
	general := &fallbackWorker{}
	registry := NewRegistry(general)
	manifest := caged()
	manifest.Whitelist = []string{"read", "bash"}

	const input = `{"brief":"reconcile the March statement"}`
	result, err := Deopt(context.Background(), registry, manifest, json.RawMessage(input), UnwiredEnv{}, "a guard did not pass")
	if err != nil {
		t.Fatalf("the long way: %v", err)
	}
	if general.ran != 1 {
		t.Fatalf("the long way is the generalist, run once; it ran %d times", general.ran)
	}
	if general.saw != "reconcile the March statement" {
		t.Fatalf("the long way was handed %q, want the original brief", general.saw)
	}
	if !FellBack(result) {
		t.Fatal("a run handled the long way does not say so")
	}
	if !strings.Contains(result.FellBack, DeoptWord) {
		t.Fatalf("the sentence is %q, want the one word both surfaces quote", result.FellBack)
	}
}

// fallbackWorker is the worker under the fallback, counted rather than
// scripted: what these tests are about is whether it was reached at all.
type fallbackWorker struct {
	ran int
	saw string
}

func (c *fallbackWorker) Subharness() string { return LinearSubharness }

func (c *fallbackWorker) Run(_ context.Context, task Task) (*Outcome, error) {
	c.ran++
	c.saw = task.Brief
	return &Outcome{Text: "reconciled by hand", Stop: StopDone}, nil
}
