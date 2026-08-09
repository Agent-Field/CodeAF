package mergecoordinator

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeTimer struct {
	fn func()
}

func (*fakeTimer) Stop() {}

type timerRecorder struct {
	mu     sync.Mutex
	delays []float64
	timers []*fakeTimer
}

func (r *timerRecorder) factory(ms float64, fn func()) Timer {
	r.mu.Lock()
	defer r.mu.Unlock()
	timer := &fakeTimer{fn: fn}
	r.delays = append(r.delays, ms)
	r.timers = append(r.timers, timer)
	return timer
}

func (r *timerRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.timers)
}

func (r *timerRecorder) delay(index int) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.delays[index]
}

func (r *timerRecorder) fire(index int) {
	r.mu.Lock()
	fn := r.timers[index].fn
	r.mu.Unlock()
	fn()
}

func sequenceClock(values ...float64) Clock {
	var mu sync.Mutex
	index := 0
	return func() float64 {
		mu.Lock()
		defer mu.Unlock()
		value := values[index]
		index++
		return value
	}
}

func TestComputeMergeRiskWithRunner(t *testing.T) {
	var gotArgv []string
	var gotCWD string
	got := ComputeMergeRiskWithRunner("/leaf", "abc123", func(argv []string, cwd string) (DiffResult, error) {
		gotArgv = append([]string{}, argv...)
		gotCWD = cwd
		return DiffResult{Code: 0, Stdout: []byte("2\t3\tsrc/a.ts\n1\t4\ttest/b.ts\n")}, nil
	})
	if got != 40 { // 2 files * 10 changed lines * 2 top-level dirs
		t.Fatalf("risk = %v, want 40", got)
	}
	if !reflect.DeepEqual(gotArgv, []string{"git", "diff", "--numstat", "abc123", "HEAD"}) {
		t.Fatalf("argv = %#v", gotArgv)
	}
	if gotCWD != "/leaf" {
		t.Fatalf("cwd = %q", gotCWD)
	}

	if got := ComputeMergeRiskWithRunner("", "", func([]string, string) (DiffResult, error) {
		return DiffResult{Code: 7, Stdout: []byte("1\t2\ta")}, nil
	}); got != defaultRisk {
		t.Fatalf("nonzero exit risk = %v", got)
	}
	if got := ComputeMergeRiskWithRunner("", "", func([]string, string) (DiffResult, error) {
		return DiffResult{}, errors.New("spawn failed")
	}); got != defaultRisk {
		t.Fatalf("rejected runner risk = %v", got)
	}
}

