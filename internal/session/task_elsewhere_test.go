package session

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Contract 6 makes a misspelt computer repairable at the tool boundary.
func TestAnUnknownComputerNameIsAToolErrorListingTheComputers(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.ProfileDir = t.TempDir()
		c.TaskComputers = testTaskComputers{}
		if err := config.WriteTaskRunsOn(c.ProfileDir, "dev_0123456789abcdef0123456789abcdef"); err != nil {
			t.Fatal(err)
		}
	})
	result, refused, err := a.stageTask(context.Background(), json.RawMessage(`{"title":"Repair export","summary":"Repair the export path","brief":"Fix the export failure in the project","deliverable":"working export","acceptance":"export succeeds","computer":"missing"}`)).Commit(context.Background())
	if err != nil || !refused || !strings.Contains(result, `no paired computer is named "missing"`) || !strings.Contains(result, "work computer") {
		t.Fatalf("tool result = %q, %v, %v", result, refused, err)
	}
}

// The absence law keeps an unusable argument off every unpaired belt.
func TestTheComputerArgumentIsAbsentWhenNothingIsPaired(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, present := schemaProperties(t, a.taskTools()[0].Schema)["computer"]; present {
		t.Fatal("unpaired schema offers computer")
	}

}

type testTaskComputers struct{ engine TaskRunEngine }

func (testTaskComputers) List(context.Context) ([]TaskComputer, error) {
	return []TaskComputer{{Device: "dev_0123456789abcdef0123456789abcdef", Name: "work computer", CanTakeTasks: true}}, nil
}
func (testTaskComputers) ThisComputer() string { return "chat computer" }
func (c testTaskComputers) Engine(context.Context, string) (TaskRunEngine, error) {
	return c.engine, nil
}

// The fake stands at the process/network boundary; local copies, stores,
// notices and landing remain the conversation's real roads.
type scriptedTaskEngine struct {
	start   func(context.Context, RunSpec) RunSummary
	resume  func(context.Context, RunSpec) RunSummary
	check   func(context.Context, string, string) error
	stop    func()
	abandon func()
}

func (e *scriptedTaskEngine) Start(ctx context.Context, spec RunSpec) RunSummary {
	return e.start(ctx, spec)
}
func (e *scriptedTaskEngine) Resume(ctx context.Context, spec RunSpec) RunSummary {
	if e.resume != nil {
		return e.resume(ctx, spec)
	}
	return e.start(ctx, spec)
}
func (e *scriptedTaskEngine) Land(context.Context, *plandb.Store, string, string, string) (RunLanding, error) {
	panic("a remote engine must never land here")
}
func (e *scriptedTaskEngine) Stop(context.Context, TaskRemoteRecord) error {
	if e.stop != nil {
		e.stop()
	}
	return nil
}
func (e *scriptedTaskEngine) Abandon(context.Context, TaskRemoteRecord) error {
	if e.abandon != nil {
		e.abandon()
	}
	return nil
}
func (e *scriptedTaskEngine) Acknowledge(context.Context, TaskRemoteRecord) error { return nil }
func (e *scriptedTaskEngine) CheckCopy(ctx context.Context, path, fingerprint string) error {
	if e.check != nil {
		return e.check(ctx, path, fingerprint)
	}
	return nil
}

func remoteTaskAgent(t *testing.T, remote TaskRunEngine, options func(*Config)) (*Agent, *beltRunDouble) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	local := newBeltRunDouble("done here")
	local.real = true
	local.honoursStop = true
	registerBeltRunEngine(t, local)
	place, repo := t.TempDir(), beltRunCommittedRepo(t)
	a, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace = repo
		c.Place = Place{Dir: place}
		c.SessionFile = c.Place.Transcript()
		c.ProfileDir = t.TempDir()
		c.AskConsent = false
		c.TaskComputers = testTaskComputers{engine: remote}
		if options != nil {
			options(c)
		}
	})
	return a, local
}
func remoteRecordFor(spec RunSpec) TaskRemoteRecord {
	r := *spec.Remote
	r.Attempt = "att_0123456789abcdef"
	r.Fingerprint = "sent-copy"
	return r
}
func waitRemoteRun(t *testing.T, a *Agent) {
	t.Helper()
	beltRunWaitFor(t, "remote task to settle", func() bool { a.beltMu.Lock(); defer a.beltMu.Unlock(); return a.beltRun == nil })
}
func approveRemoteProposal(t *testing.T, a *Agent, args taskArguments) (string, bool) {
	t.Helper()
	raw, _ := json.Marshal(args)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, stop := a.WatchTaskUpdates()
	defer stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.Task != nil && event.Kind == EventTaskProposal {
					a.ResolveTask(event.Task.ID, TaskAnswer{Approved: true})
				}
			}
		}
	}()
	answer, refused, err := a.stageTask(ctx, raw).Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return answer, refused
}
func taskOnOtherComputer() taskArguments {
	return taskArguments{Title: "Repair export", Summary: "Repair the export failure", Brief: "Repair the export path in this project", Deliverable: "a working export", Acceptance: "the export regression passes", Checks: []string{"go test ./export"}, Computer: " WORK COMPUTER "}
}

