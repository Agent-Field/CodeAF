package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type taskTestClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []taskTestWait
}
type taskTestWait struct {
	at time.Time
	ch chan time.Time
}

func newTaskTestClock() *taskTestClock  { return &taskTestClock{now: time.Unix(100, 0)} }
func (c *taskTestClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *taskTestClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waits = append(c.waits, taskTestWait{c.now.Add(d), ch})
	return ch
}
func (c *taskTestClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	var kept []taskTestWait
	for _, w := range c.waits {
		if !w.at.After(c.now) {
			w.ch <- c.now
		} else {
			kept = append(kept, w)
		}
	}
	c.waits = kept
}

// Contract 9 names an unanswered computer within sixty injected seconds,
// even while a watch dial itself is still waiting to return.
func TestAComputerThatStopsAnsweringIsSaidWithinAMinute(t *testing.T) {
	clock := newTaskTestClock()
	watching := make(chan struct{})
	missing := make(chan bool, 2)
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		_ = spec.SaveRemote(r, true)
		err := FollowTaskWatch(ctx, clock, 0, func(ctx context.Context, from uint64, alive func(), deliver func(uint64) error) error {
			close(watching)
			<-ctx.Done()
			return ctx.Err()
		}, func(uint64) error { return nil }, func(no bool) {
			r.Unreachable = no
			_ = spec.SaveRemote(r, true)
			spec.OnRemotePhase(TaskComputerStoppedAnswering("work computer"))
			missing <- no
		})
		return RunSummary{Outcome: "interrupted", Result: err.Error(), KeepCopy: true}
	}}
	a, _ := remoteTaskAgent(t, remote, nil)
	if _, _, _, err := a.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	<-watching
	// Each turn yields to the watch driver after waking its injected timer.
	for i := 0; i < 60; i++ {
		clock.advance(time.Second)
		time.Sleep(time.Millisecond)
	}
	select {
	case no := <-missing:
		if !no {
			t.Fatal("computer still shown reachable")
		}
	case <-time.After(time.Second):
		t.Fatal("missing notice after sixty seconds")
	}
	notice, ok := runRowOf(a.graph(), 1)
	if !ok || !notice.Unreachable || notice.Doing != "work computer stopped answering — it may still be working" {
		t.Fatalf("task notice = %+v", notice)
	}
	_ = a.Close()
	waitRemoteRun(t, a)
}

// Contract 9 replays a cursor after a drop and never repeats an observed step.
func TestReconnectingReplaysMissedStepsWithoutDoubles(t *testing.T) {
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		_ = spec.SaveRemote(r, true)
		var seq uint64
		tries := 0
		watch := func(ctx context.Context, from uint64, alive func(), deliver func(uint64) error) error {
			tries++
			alive()
			frames := []uint64{1}
			if tries == 2 {
				if from != 1 {
					t.Errorf("reconnected cursor = %d", from)
				}
				frames = []uint64{1, 2, 3}
			}
			for _, next := range frames {
				seq = next
				if err := deliver(next); err != nil {
					return err
				}
			}
			if tries == 1 {
				return errors.New("network dropped")
			}
			return ErrTaskWatchEnded
		}
		err := FollowTaskWatch(ctx, nil, 0, watch, func(n uint64) error {
			return spec.OnRemoteStep(n, []byte(fmt.Sprintf(`{"kind":"step","step":%d,"command":"step %d","observation":"bounded head","full_output":"/other/home/output"}`, seq, seq)))
		}, func(bool) {})
		if err != nil {
			t.Error(err)
		}
		_ = spec.Store.CompleteRoot("done")
		return RunSummary{Outcome: "done"}
	}}
	a, _ := remoteTaskAgent(t, remote, nil)
	if _, _, _, err := a.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, a)
	page, ok := a.PlanTaskPage("t-1")
	if !ok {
		t.Fatal("task page missing")
	}
	if len(page.Steps) != 3 {
		t.Fatalf("task steps = %+v", page.Steps)
	}
	for i, step := range page.Steps {
		if step.Command != fmt.Sprintf("step %d", i+1) || step.FullOutput != "" {
			t.Fatalf("step %d = %+v", i, step)
		}
	}
}

