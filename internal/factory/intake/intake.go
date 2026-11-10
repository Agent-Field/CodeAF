// Package intake applies the repository's start rules after the cheap read.
// Policy is separate from triage so a model never owns the launch decision.
package intake

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/estimate"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/route"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// Options injects repository policy and the existing launch door; tests need no model.
type Options struct {
	// Standing reads GitHub before an automatic start or ask. Nil keeps the existing policy.
	Standing func(context.Context, factory.Item) (factory.SourceStanding, error)
	Refresh  func(context.Context) error
	Load     func(string) (pipeline.File, error)
	// LoadJob reads ONE JOB'S OWN FILE (pipeline.JobFile): the one it was
	// decided on ([factory.Item.DecidePipeline]), else its repository's as
	// Load reads it. Nil reads Load. Intake decides a job's file when it
	// stamps the job's steps first.
	LoadJob func(factory.Item) (pipeline.File, error)
	Launch  func(int) error
	Record  *record.Record
	// Owner is the factory owner's forge handle (its operator), "" when it
	// cannot be told. A JOB A START RULE STARTS IS THE OWNER'S: it becomes the
	// job's person when it has none, so its approvals have someone to answer
	// them and Auto can pass the routine ones. Nil leaves such a job with no
	// person, as before.
	Owner func(context.Context) string
	// Scout looks at one job closer (Look closer) and writes the look on it.
	// AN AUTOMATIC START GETS THE BETTER READ FIRST: a job the rules would
	// start is looked at before it launches and decided again on what the
	// look changed. A failed look (a limit, no answer) never stops the start.
	// Nil starts on the arrival read alone.
	Scout func(ctx context.Context, id int) error
	// Judge answers a routing rule's question ([route.Match.Ask]) about one
	// job, at most once per rule per job per pass, and only when every other
	// field of the rule already holds. Nil is no judge: a rule that asks a
	// question then never matches, and nothing is paid.
	Judge route.Judge
	// LabelStarts says whether a job NO ROUTING RULE MATCHED may be started
	// or held by its labels this pass: the factory's Pick up issues switch on
	// a factory on a person's own machine. IT GOVERNS THE LABEL RULE ONLY.
	// The routing table is read for every new job whatever it says, so a
	// rule's repository, pipeline, priority and start (auto, ask or backlog)
	// apply with the switch off. Nil is on, which is what a team server and
	// the command line have always done.
	LabelStarts func() bool
	// Now is the clock the go-ahead pass reads an allowance's end against
	// ([Intake.GoAheads]). Nil is the wall clock.
	Now func() time.Time
}

// now is the intake's clock.
func (in *Intake) now() time.Time {
	if in.opts.Now != nil {
		return in.opts.Now()
	}
	return time.Now()
}

type Intake struct {
	st   *store.Store
	opts Options
}

func New(st *store.Store, opts Options) *Intake { return &Intake{st: st, opts: opts} }

// AskFirst supplies the exact policy sentences for the triage prompt.
func (in *Intake) AskFirst(it factory.Item) []string {
	if in.opts.Load == nil {
		return nil
	}
	f, err := in.load(it)
	if err != nil {
		return nil
	}
	return f.Start.AskFirst
}

