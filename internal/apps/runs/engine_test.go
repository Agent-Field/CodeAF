package runs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	if got, err := e.Get(run.ID); err != nil || got.State != runs.Running || got.WakeAt.IsZero() || got.Step != "pause" {
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
params:
  items: {type: list, default: [a,b,c]}
steps:
  - id: each
    loop: {over: $params.items, as: item, max: 2}
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

func TestDispatchDoesNotHoldOtherRunsBehindAgent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	blocked := make(chan struct{})
	done := make(chan struct{})
	e, err := runs.Open(runs.Config{
		Root: t.TempDir(),
		Load: func(_, run string) (runs.Definition, error) {
			if run == "slow" {
				return runs.Parse([]byte("run: slow\nsteps:\n  - id: model\n    agent: Example\n    prompt: Go"))
			}
			return runs.Parse([]byte("run: normal\nsteps:\n  - id: ask\n    gate: {approval: plan}"))
		},
		Agent: func(ctx context.Context, _ runs.AgentCall) (runs.Reply, error) {
			close(blocked)
			<-ctx.Done()
			return runs.Reply{}, ctx.Err()
		},
		Ask: func(r runs.Run) {
			if r.DefinitionID == "normal" {
				close(done)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if _, err := e.Start("app", "slow", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.Dispatch(ctx, func(err error) { t.Error(err) }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatal("slow run never reached the model")
	}
	normal, err := e.Start("app", "normal", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Dispatch(ctx, func(err error) { t.Error(err) }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		if got := state(t, e, normal.ID); got.Approval == nil {
			t.Fatalf("run = %+v", got)
		}
	case <-ctx.Done():
		t.Fatal("another run's model blocked the approval")
	}
}

func TestIfUsesScopedRecordsAndDollarReferences(t *testing.T) {
	fake := runs.NewFakeConnector(map[string]runs.Reply{"demo.read": {Output: "ok"}})
	e := newEngine(t, `
run: example
params:
  currency: {type: text, default: '$USD'}
steps:
  - id: only
    if: "count(records('deal')) == 1 && $me == me() && $params.currency == '$USD'"
    tool: demo.read
`, runs.Config{
		Connector: fake,
		ReadRecords: func(app, typ string) ([]any, error) {
			if app != "app" || typ != "deal" {
				t.Fatalf("record read = %s %s", app, typ)
			}
			return []any{map[string]any{"name": "Deal"}}, nil
		},
	})
	r := start(t, e)
	tick(t, e)
	if got := state(t, e, r.ID); got.State != runs.Done || len(fake.Calls()) != 1 {
		t.Fatalf("run = %+v, calls = %+v", got, fake.Calls())
	}
}

func TestScheduleCatchesUpWhenSavedBeforeFirstScan(t *testing.T) {
	now := time.Date(2026, 11, 25, 14, 22, 0, 0, time.UTC)
	e := newEngine(t, "run: example\nsteps:\n  - id: wait\n    wait: {for: 1h}", runs.Config{
		Now: func() time.Time { return now },
		Triggers: func() ([]runs.Trigger, error) {
			return []runs.Trigger{{App: "app", ID: "hourly", On: "schedule", Cron: "0 * * * *",
				Run: "example", Since: now.Add(-4 * time.Hour)}}, nil
		},
	})
	tick(t, e)
	tick(t, e)
	list, err := e.List("app", "example")
	if err != nil || len(list) != 1 || !list[0].CaughtUp {
		t.Fatalf("catch-up list = %+v, %v", list, err)
	}
}

func TestOnErrorJumpsFromLoopToEnclosingStep(t *testing.T) {
	fake := runs.NewFakeConnector(map[string]runs.Reply{"demo.recover": {Output: "recovered"}})
	e := newEngine(t, `
run: example
steps:
  - id: loop
    loop: {max: 2, until: "false"}
    steps:
      - id: fail
        tool: demo.missing
        on_error: goto recover
  - id: skip
    tool: demo.skip
  - id: recover
    tool: demo.recover
`, runs.Config{Connector: fake})
	r := start(t, e)
	tick(t, e)
	calls := fake.Calls()
	if state(t, e, r.ID).State != runs.Done || len(calls) != 2 || calls[1].Step != "recover" {
		t.Fatalf("run did not reach recovery: %+v", calls)
	}
}

func TestRecordChangesLoadsTriggerDefinitionsOncePerBatch(t *testing.T) {
	scans := 0
	e := newEngine(t, "run: example\nsteps:\n  - id: pause\n    wait: {for: 1h}", runs.Config{
		Triggers: func() ([]runs.Trigger, error) {
			scans++
			return []runs.Trigger{{App: "crm", ID: "won", On: "record.changed", Type: "deal", Run: "example",
				When: "changed('stage') && stage == 'Won'"}}, nil
		},
	})
	err := e.RecordChanges([]runs.Change{
		{Type: "deal", ID: "first", Before: map[string]any{"stage": "Lead"}, After: map[string]any{"stage": "Won"}},
		{Type: "deal", ID: "second", Before: map[string]any{"stage": "Won"}, After: map[string]any{"stage": "Won"}},
		{Type: "deal", ID: "third", Before: map[string]any{"stage": "Lead"}, After: map[string]any{"stage": "Won"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := e.List("crm", "example")
	if err != nil || len(list) != 2 || scans != 1 {
		t.Fatalf("runs = %+v, scans = %d, error = %v", list, scans, err)
	}
}

func TestManifestOutboundOperationUsesProofApproval(t *testing.T) {
	fake := runs.NewFakeConnector(map[string]runs.Reply{"github.pr.create": {Output: "opened"}})
	e := newEngine(t, "run: example\nsteps:\n  - id: open\n    tool: github.pr.create", runs.Config{Connector: fake})
	r := start(t, e)
	tick(t, e)
	gate := state(t, e, r.ID).Approval
	if gate == nil || gate.Kind != "proof" || len(fake.Calls()) != 0 {
		t.Fatalf("manifest gate = %+v, calls = %+v", gate, fake.Calls())
	}
	if err := e.Answer(r.ID, gate.ID, true); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if len(fake.Calls()) != 1 || state(t, e, r.ID).State != runs.Done {
		t.Fatal("proof approval did not release the step exactly once")
	}
}

func TestBeforeMeetingTriggerUsesCalendarStartAndDeduplicates(t *testing.T) {
	// Calendar events carry an offset on start.dateTime.
	calendar := map[string]any{"summary": "Asterloom evaluation", "start": map[string]any{"dateTime": "2026-11-27T10:00:00-05:00"}}
	start := calendar["start"].(map[string]any)["dateTime"]
	row := map[string]any{"id": "01JD8Z0000000000000000000A", "title": calendar["summary"], "starts_at": start, "external": true}
	now := time.Date(2026, 11, 27, 14, 29, 59, 0, time.UTC)
	root := t.TempDir()
	const manifest = `{
		"id":"desk","name":"Account desk","kit":2,
		"records":{"meeting":{"fields":{
			"title":{"type":"text"},"starts_at":{"type":"datetime"},"external":{"type":"bool"}
		}}},
		"capabilities":{"records":{"meeting":"r"}},
		"triggers":[{"id":"prep","on":"record.before","run":"brief","type":"meeting","at":"starts_at","lead":"30m","when":"external == true"}]
	}`
	const run = `run: brief
steps:
  - id: review
    gate: {approval: proof}
`
	current := filepath.Join(root, "apps", "desk", "current")
	if err := os.MkdirAll(filepath.Join(current, "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, contents := range map[string]string{
		filepath.Join(current, "../meta.json"):       `{"version":1}`,
		filepath.Join(current, "app.json"):           manifest,
		filepath.Join(current, "runs", "brief.yaml"): run,
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files := runs.Files(filepath.Join(root, "apps"))
	cfg := runs.Config{
		Root: filepath.Join(root, "runs"),
		Now:  func() time.Time { return now },
		ReadRecords: func(app, typ string) ([]any, error) {
			if app != "desk" || typ != "meeting" {
				t.Fatalf("read the wrong records %q %q", app, typ)
			}
			return []any{row}, nil
		},
		Triggers: files.Triggers,
		Load:     files.Load,
	}
	check := func(e *runs.Engine, want int) {
		t.Helper()
		if err := e.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
		got, err := e.List("desk", "brief")
		if err != nil || len(got) != want {
			t.Fatalf("at %s: got %d runs, want %d, err %v", now, len(got), want, err)
		}
		if want == 1 && got[0].Approval == nil {
			t.Fatal("the due run did not reach its review gate")
		}
	}
	e, err := runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	check(e, 0)
	now = now.Add(time.Second)
	check(e, 1)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e, err = runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	check(e, 1)
}
