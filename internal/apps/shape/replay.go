package shape

import (
	"context"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// replayHook runs asynchronously after Apply releases its locks. A newer base
// invalidates in-flight results before the same per-draft worker resumes.
func (s *Service) replay(app string, newVersion int) {
	ctx := s.lifetime()
	def, err := s.all.Store.Definition(ctx, app)
	if err != nil || def == nil || def.App.Version < newVersion {
		return
	}
	versions, err := s.ListVersions(ctx, svc.Scope{App: app}, bridge.Empty{})
	if err != nil {
		return
	}
	by := svc.Owner
	for _, v := range versions.Versions {
		if v.N == def.App.Version && v.By != "" {
			by = v.By
			break
		}
	}
	s.replayOn(app, def.App.Version, by, def.Files)
}

func (s *Service) replayOn(app string, version int, by string, files spec.Files) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.drafts {
		if d.App != app || d.Base >= version || d.State == "applied" || d.State == "discarded" {
			continue
		}
		from := d.Base
		if err := d.Rebase(version, files); err != nil {
			continue
		}
		d.Replayed = &bridge.Replay{From: from, To: version, By: by, Requests: len(d.Changes)}
		if err := d.save(); err != nil {
			continue
		}
		scope := d.overlayScope()
		s.pipeline.epochs[draftKey(scope)]++
		s.pipeline.replaying[draftKey(scope)] = true
		s.startDraft(scope)
	}
}

func (s *Service) finishReplay(d *draft) {
	key := draftKey(d.overlayScope())
	if !s.pipeline.replaying[key] || d.Replayed == nil {
		return
	}
	for _, c := range d.Changes {
		if c.State == "checking" {
			return
		}
	}
	d.Replayed.Requests = len(d.Changes)
	d.Replayed.Failed = 0
	for _, c := range d.Changes {
		if c.State != "passed" {
			d.Replayed.Failed++
		}
	}
	if err := d.save(); err != nil {
		return
	}
	delete(s.pipeline.replaying, key)
	if d.Replayed.Failed == 0 && s.all.Hub != nil {
		value := d.snapshot()
		s.all.Hub.Publish(bridge.TopicShape(d.App, d.Member), bridge.ShapeEvent{Kind: "replayed", Draft: &value, At: timestamp()})
	}
}

func (s *Service) question(job shapeJob, q *bridge.ShapeQuestion) {
	if !s.update(job, func(d *draft, c *bridge.Change) error {
		c.State, c.Question = "question", q
		if err := d.SetChange(*c, d.Diffs[c.ID]); err != nil {
			return err
		}
		s.emitChange(d, "question", *c)
		return nil
	}) {
		return
	}
	def, err := spec.Decode(job.files)
	if err != nil || s.all.Approvals == nil {
		return
	}
	_, err = s.all.Approvals.Open(s.lifetime(), job.scope, bridge.Approval{
		ID: "shape_question_" + job.change.ID + "_" + fmt.Sprint(job.base), App: job.scope.App, Kind: "shape_question", Who: job.scope.Caller.Member,
		Title: def.App.Name + " · Builder needs you", Summary: q.Text,
		Payload: map[string]any{"app": job.scope.App, "change": job.change.ID, "question": q, "action": "shape.answer"},
	})
	if err != nil {
		s.publishInboxError(job.scope, err)
	}
}

func (s *Service) Answer(ctx context.Context, scope svc.Scope, p bridge.ShapeAnswerParams) (bridge.Change, error) {
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
	c := d.Changes[i]
	if c.State != "question" || c.Question == nil {
		return bridge.Change{}, bridge.Errorf(bridge.CodeConflict, "this change has no question")
	}
	text := strings.TrimSpace(p.Text)
	if p.Option != "" {
		found := false
		for _, option := range c.Question.Options {
			if option.ID == p.Option {
				found = true
				if text == "" {
					text = option.Label
				}
				break
			}
		}
		if !found {
			return bridge.Change{}, bridge.Errorf(bridge.CodeInvalid, "unknown question option")
		}
	}
	if p.Option == "drop" {
		if err := d.removeChange(c.ID); err != nil {
			return bridge.Change{}, err
		}
		s.pipeline.epochs[draftKey(scope)]++
		c.State, c.Question, c.Error = "cancelled", nil, ""
		s.emitChange(d, "change", c)
		s.finishReplay(d)
		return c, nil
	}
	if text == "" {
		return bridge.Change{}, bridge.Errorf(bridge.CodeInvalid, "choose an option or answer the question")
	}
	if d.Replayed != nil {
		s.pipeline.replaying[draftKey(scope)] = true
	}
	return s.restartChange(scope, d, i, text, c.From)
}

