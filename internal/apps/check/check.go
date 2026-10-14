package check

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Service struct{ all *svc.Services }

var _ svc.Check = (*Service)(nil)

func New(_ svc.Config, all *svc.Services) *Service { return &Service{all: all} }

// Report retains substeps and repair hints locally; the bridge's fixed report
// carries the hints in issue messages and progress in BuildEvent.Log.
type Report struct {
	Passed   bool     `json:"passed"`
	Steps    []Step   `json:"steps"`
	Issues   []Issue  `json:"issues"`
	Rendered []string `json:"rendered"`
	Runs     []string `json:"runs"`
}

type Step struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Issue struct {
	spec.Issue
	FixHint string `json:"fix_hint"`
}

func (s *Service) Check(ctx context.Context, appDir string) (Report, error) {
	files, err := spec.ReadDir(appDir)
	if err != nil {
		return Report{}, err
	}
	b, err := spec.Decode(files)
	app := ""
	if err == nil {
		app = b.App.ID
	}
	return s.check(ctx, app, files, 1)
}

func (s *Service) Run(ctx context.Context, app string, files spec.Files, round int) (bridge.CheckReport, error) {
	report, err := s.check(ctx, app, files, round)
	return report.wire(round), err
}

func (r Report) wire(round int) bridge.CheckReport {
	issues := make([]spec.Issue, 0, len(r.Issues))
	for _, issue := range r.Issues {
		wire := issue.Issue
		if issue.FixHint != "" {
			wire.Message += "; fix: " + issue.FixHint
		}
		issues = append(issues, wire)
	}
	return bridge.CheckReport{Pass: r.Passed, Round: round, Issues: issues, Rendered: r.Rendered, Runs: r.Runs}
}

func (s *Service) check(ctx context.Context, app string, files spec.Files, round int) (r Report, err error) {
	r = Report{Issues: []Issue{}, Rendered: []string{}, Runs: []string{}}
	for _, name := range []string{"schema", "kit", "render", "behaviour"} {
		r.Steps = append(r.Steps, Step{Name: name, Status: "skipped"})
	}
	defer func() {
		if err == nil {
			err = ctx.Err()
		}
		if err != nil && ctx.Err() == nil {
			r.Issues = append(r.Issues, finding(spec.AppFile, "", "check/error", err.Error(), "the message names the file and value Check could not load; repair that value against the File schemas and kit catalog"))
			err = nil
		}
		r.Passed = err == nil && len(r.Issues) == 0
		for _, step := range r.Steps {
			if step.Status == "skipped" {
				s.progress(app, round, step.Name, "skipped", &r, 0)
			}
		}
	}()
	if err = ctx.Err(); err != nil {
		return
	}
	s.progress(app, round, "schema", "running", nil, 0)
	r.Issues = append(r.Issues, validateFiles(files)...)
	s.finish(app, round, &r, 0, 0, 0)
	if len(r.Issues) > 0 {
		return
	}
	b, decodeErr := spec.Decode(files)
	if decodeErr != nil {
		return r, decodeErr
	}
	kit, kitErr := spec.LoadKit()
	if kitErr != nil {
		return r, kitErr
	}
	cap := b.App.Budget.Month
	s.progress(app, round, "kit", "running", nil, cap)
	r.Issues = append(r.Issues, lint(b, kit)...)
	r.Issues = append(r.Issues, computedExpressions(b)...)
	s.finish(app, round, &r, 1, 0, cap)
	if err = ctx.Err(); err != nil {
		return
	}
	s.progress(app, round, "render", "running", nil, cap)
	if s.all == nil || s.all.Sample == nil {
		r.Steps[2].Status = "failed"
		s.progress(app, round, "render", "failed", &r, cap)
		return r, bridge.NotBuilt("sample")
	}
	rows, sampleErr := s.all.Sample.Generate(ctx, b)
	if sampleErr != nil {
		r.Steps[2].Status = "failed"
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		r.Issues = append(r.Issues, finding(spec.AppFile, "/records", "render/sample", sampleErr.Error(), "repair the named field or computed expression in app.json; use now - days(7) for a week ago"))
		s.progress(app, round, "render", "failed", &r, cap)
		return r, nil
	}
	before := len(r.Issues)
	for _, id := range b.ScreenIDs() {
		if err = ctx.Err(); err != nil {
			return
		}
		issues := render(ctx, b, kit, b.Screens[id], rows)
		r.Issues = append(r.Issues, issues...)
		if len(issues) == 0 {
			r.Rendered = append(r.Rendered, id)
		}
	}
	s.finish(app, round, &r, 2, before, cap)
	if len(r.Issues) > 0 {
		return
	}
	s.progress(app, round, "behaviour", "running", nil, cap)
	status, runErr := s.behaviour(ctx, app, b, rows, &r)
	err = runErr
	r.Steps[3].Status = status
	s.progress(app, round, "behaviour", status, &r, cap)
	return
}

