package session

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"sync/atomic"
	"testing"
	"time"
)

// countingWatch is a [treeWatch] whose reading is a counter rather than a git
// process, so a test can say exactly how many times the tree was asked. Each
// reading answers a fingerprint nobody has seen, which is a tree that moved.
func countingWatch(t *testing.T) (*treeWatch, *atomic.Int64) {
	t.Helper()
	var readings atomic.Int64
	watch := newTreeWatch(t.TempDir())
	watch.read = func(context.Context, string) string {
		n := readings.Add(1)
		return string(rune('a' + n))
	}
	t.Cleanup(watch.close)
	return watch, &readings
}

// batch runs one batch of calls through the run's bookkeeping the way
// [childRun.observe] does — every begin, then every end — and answers which of
// its calls were credited with moving the tree.
func batch(run *childRun, calls ...Event) []bool {
	for range calls {
		run.batchBegan()
	}
	moved := make([]bool, len(calls))
	for index, call := range calls {
		run.batchEnded()
		_, wrote := changedPath(call, "")
		moved[index] = run.batchMoved(call, wrote && call.Kind == EventToolEnd)
	}
	return moved
}

// THE LAW: A BATCH ASKS ITS TREE AT MOST ONCE, AND A BATCH OF READING HANDS NEVER.
//
// The runner used to run `git status` for every finished call, and every one of
// them read the same tree — a batch's ends arrive together, after the batch has
// run — so eight calls were eight processes answering one question, the first
// of which (a `read`, as often as not) was credited with whatever the batch did.
func TestABatchAsksItsTreeOnceAndReadingHandsNever(t *testing.T) {
	watch, readings := countingWatch(t)
	run := &childRun{tree: watch}

	read := Event{Kind: EventToolEnd, Tool: "read", Args: `{"path":"a.go"}`}
	grep := Event{Kind: EventToolEnd, Tool: "grep", Args: `{"pattern":"x"}`}
	bash := Event{Kind: EventToolEnd, Tool: "bash", Args: `{"command":"go build ./..."}`}
	edit := Event{Kind: EventToolEnd, Tool: "edit", Args: `{"path":"a.go","oldText":"a","newText":"b"}`}
	refused := Event{Kind: EventToolFailed, Tool: "bash", Args: `{"command":"rm -rf /"}`, HarnessMade: true}

	// Exploring: nothing a reading hand does can move the tree, so nothing asks.
	if moved := batch(run, read, grep, read); moved[0] || moved[1] || moved[2] {
		t.Fatalf("a batch of reading hands was credited with moving the tree: %v", moved)
	}
	if n := readings.Load(); n != 0 {
		t.Fatalf("a batch of reading hands asked the tree %d times, want never", n)
	}

	// A read ahead of two commands: the first COMMAND asks, once, and is the one
	// that moved it — not the read that happened to end first.
	if moved := batch(run, read, bash, bash); moved[0] || !moved[1] || moved[2] {
		t.Fatalf("moved = %v, want the first command alone credited", moved)
	}
	if n := readings.Load(); n != 1 {
		t.Fatalf("one batch asked the tree %d times, want once", n)
	}

	// A save that names its file is its own evidence and asks nothing; neither
	// does a call the harness refused, which never reached the world.
	if moved := batch(run, edit, refused); moved[0] || moved[1] {
		t.Fatalf("moved = %v; a named save and a refusal need no reading", moved)
	}
	if n := readings.Load(); n != 1 {
		t.Fatalf("a named save or a refusal asked the tree; readings = %d, want still 1", n)
	}

	// And the next batch has its own one reading to spend.
	batch(run, bash)
	if n := readings.Load(); n != 2 {
		t.Fatalf("the next batch's command did not ask the tree; readings = %d, want 2", n)
	}
}

// A READING THE RUNNER CANNOT HAVE YET DOES NOT HOLD IT. The drain publishes
// the node's room and has to reach the end of the stream before the node can
// finish, so a tree git cannot read quickly costs it the bound nobody feels and
// no more — and the same reading answers the next time the tree is asked.
func TestATreeReadingThatIsNotInYetIsTakenByTheNextBatch(t *testing.T) {
	release := make(chan struct{})
	watch := newTreeWatch(t.TempDir())
	var readings atomic.Int64
	watch.read = func(context.Context, string) string {
		readings.Add(1)
		<-release
		return "moved"
	}
	t.Cleanup(watch.close)

	if watch.moved(time.Millisecond) {
		t.Fatal("a reading that had not come back was read as movement")
	}
	close(release)
	if !watch.moved(time.Minute) {
		t.Fatal("the reading that was still in flight did not answer the next ask")
	}
	if n := readings.Load(); n != 1 {
		t.Fatalf("the tree was read %d times, want the one reading carried over", n)
	}
}

// NOTHING THE WATCH STARTED OUTLIVES THE RUN. Closing it stops a reading still
// in flight — the context its git runs under — and waits for the goroutine,
// which is the run's join point ([runTaskChild]).
func TestClosingTheWatchStopsTheReadingInFlight(t *testing.T) {
	watch := newTreeWatch(t.TempDir())
	stopped := make(chan struct{})
	watch.read = func(ctx context.Context, _ string) string {
		<-ctx.Done()
		close(stopped)
		return ""
	}
	watch.moved(time.Millisecond)

	closed := make(chan struct{})
	go func() { watch.close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("closing the watch did not come back; its reading outlived it")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("the reading in flight was never told to stop")
	}
}

// THE RUNNER READS ITS TREE THROUGH ONE DOOR. A `git status` called from
// anywhere else in the run — from [childRun.step] above all, which is where the
// per-call reading lived — is the defect this watch replaced, so the only
// function in task_child_run.go that may name the fingerprint is the one that
// builds the watch.
func TestTheRunnerReadsItsTreeOnlyThroughTheWatch(t *testing.T) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "task_child_run.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name.Name == "newTreeWatch" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok && (ident.Name == "worktreeDirt" || ident.Name == "worktreeDirtIn") {
				t.Errorf("%s: %s reads the tree itself; a run reads it through its treeWatch, once a batch",
					files.Position(ident.Pos()), fn.Name.Name)
			}
			return true
		})
	}
}