// Contract 10 closes and reopens the actual conversation record; recovery
// watches the saved attempt/cursor and cannot issue another remote start.
func TestAReopenedConversationReattachesWithoutStartingAgain(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	local := newBeltRunDouble("done")
	local.real = true
	registerBeltRunEngine(t, local)
	var starts, resumes atomic.Int32
	ready, resumed := make(chan struct{}), make(chan RunSpec, 1)
	finish := make(chan struct{})
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		starts.Add(1)
		r := remoteRecordFor(spec)
		r.Seq = 7
		_ = spec.SaveRemote(r, true)
		close(ready)
		<-ctx.Done()
		return RunSummary{Outcome: "interrupted", KeepCopy: true}
	}, resume: func(ctx context.Context, spec RunSpec) RunSummary {
		resumes.Add(1)
		resumed <- spec
		<-finish
		r := *spec.Remote
		r.Finished = true
		_ = spec.SaveRemote(r, true)
		_ = spec.Store.CompleteRoot("done elsewhere")
		return RunSummary{Outcome: "done", Result: "done elsewhere"}
	}}
	place, repo, profile := t.TempDir(), beltRunCommittedRepo(t), t.TempDir()
	open := func() *Agent {
		a, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
			c.Workspace = repo
			c.Place = Place{Dir: place}
			c.SessionFile = c.Place.Transcript()
			c.ProfileDir = profile
			c.TaskComputers = testTaskComputers{engine: remote}
		})
		return a
	}
	first := open()
	if _, _, _, err := first.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	<-ready
	_ = first.Close()
	waitRemoteRun(t, first)
	second := open()
	select {
	case spec := <-resumed:
		if spec.Remote.Seq != 7 || spec.Remote.Attempt != "att_0123456789abcdef" {
			t.Fatalf("resume record = %+v", spec.Remote)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reopen did not reattach")
	}
	if starts.Load() != 1 || resumes.Load() != 1 || local.didRun() {
		t.Fatal("reopen started another worker")
	}
	close(finish)
	waitRemoteRun(t, second)
	if local.lands() != 1 {
		t.Fatalf("landings = %d", local.lands())
	}
}

// Contracts 8 and 11 fence a dropped attempt before its local replacement.
// Even a result callback arriving after that fence cannot touch the copy.
func TestRunItHereRunsLocallyAndAnAbandonedResultNeverLands(t *testing.T) {
	ready := make(chan struct{})
	late := make(chan error, 1)
	abandoned := make(chan struct{}, 1)
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		r.Unreachable = true
		_ = spec.SaveRemote(r, true)
		close(ready)
		<-ctx.Done()
		late <- spec.WithRemoteCopy(func() error {
			return os.WriteFile(filepath.Join(spec.Workspace, "late.txt"), []byte("late remote result"), 0600)
		})
		return RunSummary{Outcome: "done", Result: "late result"}
	}, abandon: func() { abandoned <- struct{}{} }}
	a, local := remoteTaskAgent(t, remote, nil)
	if _, _, _, err := a.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	<-ready
	receipt, err := a.RunTaskHere(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(receipt, "here") {
		t.Fatalf("receipt = %s", receipt)
	}
	select {
	case <-local.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("local replacement never started")
	}
	if err := <-late; !errors.Is(err, ErrTaskAttemptAbandoned) {
		t.Fatalf("late result = %v", err)
	}
	<-abandoned
	endBeltRun(t, a, local)
	if _, err := os.Stat(filepath.Join(a.config.Workspace, "late.txt")); !os.IsNotExist(err) {
		t.Fatal("abandoned result landed")
	}
	kept, _ := runRowOf(a.graph(), 1)
	if kept.Computer != "" || kept.Unreachable {
		t.Fatalf("local placement = %+v", kept)
	}
	if local.lands() != 1 {
		t.Fatalf("landing calls = %d", local.lands())
	}
}

