package gate

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ── fakes ───────────────────────────────────────────────────────────────────

// requester stands in for store.RequestCommand: it records what would have
// been committed and hands back a command stamped with a journal sequence,
// which is the only part of the store's answer the gate carries onward.
type requester struct {
	mu    sync.Mutex
	calls []store.Command
	err   error
}

func (r *requester) request(command store.Command) (store.Command, error) {
	r.mu.Lock()
	r.calls = append(r.calls, command)
	seq := int64(len(r.calls))
	err := r.err
	r.mu.Unlock()
	if err != nil {
		return store.Command{}, err
	}
	command.Seq = seq
	return command, nil
}

func (r *requester) instructions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	said := make([]string, 0, len(r.calls))
	for _, call := range r.calls {
		said = append(said, call.Instruction)
	}
	return said
}

func (r *requester) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// recorder collects events in order and lets a test block on the next one.
type recorder struct {
	mu     sync.Mutex
	events []Event
	ch     chan Event
}

func newRecorder() *recorder { return &recorder{ch: make(chan Event, 64)} }

func (r *recorder) on(event Event) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
	r.ch <- event
}

// next waits for the next event, failing the test rather than hanging forever
// if the countdown never resolves.
func (r *recorder) next(t *testing.T) Event {
	t.Helper()
	select {
	case event := <-r.ch:
		return event
	case <-time.After(3 * time.Second):
		t.Fatalf("no event within 3s; saw %v", r.kinds())
		return Event{}
	}
}

func (r *recorder) kinds() []EventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	kinds := make([]EventKind, 0, len(r.events))
	for _, event := range r.events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

func task(instruction string) store.Command {
	return store.Command{SessionID: "test", Kind: store.CommandSplice, Instruction: instruction}
}

// ── the three moments ───────────────────────────────────────────────────────

func TestAStagedCommandFiresWhenTheCountdownExpires(t *testing.T) {
	fake := &requester{}
	events := newRecorder()
	commander := New(fake.request, 30*time.Millisecond, events.on)

	seq, err := commander.Stage(task("ship the retry fix"))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}

	staged := events.next(t)
	if staged.Kind != EventStaged || staged.Staged.Seq != seq {
		t.Fatalf("first event = %+v, want staged seq %d", staged, seq)
	}
	if fake.count() != 0 {
		t.Fatalf("command committed before the countdown expired: %v", fake.instructions())
	}
	if pending := commander.Pending(); len(pending) != 1 || pending[0].Seq != seq {
		t.Fatalf("pending = %+v, want the one staging", pending)
	}

	fired := events.next(t)
	if fired.Kind != EventFired {
		t.Fatalf("second event = %+v, want fired", fired)
	}
	if fired.Result.Seq != 1 || fired.Result.Instruction != "ship the retry fix" {
		t.Fatalf("fired result = %+v, want the store's answer carried through", fired.Result)
	}
	if got := fake.instructions(); len(got) != 1 || got[0] != "ship the retry fix" {
		t.Fatalf("requested %v, want the one command", got)
	}
	if pending := commander.Pending(); len(pending) != 0 {
		t.Fatalf("pending after firing = %+v, want empty", pending)
	}
}

func TestCancelInsideTheWindowPreventsTheCommand(t *testing.T) {
	fake := &requester{}
	events := newRecorder()
	commander := New(fake.request, 40*time.Millisecond, events.on)

	seq, err := commander.Stage(task("delete the staging environment"))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if event := events.next(t); event.Kind != EventStaged {
		t.Fatalf("first event = %+v, want staged", event)
	}
	if err := commander.Cancel(seq); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	cancelled := events.next(t)
	if cancelled.Kind != EventCancelled || cancelled.Staged.Seq != seq {
		t.Fatalf("event = %+v, want cancelled seq %d", cancelled, seq)
	}

	// Well past the deadline the command must still never have been requested.
	time.Sleep(120 * time.Millisecond)
	if fake.count() != 0 {
		t.Fatalf("cancelled command was committed anyway: %v", fake.instructions())
	}
	if kinds := events.kinds(); len(kinds) != 2 {
		t.Fatalf("events = %v, want staged then cancelled and nothing else", kinds)
	}
	if err := commander.Cancel(seq); !errors.Is(err, ErrNotStaged) {
		t.Fatalf("second cancel = %v, want ErrNotStaged", err)
	}
}

func TestFireNowSkipsTheRemainingWait(t *testing.T) {
	fake := &requester{}
	events := newRecorder()
	commander := New(fake.request, time.Minute, events.on)

	seq, err := commander.Stage(task("run it already"))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if event := events.next(t); event.Kind != EventStaged {
		t.Fatalf("first event = %+v, want staged", event)
	}

	start := time.Now()
	if err := commander.FireNow(seq); err != nil {
		t.Fatalf("fire now: %v", err)
	}
	fired := events.next(t)
	if fired.Kind != EventFired {
		t.Fatalf("event = %+v, want fired", fired)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Fatalf("fire now waited %v, want the countdown skipped", waited)
	}
	if fake.count() != 1 {
		t.Fatalf("requested %d commands, want 1", fake.count())
	}
	if err := commander.FireNow(seq); !errors.Is(err, ErrNotStaged) {
		t.Fatalf("second fire now = %v, want ErrNotStaged", err)
	}
}

