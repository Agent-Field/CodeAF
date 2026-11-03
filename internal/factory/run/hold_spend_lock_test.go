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

	// Pause the hold immediately before its store write. SpendAnswer then
	// owns lp.mu while its caller check holds the item flock, as in the probe.
	atWrite, advance := make(chan struct{}), make(chan struct{})
	r.opts.Now = func() time.Time {
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
	atAnswer, releaseAnswer := make(chan struct{}), make(chan struct{})
	answerDone := make(chan error, 1)
	go func() {
		_, err := r.SpendAnswer(id, rev, true, func(factory.Item) error {
			close(atAnswer)
			<-releaseAnswer
			return nil
		})
		answerDone <- err
	}()
	select {
	case <-atAnswer:
	case <-time.After(time.Second):
		t.Fatal("SpendAnswer never reached its item write")
	}
	close(advance)
	// Ensure Hold is inside the store before releasing the spending answer.
	// This is the inverse scheduling of the supplied probe: both doors still
	// have to obey the same loop-before-item lock order.
	deadline := time.Now().Add(time.Second)
	for {
		stack := make([]byte, 1<<16)
		stack = stack[:runtime.Stack(stack, true)]
		if strings.Contains(string(stack), "(*Store).underLock") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Hold never waited for the item lock")
		}
		time.Sleep(time.Millisecond)
	}
	close(releaseAnswer)
	for _, done := range []chan error{holdDone, answerDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("Hold and SpendAnswer deadlocked")
		}
	}
}