// Once is called after a poll and after each triage tick. The shared lock keeps
// those two readers from launching or recording the same job twice.
func (in *Intake) Once(ctx context.Context) error {
	if in.st == nil || in.opts.Load == nil || in.opts.Launch == nil || in.opts.Record == nil {
		return nil
	}
	release, ok := in.st.TryIntake()
	if !ok {
		return nil
	}
	defer release()
	if in.opts.Refresh != nil {
		if err := in.opts.Refresh(ctx); err != nil {
			return err
		}
	}
	items, err := in.st.List()
	if err != nil {
		return err
	}
	// A REPOSITORY THAT IS OFF IS PASSED BY ([factory.RepoSwitch]): its
	// labelled jobs are neither started nor asked about, and its rules are not
	// even read, because the factory does not work on it.
	sw, err := in.st.RepoSwitch()
	if err != nil {
		return err
	}
	// The ledger is read once a pass, from the same list: what jobs like
	// each one cost is measured from the floor's finished jobs.
	book := estimate.Open(items)
	// THE ROUTING TABLE IS READ ONCE A PASS, and an unreadable one stops the
	// pass rather than reading as no rules ([store.Store.Routes]).
	rules, err := in.st.Routes()
	if err != nil {
		return err
	}
	var errs []error
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		routed, ok, err := in.admit(ctx, it, rules, sw)
		if err != nil {
			errs = append(errs, err)
		}
		if !ok {
			continue
		}
		if err := in.decide(ctx, it, book, routed); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// admit is THE ORDERED FILTER of one pass: whether intake works on it now,
// and its routing decision when it does. The order is the law, because each
// step answers a question the next one needs:
//
//  1. its own state: a job a person stopped, one that already ran
//     ([factory.EverRan]: a failed run waits for a person, never for a
//     rule), one done, a non-issue, a first-run import held for its person, one not new, or one already
//     waiting on an ask-first hold is passed by, and costs nothing more;
//  2. ROUTING, before anything that reads its labels or its repository: an
//     inbox or Jira job arrives with neither, and a rule is what gives it a
//     repository and admits it without a label;
//  3. A JOB A RULE ALREADY ROUTED KEEPS ITS ROUTE when no rule matches it
//     now (its ticket was edited, the rule was changed): its repository,
//     priority and steps stay as that rule left them, and only a DIFFERENT
//     rule that matches moves it;
//  4. today's label skip, for a job NO rule matched only: unlabelled work a
//     person filed waits for a person, exactly as before routing existed, and
//     so does labelled work while [Options.LabelStarts] is off;
//  5. a repository, as routed: none has no rules to read and could not be
//     launched ([factory.NoRepoRefusal]), so it waits in Backlog, which its
//     read says (`Waiting for a repository`);
//  6. that repository's switch ([factory.RepoSwitch]): one that is off is
//     passed by, its rules unread;
//  7. a job its rule already filed in Backlog is left there, so a pass does
//     not read its file again for nothing.
//
// A routing error (a judge that cannot be kept) is answered with false, and
// the pass goes on to the next job.
func (in *Intake) admit(ctx context.Context, it factory.Item, rules []route.Rule, sw factory.RepoSwitch) (*routing, bool, error) {
	if factory.StoppedByPerson(it) || factory.EverRan(it) || factory.JobState(it) == "done" || !factory.IsIssueJob(it) || it.SetupHeld || it.State != factory.StateNew || it.AskedFirstReason != "" {
		return nil, false, nil
	}
	routed, err := in.routeFor(ctx, rules, it)
	if err != nil {
		return nil, false, err
	}
	target := routedTo(it, routed)
	switch {
	case routed == nil && it.Route != "":
		return nil, false, nil
	case routed == nil && (len(it.Labels) == 0 || !in.labelsStart()):
		return nil, false, nil
	case factory.NamesNoRepo(target):
		return nil, false, nil
	case sw.OffJob(target):
		return nil, false, nil
	case routed != nil && routed.rule.Then.Start == route.StartBacklog && it.Route == routed.rule.Name:
		return nil, false, nil
	}
	return routed, true, nil
}

// labelsStart is [Options.LabelStarts], on when it is not set.
func (in *Intake) labelsStart() bool {
	return in.opts.LabelStarts == nil || in.opts.LabelStarts()
}

// LabelsGated is in with [Options.LabelStarts] set to on: the same intake,
// whose label rule is governed by on, and whose routing table is read for
// every new job either way.
func (in *Intake) LabelsGated(on func() bool) *Intake {
	cp := *in
	cp.opts.LabelStarts = on
	return &cp
}

// load is the file job it is decided under: its own ([Options.LoadJob]),
// else its repository's.
func (in *Intake) load(it factory.Item) (pipeline.File, error) {
	// A JOB AN AGENTFIELD REASONER RUNS, WITH NO REPOSITORY, HAS NO FILE TO
	// READ: its start is decided under the built-in rules, as a job filed
	// with run_on is budgeted by the factory's own default.
	if strings.TrimSpace(it.Repo) == "" && strings.TrimSpace(it.RunOn) != "" {
		return pipeline.Defaults(), nil
	}
	if in.opts.LoadJob != nil {
		return in.opts.LoadJob(it)
	}
	return in.opts.Load(it.Repo)
}

// Cost is what jobs like it cost, as the start rules read it: the high end
// of its measured range, and unknown when the ledger has too few similar
// jobs.
func Cost(book estimate.Book, it factory.Item) pipeline.Measured {
	r, ok := book.For(it)
	return pipeline.Measured{USD: r.USDHi, Known: ok}
}

// decide starts, holds or leaves one job. routed is its routing decision,
// nil when no rule matched; everything that changes goes through the routing
// seam (route.go).
func (in *Intake) decide(ctx context.Context, it factory.Item, book estimate.Book, routed *routing) error {
	f, err := in.load(routedTo(it, routed))
	if err != nil {
		return err
	}
	// A RULE THE TARGET'S FILE CANNOT HONOUR IS REFUSED ON THE JOB, NOT ON
	// THE PASS: the job stays as it is, unrouted, one activity row says why,
	// and every other job (and every source's poll) goes on as before.
	if routed != nil {
		if why := refusedBy(routed.rule, routedTo(it, routed), f); why != "" {
			return in.refuse(ctx, it, why)
		}
	}
	decision := startDecision(f, it, Cost(book, it), routed)
	if (decision.Start || decision.Ask) && factory.ClosedForAutomation(ctx, in.st.Root(), it, in.opts.Standing) {
		return nil
	}
	if decision.Start {
		if looked, ok := in.lookFirst(ctx, it); ok {
			it = looked
			decision = startDecision(f, it, Cost(book, it), routed)
		}
	}
	if !decision.Start && !decision.Ask {
		return in.file(ctx, it, f, routed)
	}
	// ONLY A JOB THE RULES START IS THE OWNER'S. One they hold waits for
	// whoever approves it: owning it would shut out a teammate who could
	// approve it before.
	owner := ""
	if decision.Start && it.Person() == "" && in.opts.Owner != nil {
		owner = factory.NormalizeHandle(in.opts.Owner(ctx))
	}
	own := func(cur *factory.Item) {
		if cur.Person() == "" && owner != "" {
			cur.Starter = owner
		}
	}
	kind, reason := record.AutoStarted, startReason(routed)
	var staged []factory.Stage
	if decision.Ask {
		kind, reason = record.AskedFirst, strings.Join(decision.Reasons, "; ")
		err = in.st.Update(it.ID, func(cur *factory.Item) error {
			if err := in.canStart(ctx, *cur); err != nil {
				return err
			}
			if err := stage(cur, f, routed); err != nil {
				return err
			}
			staged = cur.Stages
			cur.StartRules, cur.RuleHold = pipeline.StartRulesOf(f), ""
			cur.DecidePipeline(f.Copy, time.Now())
			if cur.Cap <= 0 {
				cur.Cap = f.DefaultBudget
			}
			cur.State, cur.QKind = factory.StateNeedsYou, factory.QKindAskFirst
			cur.AskedFirstReason = reason
			cur.SetStartCheck(factory.CheckAsked)
			cur.Question = "Approval needed: start · " + reason
			return nil
		})
	} else {
		// YAML may define a different pipeline from the one copied on arrival.
		// The launch door must see those steps before it compiles the run.
		err = in.st.Update(it.ID, func(cur *factory.Item) error {
			if err := in.canStart(ctx, *cur); err != nil {
				return err
			}
			if err := stage(cur, f, routed); err != nil {
				return err
			}
			staged = cur.Stages
			cur.StartRules, cur.RuleHold = pipeline.StartRulesOf(f), ""
			// THE JOB'S FILE IS DECIDED WITH ITS STEPS: the launch just below
			// reads this one, not a copy saved in between.
			cur.DecidePipeline(f.Copy, time.Now())
			own(cur)
			cur.SetStartCheck(factory.CheckPassed)
			if cur.Cap <= 0 {
				cur.Cap = f.DefaultBudget
			}
			return nil
		})
		if err == nil {
			// The replacement is said before the launch, which may refuse:
			// the steps changed either way.
			if err = in.noteReplaced(ctx, it, routed, staged); err == nil {
				err = in.opts.Launch(it.ID)
			}
		}
	}
	if errors.Is(err, errMoved) {
		return nil
	}
	if err != nil {
		return err
	}
	if decision.Ask {
		if err := in.noteReplaced(ctx, it, routed, staged); err != nil {
			return err
		}
	}
	handle := it.Person()
	if handle == "" {
		handle = owner
	}
	_, err = in.opts.Record.Append(ctx, record.Event{Kind: kind, Job: it.ID, Handle: handle, Detail: reason})
	return err
}

// noteReplaced writes the activity row of a job a DIFFERENT rule now routes
// than the one that routed it before: which rule, in place of which, and the
// steps it now has, so a pipeline that changed under a person is never
// changed silently. A job routed for the first time writes nothing here.
func (in *Intake) noteReplaced(ctx context.Context, it factory.Item, r *routing, staged []factory.Stage) error {
	if r == nil || it.Route == "" || it.Route == r.rule.Name {
		return nil
	}
	_, err := in.opts.Record.Append(ctx, record.Event{Kind: record.Routed, Job: it.ID, Detail: replacedWords(it, r, staged)})
	return err
}

// refuse keeps a matched rule that cannot be applied off the job: nothing of
// the job changes but [factory.Item.RouteRefused], and the activity row
// saying why is written when the sentence is new, so a pass that finds the
// same refusal again writes nothing.
func (in *Intake) refuse(ctx context.Context, it factory.Item, why string) error {
	fresh := false
	err := in.st.Update(it.ID, func(cur *factory.Item) error {
		if err := in.canStart(ctx, *cur); err != nil {
			return err
		}
		if cur.RouteRefused == why {
			return errMoved
		}
		cur.RouteRefused, fresh = why, true
		return nil
	})
	if errors.Is(err, errMoved) {
		return nil
	}
	if err != nil || !fresh {
		return err
	}
	_, err = in.opts.Record.Append(ctx, record.Event{Kind: record.Routed, Job: it.ID, Detail: why})
	return err
}

var errMoved = errors.New("intake: job already moved")

// file keeps a routed job that starts nothing where it is, in Backlog, with
// its rule's decision written on it ([applyRoute]) so the board shows the
// repository, priority and steps the rule chose. An unrouted job is left
// untouched, as before.
//
// A JOB A RULE FILES IN BACKLOG SAYS SO IN ITS ACTIVITY, once, when the rule
// first files it: `routing rule <name> · Backlog`, and, when it takes the
// place of the rule that routed it before, which one and the steps it has
// now.
func (in *Intake) file(ctx context.Context, it factory.Item, f pipeline.File, routed *routing) error {
	if routed == nil {
		return nil
	}
	var staged []factory.Stage
	err := in.st.Update(it.ID, func(cur *factory.Item) error {
		if err := in.canStart(ctx, *cur); err != nil {
			return err
		}
		if err := stage(cur, f, routed); err != nil {
			return err
		}
		staged = cur.Stages
		return nil
	})
	if errors.Is(err, errMoved) {
		return nil
	}
	if err != nil || it.Route == routed.rule.Name {
		return err
	}
	detail := routed.reason() + " · Backlog"
	if it.Route != "" {
		detail = replacedWords(it, routed, staged) + " · Backlog"
	}
	_, err = in.opts.Record.Append(ctx, record.Event{Kind: record.Routed, Job: it.ID, Detail: detail})
	return err
}

// lookFirst looks at a job closer before an automatic start and answers the
// job as the look left it; false when there is no scout, the job already has
// a current look, or the look failed.
func (in *Intake) lookFirst(ctx context.Context, it factory.Item) (factory.Item, bool) {
	if in.opts.Scout == nil || (it.Scout != nil && !it.Scout.Stale(it)) {
		return it, false
	}
	if in.opts.Scout(ctx, it.ID) != nil {
		return it, false
	}
	looked, err := in.st.Get(it.ID)
	return looked, err == nil
}

// canStart reads the stop history under the job's update lock, so a stop
// after Once listed the backlog still wins. Only a person's Run door may
// restart a job they stopped; labels never restore that permission.
func (in *Intake) canStart(ctx context.Context, it factory.Item) error {
	if factory.StoppedByPerson(it) || factory.EverRan(it) || it.SetupHeld || factory.JobState(it) == "done" {
		return errMoved
	}
	if it.State != factory.StateNew {
		return errMoved
	}
	events, err := in.opts.Record.Since(ctx, time.Time{})
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.Job == it.ID && (event.Kind == record.Stopped || event.Kind == record.ClosedLocally) {
			return errMoved
		}
	}
	return nil
}

