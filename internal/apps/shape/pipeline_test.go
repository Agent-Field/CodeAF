package shape

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/sample"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type fakeShaper struct {
	mu     sync.Mutex
	inputs []svc.ShapeInput
	shape  func(svc.ShapeInput) (svc.ShapeOutput, error)
}

func (f *fakeShaper) Shape(_ context.Context, in svc.ShapeInput) (svc.ShapeOutput, error) {
	f.mu.Lock()
	f.inputs = append(f.inputs, in)
	f.mu.Unlock()
	if f.shape != nil {
		return f.shape(in)
	}
	return svc.ShapeOutput{Files: spec.Files{"agent.md": append(in.Files["agent.md"], []byte("\n"+in.Request)...)}, Summary: "Changed the agent"}, nil
}
func (f *fakeShaper) calls() []svc.ShapeInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]svc.ShapeInput{}, f.inputs...)
}

type fakeShapeCheck struct {
	run func(spec.Files, int) (bridge.CheckReport, error)
}

func (f fakeShapeCheck) Run(_ context.Context, _ string, files spec.Files, round int) (bridge.CheckReport, error) {
	if f.run != nil {
		return f.run(files, round)
	}
	return bridge.CheckReport{Pass: true, Round: round, Rendered: []string{"a", "b", "c", "d"}, Runs: []string{"one", "two", "three"}}, nil
}

type fakeShapeApprovals struct {
	svc.Approvals
	mu    sync.Mutex
	items []bridge.Approval
}

func (f *fakeShapeApprovals) Open(_ context.Context, _ svc.Scope, a bridge.Approval) (bridge.Approval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = append(f.items, a)
	return a, nil
}
func (f *fakeShapeApprovals) list() []bridge.Approval {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bridge.Approval{}, f.items...)
}

// Fake apply exercises the pipeline's service seam while the separate apply
// lane owns disk migrations, version tags and the applied event.
type fakeShapeApply struct {
	svc.Shape
	s         *Service
	migration []bridge.MigrationLine
	applied   chan int
}

func (f *fakeShapeApply) Preview(context.Context, svc.Scope, bridge.Empty) (bridge.ApplyPreview, error) {
	return bridge.ApplyPreview{Migration: f.migration}, nil
}
func (f *fakeShapeApply) Apply(_ context.Context, scope svc.Scope, p bridge.ShapeApplyParams) (bridge.AppSummary, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	d := f.s.drafts[draftKey(scope)]
	if d == nil || d.State != "ready" {
		return bridge.AppSummary{}, fmt.Errorf("not ready")
	}
	version := bridge.Version{N: p.Base + 1}
	f.s.all.Hub.Publish(bridge.TopicShape(scope.App, scope.Caller.Member), bridge.ShapeEvent{Kind: "applied", Version: &version})
	delete(f.s.drafts, draftKey(scope))
	f.applied <- version.N
	return bridge.AppSummary{Version: version.N}, nil
}

func pipelineRig(t *testing.T) (*Service, *svc.Services, svc.Scope, *fakeShaper, <-chan svc.Event) {
	t.Helper()
	s, cfg, all, scope := setupDraft(t)
	all.Sample = sample.New(cfg, all)
	shaper := &fakeShaper{}
	all.Shaper = shaper
	all.Check = fakeShapeCheck{}
	all.Approvals = &fakeShapeApprovals{}
	// Workspace semantics retain the draft; personal behavior is tested separately.
	all.Shape = &fakeShapeApply{Shape: s, s: s, migration: []bridge.MigrationLine{{Op: "add"}}, applied: make(chan int, 4)}
	openDraft(t, s, scope)
	events, cancel := all.Hub.Subscribe(bridge.TopicShape(scope.App, scope.Caller.Member))
	t.Cleanup(cancel)
	t.Cleanup(func() { waitWorkers(t, s) })
	return s, all, scope, shaper, events
}

