package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/draft"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// loopRoad builds the app through a task worker that types `codeaf app` verbs.
// The journal those verbs write is rendered into the app's files as it grows.
// Every failure of the loop falls to the calls road's own steps (loop_ladder.go),
// so this road cannot do worse than the calls road on success rate.
var loopRoad = road{
	plan:     (*Service).loopPlan,
	write:    (*Service).loopWrite,
	edit:     (*Service).loopEdit,
	restore:  (*Service).loopRestore,
	editable: map[bridge.AppState]bool{bridge.StatePlanning: true, bridge.StateBuilding: true},
}

// loopPlan is apps.create's background work on the loop road: the plan task,
// the plan card, and, when the owner skipped the card, the write run. A plan
// task that did not reach "plan ready" hands the whole build to the calls road,
// whose plan call is the plan the card then shows.
func (s *Service) loopPlan(ctx context.Context, b *build, justBuild bool) error {
	s.step(b, bridge.StepPlan, bridge.BuildRunning, "Reading what you asked for")
	t, err := s.openSeat(b)
	if err == nil {
		err = s.runLoopTask(ctx, b, t, planTask(s.planBrief(b)))
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil && isBudget(err) {
		return err
	}
	if !s.planReady(t) {
		b.demote()
		return s.planThenMaybeWrite(ctx, b, justBuild)
	}
	t.feed.pump(ctx)
	if !justBuild {
		return nil
	}
	if err := s.setState(ctx, b, bridge.StateBuilding); err != nil {
		return err
	}
	return s.loopWrite(ctx, b)
}

// isBudget says the build stopped at its spending cap (rule 10), which no
// other road may spend past.
func isBudget(err error) bool {
	var e *bridge.Error
	return errors.As(err, &e) && e.Code == bridge.CodeBudget
}

// planReady says whether the journal holds a plan the card can show.
func (s *Service) planReady(t *seat) bool {
	if t == nil {
		return false
	}
	st, err := t.drafts.State()
	return err == nil && phaseAtLeast(st.Phase, draft.PhasePlanned)
}

// loopWrite is Write on the loop road: the build task, then the rescue ladder
// picks the way the draft becomes a preview.
func (s *Service) loopWrite(ctx context.Context, b *build) error {
	t, err := s.openSeat(b)
	if err != nil {
		return s.climb(ctx, b, nil)
	}
	if err := s.saveResult(b, savedResult{}); err != nil {
		return err
	}
	if !s.buildDone(t) {
		err = s.runLoopTask(ctx, b, t, buildTask(s.buildBrief(b)))
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil && isBudget(err) {
		return err
	}
	t.feed.pump(ctx)
	return s.climb(ctx, b, t)
}

// buildDone says the journal already reached done, as it does when the server
// restarted between the worker's last verb and the preview: the preview is all
// that is left, and a second worker would only spend money.
func (s *Service) buildDone(t *seat) bool {
	st, err := t.drafts.State()
	return err == nil && st.Phase == draft.PhaseDone
}

// loopRestore reads the plan back from the journal after a restart, so a build
// resumes from what the worker had committed and not from the last render.
func (s *Service) loopRestore(b *build) {
	if _, err := os.Stat(filepath.Join(s.buildDir(b.app), draft.JournalFile)); err != nil {
		return
	}
	t, err := s.openSeat(b)
	if err != nil {
		return
	}
	st, err := t.drafts.State()
	if err != nil || !phaseAtLeast(st.Phase, draft.PhasePlanned) {
		return
	}
	if plan, err := decodePlan(string(s.render(b, st)[spec.PlanFile])); err == nil {
		b.setPlan(plan)
	}
}

// verbGroup names a plan card section the way the plan verbs do.
var verbGroup = map[string]string{
	prompt.GroupData: "data", prompt.GroupAutomation: "run", prompt.GroupScreens: "screen",
}

// ownerVerb is a line edit as the journal's verb and arguments. The verb is the
// registry's own name for it, such as "plan data".
func ownerVerb(e prompt.Edit) (verb string, args []string) {
	switch {
	case e.Group == prompt.GroupAgent:
		return "plan agent", []string{e.Line}
	case e.Op == prompt.OpRemove:
		return "plan drop", []string{verbGroup[e.Group], e.ID}
	}
	return "plan " + verbGroup[e.Group], []string{e.ID, e.Line}
}

// ownerNote is the edit in the words the worker is told.
func ownerNote(e prompt.Edit) string {
	if e.Op == prompt.OpRemove {
		return "Owner removed " + e.Group + " " + e.ID + " from the plan."
	}
	return "Owner changed " + e.Group + " " + e.ID + ": " + e.Line
}

// loopEdit applies the plan card's edits to the journal as the owner. A line
// edit shows at once, and a worker that is live is told by a note on its task,
// which the run engine hands over at its next step. A free request goes to the
// live worker as a note too, or starts a short replan task when none is live.
func (s *Service) loopEdit(ctx context.Context, b *build, edits []bridge.PlanEdit) (prompt.Plan, error) {
	t, err := s.openSeat(b)
	if err != nil {
		return prompt.Plan{}, err
	}
	lines, requests := splitEdits(b.currentPlan(), edits)
	for _, e := range lines {
		verb, args := ownerVerb(e)
		if _, err := t.drafts.Append(draft.AuthorOwner, verb, args, ""); err != nil {
			return prompt.Plan{}, bridge.Errorf(bridge.CodeInvalid, strings.TrimPrefix(err.Error(), "error: "))
		}
		t.tell(ownerNote(e))
	}
	t.feed.pump(ctx)
	for _, request := range requests {
		if err := s.steer(b, t, request); err != nil {
			return prompt.Plan{}, err
		}
	}
	return b.currentPlan(), nil
}

// tell leaves a note on the live worker's task, and does nothing between tasks.
func (t *seat) tell(words string) {
	if id := t.liveTask(); id != "" {
		_, _ = t.tasks.AddPersonNote(id, words)
	}
}

// steer hands a free request to the live worker, or starts a replan for it.
func (s *Service) steer(b *build, t *seat, request string) error {
	if t.liveTask() != "" {
		t.tell("Owner asks: " + request)
		return nil
	}
	return s.start(b, func() error { return nil }, func(ctx context.Context) error {
		return s.replan(ctx, b, t, request)
	})
}

// replan runs the short task that changes the plan, then shows the card again.
func (s *Service) replan(ctx context.Context, b *build, t *seat, request string) error {
	s.step(b, bridge.StepPlan, bridge.BuildRunning, "Changing the plan")
	err := s.runLoopTask(ctx, b, t, replanTask(s.replanBrief(request)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil && isBudget(err) {
		return err
	}
	t.feed.pump(ctx)
	s.emitPlan(b, b.currentPlan())
	return nil
}

// filesHold says whether the render has an app.json and at least one screen:
// the least that is worth checking and fixing instead of writing again.
func filesHold(files spec.Files) bool {
	if files[spec.AppFile] == nil {
		return false
	}
	for _, path := range files.Paths() {
		if strings.HasPrefix(path, "screens/") {
			return true
		}
	}
	return false
}
