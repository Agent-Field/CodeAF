package orchestrate

// THE LOOP GUARD, at this layer: a planner that keeps aiming nodes at one file
// that keeps coming back unwritten is stopped, and the run says so and writes up
// what is actually there.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestARunStopsSendingOutAFileThatKeepsComingBackUnwritten(t *testing.T) {
	var (
		mu     sync.Mutex
		minted int
		ran    int
		brief  string
	)
	planner := plannerFunc(func(_ context.Context, _ View) (Amendment, error) {
		mu.Lock()
		minted++
		id := "n" + string(rune('0'+minted))
		mu.Unlock()
		return Amendment{Add: []Node{{
			ID: id, Goal: "write the final report to report.md", WriteScope: []string{"report.md"},
		}}}, nil
	})
	exec := execFunc(func(_ context.Context, n Node, _ []NodeStatus) (string, float64, error) {
		if n.ID == SynthesisID {
			mu.Lock()
			brief = n.Goal
			mu.Unlock()
			return "the write-up", 0, nil
		}
		mu.Lock()
		ran++
		mu.Unlock()
		return "INCOMPLETE: nothing was written", 0, errors.New("INCOMPLETE: nothing was written")
	})

	run := New("write up the research", planner, exec, Options{Cap: 5})
	snap, err := run.Run(testContext(t))
	if err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	attempts, wrote := ran, brief
	mu.Unlock()
	if attempts != RepeatLimit {
		t.Fatalf("the run burned %d workers on one brief, want %d", attempts, RepeatLimit)
	}
	if len(snap.Nodes) != RepeatLimit {
		t.Fatalf("the graph holds %d nodes: %+v", len(snap.Nodes), snap.Nodes)
	}
	if !snap.Done || snap.Stopped {
		t.Fatalf("a run that gave up is done and is not a stop: %+v", snap)
	}

	var honest bool
	for _, note := range snap.Notes {
		if strings.Contains(note, "report.md has been handed out 2 times and nothing was written to it") &&
			strings.Contains(note, "reports what is actually there") {
			honest = true
		}
	}
	if !honest {
		t.Fatalf("the run gave up quietly: %+v", snap.Notes)
	}
	if !strings.Contains(wrote, "incomplete") || !strings.Contains(wrote, "report.md") {
		t.Fatalf("the write-up was not asked to name the unwritten file: %q", wrote)
	}
	if snap.Answer != "the write-up" {
		t.Fatalf("a run that gave up still owes an answer, got %q", snap.Answer)
	}
}

// A FILE TWO NODES WROTE IS A FILE THE RUN IS MAKING PROGRESS ON. The guard
// counts nodes that FAILED at a path, never nodes that touched it: a third node
// editing a file two earlier ones wrote is ordinary work, and refusing it would
// make every multi-step deliverable impossible.
func TestTheRepeatGuardCountsFailuresAndNotVisits(t *testing.T) {
	run := New("edit the report", plannerFunc(func(context.Context, View) (Amendment, error) {
		return Amendment{}, nil
	}), execFunc(func(context.Context, Node, []NodeStatus) (string, float64, error) {
		return "", 0, nil
	}), Options{})

	scope := []string{"report.md"}
	run.nodes = []*NodeStatus{
		{Node: Node{ID: "n1", WriteScope: scope}, State: Done},
		{Node: Node{ID: "n2", WriteScope: scope}, State: Done},
	}
	if repeat := run.repeatedLocked(Node{ID: "n3", WriteScope: scope}); repeat != nil {
		t.Fatalf("ordinary follow-up work was refused: %v", repeat)
	}
	run.nodes[1].State = Failed
	if repeat := run.repeatedLocked(Node{ID: "n3", WriteScope: scope}); repeat != nil {
		t.Fatalf("one failure is news, not a pattern: %v", repeat)
	}
	run.nodes[0].State = Failed
	repeat := run.repeatedLocked(Node{ID: "n3", WriteScope: scope})
	if repeat == nil {
		t.Fatal("two failures at one path did not stop the third")
	}
	if repeat.scope != "report.md" || repeat.tries != 2 {
		t.Fatalf("the refusal is %+v", repeat)
	}
	// Read-only work is never counted and never refused: there is no file for it
	// to fail to produce.
	if got := run.repeatedLocked(Node{ID: "n4", Goal: "read the report"}); got != nil {
		t.Fatalf("read-only work was refused: %v", got)
	}
}
