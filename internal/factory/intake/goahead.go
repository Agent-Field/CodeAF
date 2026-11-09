package intake

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// GoAheadDetail is the start record's detail for a job allowance id started:
// the app reads the id back out of it to say which go-ahead started the job.
func GoAheadDetail(id string) string { return "allowance " + id }

// covering is one allowance that names a job, with the policy that holds it.
type covering struct {
	policy    factory.GroupPolicy
	allowance factory.Allowance
}

// GoAheads is the go-ahead pass: it starts every job a group policy covers
// whose time has come, and writes on every other covered job why it waits.
//
// A COVERED JOB STARTS ONLY WHEN ALL OF THIS IS TRUE: it never ran and sits in
// Backlog, nobody stopped it, its repository is on, every job it waits on has
// merged, it is not someone else's, and one allowance naming it is before its
// end, inside its cap, in a policy that is not stopped. The allowance holds the
// job's whole budget from the moment it starts (lowered to what is left of the
// cap when that is less), so starts in one pass cannot together pass the cap.
// The start is a person's start made in advance: the job takes its file's
// steps and rules, becomes the policy owner's when it has no person, and is
// recorded as started by that allowance ([GoAheadDetail]).
//
// It reads nothing from GitHub and no app need be attached: it runs after
// every poll, beside the start rules, and it is on whether or not the start
// rules are, because a go-ahead is a person's own yes.
func (in *Intake) GoAheads(ctx context.Context) error {
	if in.st == nil || in.opts.Launch == nil || in.opts.Record == nil {
		return nil
	}
	release, ok := in.st.TryIntake()
	if !ok {
		return nil
	}
	defer release()
	policies, err := in.st.Policies()
	if err != nil {
		return err
	}
	items, err := in.st.List()
	if err != nil {
		return err
	}
	sw, err := in.st.RepoSwitch()
	if err != nil {
		return err
	}
	// THE OLDEST JOB IS DECIDED FIRST, so when a cap fits only some of the
	// jobs it covers, the ones made first are the ones that start.
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	now := in.now()
	byID := make(map[int]factory.Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	covers := coverings(policies)
	spend := map[string]map[string]factory.AllowanceSpend{}
	for group := range policies {
		spend[group] = factory.PolicySpend(items, group)
	}
	var errs []error
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		list := covers[it.ID]
		if len(list) == 0 || it.PolicyRun != "" || !neverRan(it) || sw.OffJob(it) {
			errs = append(errs, in.block(it, nil, now))
			continue
		}
		pick, why := choose(it, list, spend, now)
		if why == nil {
			why = waiting(it, byID)
		}
		if why != nil {
			errs = append(errs, in.block(it, why, now))
			continue
		}
		budget, err := in.startGoAhead(ctx, it, pick, spend[pick.policy.Group][pick.allowance.ID], now)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		s := spend[pick.policy.Group][pick.allowance.ID]
		s.Committed += budget
		spend[pick.policy.Group][pick.allowance.ID] = s
	}
	return errors.Join(errs...)
}