func waitShape(t *testing.T, events <-chan svc.Event, match func(bridge.ShapeEvent) bool) []bridge.ShapeEvent {
	t.Helper()
	seen := []bridge.ShapeEvent{}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			value, ok := event.Data.(bridge.ShapeEvent)
			if !ok {
				t.Fatalf("unexpected event %+v", event)
			}
			seen = append(seen, value)
			if match(value) {
				return seen
			}
		case <-timer.C:
			t.Fatalf("timed out; events=%+v", seen)
		}
	}
}
func waitChange(t *testing.T, events <-chan svc.Event, id, state string) []bridge.ShapeEvent {
	return waitShape(t, events, func(e bridge.ShapeEvent) bool {
		return e.Kind == "change" && e.Change != nil && e.Change.ID == id && e.Change.State == state
	})
}

func TestPipelinePassStreamsFourSteps(t *testing.T) {
	s, _, scope, shaper, events := pipelineRig(t)
	c, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "add agent advice"})
	if err != nil || c.State != "checking" {
		t.Fatalf("request=%+v %v", c, err)
	}
	seen := waitChange(t, events, c.ID, "passed")
	steps := []string{}
	for _, e := range seen {
		if e.Kind == "step" && e.Change != nil {
			for _, step := range e.Change.Steps {
				if step.Status == "done" && !slicesContains(steps, step.Step) {
					steps = append(steps, step.Step)
				}
			}
		}
	}
	if !reflect.DeepEqual(steps, []string{"plan", "write", "render", "behaviour"}) {
		t.Fatalf("steps=%v", steps)
	}
	got, err := s.Get(context.Background(), scope, bridge.Empty{})
	if err != nil || got.Draft.State != "ready" || got.Draft.Changes[0].Summary != "Changed the agent" {
		t.Fatalf("draft=%+v %v", got, err)
	}
	if len(shaper.calls()) != 1 || len(shaper.calls()[0].Sample) == 0 {
		t.Fatal("missing synthetic sample")
	}
	diff, err := s.Diff(context.Background(), scope, bridge.ShapeDiffParams{Change: c.ID})
	if err != nil || len(diff.Files) != 1 || diff.Files[0].Path != "agent.md" {
		t.Fatalf("diff=%+v %v", diff, err)
	}
}
func slicesContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func TestPipelineThreeRoundsFixAndCancel(t *testing.T) {
	s, all, scope, shaper, events := pipelineRig(t)
	all.Check = fakeShapeCheck{run: func(files spec.Files, round int) (bridge.CheckReport, error) {
		if strings.Contains(string(files["agent.md"]), "Fix: wire the invoice run") {
			return bridge.CheckReport{Pass: true, Round: round}, nil
		}
		return bridge.CheckReport{Round: round, Issues: []spec.Issue{{Rule: "behaviour/run", Message: "no run handles Won"}}}, nil
	}}
	c, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "create an invoice on Won"})
	if err != nil {
		t.Fatal(err)
	}
	waitChange(t, events, c.ID, "failed")
	got, _ := s.Get(context.Background(), scope, bridge.Empty{})
	failed := got.Draft.Changes[0]
	if got.Draft.State == "ready" || failed.Round != 3 || failed.Error != "Check failed: no run handles Won  round 3 of 3" {
		t.Fatalf("failed=%+v", got)
	}
	calls := shaper.calls()
	if len(calls) != 3 || calls[0].Report != nil || calls[1].Report == nil || calls[2].Report.Round != 2 {
		t.Fatalf("calls=%+v", calls)
	}
	fixed, err := s.FixChange(context.Background(), scope, bridge.ShapeChangeFixParams{Change: c.ID, Hint: "wire the invoice run"})
	if err != nil || fixed.Request != "Fix: wire the invoice run" {
		t.Fatalf("fix=%+v %v", fixed, err)
	}
	waitChange(t, events, c.ID, "passed")
	if len(shaper.calls()) != 4 || shaper.calls()[3].Round != 1 {
		t.Fatal("Fix did not restart round count")
	}
	other, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "extra note"})
	if err != nil {
		t.Fatal(err)
	}
	waitChange(t, events, other.ID, "passed")
	draft, err := s.CancelChange(context.Background(), scope, bridge.ShapeChangeCancelParams{Change: other.ID})
	if err != nil || len(draft.Changes) != 1 || draft.Changes[0].ID != c.ID {
		t.Fatalf("cancel=%+v %v", draft, err)
	}
	select {
	case <-all.Shape.(*fakeShapeApply).applied:
		t.Fatal("failure reached Apply")
	default:
	}
}

