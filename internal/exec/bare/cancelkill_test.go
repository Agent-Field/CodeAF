//go:build !windows

package bare

// A CANCELLED COMMAND IS GONE, NOT ONLY ANSWERED FOR.
//
// cancelwait_test.go measures how soon a cancelled call comes BACK. These two
// measure the other half, which nothing did: that the command the call started
// is no longer running. A call that answers "Command aborted" over a shell that
// is still looping is the fault: the shell is a detached session leader, so it
// outlives the worker, the run and the folder it was started in.
//
// THE PROCESS IS FOUND THROUGH THE PROMOTER'S DOOR, which is handed the running
// call the moment it starts. A pid the command wrote to a file would not do for
// the first test, where the kill may land before the shell has run a line.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// pidWitness is a promoter that adopts nothing and remembers the process.
type pidWitness struct {
	mu  sync.Mutex
	pid int
}

func (w *pidWitness) Started(call *BashCall) func() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if process := call.Process().Process; process != nil {
		w.pid = process.Pid
	}
	return nil
}

func (w *pidWitness) TimedOut(*BashCall) bool { return false }

func (w *pidWitness) seen() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pid
}

func bashOnTheBareBelt(t *testing.T) Tool {
	t.Helper()
	for _, tool := range AllTools(t.TempDir()) {
		if tool.Name == "bash" {
			return tool
		}
	}
	t.Fatal("no bash tool on the bare belt")
	return Tool{}
}

// goneWithin reports whether the command's process group has no member left,
// asking until the wait is over. The reaper runs behind the call, so a moment
// is allowed for it.
func goneWithin(pid int, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for processgroup.Alive(pid) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
	return true
}

var loopForever = func() json.RawMessage {
	args, _ := json.Marshal(map[string]any{
		"command": `while true; do echo turning; sleep 0.05; done`,
		"timeout": 120,
	})
	return args
}()

// TestACommandStartedUnderAContextAlreadyDoneIsEnded is the cancel that arrives
// as the command starts, at its limit: the context is done before the call is
// made. The call's wait returns at once, and the watcher may not have reached
// its select by then. Two hundred times, because the fault was a coin.
func TestACommandStartedUnderAContextAlreadyDoneIsEnded(t *testing.T) {
	bash := bashOnTheBareBelt(t)
	for round := 1; round <= 200; round++ {
		witness := &pidWitness{}
		ctx, cancel := context.WithCancel(WithBashPromoter(context.Background(), witness))
		cancel()
		text, bad, _ := bash.Execute(ctx, loopForever)
		if !bad || !strings.Contains(text, "aborted") {
			t.Fatalf("round %d: the call reported %q (isError=%v), want it named as aborted", round, text, bad)
		}
		pid := witness.seen()
		if pid == 0 {
			t.Fatalf("round %d: the command never started, so the round proved nothing", round)
		}
		if !goneWithin(pid, 5*time.Second) {
			// It is this test's own process, by the pid it was handed, and it
			// must not outlive the test that found it running.
			_ = processgroup.Kill(pid)
			t.Fatalf("round %d: the call came back aborted and its command (pid %d) was still running", round, pid)
		}
	}
}

// TestACommandCancelledWhileItRunsIsEnded is the ordinary stop: the command is
// well under way, the context is cancelled, and the process is gone as well as
// the call answered.
func TestACommandCancelledWhileItRunsIsEnded(t *testing.T) {
	bash := bashOnTheBareBelt(t)
	witness := &pidWitness{}
	ctx, cancel := context.WithCancel(WithBashPromoter(context.Background(), witness))
	defer cancel()

	type answer struct {
		text string
		bad  bool
	}
	done := make(chan answer, 1)
	go func() {
		text, bad, _ := bash.Execute(ctx, loopForever)
		done <- answer{text, bad}
	}()

	started := time.Now()
	for witness.seen() == 0 {
		if time.Since(started) > 10*time.Second {
			t.Fatal("the command never started")
		}
		time.Sleep(2 * time.Millisecond)
	}
	pid := witness.seen()
	if !processgroup.Alive(pid) {
		t.Fatalf("the command (pid %d) was not running before the cancel, so the test proves nothing", pid)
	}
	cancel()

	select {
	case got := <-done:
		if !got.bad || !strings.Contains(got.text, "aborted") {
			t.Fatalf("the cancelled call reported %q (isError=%v), want it named as aborted", got.text, got.bad)
		}
	case <-time.After(10 * time.Second):
		_ = processgroup.Kill(pid)
		t.Fatal("the cancelled call never came back")
	}
	if !goneWithin(pid, 5*time.Second) {
		_ = processgroup.Kill(pid)
		t.Fatalf("the call came back aborted and its command (pid %d) was still running", pid)
	}
}
