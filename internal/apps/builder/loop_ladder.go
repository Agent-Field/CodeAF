package builder

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/draft"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// rung is one way a finished or stopped loop becomes a preview (DESIGN §9).
// The first rung that fits runs, and the road it names is recorded in
// result.json. A rung that fails ends in today's failed card (Fix it · Cancel),
// which is the same card the calls road shows.
type rung struct {
	road string
	// fits says whether the rung can start from what the worker left.
	fits func(done bool, files spec.Files) bool
	run  func(s *Service, ctx context.Context, b *build, files spec.Files) error
}

var ladder = []rung{
	{roadLoop, func(done bool, _ spec.Files) bool { return done }, (*Service).previewDraft},
	{roadLoopFix, func(_ bool, files spec.Files) bool { return filesHold(files) }, (*Service).fixDraft},
	{roadCalls, func(bool, spec.Files) bool { return true }, (*Service).rewriteFromPlan},
}

// climb picks the rung for what the worker left in the draft. A nil seat means
// the loop could not start at all, which is the last rung.
func (s *Service) climb(ctx context.Context, b *build, t *seat) error {
	var (
		files spec.Files
		done  bool
	)
	if t != nil {
		if st, err := t.drafts.State(); err == nil {
			files, done = s.render(b, st), st.Phase == draft.PhaseDone
		}
	}
	done = done && s.passedOnExactly(t, files)
	for _, r := range ladder {
		if r.fits(done, files) {
			if err := s.tagRoad(b, r.road); err != nil {
				return err
			}
			return r.run(s, ctx, b, files)
		}
	}
	return nil
}

// passedOnExactly says the last Check passed on these very files. The done
// phase alone is not proof: a worker may keep editing after `done` (zoo run,
// it-asset-register: round 10 passed, the worker added a run, round 11 failed,
// and the preview still followed). A preview follows only a passing Check of the
// exact files it shows; the report is the one the worker's check or done got, so
// the model is never asked to re-roll it. After a restart no report is kept, and
// the draft goes through the fix rung's Check instead.
func (s *Service) passedOnExactly(t *seat, files spec.Files) bool {
	if t == nil {
		return false
	}
	report, ok := t.checked.get(digest(files))
	return ok && report.Pass
}

// previewDraft is the best case: the worker reached `done`, so the check
// passed on exactly these files.
func (s *Service) previewDraft(ctx context.Context, b *build, files spec.Files) error {
	s.emit(b, bridge.BuildEvent{Step: bridge.StepWrite, Status: bridge.BuildDone, Files: files.Paths(), Log: "All files written"})
	return s.preview(ctx, b, files)
}

// fixDraft runs today's check and fix calls over a draft the worker did not
// finish: up to three fix calls, then the fallback that leaves failing runs out.
//
// Its failure does not show the failed card: the rung below it, today's one-call
// write from the plan, gets its turn first, and only that rung may fail the build.
func (s *Service) fixDraft(ctx context.Context, b *build, files spec.Files) error {
	s.step(b, bridge.StepWrite, bridge.BuildRunning, "Finishing the parts that are left")
	b.holdFailure(true)
	err := s.fixHeld(ctx, b, files)
	b.holdFailure(false)
	if errors.Is(err, errRungFailed) {
		if err := s.tagRoad(b, roadCalls); err != nil {
			return err
		}
		return s.rewriteFromPlan(ctx, b, nil)
	}
	return err
}

func (s *Service) fixHeld(ctx context.Context, b *build, files spec.Files) error {
	if err := s.all.Store.WriteFiles(ctx, b.app, files); err != nil {
		return s.failWrite(ctx, b, err)
	}
	return s.checkAndFix(ctx, b, files, "", nil)
}

// errRungFailed says a held rung ended in a failed check or write.
var errRungFailed = errors.New("the rung did not pass")

// rewriteFromPlan is today's one-call pipeline from the plan, for a draft too
// thin to fix.
func (s *Service) rewriteFromPlan(ctx context.Context, b *build, _ spec.Files) error {
	s.step(b, bridge.StepWrite, bridge.BuildRunning, "Writing the app another way")
	return s.writeAndCheck(ctx, b)
}