// Contract 6 sends the same settled instruction a local root worker would read.
func TestAProposalNamingAPairedComputerRunsThere(t *testing.T) {
	reached := make(chan RunSpec, 1)
	release := make(chan struct{})
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		_ = spec.SaveRemote(r, true)
		reached <- spec
		select {
		case <-release:
		case <-ctx.Done():
		}
		_ = spec.Store.CompleteRoot("done there")
		return RunSummary{Outcome: "done", Result: "done there"}
	}}
	a, local := remoteTaskAgent(t, remote, func(c *Config) {
		if err := config.WriteTaskRunsOn(c.ProfileDir, "dev_0123456789abcdef0123456789abcdef"); err != nil {
			t.Fatal(err)
		}
		c.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) {
			t.Error("remote work consulted the origin's crew")
			return crewroute.Decision{}, errors.New("origin crew must stay unused")
		}
	})
	args := taskOnOtherComputer()
	args.Model = "test/model"
	args.Effort = "cheap"
	args.MaxSteps = 17
	args.NoProgress = 9
	answer, refused := approveRemoteProposal(t, a, args)
	if refused {
		t.Fatalf("proposal refused: %s", answer)
	}
	spec := <-reached
	if spec.NamedModel != "test/model" || spec.NamedEffort != "cheap" || spec.StepsPerTask != 17 || spec.NoProgress != 9 {
		t.Fatalf("named controls = %+v", spec)
	}
	if !strings.Contains(spec.Brief, "a working export") || !strings.Contains(spec.Brief, "the export regression passes") || !strings.Contains(spec.Brief, "go test ./export") {
		t.Fatalf("instruction lost its contract: %s", spec.Brief)
	}
	if local.didRun() {
		t.Fatal("local model ran the remote task")
	}
	close(release)
	waitRemoteRun(t, a)
	if local.lands() != 1 {
		t.Fatalf("local landing count = %d", local.lands())
	}
}

// Contract 7 uses the stable preference only when the proposal omits placement.
func TestTheRunTasksOnSettingSendsATaskProposedWithoutAComputer(t *testing.T) {
	reached := make(chan struct{}, 1)
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		reached <- struct{}{}
		_ = spec.Store.CompleteRoot("done there")
		return RunSummary{Outcome: "done"}
	}}
	a, local := remoteTaskAgent(t, remote, func(c *Config) {
		if err := config.WriteTaskRunsOn(c.ProfileDir, "dev_0123456789abcdef0123456789abcdef"); err != nil {
			t.Fatal(err)
		}
	})
	args := taskOnOtherComputer()
	args.Computer = ""
	answer, refused := approveRemoteProposal(t, a, args)
	if refused {
		t.Fatalf("proposal: %s", answer)
	}
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("setting did not send the task")
	}
	waitRemoteRun(t, a)
	if local.didRun() {
		t.Fatal("setting ran locally")
	}
}

