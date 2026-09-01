package resident

import (
	"context"
	"strings"
	"testing"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ranOutJob is a single claimable leaf for the runner to settle one way or the
// other.
func ranOutJob(t *testing.T) *store.Store {
	t.Helper()
	graph := openRunnerStore(t)
	spliceOneLeaf(t, graph, "task-1")
	return graph
}

// THE SEAM. A leaf that ran out of its tokens reaches the scheduler with err ==
// nil — running out is not an error, the executor grants a landing reserve and
// the leaf lands — and the ending has to cross with it or the scheduler cannot
// tell it from a leaf that finished.
//
// `resident.ExecResult` carried a summary, some money and some turns and nothing
// about how the work ENDED, so `runOne` saw no error and called `graph.Complete`.
// One earlier attempt at this defect added the fields and never populated them at
// the construction site, which is dead code that reads like a fix; the fields are
// asked for here through the same door production fills them through.
func TestALeafThatRanOutOfItsBudgetIsNotCompleted(t *testing.T) {
	graph := ranOutJob(t)
	if err := graph.RecordTranscript("task-1", "worker/model", []store.TranscriptEntry{
		{Turn: 1, Kind: store.TranscriptAssistant, Text: "editing runner.go"},
	}); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{
			Summary: "All 722 tests pass. Let me verify the dry-run tests specifically:",
			Stopped: true, Stop: executor.StopBudget,
			Meter: executor.Meter{Name: executor.MeterCost, Reached: 199131,
				Allowed: 176834, Unit: "tokens of billed work"},
		}, nil
	}, "budget-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()

	node, ok, err := graph.Node("task-1")
	if err != nil || !ok {
		t.Fatalf("node: ok=%v err=%v", ok, err)
	}
	if node.Status == store.Done {
		t.Fatalf("a leaf that ran out mid-edit was settled done on its own last sentence: %+v", node)
	}
	if node.Status != store.Pending {
		t.Fatalf("a leaf that ran out was settled %q, want it back on the queue", node.Status)
	}
	if node.Summary != "" {
		t.Fatalf("a cut leaf's own words were kept as the node's account: %q", node.Summary)
	}
}

// And the ending is only news where nothing else is carrying the work. A leaf
// whose remainder was spliced has a successor holding it, and the node itself is
// finished with — which is what `Continued` says and why `RanOut` asks it.
func TestALeafWhoseRemainderWasSplicedStillSettles(t *testing.T) {
	graph := ranOutJob(t)
	runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{Summary: "as far as it got", Stopped: true,
			Stop: executor.StopBudget, Continued: true}, nil
	}, "continued-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	node, ok, err := graph.Node("task-1")
	if err != nil || !ok {
		t.Fatalf("node: ok=%v err=%v", ok, err)
	}
	if node.Status != store.Done {
		t.Fatalf("a leaf whose work was already spliced on was left %q", node.Status)
	}
}

// A leaf that finished is settled exactly as it always was, and an ExecResult
// that says nothing about its ending is not read as one that ran out. A
// StopReason is a string and its zero value must never mean "it ran out".
func TestAnEndingNobodyRecordedIsNotReadAsRunningOut(t *testing.T) {
	for _, probe := range []struct {
		name   string
		result ExecResult
	}{
		{"finished", ExecResult{Summary: "done", Stopped: true, Stop: executor.StopDone}},
		{"nothing recorded", ExecResult{Summary: "done"}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			graph := ranOutJob(t)
			runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
				return probe.result, nil
			}, "done-runner", 1)
			if _, err := runner.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			runner.Wait()
			node, ok, err := graph.Node("task-1")
			if err != nil || !ok {
				t.Fatalf("node: ok=%v err=%v", ok, err)
			}
			if node.Status != store.Done {
				t.Fatalf("a leaf that finished was left %q", node.Status)
			}
		})
	}
}

// The release carries the turns the attempt banked, because the next claim
// resumes from them and the headless stream tells a requeue that is PROGRESS
// from a claim taken off a worker that never answered.
func TestTheReleaseOfACutLeafCarriesItsRecordedTurns(t *testing.T) {
	graph := ranOutJob(t)
	if err := graph.RecordTranscript("task-1", "worker/model", []store.TranscriptEntry{
		{Turn: 1, Kind: store.TranscriptAssistant, Text: "reading the runner"},
		{Turn: 2, Kind: store.TranscriptAssistant, Text: "editing the runner"},
		{Turn: 3, Kind: store.TranscriptAssistant, Text: "still editing"},
	}); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(graph, func(context.Context, store.Node) (ExecResult, error) {
		return ExecResult{Summary: "cut off", Stopped: true, Stop: executor.StopBudget,
			Meter: executor.Meter{Name: executor.MeterCost, Reached: 199131, Allowed: 176834,
				Unit: "tokens of billed work"}}, nil
	}, "release-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()

	events, err := graph.Events(0, 200)
	if err != nil {
		t.Fatal(err)
	}
	released := ""
	for _, event := range events {
		if event.Kind == store.EventNodeReleased && event.NodeID == "task-1" {
			released = string(event.Payload)
		}
	}
	if released == "" {
		t.Fatal("the node went back on the queue with no reason on the release")
	}
	if !strings.Contains(released, "still working when it ran out") {
		t.Fatalf("the release does not say what happened: %s", released)
	}
	if !strings.Contains(released, "199131") {
		t.Fatalf("the release does not carry what ran out: %s", released)
	}
	if !strings.Contains(released, `"recorded":3`) {
		t.Fatalf("the release does not carry the turns the next attempt resumes from: %s", released)
	}
}
