package builder

import (
	"context"
	"errors"
	"log"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/draft"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// The rescue road's names, as the setting and as the road tag in result.json.
// A calls pass keeps the plain "calls" tag, so a rescue count is the count of
// the two tags below.
const (
	roadRescue        = "rescue"
	roadRescueLoop    = "rescue-loop"
	roadRescueLoopFix = "rescue-loop+fix"
)

// secondPass is the one line the owner reads when the first write did not pass.
const secondPass = "Taking a second pass"

// couldNotFinish is what the failed card says when the second pass also ends
// without a preview. The owner reads a sentence, never the error behind it.
const couldNotFinish = "The Builder could not finish this app"

// rescueRoad plans and edits exactly as the calls road does and writes the same
// way too. Only when that write ends failed does it run the loop road's build
// task, seeded from the plan the owner already approved, before the failed card
// is shown. A build that passes the calls write never touches the loop, so it
// is the calls road byte for byte.
var rescueRoad = road{
	plan:     callsRoad.plan,
	write:    (*Service).rescueWrite,
	edit:     callsRoad.edit,
	restore:  callsRoad.restore,
	editable: callsRoad.editable,
}

// rescueLadder is the loop road's ladder without its calls rung: the calls
// write has already failed once, so writing again the same way cannot help.
var rescueLadder = []rung{
	{roadRescueLoop, func(done bool, _ spec.Files) bool { return done }, (*Service).previewDraft},
	{roadRescueLoopFix, func(_ bool, files spec.Files) bool { return filesHold(files) }, (*Service).fixRescued},
}

// rescueWrite is Write on the rescue road: the calls write with its failure
// held back, and the second pass when it was needed.
func (s *Service) rescueWrite(ctx context.Context, b *build) error {
	if err := s.tagRoad(b, roadCalls); err != nil {
		return err
	}
	b.holdFailure(true)
	err := s.writeAndCheck(ctx, b)
	b.holdFailure(false)
	if !errors.Is(err, errRungFailed) {
		return err
	}
	// The spending cap ends the build: no second pass may spend past it.
	if capErr := b.withinBudget(); capErr != nil {
		return s.failWrite(ctx, b, capErr)
	}
	s.step(b, bridge.StepWrite, bridge.BuildRunning, secondPass)
	return s.rescueBuild(ctx, b)
}

// rescueBuild seeds a fresh draft with the approved plan, runs the loop road's
// build task on it, and climbs the rescue ladder over what the worker left.
func (s *Service) rescueBuild(ctx context.Context, b *build) error {
	t, err := s.openSeat(b)
	if err == nil {
		err = s.seedPlan(t, b.currentPlan())
	}
	if err != nil {
		log.Printf("apps builder: rescue could not seed the draft: %v", err)
		return s.failWrite(ctx, b, errors.New(couldNotFinish))
	}
	if err := s.saveResult(b, savedResult{}); err != nil {
		return err
	}
	err = s.runLoopTask(ctx, b, t, buildTask(s.buildBrief(b)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil && isBudget(err) {
		return err
	}
	t.feed.pump(ctx)
	return s.climbRescue(ctx, b, t)
}

// climbRescue is the loop road's climb over the rescue ladder. A draft that
// fits no rung is the end: the failed card shows once, in plain words.
func (s *Service) climbRescue(ctx context.Context, b *build, t *seat) error {
	var (
		files spec.Files
		done  bool
	)
	if st, err := t.drafts.State(); err == nil {
		files, done = s.render(b, st), st.Phase == draft.PhaseDone
	}
	for _, r := range rescueLadder {
		if r.fits(done, files) {
			if err := s.tagRoad(b, r.road); err != nil {
				return err
			}
			return r.run(s, ctx, b, files)
		}
	}
	return s.failWrite(ctx, b, errors.New(couldNotFinish))
}

// fixRescued runs the check and fix calls over a draft the worker did not
// finish. It is the last rung, so its failure is not held: it ends in the
// failed card, which is the card the owner would have seen from the calls road.
func (s *Service) fixRescued(ctx context.Context, b *build, files spec.Files) error {
	s.step(b, bridge.StepWrite, bridge.BuildRunning, "Finishing the parts that are left")
	return s.fixHeld(ctx, b, files)
}

// seedPlan writes the approved plan into the draft as codeaf's own entries, so
// the worker opens on a ready plan and not on an empty draft. The feed then
// skips them: the owner has already seen this plan on the card, and replaying
// it would show the plan card a second time in the middle of the build.
func (s *Service) seedPlan(t *seat, plan prompt.Plan) error {
	type verb struct {
		name string
		args []string
	}
	verbs := []verb{{"name", []string{plan.Name}}}
	if plan.Tint != "" {
		verbs = append(verbs, verb{"tint", []string{plan.Tint}})
	}
	for _, g := range []struct {
		word  string
		lines []prompt.Line
	}{{"data", plan.Data}, {"run", plan.Automation}, {"screen", plan.Screens}} {
		for _, l := range g.lines {
			verbs = append(verbs, verb{"plan " + g.word, []string{l.ID, l.Line}})
		}
	}
	verbs = append(verbs, verb{"plan agent", []string{plan.Agent.Line}}, verb{"plan ready", nil})
	for _, v := range verbs {
		if _, err := t.drafts.Append(draft.AuthorCodeaf, v.name, v.args, ""); err != nil {
			return err
		}
	}
	t.feed.adopt()
	return nil
}

// adopt moves the feed past every entry the journal holds now and marks the
// plan card as shown, so entries codeaf wrote itself reach no one as news.
func (f *feed) adopt() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if entries, err := f.drafts.Entries(0); err == nil && len(entries) > 0 {
		f.seq = entries[len(entries)-1].Seq
	}
	f.announced = true
}
