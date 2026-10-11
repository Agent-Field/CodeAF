// Package intake applies the repository's start rules after the cheap read.
// Policy is separate from triage so a model never owns the launch decision.
package intake

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// Options injects repository policy and the existing launch door; tests need no model.
type Options struct {
	Load   func(string) (pipeline.File, error)
	Launch func(int) error
	Record *record.Record
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
	f, err := in.opts.Load(it.Repo)
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
	items, err := in.st.List()
	if err != nil {
		return err
	}
	var errs []error
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if it.State != factory.StateNew || it.Triage.TriagedAt.IsZero() || it.AskedFirstReason != "" {
			continue
		}
		if err := in.decide(ctx, it); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (in *Intake) decide(ctx context.Context, it factory.Item) error {
	f, err := in.opts.Load(it.Repo)
	if err != nil {
		return err
	}
	job := Job(it)
	job.Judged = job.Judged && slices.Equal(it.Triage.StartRules, f.Start.AskFirst)
	decision := f.Decide(job)
	if !decision.Start && !decision.Ask {
		return nil
	}
	kind, reason := record.AutoStarted, "inside start rules"
	if decision.Ask {
		kind, reason = record.AskedFirst, strings.Join(decision.Reasons, "; ")
		err = in.st.Update(it.ID, func(cur *factory.Item) error {
			if cur.State != factory.StateNew {
				return errMoved
			}
			cur.Stages = factory.CopyStages(f.Recipe().For(cur.Kind))
			cur.State, cur.QKind = factory.StateNeedsYou, factory.QKindAskFirst
			cur.AskedFirstReason = reason
			cur.Question = "asked first: " + reason + ". Start this job?"
			return nil
		})
	} else {
		// YAML may define a different pipeline from the one copied on arrival.
		// The launch door must see those steps before it compiles the run.
		err = in.st.Update(it.ID, func(cur *factory.Item) error {
			if cur.State != factory.StateNew {
				return errMoved
			}
			cur.Stages = factory.CopyStages(f.Recipe().For(cur.Kind))
			return nil
		})
		if err == nil {
			err = in.opts.Launch(it.ID)
		}
	}
	if errors.Is(err, errMoved) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = in.opts.Record.Append(ctx, record.Event{Kind: kind, Job: it.ID, Handle: it.Person(), Detail: reason})
	return err
}

var errMoved = errors.New("intake: job already moved")

// pathWords retains directories as well as filenames, so "touch auth/" asks
// before a plan exists. Plans and triage's inferred paths use the same matcher.
var pathWords = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.*?-]*)+`)

// Job translates persisted facts into the evaluator's inputs. A truncated PR
// file list is not a line measurement, so it asks rather than guessing small.
func Job(it factory.Item) pipeline.Job {
	j := pipeline.Job{
		ID: it.ID, Kind: it.Kind, Tier: it.Tier, Labels: it.Labels,
		Size: it.Triage.Size, Cost: it.Triage.Est, CostKnown: it.Triage.Est > 0,
		Cap: it.Cap, Judged: it.Triage.StartJudged, Matched: it.Triage.StartMatched,
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
	j.LinesKnown = it.Kind == factory.KindPR && it.Files != nil && len(it.Files) < 20
	for _, file := range it.Files {
		j.Paths = append(j.Paths, file.Path)
		j.Lines += file.Added + file.Removed
	}
	return j
}