func (s *Service) DismissReplay(ctx context.Context, scope svc.Scope, _ bridge.Empty) (bridge.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx, scope); err != nil {
		return bridge.Draft{}, err
	}
	d, err := s.draftFor(scope)
	if err != nil {
		return bridge.Draft{}, err
	}
	if d.Replayed != nil {
		d.Replayed.Dismissed = true
		if err := d.save(); err != nil {
			return bridge.Draft{}, err
		}
	}
	return d.snapshot(), nil
}

func (s *Service) autoApply(job shapeJob) {
	def, err := spec.Decode(job.files)
	if err != nil || def.App.Space != "personal" || s.all.Shape == nil {
		return
	}
	if s.all.Shape == s && s.applyPersonalQueue(job) {
		return
	}
	s.mu.Lock()
	d := s.drafts[draftKey(job.scope)]
	eligible := d == job.draft && d != nil && d.State == "ready" && d.Replayed == nil
	s.mu.Unlock()
	if !eligible {
		return
	}
	preview, err := s.all.Shape.Preview(s.lifetime(), job.scope, bridge.Empty{})
	if err != nil {
		s.publishInboxError(job.scope, err)
		return
	}
	if len(preview.Migration) != 0 {
		return
	}
	_, err = s.all.Shape.Apply(s.lifetime(), job.scope, bridge.ShapeApplyParams{Base: job.base})
	if err != nil {
		s.publishInboxError(job.scope, err)
	}
	// Apply owns the applied event and version metadata.
}

func (s *Service) Ask(ctx context.Context, scope svc.Scope, p bridge.ShapeAskParams) (bridge.ShapeAskResult, error) {
	text := strings.TrimSpace(p.Text)
	if text == "" {
		return bridge.ShapeAskResult{}, bridge.Errorf(bridge.CodeInvalid, "say what to change")
	}
	if err := s.ready(ctx, scope); err != nil {
		return bridge.ShapeAskResult{}, err
	}
	if s.all.Approvals == nil {
		return bridge.ShapeAskResult{}, bridge.NotBuilt("approvals")
	}
	def, err := s.all.Store.Definition(ctx, scope.App)
	if err != nil {
		return bridge.ShapeAskResult{}, err
	}
	versions, err := s.ListVersions(ctx, scope, bridge.Empty{})
	if err != nil {
		return bridge.ShapeAskResult{}, err
	}
	to := svc.Owner
	for _, v := range versions.Versions {
		if v.By != "" {
			to = v.By
			break
		}
	}
	_, err = s.all.Approvals.Open(ctx, scope, bridge.Approval{
		App: scope.App, Kind: "shape_ask", Who: to,
		Title:   "@" + strings.TrimPrefix(scope.Caller.Member, "@") + " asks for a change to " + def.App.Name + " “" + text + "”",
		Summary: text,
		// The approved payload is the desk's handover: open Shape with the
		// request pre-filled, then wait for the Editor to send it.
		Payload: map[string]any{"app": scope.App, "request": text, "from": scope.Caller.Member, "action": "shape_this", "label": "Shape this", "decline_label": "Decline"},
	})
	return bridge.ShapeAskResult{To: to}, err
}

func (s *Service) publishInboxError(scope svc.Scope, err error) {
	if s.all.Hub != nil {
		s.all.Hub.Publish(bridge.TopicShape(scope.App, scope.Caller.Member), bridge.ShapeEvent{Kind: "change", Change: &bridge.Change{State: "failed", Error: err.Error()}, At: timestamp()})
	}
}