func (s *Service) finish(app string, round int, r *Report, index, before int, cap float64) {
	status := "done"
	if len(r.Issues) > before {
		status = "failed"
	}
	r.Steps[index].Status = status
	s.progress(app, round, r.Steps[index].Name, status, r, cap)
}

func (s *Service) progress(app string, round int, step, status string, r *Report, cap float64) {
	if s.all == nil || s.all.Hub == nil || app == "" {
		return
	}
	event := bridge.BuildEvent{Step: bridge.StepCheck, Status: bridge.BuildRunning, Round: round,
		Log: step + " · " + status, Cost: bridge.Cost{Cap: cap}, At: time.Now().UTC().Format(time.RFC3339Nano)}
	if r != nil {
		report := r.wire(round)
		event.Check = &report
	}
	// The Builder owns the round's final status and its spend. Substeps stay
	// running at the top level so none announces Preview before the round ends.
	s.all.Hub.Publish(bridge.TopicBuild(app), event)
}

func (s *Service) behaviour(ctx context.Context, app string, b *spec.Bundle, rows map[string][]bridge.Record, r *Report) (string, error) {
	if len(b.Runs) == 0 {
		return "done", nil
	}
	if s.all.Runs == nil || s.all.Records == nil {
		return "skipped", nil
	}
	scope := svc.Scope{App: app, Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, Data: svc.DataSample}
	pending := false
	for _, id := range b.RunIDs() {
		if err := s.all.Records.Seed(ctx, app, rows); err != nil {
			return "failed", err
		}
		params, paramErr := behaviourParams(b, id, rows)
		if paramErr != nil {
			r.Issues = append(r.Issues, finding(spec.AppFile, "/triggers", "behaviour/params", paramErr.Error(), "bind declared run params from $record.id or $record.<field> in the triggering record"))
			continue
		}
		state, err := s.all.Runs.Start(ctx, scope, bridge.StartRunParams{Run: id, Params: params})
		var serviceErr *bridge.Error
		if errors.As(err, &serviceErr) && serviceErr.Code == bridge.CodeNotBuilt {
			pending = true
			continue
		}
		if err != nil {
			r.Issues = append(r.Issues, finding("runs/"+id+".yaml", "", "behaviour/run", err.Error(), runFailureHint(err.Error())))
			continue
		}
		if state.Status == bridge.RunFailed || state.Status == bridge.RunStopped {
			r.Issues = append(r.Issues, finding("runs/"+id+".yaml", "", "behaviour/run", "run "+string(state.Status)+": "+state.Error, runFailureHint(state.Error)))
			continue
		}
		if state.Status != bridge.RunDone {
			// Start is asynchronous and the seam offers no bounded dry-run or
			// cancellation. Starting is not evidence that behaviour passed.
			pending = true
			continue
		}
		if !state.Fake {
			r.Issues = append(r.Issues, finding("runs/"+id+".yaml", "", "behaviour/fakes", "run did not use connector fakes", "use sample mode and fake connectors"))
			continue
		}
		r.Runs = append(r.Runs, id)
	}
	if len(r.Issues) > 0 {
		return "failed", nil
	}
	if pending {
		return "skipped", nil
	}
	return "done", nil
}

func finding(file, path, rule, message, hint string) Issue {
	return Issue{Issue: spec.Issue{File: file, Path: path, Rule: rule, Message: message}, FixHint: hint}
}

func ptr(path, key string) string {
	return path + "/" + escape(key)
}

func nodeChildren(n *spec.Node, path string, visit func(*spec.Node, string)) {
	for i, child := range n.Children {
		visit(child, fmt.Sprintf("%s/children/%d", path, i))
	}
	for _, key := range keys(n.Slots) {
		visit(n.Slots[key], ptr(path+"/slots", key))
	}
}