func TestComputeMergeRiskRealGit(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q", "-b", "main")
	for name, body := range map[string]string{
		"src/a.ts":  "one\n",
		"docs/b.md": "only\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "-q", "-m", "base")
	base := git("rev-parse", "HEAD")
	base = base[:len(base)-1]

	if err := os.WriteFile(filepath.Join(dir, "src/a.ts"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "docs/b.md")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "change")

	// src: +2, docs: -1. Two files, three changed lines, two top dirs.
	if got := ComputeMergeRisk(dir, base); got != 12 {
		t.Fatalf("risk = %v, want 12", got)
	}
}

func TestSettlingWindowOrdersOneBatch(t *testing.T) {
	timers := &timerRecorder{}
	coordinator := New(Options{
		Now: sequenceClock(100, 101, 102), TimerFactory: timers.factory,
	})
	order := []string{}
	var orderMu sync.Mutex
	work := func(id string) Work {
		return func() (any, error) {
			orderMu.Lock()
			order = append(order, id)
			orderMu.Unlock()
			return id + "-result", nil
		}
	}

	high := coordinator.Submit("high", 100, work("high"))
	low := coordinator.Submit("low", 1, work("low"))
	mid := coordinator.Submit("mid", 10, work("mid"))

	if got := coordinator.Inspect(); got != (InspectResult{Queued: 3, Processing: false}) {
		t.Fatalf("before fire inspect = %#v", got)
	}
	if timers.count() != 1 {
		t.Fatalf("new arrivals restarted timer: count=%d", timers.count())
	}
	if got := timers.delay(0); got != settlingMS {
		t.Fatalf("settling delay = %v", got)
	}

	timers.fire(0)
	if !reflect.DeepEqual(order, []string{"low", "mid", "high"}) {
		t.Fatalf("dispatch order = %#v", order)
	}
	for future, want := range map[*Future]string{
		high: "high-result", low: "low-result", mid: "mid-result",
	} {
		got, err := future.Await()
		if err != nil || got != want {
			t.Fatalf("future got=%#v err=%v want=%q", got, err, want)
		}
	}
	if got := coordinator.Inspect(); got != (InspectResult{}) {
		t.Fatalf("after drain inspect = %#v", got)
	}
}

func TestEqualRiskUsesArrivalThenStableInsertion(t *testing.T) {
	timers := &timerRecorder{}
	coordinator := New(Options{
		Now: sequenceClock(300, 100, 100), TimerFactory: timers.factory,
	})
	var order []string
	for _, id := range []string{"late", "first-at-100", "second-at-100"} {
		id := id
		coordinator.Submit(id, 5, func() (any, error) {
			order = append(order, id)
			return nil, nil
		})
	}
	timers.fire(0)
	want := []string{"first-at-100", "second-at-100", "late"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order=%#v want=%#v", order, want)
	}
}

func TestArrivalsDuringWorkDrainWithoutNewWindow(t *testing.T) {
	timers := &timerRecorder{}
	coordinator := New(Options{
		Now: sequenceClock(1, 2, 3), TimerFactory: timers.factory,
	})

	started := make(chan string, 3)
	releaseHigh := make(chan struct{})
	var orderMu sync.Mutex
	order := []string{}
	high := coordinator.Submit("high", 100, func() (any, error) {
		orderMu.Lock()
		order = append(order, "high")
		orderMu.Unlock()
		started <- "high"
		<-releaseHigh
		return "high", nil
	})
	mid := coordinator.Submit("mid", 10, func() (any, error) {
		orderMu.Lock()
		order = append(order, "mid")
		orderMu.Unlock()
		started <- "mid"
		return "mid", nil
	})

	fired := make(chan struct{})
	go func() {
		timers.fire(0)
		close(fired)
	}()
	select {
	case id := <-started:
		if id != "mid" {
			t.Fatalf("first started = %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("first work did not start")
	}
	// mid resolves immediately, so high becomes active and blocks.
	select {
	case id := <-started:
		if id != "high" {
			t.Fatalf("second started = %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("second work did not start")
	}
	if got := coordinator.Inspect(); got != (InspectResult{Queued: 0, Processing: true}) {
		t.Fatalf("while high runs inspect = %#v", got)
	}

	low := coordinator.Submit("late-low", 1, func() (any, error) {
		orderMu.Lock()
		order = append(order, "late-low")
		orderMu.Unlock()
		started <- "late-low"
		return "low", nil
	})
	if timers.count() != 1 {
		t.Fatalf("arrival during processing armed timer: count=%d", timers.count())
	}
	close(releaseHigh)
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("batch did not drain")
	}
	if id := <-started; id != "late-low" {
		t.Fatalf("third started = %q", id)
	}
	orderMu.Lock()
	gotOrder := append([]string{}, order...)
	orderMu.Unlock()
	if !reflect.DeepEqual(gotOrder, []string{"mid", "high", "late-low"}) {
		t.Fatalf("order = %#v", gotOrder)
	}
	for _, future := range []*Future{mid, high, low} {
		if _, err := future.Await(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewBatchGetsNewSettlingWindow(t *testing.T) {
	timers := &timerRecorder{}
	coordinator := New(Options{
		Now: sequenceClock(1, 2), TimerFactory: timers.factory,
	})
	first := coordinator.Submit("first", 1, func() (any, error) { return 1, nil })
	timers.fire(0)
	if got, err := first.Await(); err != nil || got != 1 {
		t.Fatalf("first got=%v err=%v", got, err)
	}

	secondRan := false
	second := coordinator.Submit("second", 1, func() (any, error) {
		secondRan = true
		return 2, nil
	})
	if timers.count() != 2 {
		t.Fatalf("new idle batch did not arm a timer: count=%d", timers.count())
	}
	if secondRan {
		t.Fatal("second batch bypassed settling")
	}
	timers.fire(1)
	if got, err := second.Await(); err != nil || got != 2 {
		t.Fatalf("second got=%v err=%v", got, err)
	}
}

func TestFailureAndPanicRejectOnlyTheirOwnWork(t *testing.T) {
	timers := &timerRecorder{}
	coordinator := New(Options{
		Now: sequenceClock(1, 2, 3), TimerFactory: timers.factory,
	})
	wantErr := errors.New("merge failed")
	wantPanicErr := errors.New("panic error")
	failed := coordinator.Submit("failed", 1, func() (any, error) {
		return nil, wantErr
	})
	panicked := coordinator.Submit("panicked", 2, func() (any, error) {
		panic(wantPanicErr)
	})
	succeeded := coordinator.Submit("succeeded", 3, func() (any, error) {
		return "landed", nil
	})
	timers.fire(0)

	if _, err := failed.Await(); !errors.Is(err, wantErr) {
		t.Fatalf("failure err = %v", err)
	}
	if _, err := panicked.Await(); !errors.Is(err, wantPanicErr) {
		t.Fatalf("panic err = %v", err)
	}
	if got, err := succeeded.Await(); err != nil || got != "landed" {
		t.Fatalf("success got=%#v err=%v", got, err)
	}
}

func TestFutureCanBeAwaitedMoreThanOnce(t *testing.T) {
	timers := &timerRecorder{}
	coordinator := New(Options{
		Now: sequenceClock(1), TimerFactory: timers.factory,
	})
	future := coordinator.Submit("a", 1, func() (any, error) {
		return "same promise value", nil
	})
	timers.fire(0)
	for i := 0; i < 2; i++ {
		got, err := future.Await()
		if err != nil || got != "same promise value" {
			t.Fatalf("await %d got=%#v err=%v", i, got, err)
		}
	}
}

func TestRunWaitsForCompletion(t *testing.T) {
	timers := &timerRecorder{}
	coordinator := New(Options{
		Now: sequenceClock(1), TimerFactory: timers.factory,
	})
	type result struct {
		value any
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := coordinator.Run("a", 1, func() (any, error) {
			return 42, nil
		})
		done <- result{value, err}
	}()

	deadline := time.After(time.Second)
	for timers.count() == 0 {
		select {
		case <-deadline:
			t.Fatal("Run did not enqueue")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	select {
	case got := <-done:
		t.Fatalf("Run returned before settling timer: %#v", got)
	default:
	}
	timers.fire(0)
	select {
	case got := <-done:
		if got.err != nil || got.value != 42 {
			t.Fatalf("Run result = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after work")
	}
}