// Contract 7 keeps quick work beside its caller even with a remote preference.
func TestAQuickTaskRunsHereEvenWhenTheSettingNamesAnotherComputer(t *testing.T) {
	a, _ := remoteTaskAgent(t, &scriptedTaskEngine{start: func(context.Context, RunSpec) RunSummary {
		t.Error("quick work reached the other computer")
		return RunSummary{}
	}}, func(c *Config) { _ = config.WriteTaskRunsOn(c.ProfileDir, "dev_0123456789abcdef0123456789abcdef") })
	// The quick tool has its ordinary legacy-node road even when placement
	// preferences name another computer; it never reaches a remote RunEngine.
	t.Setenv("CODEAF_TASK_BELT", "off")
	answer, refused, quickErr := a.startQuickTask(context.Background(), json.RawMessage(`{"line":"Inspect the project","items":["Inspect the project"]}`))
	if quickErr != nil || refused {
		t.Fatalf("quick tool: %s, %v", answer, quickErr)
	}
	node := a.graph().node(1)
	waitDoneNode(t, node)
	if node.spec.kind() != TaskKindQuick {
		t.Fatal("quick tool did not create quick work")
	}
	p, err := a.resolveTaskComputer(context.Background(), "", taskStand{mode: TaskModeInPlace}, TaskKindQuick, false)
	if err != nil || p.computer != nil || p.sentence != "" {
		t.Fatalf("quick placement = %+v, %v", p, err)
	}
	p, err = a.resolveTaskComputer(context.Background(), "work computer", taskStand{mode: TaskModeInPlace}, TaskKindQuick, false)
	if err != nil || p.computer != nil || !strings.Contains(p.sentence, "a quick task runs here") {
		t.Fatalf("named quick placement = %+v, %v", p, err)
	}
}

// Contract 5 mirrors one paid call, skipping replay and the final-total fold.
func TestRemoteSpendLandsOnThisLedgerOnce(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		r := remoteRecordFor(spec)
		_ = spec.SaveRemote(r, true)
		charge := RunCharge{Model: "remote/model", TokensIn: 40, TokensOut: 8, USD: .25}
		var current uint64
		calls := 0
		watch := func(ctx context.Context, from uint64, alive func(), deliver func(uint64) error) error {
			calls++
			alive()
			for _, seq := range []uint64{1, 1, 2} {
				current = seq
				if err := deliver(seq); err != nil {
					return err
				}
			}
			return ErrTaskWatchEnded
		}
		err := FollowTaskWatch(ctx, nil, 0, watch, func(seq uint64) error {
			if current == 1 {
				return spec.OnRemoteCharge(seq, charge, "worker")
			}
			return nil
		}, func(bool) {})
		if err != nil {
			t.Error(err)
		}
		_ = spec.Store.CompleteRoot("done")
		return RunSummary{Outcome: "done", USD: .25}
	}}
	a, _ := remoteTaskAgent(t, remote, func(c *Config) { c.usageLedger = ledger })
	if _, _, _, err := a.StartTaskOn(context.Background(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, a)
	FlushUsage()
	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].USD != .25 || lines[0].Computer != "work computer" || lines[0].Task != "1" || lines[0].Session != a.journalID() {
		t.Fatalf("ledger = %+v", lines)
	}
	if got := a.Usage(); got.CostUSD != .25 || got.Calls != 1 || got.Input != 40 {
		t.Fatalf("usage = %+v", got)
	}
}

// Contract 7 keeps an explicit in-place request here even when repository
// isolation preserves a local branch for that request.
func TestAnExplicitInPlaceTaskRunsHereEvenWhenAComputerWasNamed(t *testing.T) {
	elsewhere := make(chan struct{}, 1)
	remote := &scriptedTaskEngine{start: func(ctx context.Context, spec RunSpec) RunSummary {
		elsewhere <- struct{}{}
		_ = spec.Store.CompleteRoot("wrong computer")
		return RunSummary{Outcome: "done"}
	}}
	a, local := remoteTaskAgent(t, remote, func(c *Config) {
		if err := config.WriteTaskRunsOn(c.ProfileDir, "dev_0123456789abcdef0123456789abcdef"); err != nil {
			t.Fatal(err)
		}
	})
	args := taskOnOtherComputer()
	args.Where = "in place"
	args.Deliverable = "a corrected " + filepath.Join(a.config.Workspace, "output.txt")
	args.Brief = "Write the corrected output in " + filepath.Join(a.config.Workspace, "output.txt")
	receipt, refused := approveRemoteProposal(t, a, args)
	if refused {
		t.Fatalf("proposal refused: %s", receipt)
	}
	select {
	case <-elsewhere:
		t.Fatal("explicit in-place task went to another computer")
	case <-local.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("local in-place task never started")
	}
	if !strings.Contains(receipt, "runs here") {
		t.Fatalf("named computer had no local-placement explanation: %s", receipt)
	}
	endBeltRun(t, a, local)
}
