package shape

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type pipeline struct {
	workers   sync.WaitGroup
	running   map[string]bool
	epochs    map[string]int
	replaying map[string]bool
	active    map[string]string
}

type shapeJob struct {
	scope  svc.Scope
	draft  *draft
	change bridge.Change
	base   int
	epoch  int
	files  spec.Files
}

func (s *Service) initPipeline() {
	s.pipeline = &pipeline{running: map[string]bool{}, epochs: map[string]int{}, replaying: map[string]bool{}, active: map[string]string{}}
	s.replayHook = func(app string, version int) {
		if !s.pipelineReady() {
			return
		}
		s.pipeline.workers.Add(1)
		go func() { defer s.pipeline.workers.Done(); s.replay(app, version) }()
	}
	if s.loadErr != nil {
		return
	}
	for _, d := range s.drafts {
		s.pipeline.replaying[draftKey(d.overlayScope())] = d.Replayed != nil && d.State == "checking"
	}
}

func (s *Service) Request(ctx context.Context, scope svc.Scope, p bridge.ShapeRequestParams) (bridge.Change, error) {
	text := strings.TrimSpace(p.Text)
	if text == "" {
		return bridge.Change{}, bridge.Errorf(bridge.CodeInvalid, "say what to change")
	}
	if p.From == "" {
		p.From = "user"
	}
	if !slices.Contains([]string{"user", "plan", "agent", "fix", "broken"}, p.From) {
		return bridge.Change{}, bridge.Errorf(bridge.CodeInvalid, "invalid request source")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx, scope); err != nil {
		return bridge.Change{}, err
	}
	if !s.pipelineReady() {
		return bridge.Change{}, bridge.NotBuilt("shape pipeline")
	}
	d, err := s.draftFor(scope)
	if err != nil {
		return bridge.Change{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return bridge.Change{}, err
	}
	c := bridge.Change{ID: "change_" + hex.EncodeToString(id[:]), Request: text, From: p.From, State: "checking", Steps: []bridge.ShapeStep{}, Files: []string{}}
	if err := d.AddChange(c, nil); err != nil {
		return bridge.Change{}, err
	}
	c = cloneChange(d.Changes[d.index(c.ID)])
	s.emitChange(d, "change", c)
	s.startDraft(scope)
	return c, nil
}

func (s *Service) EditPlan(ctx context.Context, scope svc.Scope, p bridge.ShapePlanEditParams) (bridge.Change, error) {
	if strings.TrimSpace(p.Section) == "" || strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Text) == "" {
		return bridge.Change{}, bridge.Errorf(bridge.CodeInvalid, "section, id and text are required")
	}
	return s.Request(ctx, scope, bridge.ShapeRequestParams{Text: p.Section + " › " + p.ID + ": " + p.Text, From: "plan"})
}

func (s *Service) FixChange(ctx context.Context, scope svc.Scope, p bridge.ShapeChangeFixParams) (bridge.Change, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx, scope); err != nil {
		return bridge.Change{}, err
	}
	d, err := s.draftFor(scope)
	if err != nil {
		return bridge.Change{}, err
	}
	i := d.index(p.Change)
	if i < 0 {
		return bridge.Change{}, bridge.Errorf(bridge.CodeNotFound, "no change "+p.Change)
	}
	if d.Changes[i].State != "failed" {
		return bridge.Change{}, bridge.Errorf(bridge.CodeConflict, "only a failed change can be fixed")
	}
	hint := strings.TrimSpace(p.Hint)
	if hint == "" {
		hint = d.Changes[i].Request + "; " + d.Changes[i].Error
	}
	return s.restartChange(scope, d, i, "Fix: "+hint, "fix")
}

func (s *Service) restartChange(scope svc.Scope, d *draft, i int, text, from string) (bridge.Change, error) {
	candidate := d.copy()
	candidate.Changes[i].Request, candidate.Changes[i].From = text, from
	for j := i; j < len(candidate.Changes); j++ {
		c := &candidate.Changes[j]
		c.State, c.Round, c.Error, c.Summary = "checking", 0, "", ""
		c.Steps, c.Files, c.Question = []bridge.ShapeStep{}, []string{}, nil
		if j != i {
			c.Check = nil
		}
		candidate.Diffs[c.ID] = nil
	}
	if err := d.commit(candidate); err != nil {
		return bridge.Change{}, err
	}
	s.pipeline.epochs[draftKey(scope)]++
	c := cloneChange(d.Changes[i])
	s.emitChange(d, "change", c)
	s.startDraft(scope)
	return c, nil
}

// startDraft is called under s.mu, except during construction before publication.
// The journal is the queue, so a restart resumes every checking request in order.
func (s *Service) startDraft(scope svc.Scope) {
	key := draftKey(scope)
	if !s.pipelineReady() {
		return
	}
	if s.pipeline.running[key] {
		return
	}
	s.pipeline.running[key] = true
	s.pipeline.workers.Add(1)
	go func() { defer s.pipeline.workers.Done(); s.runDraft(scope) }()
}