// coverings lists, by job, every allowance that names it, in group and then
// allowance order, so the pass decides the same way every time.
func coverings(policies factory.Policies) map[int][]covering {
	groups := make([]string, 0, len(policies))
	for group := range policies {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	out := map[int][]covering{}
	for _, group := range groups {
		p := policies[group]
		allowances := append([]factory.Allowance(nil), p.Allowances...)
		sort.Slice(allowances, func(i, j int) bool { return allowances[i].ID < allowances[j].ID })
		for _, a := range allowances {
			for _, job := range a.Jobs {
				out[job] = append(out[job], covering{policy: p, allowance: a})
			}
		}
	}
	return out
}

// neverRan is whether the job is one a go-ahead may start: in Backlog, never
// run, not stopped by a person, not ended and not a first-run import waiting
// for its person.
func neverRan(it factory.Item) bool {
	return it.State == factory.StateNew && factory.JobState(it) == "backlog" && it.Stream == nil &&
		len(it.Adapted) == 0 && !factory.StoppedByPerson(it) && !it.SetupHeld && it.DoneHow == "" && it.QKind == ""
}

// choose is the first covering allowance that may start the job now, or why
// none may: the first one's reason when every one is stopped, ended, spent or
// held by someone else's job.
func choose(it factory.Item, list []covering, spend map[string]map[string]factory.AllowanceSpend, now time.Time) (covering, *factory.JobBlocked) {
	var why *factory.JobBlocked
	for _, c := range list {
		s := spend[c.policy.Group][c.allowance.ID]
		reason := ""
		switch {
		case c.policy.Stopped:
			reason = "its go-ahead was stopped"
		case !now.Before(c.allowance.Ends()):
			reason = "its go-ahead ended"
		case s.Committed >= c.allowance.CapUSD:
			reason = "its go-ahead's cap is spent"
		case c.policy.Owner != "" && it.Person() != "" && it.Person() != c.policy.Owner:
			reason = "it is @" + it.Person() + "'s, so it waits for them to start it"
		default:
			return c, nil
		}
		if why == nil {
			why = &factory.JobBlocked{By: factory.BlockedByPolicy, Reason: reason, Allowance: c.allowance.ID}
		}
	}
	return covering{}, why
}

// waiting is why the job still waits on another job, or nil when every job it
// waits on has merged.
func waiting(it factory.Item, byID map[int]factory.Item) *factory.JobBlocked {
	for _, id := range it.WaitsOn {
		dep, ok := byID[id]
		switch {
		case !ok:
			return &factory.JobBlocked{By: factory.BlockedByWaitsOn, Reason: fmt.Sprintf("it waits on job %d, which this factory does not have", id), Job: id}
		case factory.Merged(dep):
			continue
		case factory.JobState(dep) == "done":
			return &factory.JobBlocked{By: factory.BlockedByWaitsOn, Reason: fmt.Sprintf("it waits on job %d, which ended without merging", id), Job: id}
		default:
			return &factory.JobBlocked{By: factory.BlockedByWaitsOn, Reason: fmt.Sprintf("it waits on job %d to merge", id), Job: id}
		}
	}
	return nil
}

// block writes why on the job when it says something new, and clears it when
// why is nil. It is an annotation: the job did not move.
func (in *Intake) block(it factory.Item, why *factory.JobBlocked, now time.Time) error {
	if it.Blocked.Same(why) {
		return nil
	}
	if why != nil {
		why.Since = now
	}
	err := in.st.Annotate(it.ID, func(cur *factory.Item) error {
		if cur.Blocked.Same(why) {
			return errMoved
		}
		cur.Blocked = why
		return nil
	})
	if errors.Is(err, errMoved) {
		return nil
	}
	return err
}

// startGoAhead is one go-ahead's start of one job, and the budget it holds
// against the allowance. Every check is made again under the job's own lock.
func (in *Intake) startGoAhead(ctx context.Context, it factory.Item, c covering, s factory.AllowanceSpend, now time.Time) (float64, error) {
	if factory.ClosedForAutomation(ctx, in.st.Root(), it, in.opts.Standing) {
		return 0, nil
	}
	var file *pipeline.File
	if in.opts.Load != nil || in.opts.LoadJob != nil {
		f, err := in.load(it)
		if err != nil {
			return 0, err
		}
		file = &f
	}
	left := c.allowance.CapUSD - s.Committed
	owner := factory.NormalizeHandle(c.policy.Owner)
	var budget float64
	err := in.st.Update(it.ID, func(cur *factory.Item) error {
		if err := in.canStart(ctx, *cur); err != nil {
			return err
		}
		if cur.PolicyRun != "" || !neverRan(*cur) {
			return errMoved
		}
		if file != nil {
			cur.Stages = factory.CopyStages(file.Recipe().For(cur.Kind))
			cur.StartRules, cur.RuleHold = pipeline.StartRulesOf(*file), ""
			cur.DecidePipeline(file.Copy, now)
			if cur.Cap <= 0 {
				cur.Cap = file.DefaultBudget
			}
		}
		if cur.Cap <= 0 {
			cur.Cap = factory.DefaultJobBudget
		}
		// THE JOB'S BUDGET IS WHAT IS LEFT OF THE CAP when that is less, so
		// the factory's own budget stop holds the allowance's line.
		cur.Cap = min(cur.Cap, left)
		budget = cur.Cap
		if cur.Person() == "" && owner != "" {
			cur.Starter = owner
		}
		cur.PolicyRun, cur.PolicyGroup, cur.Blocked = c.allowance.ID, c.policy.Group, nil
		return nil
	})
	if errors.Is(err, errMoved) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if err := in.opts.Launch(it.ID); err != nil {
		// A LAUNCH THAT REFUSED TAKES THE MARK BACK, so the job is not
		// counted as started and the next pass tries it again.
		_ = in.st.Update(it.ID, func(cur *factory.Item) error {
			cur.PolicyRun, cur.PolicyGroup = "", ""
			cur.Blocked = &factory.JobBlocked{By: factory.BlockedByPolicy, Reason: "the factory could not start it yet, and tries again", Allowance: c.allowance.ID, Since: now}
			return nil
		})
		return 0, err
	}
	handle := factory.NormalizeHandle(it.Person())
	if handle == "" {
		handle = owner
	}
	_, err = in.opts.Record.Append(ctx, record.Event{Kind: record.AutoStarted, Job: it.ID, Handle: handle, Detail: GoAheadDetail(c.allowance.ID)})
	return budget, err
}