func TestStagingsFireInTheOrderTheirCountdownsExpire(t *testing.T) {
	fake := &requester{}
	events := newRecorder()
	commander := New(fake.request, 120*time.Millisecond, events.on)

	if _, err := commander.Stage(task("first")); err != nil {
		t.Fatalf("stage first: %v", err)
	}
	if event := events.next(t); event.Kind != EventStaged {
		t.Fatalf("event = %+v, want staged", event)
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := commander.Stage(task("second")); err != nil {
		t.Fatalf("stage second: %v", err)
	}
	if event := events.next(t); event.Kind != EventStaged {
		t.Fatalf("event = %+v, want staged", event)
	}

	for round := 0; round < 2; round++ {
		fired := events.next(t)
		if fired.Kind != EventFired {
			t.Fatalf("event %d = %+v, want fired", round, fired)
		}
	}
	got := fake.instructions()
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("requested %v, want [first second]", got)
	}
}

func TestATTLOfZeroStillAnnouncesTheStagingFirst(t *testing.T) {
	fake := &requester{}
	events := newRecorder()
	commander := New(fake.request, 0, events.on)

	if _, err := commander.Stage(task("no countdown wanted")); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if first := events.next(t); first.Kind != EventStaged {
		t.Fatalf("first event = %+v, want staged even at ttl 0", first)
	}
	if second := events.next(t); second.Kind != EventFired {
		t.Fatalf("second event = %+v, want fired", second)
	}
	if fake.count() != 1 {
		t.Fatalf("requested %d commands, want 1", fake.count())
	}
}

func TestARefusedRequestBecomesAFailedEvent(t *testing.T) {
	refusal := errors.New("the graph said no")
	fake := &requester{err: refusal}
	events := newRecorder()
	commander := New(fake.request, time.Millisecond, events.on)

	seq, err := commander.Stage(task("cancel everything"))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if first := events.next(t); first.Kind != EventStaged {
		t.Fatalf("first event = %+v, want staged", first)
	}
	failed := events.next(t)
	if failed.Kind != EventFailed || failed.Staged.Seq != seq {
		t.Fatalf("event = %+v, want failed seq %d", failed, seq)
	}
	if !errors.Is(failed.Err, refusal) {
		t.Fatalf("failed err = %v, want %v", failed.Err, refusal)
	}
	if pending := commander.Pending(); len(pending) != 0 {
		t.Fatalf("pending after a refusal = %+v, want empty: a refusal is an answer", pending)
	}
}

// ── the callback is not a lock ───────────────────────────────────────────────

func TestTheCallbackMayReenterTheCommander(t *testing.T) {
	// The surface's own handler will want to act on what it just heard —
	// cancelling a staging from inside its own Staged event is the obvious
	// case, and it must not deadlock on the Commander's mutex.
	fake := &requester{}
	fired := make(chan Event, 4)
	var commander *Commander
	commander = New(fake.request, 30*time.Millisecond, func(event Event) {
		switch event.Kind {
		case EventStaged:
			commander.Pending()
			if event.Staged.Command.Instruction == "reconsidered" {
				_ = commander.Cancel(event.Staged.Seq)
			}
		case EventFired, EventCancelled:
			fired <- event
		}
	})

	if _, err := commander.Stage(task("reconsidered")); err != nil {
		t.Fatalf("stage: %v", err)
	}
	select {
	case event := <-fired:
		if event.Kind != EventCancelled {
			t.Fatalf("event = %+v, want cancelled from inside the callback", event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reentrant cancel deadlocked")
	}
	time.Sleep(80 * time.Millisecond)
	if fake.count() != 0 {
		t.Fatalf("requested %d commands, want none", fake.count())
	}
}

// ── timers ──────────────────────────────────────────────────────────────────

func TestNoGoroutineIsLeftBehind(t *testing.T) {
	fake := &requester{}
	commander := New(fake.request, 20*time.Millisecond, func(Event) {})

	// A warm-up cycle first: the runtime and the timer machinery start their
	// own goroutines lazily, and counting those as a leak would be a lie.
	warm, err := commander.Stage(task("warm up"))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := commander.Cancel(warm); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	settle(t, func() bool { return true })
	before := runtime.NumGoroutine()

	for round := 0; round < 20; round++ {
		if _, err := commander.Stage(task("expire")); err != nil {
			t.Fatalf("stage: %v", err)
		}
		cancelled, err := commander.Stage(task("cancel"))
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		if err := commander.Cancel(cancelled); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		hurried, err := commander.Stage(task("hurry"))
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		if err := commander.FireNow(hurried); err != nil {
			t.Fatalf("fire now: %v", err)
		}
	}

	settle(t, func() bool { return fake.count() == 40 && len(commander.Pending()) == 0 })
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines %d → %d: a timer was left running", before, after)
	}
}

// settle waits for the work to finish and for the goroutines it spawned to go
// away, then gives the scheduler a last moment to retire them.
func settle(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("work did not settle within 3s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
}

func TestConcurrentStagingAndCancellingIsSafe(t *testing.T) {
	fake := &requester{}
	var seen struct {
		sync.Mutex
		staged, resolved int
	}
	commander := New(fake.request, 5*time.Millisecond, func(event Event) {
		seen.Lock()
		defer seen.Unlock()
		if event.Kind == EventStaged {
			seen.staged++
			return
		}
		seen.resolved++
	})

	const workers, each = 8, 25
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for round := 0; round < each; round++ {
				seq, err := commander.Stage(task("concurrent"))
				if err != nil {
					t.Errorf("stage: %v", err)
					return
				}
				switch (worker + round) % 3 {
				case 0:
					// Racing the countdown on purpose: either answer is correct,
					// and the only wrong outcome is a panic or a double fire.
					if err := commander.Cancel(seq); err != nil && !errors.Is(err, ErrNotStaged) {
						t.Errorf("cancel: %v", err)
					}
				case 1:
					if err := commander.FireNow(seq); err != nil && !errors.Is(err, ErrNotStaged) {
						t.Errorf("fire now: %v", err)
					}
				}
				commander.Pending()
			}
		}(worker)
	}
	wait.Wait()

	total := workers * each
	settle(t, func() bool {
		seen.Lock()
		defer seen.Unlock()
		return seen.resolved == total && len(commander.Pending()) == 0
	})
	seen.Lock()
	defer seen.Unlock()
	if seen.staged != total {
		t.Fatalf("staged events = %d, want %d", seen.staged, total)
	}
	// Every staging resolves exactly once, whichever way it went.
	if seen.resolved != total {
		t.Fatalf("resolved events = %d, want %d", seen.resolved, total)
	}
}

