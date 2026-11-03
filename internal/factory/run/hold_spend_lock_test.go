package run

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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

func TestHoldPersistsBeforeStepBoundary(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		name := "saved"
		if failWrite {
			name = "failed write"
		}
		t.Run(name, func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			id, err := st.Add(context.Background(), factory.Item{Title: "pause boundary", Repo: "acme/api", State: factory.StateRunning, Stream: &factory.Stream{}})
			if err != nil {
				t.Fatal(err)
			}
			r := New(Options{Store: st})
			lp := r.loop()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			roundCtx, roundCancel := context.WithCancel(ctx)
			defer roundCancel()
			c := &loopCtl{id: id, ctx: ctx, cancel: cancel, roundCancel: roundCancel}
			lp.ctls[id] = c
			atSave, save := make(chan struct{}), make(chan struct{})
			var savingHold atomic.Bool
			// Hold reads the item before setPaused takes pauseMu. Arm only
			// when setPaused samples its clock, so that read cannot block here.
			r.opts.Clock = func() time.Time {
				savingHold.Store(true)
				return time.Now()
			}
			var firstSave sync.Once
			st.SetOutputPolicy(func() (int, int) {
				if !savingHold.Load() {
					return 0, 0
				}
				// A later write in the same hold passes through; only the first is held.
				firstSave.Do(func() {
					close(atSave)
					<-save
					if failWrite {
						// Refuse the final rename after the callback has run.
						if err := os.Rename(st.ItemPath(id), st.ItemPath(id)+".before"); err != nil {
							return
						}
						_ = os.Mkdir(st.ItemPath(id), 0700)
					}
				})
				return 0, 0
			})
			holdDone := make(chan error, 1)
			go func() { holdDone <- r.Hold(id, true) }()
			select {
			case <-atSave:
			case <-time.After(time.Second):
				t.Fatal("Hold never reached save")
			}
			boundary := make(chan bool, 1)
			go func() { boundary <- lp.held(c) }()
			select {
			case <-boundary:
				t.Error("drive passed its boundary before the hold was saved")
			case <-time.After(30 * time.Millisecond):
			}
			if roundCtx.Err() != nil || ctx.Err() != nil {
				t.Error("hold cancelled execution before its log was saved")
			}
			close(save)
			select {
			case err := <-holdDone:
				if (err != nil) != failWrite {
					t.Fatalf("Hold error = %v, failWrite = %v", err, failWrite)
				}
			case <-time.After(time.Second):
				t.Fatal("Hold did not finish")
			}
			if failWrite {
				if err := os.Remove(st.ItemPath(id)); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(st.ItemPath(id)+".before", st.ItemPath(id)); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case allowed := <-boundary:
				if allowed != failWrite {
					t.Errorf("step allowed = %v, failWrite = %v", allowed, failWrite)
				}
			case <-time.After(time.Second):
				t.Fatal("step boundary did not settle")
			}
			it, err := st.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if c.paused == failWrite || it.Stream.Paused == failWrite || logHas(it, sayPaused) == failWrite {
				t.Errorf("hold did not commit control, stream and log together: paused=%v stream=%+v", c.paused, it.Stream)
			}
			if (roundCtx.Err() != nil) == failWrite {
				t.Errorf("round cancellation does not match saved hold: %v", roundCtx.Err())
			}
		})
	}
}
