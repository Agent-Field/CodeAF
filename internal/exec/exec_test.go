package exec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

func workspace(t *testing.T) *Workspace {
	t.Helper()
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return space
}

// TestClampKeepsBothEnds guards the truncation rule. Keeping only the head is
// the obvious implementation and loses the most valuable line: a command's
// verdict is at the end, so head-only truncation reliably discards the error
// that made the output worth reading.
func TestClampKeepsBothEnds(t *testing.T) {
	body := strings.Repeat("a", maxToolResultBytes) + "FATAL: the thing that matters"
	clamped := clamp(body)

	if len(clamped) > maxToolResultBytes+128 {
		t.Errorf("clamped to %d bytes, want about %d", len(clamped), maxToolResultBytes)
	}
	if !strings.Contains(clamped, "FATAL: the thing that matters") {
		t.Error("truncation dropped the tail, which is where errors live")
	}
	if !strings.HasPrefix(clamped, "aaa") {
		t.Error("truncation dropped the head")
	}
	if !strings.Contains(clamped, "elided") {
		t.Error("truncation did not say that anything was removed")
	}
}

// TestToolFailuresAreResults is the rule that keeps a run alive. A mistyped
// path or a failing command has to come back as something the model can read
// and correct; returning a Go error instead throws away every turn before it.
func TestToolFailuresAreResults(t *testing.T) {
	tools := NewToolbox(workspace(t), 1, nil)
	ctx := context.Background()

	cases := []struct{ name, tool, args, want string }{
		{"unknown tool", "nope", `{}`, "sh, write, edit, web"},
		{"bad json", "sh", `{oops`, "valid JSON"},
		{"missing file", "edit", `{"path":"none.md","old":"x","new":"y"}`, "could not read"},
		{"failing command", "sh", `{"cmd":"exit 3"}`, "exit"},
		{"escaping path", "write", `{"path":"../outside.md","text":"x"}`, "escapes the workspace"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			result := tools.Execute(ctx, item.tool, item.args)
			if !result.IsError {
				t.Errorf("expected an error result, got %q", result.Content)
			}
			if !strings.Contains(result.Content, item.want) {
				t.Errorf("result %q does not mention %q", result.Content, item.want)
			}
		})
	}
}

// TestEditRefusesAmbiguousMatch keeps a silent wrong edit from happening.
// Replacing the first of several matches looks like success and is the hardest
// kind of mistake to notice later.
func TestEditRefusesAmbiguousMatch(t *testing.T) {
	space := workspace(t)
	path := filepath.Join(space.Root(), "doc.md")
	if err := os.WriteFile(path, []byte("alpha\nalpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tools := NewToolbox(space, 1, nil)

	result := tools.Execute(context.Background(), "edit", `{"path":"doc.md","old":"alpha","new":"beta"}`)
	if !result.IsError || !strings.Contains(result.Content, "appears 2 times") {
		t.Fatalf("ambiguous edit was not refused: %+v", result)
	}
	body, _ := os.ReadFile(path)
	if string(body) != "alpha\nalpha\n" {
		t.Errorf("file was modified despite the refusal: %q", body)
	}
}

// TestWriteRecordsArtifact checks the other half of the result contract: files
// are tracked so dependents can be pointed at them instead of being handed the
// whole text.
func TestWriteRecordsArtifact(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, 7, nil)

	result := tools.Execute(context.Background(), "write", `{"path":"report.md","text":"body"}`)
	if result.IsError {
		t.Fatalf("write failed: %s", result.Content)
	}
	if got := space.Artifacts(7); len(got) != 1 || got[0] != "report.md" {
		t.Errorf("artifacts = %v, want [report.md]", got)
	}
	if got := space.Artifacts(8); len(got) != 0 {
		t.Errorf("artifact leaked to another node: %v", got)
	}
}

// TestSchedulerDispatchAndBlocking covers the two behaviours that decide
// whether a run is worth anything: a node with no inputs must not wait for
// anything, and one failure must cost only its own descendants.
func TestSchedulerDispatchAndBlocking(t *testing.T) {
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	independent := graph.Add(plan.Node{Stage: 1, Title: "Independent"})
	doomed := graph.Add(plan.Node{Stage: 1, Title: "Doomed"})
	downstream := graph.Add(plan.Node{Stage: 1, Title: "Downstream"})
	if err := graph.AddNeed(downstream, doomed); err != nil {
		t.Fatal(err)
	}

	fake := &scriptedExecutor{fail: map[int]bool{doomed: true}}
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 4)
	if err := scheduler.Run(context.Background(), graph); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if state := graph.Node(independent).State; state != plan.StateDone {
		t.Errorf("independent node = %s, want done — an unrelated failure stopped it", state)
	}
	if state := graph.Node(doomed).State; state != plan.StateFailed {
		t.Errorf("failing node = %s, want failed", state)
	}
	if state := graph.Node(downstream).State; state != plan.StateBlocked {
		t.Errorf("downstream node = %s, want blocked", state)
	}
	if !fake.ranBefore(independent, doomed) && !fake.ran[independent] {
		t.Error("independent node never ran")
	}
}

// TestSchedulerRoutesOnlyDeclaredInputs is the runtime half of the design's
// central claim: the edge list is a context router, so a node must receive its
// declared inputs and nothing else.
func TestSchedulerRoutesOnlyDeclaredInputs(t *testing.T) {
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	wanted := graph.Add(plan.Node{Stage: 1, Title: "Wanted"})
	graph.Add(plan.Node{Stage: 1, Title: "Unrelated"})
	consumer := graph.Add(plan.Node{Stage: 1, Title: "Consumer"})
	if err := graph.AddNeed(consumer, wanted); err != nil {
		t.Fatal(err)
	}

	fake := &scriptedExecutor{}
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 4)
	if err := scheduler.Run(context.Background(), graph); err != nil {
		t.Fatalf("Run: %v", err)
	}

	inputs := fake.inputs[consumer]
	if len(inputs) != 1 || inputs[0].Title != "Wanted" {
		t.Errorf("consumer received %v, want only the declared input", titlesOf(inputs))
	}
}

// scriptedExecutor stands in for the real loop. It is mutex-guarded because the
// scheduler calls executors concurrently — the race detector caught this double
// without one, which is the same mistake a real executor could make.
type scriptedExecutor struct {
	mutex  sync.Mutex
	fail   map[int]bool
	ran    map[int]bool
	order  []int
	inputs map[int][]Input
}

func (s *scriptedExecutor) Skill() string { return "linear" }

func (s *scriptedExecutor) Run(ctx context.Context, task Task) (*Outcome, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.ran == nil {
		s.ran = map[int]bool{}
		s.inputs = map[int][]Input{}
	}
	s.ran[task.NodeID] = true
	s.order = append(s.order, task.NodeID)
	s.inputs[task.NodeID] = task.Inputs
	if s.fail[task.NodeID] {
		return &Outcome{Stop: StopError}, context.Canceled
	}
	return &Outcome{Text: "result of " + task.Title, Turns: 1, Stop: StopDone, Elapsed: time.Millisecond}, nil
}

func (s *scriptedExecutor) ranBefore(first, second int) bool {
	for _, id := range s.order {
		if id == first {
			return true
		}
		if id == second {
			return false
		}
	}
	return false
}

func titlesOf(inputs []Input) []string {
	titles := make([]string, len(inputs))
	for index, input := range inputs {
		titles[index] = input.Title
	}
	return titles
}
