package shape

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// A personal queue keeps its unprocessed requests in the same journal while
// committing the checked prefix through Apply's migration and version door.
// Applying the whole draft would either refuse checking requests or combine
// several personal changes into one version.
func (s *Service) applyPersonalQueue(job shapeJob) bool {
	s.applyMu.Lock()
	s.mu.Lock()
	version, handled, err := s.applyPersonalPrefix(s.lifetime(), job)
	s.mu.Unlock()
	s.applyMu.Unlock()
	if err != nil {
		s.publishInboxError(job.scope, err)
	}
	if version > 0 {
		s.replayOthers(job.scope.App, version)
	}
	return handled
}

func (s *Service) applyPersonalPrefix(ctx context.Context, job shapeJob) (int, bool, error) {
	d := s.drafts[draftKey(job.scope)]
	if d != job.draft || d == nil || d.Replayed != nil || s.pipeline.epochs[draftKey(job.scope)] != job.epoch {
		return 0, false, nil
	}
	index := d.index(job.change.ID)
	if index < 0 {
		return 0, false, nil
	}
	prefix := d.copy()
	prefix.Changes = prefix.Changes[:index+1]
	for _, c := range prefix.Changes {
		if c.State != "passed" || c.Check == nil || !c.Check.Pass {
			return 0, true, nil
		}
	}
	files, err := prefix.Files()
	if err != nil {
		return 0, true, err
	}
	current, err := s.all.Store.Get(ctx, job.scope.App)
	if err != nil {
		return 0, true, err
	}
	if current.Version != d.Base {
		return 0, true, bridge.Errorf(bridge.CodeConflict, "the app version changed; replay your draft")
	}
	preview, err := s.previewCandidate(ctx, job.scope.App, current, prefix.snapshot(), files)
	if err != nil || len(preview.Migration) > 0 {
		return 0, true, err
	}
	if index+1 == len(d.Changes) {
		summary, err := s.applyLocked(ctx, job.scope, bridge.ShapeApplyParams{Base: d.Base})
		return summary.Version, true, err
	}
	return s.commitPersonalPrefix(ctx, job, d, prefix, current, files)
}

func (s *Service) commitPersonalPrefix(ctx context.Context, job shapeJob, d, prefix *draft, current bridge.AppDetail, files spec.Files) (int, bool, error) {
	requests := make([]string, 0, len(prefix.Changes))
	for _, c := range prefix.Changes {
		requests = append(requests, c.Request)
	}
	summary, err := s.commitCandidate(ctx, job.scope, current, files, requests)
	if err != nil {
		return 0, true, err
	}
	live, err := s.all.Store.Definition(ctx, job.scope.App)
	if err != nil {
		return summary.Version, true, err
	}
	candidate := d.copy()
	candidate.Changes = candidate.Changes[len(prefix.Changes):]
	for _, c := range prefix.Changes {
		delete(candidate.Diffs, c.ID)
	}
	if err := candidate.Rebase(summary.Version, live.Files); err != nil {
		return summary.Version, true, err
	}
	*d = *candidate
	s.pipeline.epochs[draftKey(job.scope)]++
	version := bridge.Version{N: summary.Version, Requests: requests, By: job.scope.Caller.Member, At: s.now().UTC().Format(time.RFC3339Nano), Changes: len(requests), Current: true, Restorable: true}
	s.publish(bridge.TopicShape(d.App, d.Member), bridge.ShapeEvent{Kind: "applied", Version: &version, At: version.At})
	return summary.Version, true, nil
}
