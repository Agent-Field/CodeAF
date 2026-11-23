package run

// ── MERGING: A PERSON'S MERGE, OR THE PIPELINE'S, CARRIED OUT ────────────
//
// The factory decides that a merge may happen ([factory.MergeGate] on the
// facts its last follow read) and leaves it beside the job's lease; the
// runner that pulls the job carries it out with the assignee's own forge
// account, exactly as it publishes. Before it asks the forge for anything it
// reads the pull request again and runs THE SAME GATE on that fresh read, and
// then it asks the forge to merge only the exact commit the gate passed.
//
// A merge that worked reports merged and the pull request it read; one that
// the gate or the forge refused reports failed with the words that refused
// it. The factory applies the report ([applyMerge]) and the job is done when
// its pull request reads merged.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// Merge carries out the merge the factory queued for job id, of exactly
// commit. It returns once the merge has begun; the forge is asked on its own
// goroutine, so [Runner.Running] holds while it works and the worker keeps
// beating.
func (r *Runner) Merge(id int, commit string) error {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return errors.New("a merge names the exact commit to merge")
	}
	lp := r.loop()
	lp.mu.Lock()
	if lp.ctls[id] != nil {
		lp.mu.Unlock()
		return errors.New("this job is already busy on this machine")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &loopCtl{id: id, ctx: ctx, cancel: cancel, steer: make(chan string, 1), answers: make(chan loopAnswer, 1), phase: -1}
	lp.ctls[id] = c
	lp.mu.Unlock()
	now := r.now()
	err := lp.write(c, func(it *factory.Item) error {
		if it.State != factory.StateShipped {
			return fmt.Errorf("%s has not opened its pull request", it.Ref())
		}
		m := it.Merging
		if m == nil || (m.State != factory.MergeQueued && m.State != factory.MergeMerging) {
			return errors.New("no merge was asked for")
		}
		cp := *m
		cp.State = factory.MergeMerging
		it.Merging = &cp
		loopSay(it, now, "said", "merging · "+shortCommit(commit))
		return nil
	})
	if err != nil {
		lp.forget(c)
		return err
	}
	go func() {
		defer lp.release(c)
		lp.mergeRun(c, commit)
	}()
	return nil
}

func (lp *floorLoop) mergeRun(c *loopCtl, commit string) {
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil {
		return
	}
	fresh, mergeSHA, why := lp.r.merge(c.ctx, it, commit)
	now := lp.r.now()
	_ = lp.write(c, func(it *factory.Item) error {
		m := it.Merging
		if m == nil {
			m = &factory.MergeRun{Commit: commit, At: now}
		}
		cp := *m
		if fresh != nil {
			pr := *fresh
			it.PullRequest = &pr
		}
		if why != "" {
			cp.State, cp.Why = factory.MergeFailed, why
			it.Merging = &cp
			loopSay(it, now, "fail", MergeFailedLine(why))
			return nil
		}
		// The merge commit rides home in Commit: the head that was merged is
		// already on the factory's own record of the merge.
		cp.State, cp.Why, cp.Commit = factory.MergeMerged, "", mergeSHA
		it.Merging = &cp
		if it.DoneAt.IsZero() {
			it.DoneAt = now
		}
		loopSay(it, now, "ok", MergedLine(*it))
		return nil
	})
	lp.emit(c.id, "", EventShipped, "merged")
}

// merge reads the pull request again, runs the gate on that read and asks
// the forge to merge exactly commit. It answers the pull request as last
// read, the merge commit when the forge merged, and why when it did not.
func (r *Runner) merge(ctx context.Context, it factory.Item, commit string) (*factory.PullRequest, string, string) {
	var src factory.Source
	if r.opts.Publisher != nil {
		target, err := r.opts.Publisher(ctx, it)
		if err != nil {
			return nil, "", "this machine has no forge account to merge with: " + loopFirstLine(err.Error())
		}
		src = target.Source
	} else if r.opts.Source != nil {
		src = r.opts.Source(it.Repo)
	}
	if src == nil {
		return nil, "", it.Ref() + " is not on a source codeaf can merge on"
	}
	follow, ok := src.(factory.Follower)
	if !ok {
		return nil, "", "this machine cannot read the pull request again before merging"
	}
	fresh, err := follow.Follow(ctx, it)
	if err != nil {
		return nil, "", "could not read the pull request: " + loopFirstLine(err.Error())
	}
	m := it.Merging
	ask := factory.MergeAsk{}
	method := ""
	if m != nil {
		ask = factory.MergeAsk{By: m.By, Auto: m.Auto}
		method = m.Method
	}
	if method == "" && it.MergePolicy != nil {
		method = it.MergePolicy.Method
	}
	// THE SAME GATE, ON THE READ JUST MADE. The merge in hand is not a merge
	// "already on its way" to itself, so the gate sees the job without it.
	check := it
	check.PullRequest = &fresh
	check.Merging = nil
	v := factory.MergeGate(check, ask)
	if !v.OK {
		return &fresh, "", v.Why
	}
	if !strings.EqualFold(v.Commit, commit) {
		return &fresh, "", "the pull request has commits you did not approve"
	}
	rcpt, err := src.Write(ctx, factory.Action{Verb: "merge", Item: check, Commit: commit, Method: method})
	if err != nil {
		// A LOST ANSWER MAY HIDE A MERGE. Read the pull request again before
		// saying the merge failed; never send it a second time.
		if again, ferr := follow.Follow(ctx, it); ferr == nil && again.State == factory.PRMerged {
			return &again, "", ""
		}
		return &fresh, "", mergeRefusal(err)
	}
	merged := fresh
	merged.State = factory.PRMerged
	merged.MergedAt = r.now()
	if m != nil && m.By != "" {
		merged.MergedBy = factory.NormalizeHandle(m.By)
	}
	return &merged, strings.TrimSpace(rcpt.ID), ""
}

