package bare

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rejectionTool(t *testing.T, dir, name string) Tool {
	t.Helper()
	for _, tool := range AllTools(dir) {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("missing %s", name)
	return Tool{}
}

func TestArgumentRejectionComesFromBuiltinParserNotResultWords(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"read", "bash", "edit", "write", "grep", "find", "ls"} {
		tool := rejectionTool(t, dir, name)
		old, oldBad, oldErr := tool.Execute(context.Background(), json.RawMessage(`{"broken":`))
		ctx, mark := WithArgumentRejection(context.Background())
		text, bad, err := tool.Execute(ctx, json.RawMessage(`{"broken":`))
		if !mark.RejectedBeforeRun() || !bad || err != nil || text != old || bad != oldBad || oldErr != nil {
			t.Fatalf("%s changed result or missed parse refusal: %q %v %v", name, text, bad, err)
		}
	}
	if files, _ := os.ReadDir(dir); len(files) != 0 {
		t.Fatal("a malformed request touched files")
	}
	ctx, mark := WithArgumentRejection(context.Background())
	text, bad, err := rejectionTool(t, dir, "bash").Execute(ctx, json.RawMessage(`{"command":"printf 'Invalid arguments: emitted by the actual command'; exit 7"}`))
	if err != nil || !bad || mark.RejectedBeforeRun() || !strings.Contains(text, "Invalid arguments: emitted by the actual command") {
		t.Fatalf("executed command misclassified %q %v %v", text, bad, err)
	}
}

func TestArgumentRejectionMarksKnownValidationBeforeOperation(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, args string }{{"bash", `{"command":"touch should-not-exist","timeout":-1}`}, {"edit", `{"path":"missing","edits":[]}`}} {
		ctx, mark := WithArgumentRejection(context.Background())
		_, bad, err := rejectionTool(t, dir, tc.name).Execute(ctx, json.RawMessage(tc.args))
		if !mark.RejectedBeforeRun() || !bad || err != nil {
			t.Fatalf("%s validation not typed", tc.name)
		}
	}
	if files, _ := os.ReadDir(dir); len(files) != 0 {
		t.Fatal("pre-operation validation wrote files")
	}
}

func TestArgumentRejectionDoesNotRelabelAnActuallyCanceledProcess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, mark := WithArgumentRejection(ctx)
	command, _ := json.Marshal(map[string]any{"command": "printf ran > started; sleep 10"})
	done := make(chan bool, 1)
	tool := rejectionTool(t, dir, "bash")
	go func() { _, bad, _ := tool.Execute(ctx, command); done <- bad }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("process never started")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case bad := <-done:
		if !bad || mark.RejectedBeforeRun() {
			t.Fatal("running process cancellation became argument rejection")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("process did not settle")
	}
}