func TestPipelineQueueAndCancelInFlight(t *testing.T) {
	s, _, scope, shaper, events := pipelineRig(t)
	entered, release := make(chan struct{}), make(chan struct{})
	shaper.shape = func(in svc.ShapeInput) (svc.ShapeOutput, error) {
		if in.Request == "first" {
			close(entered)
			<-release
		}
		return svc.ShapeOutput{Files: spec.Files{"agent.md": append(in.Files["agent.md"], []byte("\n"+in.Request)...)}}, nil
	}
	first, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	second, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if len(shaper.calls()) != 1 {
		t.Fatal("draft calls ran concurrently")
	}
	if _, err := s.CancelChange(context.Background(), scope, bridge.ShapeChangeCancelParams{Change: first.ID}); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitChange(t, events, second.ID, "passed")
	s.mu.Lock()
	d := s.drafts[draftKey(scope)]
	files, err := d.Files()
	s.mu.Unlock()
	if err != nil || strings.Contains(string(files["agent.md"]), "first") || !strings.Contains(string(files["agent.md"]), "second") {
		t.Fatalf("cancelled model result joined files: %s %v", files["agent.md"], err)
	}
}

func TestPipelinePlanEdit(t *testing.T) {
	s, _, scope, _, events := pipelineRig(t)
	c, err := s.EditPlan(context.Background(), scope, bridge.ShapePlanEditParams{Section: "Data", ID: "deal", Text: "rename a field"})
	if err != nil || c.Request != "Data › deal: rename a field" || c.From != "plan" {
		t.Fatalf("edit=%+v %v", c, err)
	}
	waitChange(t, events, c.ID, "passed")
}

func TestPersonalAutoApplyAndMigrationPause(t *testing.T) {
	for _, migration := range []bool{false, true} {
		t.Run(fmt.Sprint(migration), func(t *testing.T) {
			s, all, scope, _, events := pipelineRig(t)
			apply := all.Shape.(*fakeShapeApply)
			if !migration {
				apply.migration = nil
			}
			c, err := s.Request(context.Background(), scope, bridge.ShapeRequestParams{Text: "add a weekly chart"})
			if err != nil {
				t.Fatal(err)
			}
			if migration {
				waitChange(t, events, c.ID, "passed")
				got, _ := s.Get(context.Background(), scope, bridge.Empty{})
				if got.Draft.State != "ready" {
					t.Fatalf("draft=%+v", got)
				}
				return
			}
			seen := waitShape(t, events, func(e bridge.ShapeEvent) bool { return e.Kind == "applied" })
			if seen[len(seen)-1].Version.N != 2 {
				t.Fatalf("applied=%+v", seen)
			}
		})
	}
}

func TestShapeWriteAliasLog(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"records": map[string]any{"deal": map[string]any{"fields": map[string]any{"amount": map[string]any{"alias": "value"}}}}})
	if got := writeLog(spec.Files{"app.json": raw}); got != "Wrote app.json · alias value → amount" {
		t.Fatal(got)
	}
}

func waitWorkers(t *testing.T, s *Service) {
	t.Helper()
	done := make(chan struct{})
	go func() { s.pipeline.workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("draft workers did not stop")
	}
}