// mergeRefusal is the forge's refusal in the job header's words.
func mergeRefusal(err error) string {
	switch {
	case errors.Is(err, forge.ErrHeadMoved):
		return "the pull request has commits you did not approve"
	case errors.Is(err, forge.ErrNotMergeable):
		if _, detail, ok := strings.Cut(err.Error(), ": "); ok && strings.TrimSpace(detail) != "" {
			return "the forge would not merge it: " + loopFirstLine(detail)
		}
		return "the forge would not merge it"
	case errors.Is(err, factory.ErrReadOnly):
		return "this machine cannot merge on that forge"
	}
	return "the merge did not go through: " + loopFirstLine(err.Error())
}

// MergedLine and MergeFailedLine are the log's words for a merge, written by
// the runner and again by the factory when it applies the report, so the
// report's echo of them is dropped rather than said twice.
func MergedLine(it factory.Item) string {
	if it.PullRequest != nil && it.PullRequest.URL != "" {
		return "merged · " + it.PullRequest.URL
	}
	return "merged"
}

// MergeFailedLine is the log's words for a merge that did not happen.
func MergeFailedLine(why string) string { return "could not merge: " + why }

func shortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

// applyMerge is the factory's half of a runner's merge report. Only a merge
// the factory queued can be reported, and merged must carry the pull request
// the runner read, merged, so a report can never mark a different one.
func applyMerge(it *factory.Item, rep *factory.MergeReport, now time.Time) error {
	if rep == nil {
		return nil
	}
	m := it.Merging
	if m == nil || (m.State != factory.MergeQueued && m.State != factory.MergeMerging) {
		return errors.New("no merge was asked for this job")
	}
	cp := *m
	switch rep.State {
	case factory.MergeMerging:
		cp.State = factory.MergeMerging
		it.Merging = &cp
	case factory.MergeMerged:
		pr := rep.PR
		if pr == nil || pr.State != factory.PRMerged || it.PullRequest == nil || pr.Number != it.PullRequest.Number {
			return errors.New("a merge report carries the job's own pull request, merged")
		}
		next := *pr
		it.PullRequest = &next
		it.Merging = nil
		if it.DoneAt.IsZero() {
			it.DoneAt = now
		}
		loopSay(it, now, "ok", MergedLine(*it))
	case factory.MergeFailed:
		why := strings.TrimSpace(rep.Why)
		if why == "" {
			why = "the merge did not go through"
		}
		if pr := rep.PR; pr != nil && it.PullRequest != nil && pr.Number == it.PullRequest.Number {
			next := *pr
			it.PullRequest = &next
		}
		cp.State, cp.Why = factory.MergeFailed, why
		it.Merging = &cp
		loopSay(it, now, "fail", MergeFailedLine(why))
	default:
		return fmt.Errorf("unknown merge state %q", rep.State)
	}
	return nil
}

// mergeMoved is the merge part of [Diff]: a merge the runner finished, in the
// report's words. A merge only begun is not reported; the factory already
// knows it sent one.
func mergeMoved(prev, cur factory.Item) *factory.MergeReport {
	m := cur.Merging
	if m == nil || (prev.Merging != nil && *prev.Merging == *m) {
		return nil
	}
	switch m.State {
	case factory.MergeMerged:
		rep := &factory.MergeReport{State: factory.MergeMerged, SHA: m.Commit}
		if cur.PullRequest != nil {
			pr := *cur.PullRequest
			rep.PR = &pr
		}
		return rep
	case factory.MergeFailed:
		rep := &factory.MergeReport{State: factory.MergeFailed, Why: m.Why}
		if cur.PullRequest != nil {
			pr := *cur.PullRequest
			rep.PR = &pr
		}
		return rep
	}
	return nil
}
