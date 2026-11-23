package workgraph

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// buildShapes are ten-thousand-task inputs: what a board is tested at, the same
// with every task waiting on forty others, and the same with the longest titles,
// briefs and path lists a node may carry.
func buildShapes() map[string]Input {
	dense := scaleInput(10, 1000)
	for i := range dense.Facts {
		owner := dense.Facts[i].Node.Ref.Owner
		for k := 1; k <= 40 && k < i%1000; k++ {
			dense.Facts[i].Waits = append(dense.Facts[i].Waits, Wait{On: TaskRef{Owner: owner, Task: fmt.Sprint(k)}, Via: ViaRecord})
		}
	}
	long := scaleInput(10, 1000)
	for i := range long.Facts {
		n := &long.Facts[i].Node
		n.Instructions = &GraphText{Source: "admitted_brief", Text: strings.Repeat("x", 1000)}
		n.Title = strings.Repeat("t", 160)
		for k := 0; k < 50; k++ {
			n.Files.Paths = append(n.Files.Paths, fmt.Sprintf("internal/some/long/path/file%d.go", k))
		}
	}
	return map[string]Input{"normal": scaleInput(10, 1000), "dense": dense, "long": long}
}

func inputWeight(in Input) (weight, facts int64) {
	for i := range in.Facts {
		weight += BuildWeight(&in.Facts[i])
		facts += FactBytes(&in.Facts[i])
	}
	return weight, facts
}

// THE WEIGHT IS A CEILING ON THE ALLOCATION IT ADMITS, measured on three shapes.
func TestBuildWeightIsNeverBelowWhatBuildAllocates(t *testing.T) {
	for name, in := range buildShapes() {
		weight, facts := inputWeight(in)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		snap, err := Build(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		alloc := int64(after.TotalAlloc - before.TotalAlloc)
		runtime.KeepAlive(snap)
		t.Logf("%s 10K: facts %d KiB, build allocated %d KiB, weight %d KiB (x%.2f)", name, facts>>10, alloc>>10, weight>>10, float64(weight)/float64(alloc))
		if weight < alloc {
			t.Fatalf("%s: weight %d is under the %d bytes the build allocated", name, weight, alloc)
		}
	}
}

// A BOARD-SIZED SCOPE FITS: ten thousand ordinary tasks are admitted under the
// build gate the app runs with, and weigh no more than the gate.
func TestTenThousandOrdinaryTasksFitTheBuildGate(t *testing.T) {
	weight, _ := inputWeight(buildShapes()["normal"])
	if gate := int64(64 << 20); weight > gate {
		t.Fatalf("ten thousand ordinary tasks weigh %d MiB against a %d MiB gate", weight>>20, gate>>20)
	}
}

// A gate never runs what exceeds it: the answer is a typed refusal, before
// anything is built.
func TestAWeightOverTheGateIsRefusedTypedAndNeverRun(t *testing.T) {
	c := NewCache(CacheLimits{Bytes: 1 << 30, Entries: 8, Pending: 4, BuildBytes: 1000})
	ran := false
	_, err := c.GetWeighted(context.Background(), scopeN(1), "r", 1001, func(context.Context) (Input, error) { ran = true; return Input{}, nil })
	var tl *TooLarge
	if !errors.Is(err, ErrTooLarge) || !errors.As(err, &tl) || tl.Need != 1001 || tl.Limit != 1000 || tl.Reason != NarrowScope || ran {
		t.Fatalf("err = %v (%+v) ran %v", err, tl, ran)
	}
	if c.Pending() != 0 || c.TooLarge() != 1 {
		t.Fatalf("pending %d tooLarge %d", c.Pending(), c.TooLarge())
	}
	if _, err := NewGate(10).Acquire(context.Background(), 11); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("gate err = %v", err)
	}
}