// ── validation and rendering ────────────────────────────────────────────────

func TestStageRefusesWhatTheStoreWouldRefuse(t *testing.T) {
	fake := &requester{}
	var events int
	commander := New(fake.request, time.Millisecond, func(Event) { events++ })

	cases := []struct {
		name    string
		command store.Command
	}{
		{"empty kind", store.Command{Instruction: "do the thing"}},
		{"unknown kind", store.Command{Kind: store.CommandKind("teleport"), Instruction: "do the thing"}},
		{"empty instruction", store.Command{Kind: store.CommandSplice, Instruction: "   "}},
		{"targetless pause", store.Command{Kind: store.CommandPause, Instruction: "hold on"}},
		{"targeted reflex", store.Command{Kind: store.CommandSplice, Reflex: true, Target: "n1", Instruction: "quick"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			seq, err := commander.Stage(test.command)
			if err == nil {
				t.Fatalf("staged %+v, want a refusal", test.command)
			}
			if !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("err = %v, want store.ErrInvalid", err)
			}
			if seq != 0 {
				t.Fatalf("seq = %d, want 0 for a refusal", seq)
			}
		})
	}
	time.Sleep(30 * time.Millisecond)
	if events != 0 || fake.count() != 0 {
		t.Fatalf("%d events and %d requests, want none: nothing was ever staged", events, fake.count())
	}

	// The kinds that legitimately name no node still stage.
	global := store.Command{Kind: store.CommandHeadInterrupt, Instruction: "stop talking"}
	if _, err := commander.Stage(global); err != nil {
		t.Fatalf("stage global command: %v", err)
	}
}

func TestSeqNumbersAreMonotonic(t *testing.T) {
	commander := New((&requester{}).request, time.Minute, nil)
	previous := 0
	for round := 0; round < 5; round++ {
		seq, err := commander.Stage(task("keep counting"))
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		if seq <= previous {
			t.Fatalf("seq %d after %d, want monotonic", seq, previous)
		}
		previous = seq
		if err := commander.Cancel(seq); err != nil {
			t.Fatalf("cancel: %v", err)
		}
	}
}

func TestStagedRemainingAndLine(t *testing.T) {
	start := time.Now()
	staged := Staged{
		Seq:       1,
		Command:   store.Command{Kind: store.CommandRedirect, Target: "n7", Instruction: "no —\ndo the postgres one instead"},
		StageTime: start,
		TTL:       5 * time.Second,
	}
	if left := staged.Remaining(start.Add(2 * time.Second)); left != 3*time.Second {
		t.Fatalf("remaining = %v, want 3s", left)
	}
	if left := staged.Remaining(start.Add(time.Minute)); left != 0 {
		t.Fatalf("remaining past the deadline = %v, want 0", left)
	}
	line := staged.Line()
	if !strings.HasPrefix(line, "redirect n7 — no —") || strings.Contains(line, "\n") {
		t.Fatalf("line = %q, want a one-liner naming the kind and the target", line)
	}

	long := Staged{Command: store.Command{Kind: store.CommandSplice, Instruction: strings.Repeat("ünicode ", 40)}}
	clipped := long.Line()
	if len(clipped) > len("splice — ")+lineInstructionBytes {
		t.Fatalf("line = %q (%d bytes), want it clipped", clipped, len(clipped))
	}
	if !strings.HasSuffix(clipped, "…") {
		t.Fatalf("line = %q, want the cut marked", clipped)
	}
}
