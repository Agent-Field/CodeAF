package run_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

type admissionTestGate struct {
	mu    sync.Mutex
	allow bool
	taken int
	peak  int
}

func (g *admissionTestGate) MayStart() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.allow }
func (g *admissionTestGate) Started() {
	g.mu.Lock()
	g.taken++
	if g.taken > g.peak {
		g.peak = g.taken
	}
	g.mu.Unlock()
}
func (g *admissionTestGate) Returned()           { g.mu.Lock(); g.taken--; g.mu.Unlock() }
func (g *admissionTestGate) setAllow(allow bool) { g.mu.Lock(); g.allow = allow; g.mu.Unlock() }
func (g *admissionTestGate) count() int          { g.mu.Lock(); defer g.mu.Unlock(); return g.taken }

func TestRunAdmissionHoldsRootUntilTheMachineClears(t *testing.T) {
	store := runOpenStore(t)
	seat := newFakeSeat()
	gate := &admissionTestGate{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan run.Summary, 1)
	go func() {
		_, summary := run.Start(ctx, run.Spec{Store: store, Workspace: t.TempDir(), Gate: gate, Factory: seat.workerFor})
		done <- summary
	}()
	select {
	case <-done:
		t.Fatal("held run ended before the gate cleared")
	case <-time.After(50 * time.Millisecond):
	}
	if len(seat.launches()) != 0 || gate.count() != 0 {
		t.Fatalf("root launched while held: launches=%v, lanes=%d", seat.launches(), gate.count())
	}
	gate.setAllow(true)
	select {
	case summary := <-done:
		if summary.Outcome != run.OutcomeDone || summary.Nodes == 0 {
			t.Fatalf("cleared run = %+v", summary)
		}
	case <-ctx.Done():
		t.Fatal("held run did not resume on its own")
	}
	if gate.count() != 0 {
		t.Fatalf("run kept %d machine lanes", gate.count())
	}
}

func TestRunAdmissionHeldThroughWallStartsNothing(t *testing.T) {
	store := runOpenStore(t)
	seat := newFakeSeat()
	gate := &admissionTestGate{}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, summary := run.Start(ctx, run.Spec{Store: store, Workspace: t.TempDir(), Gate: gate, Factory: seat.workerFor})
	if summary.Nodes != 0 || len(seat.launches()) != 0 || gate.count() != 0 {
		t.Fatalf("held run launched workers: summary=%+v launches=%v lanes=%d", summary, seat.launches(), gate.count())
	}
}

func TestRunAdmissionHoldsReadyLeavesThenHonoursTheSlotBound(t *testing.T) {
	store := runOpenStore(t)
	gate := &admissionTestGate{allow: true}
	seat := newFakeSeat()
	split := splitRoot(t, store,
		plandb.TaskSpec{ID: "a", Title: "first"}, plandb.TaskSpec{ID: "b", Title: "second"}, plandb.TaskSpec{ID: "c", Title: "third"})
	seat.actions["root"] = func(ctx context.Context, task plandb.Task) (run.Report, error) {
		report, err := split(ctx, task)
		gate.setAllow(false)
		return report, err
	}
	for _, id := range []string{"a", "b", "c"} {
		seat.actions[id] = holdSeat(40 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan run.Summary, 1)
	held := make(chan struct{}, 1)
	workspace := t.TempDir()
	go func() {
		_, summary := run.Start(ctx, run.Spec{Store: store, Workspace: workspace, Slots: 2, Gate: gate, Factory: seat.workerFor,
			OnHold: func(ids []string) {
				if len(ids) > 0 {
					select {
					case held <- struct{}{}:
					default:
					}
				}
			},
		})
		done <- summary
	}()
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal("split root never reached the machine hold")
	}
	if got := seat.launches(); len(got) != 1 || got[0] != "root" {
		t.Fatalf("held leaves launched: %v", got)
	}
	gate.setAllow(true)
	select {
	case summary := <-done:
		if summary.Outcome != run.OutcomeDone || summary.Nodes != 5 {
			t.Fatalf("run after hold = %+v", summary)
		}
	case <-ctx.Done():
		t.Fatal("ready leaves did not start after the gate cleared")
	}
	if seat.peakConcurrency() > 2 || gate.count() != 0 {
		t.Fatalf("slot/lanes after run: peak=%d lanes=%d", seat.peakConcurrency(), gate.count())
	}
}

