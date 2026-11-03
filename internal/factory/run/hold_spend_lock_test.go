package run

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A child process bounds the failure even when both runner doors deadlock.
func TestHoldSpendAnswerLockOrder(t *testing.T) {
	if os.Getenv("CODEAF_HOLD_LOCK_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHoldSpendAnswerLockOrder$", "-test.timeout=8s")
		cmd.Env = append(os.Environ(), "CODEAF_HOLD_LOCK_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Hold and SpendAnswer must complete without deadlocking: %v\n%s", err, out)
		}
		return
	}

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.Add(context.Background(), factory.Item{Title: "hold and spending answer", Repo: "acme/api", State: factory.StateNeedsYou, QKind: factory.QKindSpendStopped, Question: "spend?", Stream: &factory.Stream{}})
	if err != nil {
		t.Fatal(err)
	}
	r := New(Options{Store: st})
	lp := r.loop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lp.ctls[id] = &loopCtl{id: id, ctx: ctx, cancel: cancel, pending: factory.QKindSpendStopped, answers: make(chan loopAnswer, 1)}
	rev, err := st.Revision(id)
	if err != nil {
		t.Fatal(err)
	}

	// Stop Hold just before its store write, then queue SpendAnswer first
	// on lp.mu. Hold must be able to finish that write without the loop lock.
	atWrite, advance := make(chan struct{}), make(chan struct{})
	r.opts.Clock = func() time.Time {
		close(atWrite)
		<-advance
		return time.Now()
	}
	holdDone := make(chan error, 1)
	go func() { holdDone <- r.Hold(id, true) }()
	select {
	case <-atWrite:
	case <-time.After(time.Second):
		t.Fatal("Hold never reached its write")
	}
	lp.mu.Lock()
	answerDone := make(chan error, 1)
	go func() {
		_, err := r.SpendAnswer(id, rev, true, nil)
		answerDone <- err
	}()
	waitBlocked := func(door string) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			stack := make([]byte, 1<<20)
			stack = stack[:runtime.Stack(stack, true)]
			for _, goroutine := range strings.Split(string(stack), "\n\n") {
				if strings.Contains(goroutine, door) && strings.Contains(goroutine, "sync.(*Mutex).Lock") {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s never waited for lp.mu\n%s", door, stack)
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitBlocked("(*Runner).SpendAnswer")
	// Put the mutex into starvation mode so its first waiter goes first.
	time.Sleep(20 * time.Millisecond)
	close(advance)
	waitBlocked("(*Runner).Hold")
	lp.mu.Unlock()
	for _, done := range []chan error{holdDone, answerDone} {
		select {
		case err := <-done:
			// A saved hold can legitimately make the answer's revision stale.
			if err != nil {
				if _, stale := err.(*store.StaleError); !stale {
					t.Fatal(err)
				}
			}
		case <-time.After(time.Second):
			t.Fatal("Hold and SpendAnswer deadlocked")
		}
	}
}
