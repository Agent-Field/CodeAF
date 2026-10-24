//go:build !windows

package bare

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func bareBash(t *testing.T) Tool {
	t.Helper()
	for _, tool := range AllTools(t.TempDir()) {
		if tool.Name == "bash" {
			return tool
		}
	}
	t.Fatal("no bash tool")
	return Tool{}
}

// A COMMAND A SIGNAL KILLED IS NOT A COMMAND THAT SUCCEEDED. The shell kills
// itself here, outside any deadline the call set, and the answer is the
// shell's own 128+signal — never the success a -1 used to read as.
func TestASignalledCommandIsAFailureWithTheShellsCode(t *testing.T) {
	bash := bareBash(t)
	for signal, want := range map[string]string{"KILL": "137", "TERM": "143"} {
		args, _ := json.Marshal(map[string]string{"command": "echo before; kill -" + signal + " $$; echo after"})
		text, isError, err := bash.Execute(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		if !isError || !strings.HasSuffix(strings.TrimSpace(text), "Command exited with code "+want) {
			t.Fatalf("SIG%s answered isError=%v %q", signal, isError, text)
		}
		if !strings.Contains(text, "before") || strings.Contains(text, "after") {
			t.Fatalf("SIG%s output %q", signal, text)
		}
	}
	// An ordinary exit keeps its own code, and a clean one is still clean.
	args, _ := json.Marshal(map[string]string{"command": "exit 3"})
	if text, isError, _ := bash.Execute(context.Background(), args); !isError || !strings.HasSuffix(strings.TrimSpace(text), "code 3") {
		t.Fatalf("exit 3 answered %v %q", isError, text)
	}
	args, _ = json.Marshal(map[string]string{"command": "true"})
	if _, isError, _ := bash.Execute(context.Background(), args); isError {
		t.Fatal("true answered as a failure")
	}
}

// THE WAIT'S OWN ANSWERS: a signal is 128+signal, a WaitDelay is a process that
// exited zero, and anything else is unknown — never zero.
func TestExitCodeFromWaitNeverInventsAZero(t *testing.T) {
	cmd := exec.Command("bash", "-c", "kill -KILL $$")
	if code := exitCodeFromWait(cmd.Run()); code != 137 {
		t.Fatalf("SIGKILL read as %d", code)
	}
	if code := exitCodeFromWait(exec.ErrWaitDelay); code != 0 {
		t.Fatalf("WaitDelay read as %d", code)
	}
	if code := exitCodeFromWait(errors.New("wait: no child processes")); code != exitUnknown {
		t.Fatalf("an unknown wait read as %d", code)
	}
}

func TestPipedFailureIsNotReportedAsSuccess(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"command": "printf 'check failed\\n'; false | tail -20"})
	text, isError, err := bareBash(t).Execute(context.Background(), args)
	if err != nil || !isError || !strings.Contains(text, "Command exited with code 1") {
		t.Fatalf("failed check masked: %v %v %q", err, isError, text)
	}
}