// Contract 8 preserves a refused remote start's ready copy until the person
// explicitly chooses here; that choice starts locally in the same copy.
func TestARefusedRemoteStartWaitsForRunItHere(t *testing.T) {
	var sent string
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		sent = spec.Workspace
		r := remoteRecordFor(spec)
		r.StartFailed = true
		_ = spec.SaveRemote(r, true)
		return RunSummary{Outcome: "incomplete", Result: "work computer is not taking tasks right now — run codeaf serve --take-tasks on it", KeepCopy: true}
	}}
	a, local := remoteTaskAgent(t, remote, nil)
	if _, _, _, err := a.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, a)
	if local.didRun() {
		t.Fatal("refusal fell back without a person's choice")
	}
	if _, err := os.Stat(sent); err != nil {
		t.Fatal("refusal removed the ready copy")
	}
	if _, err := a.RunTaskHere(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	<-local.entered
	local.mu.Lock()
	got := local.spec.Workspace
	local.mu.Unlock()
	if got != sent {
		t.Fatalf("replacement copy = %s, sent %s", got, sent)
	}
	endBeltRun(t, a, local)
}

// Contract 11 refuses local replacement before abandoning anything if the
// copy here changed while the other computer was working.
func TestRunItHereKeepsAChangedCopyAndTheRemoteAttempt(t *testing.T) {
	ready := make(chan struct{})
	var abandons atomic.Int32
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		r.Unreachable = true
		_ = spec.SaveRemote(r, true)
		close(ready)
		<-ctx.Done()
		return RunSummary{Outcome: "interrupted", KeepCopy: true}
	}, check: func(context.Context, string, string) error {
		return errors.New("the task copy changed here while the other computer worked; both copies were kept")
	}, abandon: func() { abandons.Add(1) }}
	a, local := remoteTaskAgent(t, remote, nil)
	if _, _, _, err := a.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	<-ready
	if _, err := a.RunTaskHere(context.Background(), 1); err == nil {
		t.Fatal("changed copy allowed replacement")
	}
	kept, _ := runRowOf(a.graph(), 1)
	if kept.Remote.Abandoned || abandons.Load() != 0 || local.didRun() {
		t.Fatal("refused replacement changed the attempt")
	}
	_ = a.Close()
	waitRemoteRun(t, a)
}

// Contract 5 includes a crash after both accounting writes but before the
// throttled cursor checkpoint. The replayed call is still one conversation call.
func TestRemoteChargeReplayAfterReopenDoesNotChargeTheConversationTwice(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	local := newBeltRunDouble("done")
	local.real = true
	registerBeltRunEngine(t, local)
	charged := make(chan struct{})
	var resumed atomic.Int32
	charge := RunCharge{Model: "remote/model", TokensIn: 40, TokensOut: 8, USD: .25}
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		_ = spec.SaveRemote(r, true)
		if err := spec.OnRemoteCharge(1, charge, "worker"); err != nil {
			t.Error(err)
		}
		close(charged)
		<-ctx.Done()
		return RunSummary{Outcome: "interrupted", KeepCopy: true}
	}, resume: func(ctx context.Context, spec RunSpec) RunSummary {
		resumed.Add(1)
		if spec.Remote.Seq != 0 {
			t.Errorf("fixture cursor = %d", spec.Remote.Seq)
		}
		if err := spec.OnRemoteCharge(1, charge, "worker"); err != nil {
			t.Error(err)
		}
		r := *spec.Remote
		r.Seq = 1
		r.USD = .25
		r.Finished = true
		_ = spec.SaveRemote(r, true)
		_ = spec.Store.CompleteRoot("done")
		return RunSummary{Outcome: "done", USD: .25}
	}}
	place, repo, profile := t.TempDir(), beltRunCommittedRepo(t), t.TempDir()
	ledger := filepath.Join(profile, UsageLedgerName)
	open := func() *Agent {
		a, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
			c.Workspace = repo
			c.Place = Place{Dir: place}
			c.SessionFile = c.Place.Transcript()
			c.ProfileDir = profile
			c.TaskComputers = testTaskComputers{engine: remote}
			c.usageLedger = ledger
		})
		return a
	}
	first := open()
	if _, _, _, err := first.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	<-charged
	_ = first.Close()
	waitRemoteRun(t, first)
	second := open()
	waitRemoteRun(t, second)
	if resumed.Load() != 1 {
		t.Fatal("task did not replay the saved attempt")
	}
	if usage := second.Usage(); usage.CostUSD != .25 || usage.Calls != 1 {
		t.Fatalf("replayed usage = %+v", usage)
	}
	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil || len(lines) != 1 {
		t.Fatalf("ledger = %+v, %v", lines, err)
	}
}