func (s *Service) lifetime() context.Context {
	if s.cfg.Ctx != nil {
		return s.cfg.Ctx
	}
	return context.Background()
}

func (s *Service) runDraft(scope svc.Scope) {
	for {
		s.mu.Lock()
		job, ok := s.nextJob(scope)
		if !ok || s.lifetime().Err() != nil {
			delete(s.pipeline.running, draftKey(scope))
			delete(s.pipeline.active, draftKey(scope))
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		s.runChange(s.lifetime(), job)
	}
}

func (s *Service) nextJob(scope svc.Scope) (shapeJob, bool) {
	d := s.drafts[draftKey(scope)]
	if d == nil {
		return shapeJob{}, false
	}
	for _, c := range d.Changes {
		if c.State != "checking" {
			continue
		}
		// A replacement for an earlier change must not see later passed edits.
		before := d.copy()
		before.Changes = before.Changes[:d.index(c.ID)]
		files, err := before.Files()
		if err != nil {
			return shapeJob{}, false
		}
		s.pipeline.active[draftKey(scope)] = c.ID
		return shapeJob{scope: scope, draft: d, change: cloneChange(c), base: d.Base, epoch: s.pipeline.epochs[draftKey(scope)], files: files}, true
	}
	s.finishReplay(d)
	return shapeJob{}, false
}

// update rejects a late model/check result after cancel, discard or rebase.
func (s *Service) update(job shapeJob, fn func(*draft, *bridge.Change) error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.drafts[draftKey(job.scope)]
	if d != job.draft || d == nil || d.Base != job.base || s.pipeline.epochs[draftKey(job.scope)] != job.epoch {
		return false
	}
	i := d.index(job.change.ID)
	if i < 0 || d.Changes[i].State != "checking" {
		return false
	}
	c := cloneChange(d.Changes[i])
	if err := fn(d, &c); err != nil {
		// Keep the journal diagnosable if a file or metadata write fails.
		c.State, c.Error = "failed", err.Error()
		_ = d.SetChange(c, d.Diffs[c.ID])
		s.emitChange(d, "change", c)
		return false
	}
	return true
}

func (s *Service) runChange(ctx context.Context, job shapeJob) {
	files := job.files
	report := job.change.Check
	rows, err := s.shapeSample(ctx, files)
	if err != nil {
		s.failJob(job, 1, err.Error())
		return
	}
	firstRound := 1
	if job.change.Round > 0 && report != nil && !report.Pass {
		firstRound = report.Round + 1
	}
	for round := firstRound; round <= prompt.MaxCheckRounds; round++ {
		out, proceed := s.shapeRound(ctx, job, files, rows, report, round)
		if !proceed || !s.writeShape(job, files, out.Files, round) {
			return
		}
		checked, proceed := s.checkRound(ctx, job, files, round)
		if !proceed {
			return
		}
		report = &checked
		if checked.Pass {
			s.passJob(job, files, out.Summary)
			return
		}
	}
	s.failJob(job, prompt.MaxCheckRounds, lastIssue(report))
}

func (s *Service) shapeRound(ctx context.Context, job shapeJob, files spec.Files, rows map[string][]bridge.Record, report *bridge.CheckReport, round int) (svc.ShapeOutput, bool) {
	if !s.step(job, round, "plan", "running", planLog(job.change, files)) {
		return svc.ShapeOutput{}, false
	}
	out, err := s.all.Shaper.Shape(ctx, svc.ShapeInput{App: job.scope.App, Files: cloneFiles(files), Request: job.change.Request, Report: report, Round: round, Sample: rows})
	if err != nil {
		s.failJob(job, round, err.Error())
		return out, false
	}
	if out.Question != nil {
		s.question(job, out.Question)
		return out, false
	}
	return out, s.step(job, round, "plan", "done", planLog(job.change, files))
}

func (s *Service) writeShape(job shapeJob, files, changed spec.Files, round int) bool {
	if !s.step(job, round, "write", "running", "Writing changed files") {
		return false
	}
	for name, data := range changed {
		if !prompt.ValidPath(name) {
			s.failJob(job, round, "invalid definition path "+name)
			return false
		}
		files[name] = slices.Clone(data)
	}
	return s.step(job, round, "write", "done", writeLog(changed))
}

func (s *Service) checkRound(ctx context.Context, job shapeJob, files spec.Files, round int) (bridge.CheckReport, bool) {
	if !s.step(job, round, "render", "running", "Rendering screens on sample data") {
		return bridge.CheckReport{}, false
	}
	checked, err := s.checkShape(ctx, job.scope.App, files, round)
	checked.Round = round
	if err != nil {
		checked.Pass = false
		checked.Issues = append(checked.Issues, spec.Issue{Rule: "check", Message: err.Error()})
	}
	if !s.step(job, round, "render", checkStatus(checked, "render"), fmt.Sprintf("Rendered %d screens on sample data", len(checked.Rendered))) {
		return checked, false
	}
	if !s.step(job, round, "behaviour", "running", "Checking behaviour on connector fakes") {
		return checked, false
	}
	if !s.step(job, round, "behaviour", checkStatus(checked, "behaviour"), fmt.Sprintf("Behaviour check · %d runs on connector fakes", len(checked.Runs))) {
		return checked, false
	}
	proceed := s.update(job, func(d *draft, c *bridge.Change) error {
		c.Check = &checked
		return d.SetChange(*c, d.Diffs[c.ID])
	})
	return checked, proceed
}

func (s *Service) shapeSample(ctx context.Context, files spec.Files) (map[string][]bridge.Record, error) {
	if s.all.Sample == nil {
		return nil, bridge.NotBuilt("sample")
	}
	def, err := spec.Decode(files)
	if err != nil {
		return nil, err
	}
	return s.all.Sample.Generate(ctx, def)
}

func (s *Service) step(job shapeJob, round int, name, status, log string) bool {
	return s.update(job, func(d *draft, c *bridge.Change) error {
		c.Round = round
		step := bridge.ShapeStep{Step: name, Status: status, Log: log}
		i := slices.IndexFunc(c.Steps, func(v bridge.ShapeStep) bool { return v.Step == name })
		if i < 0 {
			c.Steps = append(c.Steps, step)
		} else {
			c.Steps[i] = step
		}
		if err := d.SetChange(*c, d.Diffs[c.ID]); err != nil {
			return err
		}
		s.emitChange(d, "step", *c)
		return nil
	})
}

func (s *Service) failJob(job shapeJob, round int, issue string) {
	s.update(job, func(d *draft, c *bridge.Change) error {
		c.State, c.Round = "failed", round
		c.Error = fmt.Sprintf("Check failed: %s  round %d of %d", issue, round, prompt.MaxCheckRounds)
		if err := d.SetChange(*c, d.Diffs[c.ID]); err != nil {
			return err
		}
		s.emitChange(d, "change", *c)
		return nil
	})
}

func (s *Service) passJob(job shapeJob, files spec.Files, summary string) {
	passed := s.update(job, func(d *draft, c *bridge.Change) error {
		c.Summary = summary
		if c.Summary == "" {
			c.Summary = "Check passed · change " + fmt.Sprint(c.N)
		}
		if err := d.SetChange(*c, d.Diffs[c.ID]); err != nil {
			return err
		}
		if err := d.ApplyFiles(c.ID, files); err != nil {
			return err
		}
		passed := d.Changes[d.index(c.ID)]
		if len(passed.Steps) > 0 {
			passed.Steps[len(passed.Steps)-1].Log += fmt.Sprintf("\nCheck passed · change %d", passed.N)
			if err := d.SetChange(passed, d.Diffs[c.ID]); err != nil {
				return err
			}
			s.emitChange(d, "step", passed)
		}
		s.emitChange(d, "change", passed)
		return nil
	})
	if passed {
		s.autoApply(job)
	}
}

func (s *Service) emitChange(d *draft, kind string, c bridge.Change) {
	if s.all.Hub == nil {
		return
	}
	c = cloneChange(c)
	s.all.Hub.Publish(bridge.TopicShape(d.App, d.Member), bridge.ShapeEvent{Kind: kind, Change: &c, At: timestamp()})
}

func lastIssue(report *bridge.CheckReport) string {
	if report == nil || len(report.Issues) == 0 {
		return "the change did not pass Check"
	}
	return report.Issues[len(report.Issues)-1].Message
}

func checkStatus(report bridge.CheckReport, step string) string {
	for _, issue := range report.Issues {
		if strings.HasPrefix(issue.Rule, step+"/") {
			return "failed"
		}
	}
	if len(report.Rendered) == 0 && !report.Pass && step == "render" {
		return "failed"
	}
	return "done"
}

// Check uses the apply lane's validated alias projection until the shared
// schema includes rename annotations. The persisted draft retains the alias.
func (s *Service) checkShape(ctx context.Context, app string, files spec.Files, round int) (bridge.CheckReport, error) {
	if _, err := records.ShapeAliases(files); err != nil {
		return bridge.CheckReport{}, err
	}
	validation, err := store.ShapeValidationFiles(files)
	if err != nil {
		return bridge.CheckReport{}, err
	}
	return s.all.Check.Run(ctx, app, validation, round)
}

func (s *Service) pipelineReady() bool {
	return s.all != nil && s.all.Shaper != nil && s.all.Check != nil && s.all.Sample != nil
}