func TestRunAdmissionReturnsLanesOnEveryEnding(t *testing.T) {
	for _, ending := range []string{"elapsed", "wall", "failure", "panic"} {
		t.Run(ending, func(t *testing.T) {
			store := runOpenStore(t)
			gate := &admissionTestGate{allow: true}
			seat := newFakeSeat()
			seat.actions["root"] = func(ctx context.Context, _ plandb.Task) (run.Report, error) {
				switch ending {
				case "failure":
					return run.Report{}, context.DeadlineExceeded
				case "panic":
					panic("worker failed")
				default:
					<-ctx.Done()
					return run.Report{}, ctx.Err()
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			limits := run.Limits{}
			if ending == "elapsed" {
				limits.Elapsed = 50 * time.Millisecond
			}
			_, _ = run.Start(ctx, run.Spec{Store: store, Workspace: t.TempDir(), Gate: gate, Limits: limits, Factory: seat.workerFor})
			if gate.count() != 0 {
				t.Fatalf("%s left %d lanes", ending, gate.count())
			}
		})
	}
}

func TestMachineHoldReportsOnlyStartsRefusedThisPass(t *testing.T) {
	store := runOpenStore(t)
	gate := &admissionTestGate{}
	seat := newFakeSeat()
	rootStarted := make(chan struct{})
	seat.actions["root"] = func(ctx context.Context, task plandb.Task) (run.Report, error) {
		if _, err := store.AddMany([]plandb.TaskSpec{{ID: "leaf", ParentID: task.ID, Title: "leaf"}}); err != nil {
			return run.Report{}, err
		}
		gate.setAllow(false)
		close(rootStarted)
		<-ctx.Done()
		return run.Report{}, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	holds := make(chan []string, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = run.Start(ctx, run.Spec{Store: store, Workspace: t.TempDir(), Gate: gate,
			Factory: seat.workerFor, OnHold: func(ids []string) { holds <- append([]string(nil), ids...) }})
	}()
	wantHold := func(want string) {
		t.Helper()
		for {
			select {
			case ids := <-holds:
				if len(ids) == 0 && want == "leaf" {
					continue
				}
				if len(ids) != 1 || ids[0] != want {
					t.Fatalf("held ids = %v, want {%s}", ids, want)
				}
				return
			case <-ctx.Done():
				t.Fatalf("hold {%s} was not reported", want)
			}
		}
	}
	wantHold("root")
	gate.setAllow(true)
	select {
	case <-rootStarted:
	case <-ctx.Done():
		t.Fatal("root did not start")
	}
	wantHold("leaf")
	if _, err := store.Cancel("leaf", "person stopped it"); err != nil {
		t.Fatal(err)
	}
	select {
	case ids := <-holds:
		if len(ids) != 0 {
			t.Fatalf("cancelled leaf left hold %v", ids)
		}
	case <-ctx.Done():
		t.Fatal("cancelled leaf did not clear hold")
	}
	cancel()
	<-done
}

func TestFactoryPanicDoesNotTakeMachineLane(t *testing.T) {
	store := runOpenStore(t)
	gate := &admissionTestGate{allow: true}
	func() {
		defer func() { _ = recover() }()
		_, _ = run.Start(context.Background(), run.Spec{Store: store, Workspace: t.TempDir(), Gate: gate,
			Factory: func(plandb.Task) run.Worker { panic("factory broke") }})
	}()
	if got := gate.count(); got != 0 {
		t.Fatalf("factory panic left %d machine lanes", got)
	}
}
