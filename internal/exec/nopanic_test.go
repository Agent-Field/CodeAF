package exec

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
)

func quietExecLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	flags, writer := log.Flags(), log.Writer()
	log.SetOutput(buffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
	return buffer
}

// TestToolFaultBecomesAnErrorTheModelCanRead is the executor's half of the
// law: a tool that panics answers the model instead of killing the worker.
func TestToolFaultBecomesAnErrorTheModelCanRead(t *testing.T) {
	logged := quietExecLog(t)
	// A toolbox with no workspace behind it faults the moment a tool reaches
	// for one — standing in for any bug inside a tool call.
	tools := &Toolbox{}

	result := tools.Execute(context.Background(), "sh", `{"cmd":"echo hello"}`)

	if !result.IsError {
		t.Fatalf("a fault produced a non-error result: %+v", result)
	}
	if !strings.Contains(result.Content, "internal fault in this tool call") {
		t.Fatalf("result does not name the fault: %q", result.Content)
	}
	if !strings.Contains(logged.String(), "exec/tool sh") {
		t.Fatalf("the fault was not recorded to the log: %q", logged.String())
	}
}

// TestToolExecuteStillAnswersOrdinaryCalls proves the guard did not change
// what a working call returns.
func TestToolExecuteStillAnswersOrdinaryCalls(t *testing.T) {
	quietExecLog(t)
	workspace, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	tools := NewToolbox(workspace, "1", nil)
	result := tools.Execute(context.Background(), "sh", `{"cmd":"echo hello"}`)
	if result.IsError || !strings.Contains(result.Content, "hello") {
		t.Fatalf("ordinary call = %+v", result)
	}
}
