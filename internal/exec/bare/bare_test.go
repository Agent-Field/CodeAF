package bare

import (
	"context"
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
		client:   completer,
		tools:    Tools(root),
		produced: func(mark time.Time) { workspace.RecordProducedSince(leaf, mark) },
		system:   SystemPrompt(root),
		user:     "create hello.txt",
		cwd:      root,
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
