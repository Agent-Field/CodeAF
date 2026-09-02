package bare

// A CANCELLED BASH CALL COMES BACK AT ONCE, WITH WHAT IT HAD.
//
// This is issue #265's second rung, read from below. Until the cancellation arm
// in [newBashTool]'s wait, a cancelled call still waited for `cmd.Wait`, and
// `cmd.Wait` does not return while ANY holder of the output pipe is alive — a
// grandchild that escaped the process group holds it until `WaitDelay` forces
// the pipes shut three seconds later. Three seconds is not long, and it was
// three seconds of a person's stop that the person could not end.
//
// THE TEST BUILDS EXACTLY THAT GRANDCHILD, so what it measures is the wait and
// not the kill: the shell that ran the command is SIGKILLed with the group and
// dies at once, and a `setsid` child of it goes on writing into the same pipe
// afterwards.
//
// IT IS ALSO THE RACE TEST. Returning while the writer is live is precisely a
// second reader of the accumulator, which is why [outputAccumulator.sealed]
// exists; run this file with -race and the door is what keeps it green.

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestACancelledBashCallReturnsBeforeTheLeakedPipeIsClosed(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("no setsid: this test needs a grandchild that escapes the process group")
	}
	tools := AllTools(t.TempDir())
	var bash Tool
	for _, tool := range tools {
		if tool.Name == "bash" {
			bash = tool
		}
	}
	if bash.Execute == nil {
		t.Fatal("no bash tool on the bare belt")
	}

	// The grandchild inherits this command's stdout — the pipe Go handed the
	// shell — and leaves the process group, so killing the group does not reach
	// it and closing the shell does not close the pipe.
	args, err := json.Marshal(map[string]any{
		"command": `(setsid sh -c 'while true; do echo grandchild; sleep 0.05; done' &) ; ` +
			`while true; do echo parent; sleep 0.05; done`,
		"timeout": 120,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct {
		text string
		bad  bool
	}, 1)
	go func() {
		text, bad, _ := bash.Execute(ctx, args)
		done <- struct {
			text string
			bad  bool
		}{text, bad}
	}()

	// Long enough that the command is genuinely running and both writers have
	// put something in the pipe.
	time.Sleep(700 * time.Millisecond)
	cancelled := time.Now()
	cancel()

	select {
	case answer := <-done:
		// WELL INSIDE WaitDelay, which is the whole measurement: three seconds is
		// what this used to cost and one second is comfortably below it while
		// still leaving room for a loaded machine.
		if took := time.Since(cancelled); took > time.Second {
			t.Fatalf("the cancelled call took %s to come back; the wait is not armed", took)
		}
		if !answer.bad || !strings.Contains(answer.text, "aborted") {
			t.Fatalf("a cancelled call reported %q (isError=%v), want it named as aborted",
				answer.text, answer.bad)
		}
		// AND IT HANDS BACK WHAT THE COMMAND HAD SAID, which is the reason the
		// arm returns rather than simply erroring: a cut test runner still names
		// every check it reached.
		if !strings.Contains(answer.text, "parent") {
			t.Fatalf("the cancelled call threw away the output it had: %q", answer.text)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a cancelled bash call never came back")
	}
}
