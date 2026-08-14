package exec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A spinning leaf reads the same file with the same tool, gets the same
// result, and never writes anything. The no-progress guard must catch it:
// the conclude directive fires, and if the model keeps spinning, the guard
// terminates the leaf with StopNoProgress.
func TestNoProgressGuardFiresOnSpinningLeaf(t *testing.T) {
	space := workspace(t)
	// A file for the model to "read" — the scripted completer doesn't
	// actually run tools, but the repeat signal works on the RESULT hash,
	// which the completer controls.
	if err := os.WriteFile(filepath.Join(space.Root(), "data.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	// The model calls the same read tool with the same args every turn.
	// The scripted completer returns the same fake result each time.
	repeatCall := call("r1", "sh", `{"cmd":"cat data.txt"}`)
	turns := make([][]ai.ToolCall, 20)
	for i := range turns {
		turns[i] = []ai.ToolCall{repeatCall}
	}
	client := &scriptedCompleter{turns: turns}
	linear := NewLinear(client, space, nil, 100, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	// The guard must have terminated the leaf — not a normal finish, not a
	// turn cap, not a budget stop.
	if outcome.Stop != StopNoProgress {
		t.Fatalf("stop = %s, want %s (turns=%d)", outcome.Stop, StopNoProgress, outcome.Turns)
	}
	if outcome.Exhausted != StopNoProgress {
		t.Fatalf("exhausted = %q, want %q", outcome.Exhausted, StopNoProgress)
	}
	// The leaf should have been stopped well before the 20 scripted turns.
	if outcome.Turns >= 20 {
		t.Fatalf("ran %d turns — the guard never fired", outcome.Turns)
	}
}

// A normal multi-turn leaf that writes files and reads different things
// must never trigger the no-progress guard.
func TestNoProgressGuardNeverFiresOnProductiveLeaf(t *testing.T) {
	space := workspace(t)
	// A leaf that writes a different file each turn — every turn mutates
	// the workspace and produces new information.
	turns := make([][]ai.ToolCall, 12)
	for i := range turns {
		turns[i] = []ai.ToolCall{call(
			fmt.Sprintf("w%d", i), "write",
			fmt.Sprintf(`{"path":"out-%d.txt","text":"step %d"}`, i, i))}
	}
	// The last turn delivers: no tool calls, just text.
	client := &scriptedCompleter{turns: turns}
	linear := NewLinear(client, space, nil, 50, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %s, want done — a productive leaf must not be stopped", outcome.Stop)
	}
	if outcome.Exhausted != "" {
		t.Fatalf("exhausted = %q, want empty — nothing ran out", outcome.Exhausted)
	}
}

// The guard observes repeated identical tool calls by their result content.
// Even when the model calls different tools, if every result is the same
// and no mutation happens, the stagnant signal fires.
func TestNoProgressGuardFiresOnStagnantLeaf(t *testing.T) {
	space := workspace(t)
	// The model runs different commands (different call signatures, so the
	// repeat signal doesn't fire) that all produce the same successful output.
	// Since no writes happen and the tool results are all the same hash, the
	// stagnant signal (no mutations + no new info) must fire.
	turns := make([][]ai.ToolCall, 10)
	for i := range turns {
		turns[i] = []ai.ToolCall{call(
			fmt.Sprintf("r%d", i), "sh",
			fmt.Sprintf(`{"cmd":"echo same # turn %d"}`, i))}
	}
	client := &scriptedCompleter{turns: turns}
	linear := NewLinear(client, space, nil, 100, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopNoProgress {
		t.Fatalf("stop = %s, want %s (turns=%d)", outcome.Stop, StopNoProgress, outcome.Turns)
	}
}

// The conclude directive gives the model one chance to land. A leaf that
// receives the directive and then delivers (no more tool calls) finishes
// normally — StopDone, not StopNoProgress.
func TestNoProgressConcludeDirectiveGivesOneChance(t *testing.T) {
	space := workspace(t)
	repeatCall := call("r1", "sh", `{"cmd":"cat data.txt"}`)
	// The repeat cap is 4. Turns 0-3 repeat the same call; the guard fires
	// the conclude directive after turn 3. Turn 4 is the model's chance to
	// comply — it delivers with no tool calls.
	turns := make([][]ai.ToolCall, 5)
	for i := range 4 {
		turns[i] = []ai.ToolCall{repeatCall}
	}
	// Turn 4: no tool calls (the model concludes).
	turns[4] = nil
	client := &scriptedCompleter{turns: turns}
	linear := NewLinear(client, space, nil, 100, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	// The model complied with the conclude directive, so it finishes done.
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %s, want done — the model complied with the directive", outcome.Stop)
	}
}

// Test the guard's observe function directly for the repeat signal.
func TestProgressGuardRepeatSignal(t *testing.T) {
	g := newProgressGuard()
	sameCall := call("c1", "sh", `{"cmd":"echo hi"}`)
	sameResult := Result{Content: "hi\n"}

	for i := range noProgressRepeatCap - 1 {
		if v := g.observe([]ai.ToolCall{sameCall}, []Result{sameResult}, 0, 0); v != progressContinue {
			t.Fatalf("observe returned %d at iteration %d, want continue", v, i)
		}
	}
	// The next identical call triggers the conclude signal.
	if v := g.observe([]ai.ToolCall{sameCall}, []Result{sameResult}, 0, 0); v != progressConclude {
		t.Fatalf("observe returned %d at the cap, want conclude", v)
	}
}

// Test the guard's observe function directly for the stagnant signal.
func TestProgressGuardStagnantSignal(t *testing.T) {
	g := newProgressGuard()
	// Each turn calls a different tool (so the repeat signal doesn't fire)
	// but returns the same result and never writes. The first call's result
	// is "new" (newInfo=true), so stagnant counting starts from the second.
	for i := range noProgressStagnantCap {
		c := call(fmt.Sprintf("c%d", i), "sh", fmt.Sprintf(`{"cmd":"echo hi %d"}`, i))
		if v := g.observe([]ai.ToolCall{c}, []Result{{Content: "same"}}, 0, 0); v != progressContinue {
			t.Fatalf("observe returned %d at iteration %d, want continue", v, i)
		}
	}
	// The next stagnant turn triggers the conclude signal.
	c := call(fmt.Sprintf("c%d", noProgressStagnantCap), "sh", `{"cmd":"echo hi 99"}`)
	if v := g.observe([]ai.ToolCall{c}, []Result{{Content: "same"}}, 0, 0); v != progressConclude {
		t.Fatalf("observe returned %d at the stagnant cap, want conclude", v)
	}
}

// Test that a mutation resets the stagnant counter.
func TestProgressGuardMutationResetsStagnant(t *testing.T) {
	g := newProgressGuard()
	// Accumulate stagnant turns just below the cap.
	for i := range noProgressStagnantCap {
		c := call(fmt.Sprintf("r%d", i), "sh", fmt.Sprintf(`{"cmd":"cat %d"}`, i))
		g.observe([]ai.ToolCall{c}, []Result{{Content: "same"}}, 0, 0)
	}
	// A write turn resets the stagnant counter.
	writeCall := call("w1", "write", `{"path":"out.txt","text":"x"}`)
	if v := g.observe([]ai.ToolCall{writeCall}, []Result{{Content: "ok"}}, 0, 1); v != progressContinue {
		t.Fatalf("observe returned %d after a mutation, want continue", v)
	}
	// The stagnant counter was reset, so one more stagnant turn does not fire.
	c := call("r2", "sh", `{"cmd":"cat data"}`)
	if v := g.observe([]ai.ToolCall{c}, []Result{{Content: "same"}}, 1, 1); v != progressContinue {
		t.Fatalf("observe returned %d after reset, want continue", v)
	}
}

// Test the turn-floor signal: it fires on a fidgeting leaf and leaves a
// working one alone.
func TestProgressGuardTurnFloor(t *testing.T) {
	// A working leaf — a write every turn — crosses the floor untouched: the
	// floor reads the recent window, and a productive window is never
	// floor-stopped.
	working := newProgressGuard()
	for i := range noProgressTurnFloor + 5 {
		c := call(fmt.Sprintf("w%d", i), "write",
			fmt.Sprintf(`{"path":"f%d.txt","text":"x"}`, i))
		if v := working.observe([]ai.ToolCall{c}, []Result{{Content: "ok"}}, i, i+1); v != progressContinue {
			t.Fatalf("working leaf: observe returned %d at turn %d, want continue", v, i)
		}
	}

	// A fidgeting leaf — one productive turn every six, flat re-reads between,
	// just enough to keep the stagnant and repeat caps from firing — is
	// concluded once it crosses the floor.
	g := newProgressGuard()
	flatA := call("a", "read", `{"path":"a.txt"}`)
	flatB := call("b", "read", `{"path":"b.txt"}`)
	flat := []struct {
		c ai.ToolCall
		r Result
	}{{flatA, Result{Content: "aaa"}}, {flatB, Result{Content: "bbb"}}}
	turn := 0
	artifacts := 0
	step := func(c ai.ToolCall, r Result, wrote bool) progressVerdict {
		before := artifacts
		if wrote {
			artifacts++
		}
		v := g.observe([]ai.ToolCall{c}, []Result{r}, before, artifacts)
		turn++
		return v
	}
	for turn < noProgressTurnFloor-1 {
		w := call(fmt.Sprintf("w%d", turn), "write", fmt.Sprintf(`{"path":"g%d.txt","text":"x"}`, turn))
		if v := step(w, Result{Content: "ok"}, true); v != progressContinue {
			t.Fatalf("fidgeting leaf: observe returned %d at turn %d, want continue", v, turn-1)
		}
		for k := 0; k < 5 && turn < noProgressTurnFloor-1; k++ {
			f := flat[k%2]
			if v := step(f.c, f.r, false); v != progressContinue {
				t.Fatalf("fidgeting leaf: observe returned %d at turn %d, want continue", v, turn-1)
			}
		}
	}
	w := call("w-final", "write", `{"path":"final.txt","text":"x"}`)
	if v := step(w, Result{Content: "ok"}, true); v != progressConclude {
		t.Fatalf("fidgeting leaf: observe returned %d at the floor, want conclude", v)
	}
}

// Test the grace period after the conclude directive.
func TestProgressGuardGracePeriod(t *testing.T) {
	g := newProgressGuard()
	sameCall := call("c1", "sh", `{"cmd":"echo hi"}`)
	sameResult := Result{Content: "hi\n"}

	// Trigger the conclude signal.
	for i := 0; i < noProgressRepeatCap; i++ {
		g.observe([]ai.ToolCall{sameCall}, []Result{sameResult}, 0, 0)
	}
	g.markConcluded()

	// The model keeps spinning — the guard should terminate after the grace
	// period (landingTurns). Each turn the model makes the same tool call
	// with the same result; the repeat signal fires immediately because the
	// guard was already concluded.
	if v := g.observe([]ai.ToolCall{sameCall}, []Result{sameResult}, 0, 0); v != progressTerminate {
		t.Fatalf("first post-conclude observe = %d, want terminate (repeat re-trigger)", v)
	}
}
