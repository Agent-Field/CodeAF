package bare

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// What a bare leaf writes is filed under the leaf, the way exec.Toolbox files
// a shell command's output. The registry is what the delivery gate is shown:
// left empty, every bare leaf was convicted of not producing the file it had
// just produced, a second leaf was spliced in to produce it again, and the
// run told the person the file did not exist. Ten real runs out of ten.
func TestABareLeafFilesTheFilesItsToolsLeaveBehind(t *testing.T) {
	root := t.TempDir()
	workspace, err := exec.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	completer := &scriptedCompleter{t: t, turns: []oracleTurn{
		{toolCalls: []ai.ToolCall{{ID: "w1", Type: "function", Function: ai.ToolCallFunction{
			Name: "write", Arguments: `{"path":"hello.txt","content":"hello world"}`}}}},
		{text: "Done."},
	}}
	const leaf = "task-2"
	loop := &loopState{
		client: completer,
		tools:  Tools(root),
		sweep: func() func() {
			before := workspace.Snapshot()
			return func() { workspace.RecordProducedSince(leaf, before) }
		},
		system: SystemPrompt(root),
		user:   "create hello.txt",
		cwd:    root,
	}
	if outcome := loop.run(context.Background()); outcome.Stop != exec.StopDone {
		t.Fatalf("stop = %v, want done", outcome.Stop)
	}
	want := filepath.Join(root, "hello.txt")
	if got, err := os.ReadFile(want); err != nil || string(got) != "hello world" {
		t.Fatalf("hello.txt = %q, %v", got, err)
	}
	// The registry keeps paths relative to the workspace root, which is how
	// every other executor's writes are filed and read back.
	if filed := workspace.Artifacts(leaf); len(filed) != 1 || filed[0] != "hello.txt" {
		t.Fatalf("artifacts filed under the leaf = %v, want exactly hello.txt", filed)
	}
}

// The same thing again, with the clock lying about it.
//
// This test used to be the flaky one: the sweep kept whatever carried a write
// time at or after the instant the call began, and on a filesystem whose
// timestamps are coarser than the gap between "mark" and "write" the file's own
// stamp landed fractionally BEFORE the mark. Roughly one run in four filed
// nothing and the assertion above failed with an empty list.
//
// Here the tool backdates the file it wrote by an hour, which is that same
// failure made deterministic and made worse — and it also stands in for every
// real tool that preserves a timestamp it did not set (cp -p, git checkout,
// tar, rsync -t). The file is filed because the tree did not hold it before the
// call and holds it after, which is a fact about the world; the clock is not
// consulted. FAILSAFE.md rule 2.
func TestAFileTheClockCallsOldIsStillFiledWhenTheTreeGainedIt(t *testing.T) {
	root := t.TempDir()
	workspace, err := exec.NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	backdating := Tool{
		Name:   "write",
		Schema: json.RawMessage(`{"type":"object"}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			path := filepath.Join(root, "hello.txt")
			if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
				return "", true, err
			}
			old := time.Now().Add(-time.Hour)
			if err := os.Chtimes(path, old, old); err != nil {
				return "", true, err
			}
			return "wrote hello.txt", false, nil
		},
	}
	completer := &scriptedCompleter{t: t, turns: []oracleTurn{
		{toolCalls: []ai.ToolCall{{ID: "w1", Type: "function", Function: ai.ToolCallFunction{
			Name: "write", Arguments: `{}`}}}},
		{text: "Done."},
	}}
	const leaf = "task-3"
	loop := &loopState{
		client: completer,
		tools:  []Tool{backdating},
		sweep: func() func() {
			before := workspace.Snapshot()
			return func() { workspace.RecordProducedSince(leaf, before) }
		},
		system: SystemPrompt(root),
		user:   "create hello.txt",
		cwd:    root,
	}
	if outcome := loop.run(context.Background()); outcome.Stop != exec.StopDone {
		t.Fatalf("stop = %v, want done", outcome.Stop)
	}
	if filed := workspace.Artifacts(leaf); len(filed) != 1 || filed[0] != "hello.txt" {
		t.Fatalf("artifacts filed under the leaf = %v, want exactly hello.txt", filed)
	}
}