// pathWords retains directories as well as filenames, so "touch auth/" asks
// before a plan exists. Plans and triage's inferred paths use the same matcher.
var pathWords = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.*?-]*)+`)

// Job translates persisted facts into the evaluator's inputs. THE SIZE IS THE
// LABEL ALONE ([factory.JobSize]): a read's or a diff's size is for display
// and never reaches a start decision. A truncated PR
// file list is not a line measurement, so it asks rather than guessing small.
func Job(it factory.Item) pipeline.Job {
	j := pipeline.Job{
		ID: it.ID, Kind: it.Kind, Tier: it.Tier, Labels: it.Labels,
		Size: factory.JobSize(it),
		Cap:  it.Cap, Judged: it.Triage.StartJudged, Matched: it.Triage.StartMatched,
		Paths: append([]string(nil), it.Triage.StartPaths...),
	}
	text := it.Title + "\n" + it.Body
	for _, comment := range it.Comments {
		text += "\n" + comment.Body
	}
	if it.Stream != nil {
		for _, phase := range it.Stream.Phases {
			if phase.Name == "plan" && phase.Result != nil {
				text += "\n" + phase.Result.Output
			}
		}
	}
	j.Paths = append(j.Paths, pathWords.FindAllString(text, -1)...)
	j.LinesKnown = it.Kind == factory.KindPR && it.Files != nil && len(it.Files) < forge.FilesMost
	for _, file := range it.Files {
		j.Paths = append(j.Paths, file.Path)
		j.Lines += file.Added + file.Removed
	}
	return j
}