// Contract 2's durable step mirror does linear work in the record size. Replay
// reads the existing prefix once, and never writes an already held event again.
func TestRemoteStepMirroringReadsOnceAndWritesEachNewStepOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plandb.db")
	store, err := plandb.Open(path, "task", "1", "task", "brief")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mirror := &remoteStepMirror{}
	for i := 1; i <= 200; i++ {
		raw := []byte(fmt.Sprintf(`{"kind":"step","step":%d,"command":"inspect","observation":"head"}`, i))
		if err := mirror.append(store, "att_0123456789abcdef", uint64(i), raw); err != nil {
			t.Fatal(err)
		}
	}
	if mirror.readBytes != 0 {
		t.Fatalf("new record read %d bytes", mirror.readBytes)
	}
	before := mirror.writeBytes
	replay := &remoteStepMirror{}
	for i := 1; i <= 200; i++ {
		if err := replay.append(store, "att_0123456789abcdef", uint64(i), []byte(`{"kind":"step"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if replay.readBytes != before || replay.writeBytes != 0 {
		t.Fatalf("replay read/write = %d/%d, original %d", replay.readBytes, replay.writeBytes, before)
	}
	t.Logf("200 steps: %d bytes written, replay read %d bytes once and wrote %d", before, replay.readBytes, replay.writeBytes)
}

// Contract 6 keeps local placement usable while a paired directory is offline.
func TestChoosingHereDoesNotNeedTheOtherComputerOrTheDirectory(t *testing.T) {
	a, local := remoteTaskAgent(t, nil, nil)
	a.config.TaskComputers = offlineTaskComputers{}
	if _, _, _, err := a.StartTaskOn(context.Background(), "Repair export", false, "here"); err != nil {
		t.Fatal(err)
	}
	<-local.entered
	endBeltRun(t, a, local)
}

type offlineTaskComputers struct{ testTaskComputers }

func (offlineTaskComputers) List(context.Context) ([]TaskComputer, error) {
	return nil, errors.New("directory is offline")
}

// Contract 5 includes another owner appending after a long-lived process has
// already cached the ledger prefix. Its replay refreshes that tail first.
func TestRemoteSpendReplaySeesAnotherOwnersAppendedCharge(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	now := time.Now()
	first := UsageLine{At: now, Day: now.Local().Format(usageDayLayout), RemoteEvent: "att_0123456789abcdef:1", Model: "remote/model", USD: .10, Calls: 1}
	if err := appendRemoteUsage(path, first); err != nil {
		t.Fatal(err)
	}
	later := UsageLine{At: now, Day: now.Local().Format(usageDayLayout), RemoteEvent: "att_0123456789abcdef:2", Model: "remote/model", USD: .20, Calls: 1}
	raw, _ := json.Marshal(later)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write(append(raw, '\n'))
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := appendRemoteUsage(path, later); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadUsage(path, time.Time{})
	if err != nil || len(lines) != 2 {
		t.Fatalf("replayed ledger = %+v, %v", lines, err)
	}
}

// Contract 3 keeps a stopped task's partial change on its local task branch,
// and the person's Stop travels to the remote engine without cutting its watch.
func TestStoppingARemoteTaskReturnsItsPartialChangeAndKeepsItsBranch(t *testing.T) {
	ready, stop := make(chan struct{}), make(chan struct{})
	remote := &scriptedTaskEngine{stop: func() { close(stop) }, start: func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		_ = spec.SaveRemote(r, true)
		close(ready)
		select {
		case <-stop:
		case <-ctx.Done():
			t.Error("Stop cut the watch before its partial result came back")
			return RunSummary{KeepCopy: true}
		}
		if err := spec.WithRemoteCopy(func() error {
			return os.WriteFile(filepath.Join(spec.Workspace, "partial.txt"), []byte("partial work"), 0600)
		}); err != nil {
			t.Error(err)
		}
		r.Finished = true
		_ = spec.SaveRemote(r, true)
		return RunSummary{Outcome: "stopped", Result: "stopped with partial work", StorageSafe: true}
	}}
	a, local := remoteTaskAgent(t, remote, nil)
	if _, _, _, err := a.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	<-ready
	if _, err := a.Cancel("task:1"); err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, a)
	kept, _ := runRowOf(a.graph(), 1)
	if !kept.Stopped || kept.Branch == "" || ProjectTask(kept.StatusFacts()).Changes != TaskChangesKept {
		t.Fatalf("stopped task = %+v", kept)
	}
	if _, err := os.Stat(filepath.Join(a.config.Workspace, "partial.txt")); !os.IsNotExist(err) {
		t.Fatal("stopped work merged into the person's project")
	}
	if out, err := git(a.config.Workspace, "show", kept.Branch+":partial.txt"); err != nil || strings.TrimSpace(out) != "partial work" {
		t.Fatalf("kept partial change = %q, %v", out, err)
	}
	if local.didRun() {
		t.Fatal("Stop started a local worker")
	}
}

// Contract 8 applies to any saved refused task number, even after a later
// task has archived its plan. The replacement must receive that task's brief.
func TestRunItHereUsesTheRequestedTasksArchivedInstruction(t *testing.T) {
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		r.StartFailed = true
		_ = spec.SaveRemote(r, true)
		return RunSummary{Outcome: "incomplete", KeepCopy: true}
	}}
	a, local := remoteTaskAgent(t, remote, nil)
	if _, _, _, err := a.StartTaskOn(context.Background(), "Repair the export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, a)
	if _, _, _, err := a.StartTask(context.Background(), "Repair the import", false); err != nil {
		t.Fatal(err)
	}
	<-local.entered
	endBeltRun(t, a, local)
	replacement := newBeltRunDouble("done")
	replacement.real = true
	registerBeltRunEngine(t, replacement)
	if _, err := a.RunTaskHere(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	<-replacement.entered
	replacement.mu.Lock()
	brief := replacement.spec.Brief
	replacement.mu.Unlock()
	if brief != "Repair the export" {
		t.Fatalf("replacement instruction = %q", brief)
	}
	endBeltRun(t, a, replacement)
}

// Contract 9 replays a step whose append was interrupted before its newline.
func TestReplayingAStepWithAnUnfinishedAppendKeepsTheStep(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plandb.db"), "task", "1", "task", "brief")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	path := filepath.Join(plandb.TaskDir(filepath.Dir(store.Path()), "1"), "trajectory.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"kind":"step","step":1,"command":"inspect","remote_attempt":"att_0123456789abcdef","remote_seq":1}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	mirror := &remoteStepMirror{}
	if err := mirror.append(store, "att_0123456789abcdef", 1, raw); err != nil {
		t.Fatal(err)
	}
	if got := planTrajectory(filepath.Dir(store.Path()), "1"); len(got) != 1 || got[0].Command != "inspect" {
		t.Fatalf("replayed trajectory = %+v", got)
	}
}

// Contracts 3 and 10 retain an outstanding remote stop across a quit. The
// reopened watch still collects the partial result before settling the stop.
func TestQuittingWhileARemoteTaskStopsReattachesForItsPartialResult(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	local := newBeltRunDouble("done")
	local.real = true
	registerBeltRunEngine(t, local)
	ready, stopping := make(chan struct{}), make(chan struct{}, 2)
	var resumes atomic.Int32
	remote := &scriptedTaskEngine{stop: func() { stopping <- struct{}{} }, start: func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		_ = spec.SaveRemote(r, true)
		close(ready)
		<-ctx.Done()
		return RunSummary{Outcome: "interrupted", KeepCopy: true}
	}, resume: func(ctx context.Context, spec RunSpec) RunSummary {
		resumes.Add(1)
		if err := spec.WithRemoteCopy(func() error {
			return os.WriteFile(filepath.Join(spec.Workspace, "partial.txt"), []byte("stopped partial work"), 0600)
		}); err != nil {
			t.Error(err)
		}
		r := *spec.Remote
		r.Finished = true
		_ = spec.SaveRemote(r, true)
		return RunSummary{Outcome: "stopped", StorageSafe: true}
	}}
	place, repo, profile := t.TempDir(), beltRunCommittedRepo(t), t.TempDir()
	open := func() *Agent {
		a, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
			c.Workspace = repo
			c.Place = Place{Dir: place}
			c.SessionFile = c.Place.Transcript()
			c.ProfileDir = profile
			c.TaskComputers = testTaskComputers{engine: remote}
		})
		return a
	}
	first := open()
	if _, _, _, err := first.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	<-ready
	if _, err := first.Cancel("task:1"); err != nil {
		t.Fatal(err)
	}
	<-stopping
	_ = first.Close()
	waitRemoteRun(t, first)
	second := open()
	waitRemoteRun(t, second)
	if resumes.Load() != 1 {
		t.Fatal("pending stop did not reattach")
	}
	kept, _ := runRowOf(second.graph(), 1)
	if !kept.Stopped || kept.Branch == "" {
		t.Fatalf("stopped partial work was not kept: %+v", kept)
	}
}

// Contract 4 keeps a single private repository copy eligible for the remote
// preference, then uses the ordinary local Review and Apply entry points.
func TestAPrivateTaskRunsElsewhereAndWaitsForLocalReviewAndApply(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	reached := make(chan struct{}, 1)
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		reached <- struct{}{}
		r := remoteRecordFor(spec)
		r.Finished, r.Restored = true, true
		_ = spec.SaveRemote(r, true)
		if err := os.WriteFile(filepath.Join(spec.Workspace, "private-output.txt"), []byte("the returned private change\n"), 0600); err != nil {
			t.Error(err)
		}
		_ = spec.Store.CompleteRoot("private change made")
		return RunSummary{Outcome: "done", StorageSafe: true}
	}}
	a, local := remoteTaskAgent(t, remote, func(c *Config) { _ = config.WriteTaskRunsOn(c.ProfileDir, "dev_0123456789abcdef0123456789abcdef") })
	id, _, _, err := a.StartTask(context.Background(), "write private-output.txt in this project", false)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-reached:
	case <-local.entered:
		t.Fatal("private task preference ran locally")
	case <-time.After(5 * time.Second):
		t.Fatal("private task never started")
	}
	waitRemoteRun(t, a)
	if _, err := os.Stat(filepath.Join(a.config.Workspace, "private-output.txt")); !os.IsNotExist(err) {
		t.Fatal("private result applied before review")
	}
	review, err := a.ReviewTaskWorkspaceRoot(context.Background(), id, "work")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(review.Diff, "the returned private change") {
		t.Fatalf("review = %+v", review)
	}
	if _, err := a.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(a.config.Workspace, "private-output.txt")); err != nil || string(got) != "the returned private change\n" {
		t.Fatalf("applied result = %q, %v", got, err)
	}
}
