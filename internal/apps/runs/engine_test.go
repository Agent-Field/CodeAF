package runs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

func TestRunResumesAfterCompletedStepsAndDurableWait(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	fake := runs.NewFakeConnector(map[string]runs.Reply{"demo.read": {Output: "ok"}})
	cfg := runs.Config{
		Root:      t.TempDir(),
		Now:       func() time.Time { return now },
		Connector: fake,
		Load: func(app, run string) (runs.Definition, error) {
			return runs.Parse([]byte(`
run: example
steps:
  - id: first
    tool: demo.read
  - id: pause
    wait: {for: 1h}
  - id: last
    tool: demo.read
`))
		},
	}
	open := func() *runs.Engine {
		t.Helper()
		e, err := runs.Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	e := open()
	run, err := e.Start("app", "example", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := e.Get(run.ID); err != nil || got.State != runs.Waiting || got.Step != "pause" {
		t.Fatalf("before restart = %+v, %v", got, err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e = open()
	defer e.Close()
	now = now.Add(time.Hour)
	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := e.Get(run.ID); err != nil || got.State != runs.Done {
		t.Fatalf("after restart = %+v, %v", got, err)
	}
	if calls := fake.Calls(); len(calls) != 2 || calls[0].Step != "first" || calls[1].Step != "last" {
		t.Fatalf("calls = %+v", calls)
	}
}

func newEngine(t *testing.T, yaml string, cfg runs.Config) *runs.Engine {
	t.Helper()
	if cfg.Root == "" {
		cfg.Root = t.TempDir()
	}
	cfg.Load = func(_, _ string) (runs.Definition, error) { return runs.Parse([]byte(yaml)) }
	e, err := runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func tick(t *testing.T, e *runs.Engine) {
	t.Helper()
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func start(t *testing.T, e *runs.Engine) runs.Run {
	t.Helper()
	r, err := e.Start("app", "example", nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func state(t *testing.T, e *runs.Engine, id string) runs.Run {
	t.Helper()
	r, err := e.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestScheduleCatchUpConsumesAllMissedSlotsOnce(t *testing.T) {
	now := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	cfg := runs.Config{
		Root: t.TempDir(), Now: func() time.Time { return now },
		Triggers: func() ([]runs.Trigger, error) {
			return []runs.Trigger{{App: "app", ID: "hourly", On: "schedule", Cron: "0 * * * *", Run: "example"}}, nil
		},
	}
	const definition = "run: example\nsteps:\n  - id: pause\n    wait: {for: 1h}"
	e := newEngine(t, definition, cfg)
	tick(t, e)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Hour)
	e = newEngine(t, definition, cfg)
	tick(t, e)
	tick(t, e)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e = newEngine(t, definition, cfg)
	tick(t, e)
	list, err := e.List("app", "example")
	if err != nil || len(list) != 1 || !list[0].CaughtUp {
		t.Fatalf("catch-up list = %+v, %v", list, err)
	}
	now = now.Add(time.Hour)
	tick(t, e)
	list, err = e.List("app", "example")
	if err != nil || len(list) != 2 || list[0].CaughtUp {
		t.Fatalf("next regular slot = %+v, %v", list, err)
	}
}

func TestOutboundNeedsApprovalAndSurvivesReopen(t *testing.T) {
	const definition = `
run: example
steps:
  - id: send
    tool: gmail.send
    args: {to: me@example.test, body: message}
`
	fake := runs.NewFakeConnector(map[string]runs.Reply{"gmail.send": {Output: "sent"}})
	cfg := runs.Config{Root: t.TempDir(), Connector: fake}
	e := newEngine(t, definition, cfg)
	first, second := start(t, e), start(t, e)
	tick(t, e)
	if len(fake.Calls()) != 0 {
		t.Fatal("connector was called before approval")
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e = newEngine(t, definition, cfg)
	approval := state(t, e, first.ID).Approval
	if approval == nil || approval.Kind != "send" || approval.Args["body"] != "message" {
		t.Fatalf("approval = %+v", approval)
	}
	if err := e.Answer(second.ID, approval.ID, true); err == nil {
		t.Fatal("another run's approval was accepted")
	}
	if err := e.Answer(first.ID, approval.ID, true); err != nil {
		t.Fatal(err)
	}
	declined := state(t, e, second.ID).Approval
	if err := e.Answer(second.ID, declined.ID, false); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if calls := fake.Calls(); len(calls) != 1 || calls[0].ID != first.ID {
		t.Fatalf("calls = %+v", calls)
	}
	if state(t, e, first.ID).State != runs.Done || state(t, e, second.ID).State != runs.Incomplete {
		t.Fatal("expected approved run done and denied run incomplete")
	}
}

func TestLoopIncludesOnlyAllowedRounds(t *testing.T) {
	fake := runs.NewFakeConnector(map[string]runs.Reply{"demo.read": {Output: "read"}})
	e := newEngine(t, `
run: example
steps:
  - id: each
    loop: {over: [a,b,c], as: item, max: 2}
    steps:
      - id: read
        tool: demo.read
        args: {subject: $item}
`, runs.Config{Connector: fake})
	r := start(t, e)
	tick(t, e)
	got := state(t, e, r.ID)
	if got.State != runs.YourCall || got.Error != "out of rounds" {
		t.Fatalf("run = %+v", got)
	}
	calls := fake.Calls()
	if len(calls) != 2 || calls[0].Args["subject"] != "a" || calls[1].Args["subject"] != "b" || calls[0].Key == calls[1].Key {
		t.Fatalf("calls = %+v", calls)
	}
}

type connectorFunc func(context.Context, runs.Call) (runs.Reply, error)

func (f connectorFunc) Call(ctx context.Context, call runs.Call) (runs.Reply, error) {
	return f(ctx, call)
}

func TestRetryBackoffDoublesAndContinuesOnError(t *testing.T) {
	now := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	attempts := 0
	e := newEngine(t, `
run: example
steps:
  - id: flaky
    tool: demo.read
    retry: {max: 2, backoff: 30s}
    on_error: continue
  - id: conditional
    if: "false"
    tool: demo.skip
`, runs.Config{
		Now: func() time.Time { return now },
		Connector: connectorFunc(func(_ context.Context, c runs.Call) (runs.Reply, error) {
			if c.Step != "flaky" {
				t.Fatalf("skipped step called: %v", c)
			}
			attempts++
			return runs.Reply{}, errors.New("temporary error")
		}),
	})
	r := start(t, e)
	tick(t, e)
	tick(t, e)
	if got := state(t, e, r.ID); attempts != 1 || !got.WakeAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("first retry = %+v after %d attempts", got, attempts)
	}
	now = now.Add(30 * time.Second)
	tick(t, e)
	if got := state(t, e, r.ID); attempts != 2 || !got.WakeAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("second retry = %+v after %d attempts", got, attempts)
	}
	now = now.Add(time.Minute)
	tick(t, e)
	if got := state(t, e, r.ID); attempts != 3 || got.State != runs.Done {
		t.Fatalf("run = %+v after %d attempts", got, attempts)
	}
}

func TestRecordTriggerUsesCommittedChange(t *testing.T) {
	var calls []runs.RecordCall
	e := newEngine(t, `
run: example
steps:
  - id: create
    records.create: {type: handoff, set: {deal: $record.id}}
`, runs.Config{
		Triggers: func() ([]runs.Trigger, error) {
			return []runs.Trigger{{App: "crm", ID: "won", On: "record.changed", Type: "deal", Run: "example",
				When: "changed('stage') && stage == 'Won'"}}, nil
		},
		Records: func(_ context.Context, c runs.RecordCall) (runs.Reply, error) {
			calls = append(calls, c)
			return runs.Reply{}, nil
		},
	})
	for _, before := range []string{"Won", "Proposal"} {
		if err := e.RecordChanged(runs.Change{Type: "deal", ID: "d1", Before: map[string]any{"stage": before}, After: map[string]any{"stage": "Won"}}); err != nil {
			t.Fatal(err)
		}
	}
	tick(t, e)
	if len(calls) != 1 || calls[0].App != "crm" || calls[0].Type != "handoff" || calls[0].Set["deal"] != "d1" {
		t.Fatalf("record writes = %+v", calls)
	}
}

func TestBudgetStopsNextPaidStep(t *testing.T) {
	calls := 0
	e := newEngine(t, `
run: example
budget: {per_run: 0.5}
steps:
  - id: draft
    agent: Example
    prompt: Draft it
  - id: refine
    agent: Example
    prompt: Refine it
`, runs.Config{
		Agent: func(_ context.Context, c runs.AgentCall) (runs.Reply, error) {
			calls++
			if c.Remaining != 0.5 || c.Prompt != "Draft it" {
				t.Fatalf("agent call = %+v", c)
			}
			return runs.Reply{Output: "draft", Cost: 0.5}, nil
		},
	})
	r := start(t, e)
	tick(t, e)
	if got := state(t, e, r.ID); calls != 1 || got.State != runs.YourCall || got.Step != "refine" {
		t.Fatalf("run = %+v after %d model calls", got, calls)
	}
}

func TestBudgetRaiseIsDurableAndOnlyAnswersCurrentAsk(t *testing.T) {
	const definition = `
run: example
budget: {per_run: 0.25}
steps:
  - id: generate
    agent: Example
    prompt: "Generate"
`
	cfg := runs.Config{
		Root: t.TempDir(),
		Agent: func(_ context.Context, call runs.AgentCall) (runs.Reply, error) {
			if call.Remaining < 1 {
				return runs.Reply{}, runs.ErrBudget
			}
			return runs.Reply{Output: "done", Cost: 0.7}, nil
		},
	}
	e := newEngine(t, definition, cfg)
	r := start(t, e)
	tick(t, e)
	first := state(t, e, r.ID).Approval
	if first == nil || first.Kind != "budget" {
		t.Fatalf("approval = %+v", first)
	}
	if err := e.RaiseBudget(r.ID, first.ID, .5); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	second := state(t, e, r.ID).Approval
	if second == nil || second.ID == first.ID {
		t.Fatalf("second approval = %+v", second)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e = newEngine(t, definition, cfg)
	if err := e.RaiseBudget(r.ID, first.ID, 1); err == nil {
		t.Fatal("an earlier approval raised the budget")
	}
	if err := e.RaiseBudget(r.ID, second.ID, 1); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if got := state(t, e, r.ID); got.State != runs.Done || got.Cost != .7 {
		t.Fatalf("run = %+v", got)
	}
}

func TestInterruptedOutboundIsNeverReplayed(t *testing.T) {
	const definition = `
run: example
steps:
  - id: send
    tool: gmail.send
`
	cfg := runs.Config{Root: t.TempDir()}
	cfg.Connector = connectorFunc(func(_ context.Context, _ runs.Call) (runs.Reply, error) {
		panic("process disappeared after sending")
	})
	e := newEngine(t, definition, cfg)
	r := start(t, e)
	tick(t, e)
	if err := e.Answer(r.ID, state(t, e, r.ID).Approval.ID, true); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected to interrupt send")
			}
		}()
		tick(t, e)
	}()
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	fake := runs.NewFakeConnector(map[string]runs.Reply{"gmail.send": {Output: "sent"}})
	cfg.Connector = fake
	e = newEngine(t, definition, cfg)
	tick(t, e)
	if got := state(t, e, r.ID); got.State != runs.Incomplete || len(fake.Calls()) != 0 {
		t.Fatalf("run = %+v, calls = %+v", got, fake.Calls())
	}
}
