package github

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// closeSweepBudget bounds direct reads of pre-existing rows per repository per
// tick. It never lists closed history, and its progress survives a restart.
const closeSweepBudget = 40

// untouchedBacklog excludes every job that started or has work to preserve.
// A held start decision with no work is safe to finish just like a new row.
func untouchedBacklog(it factory.Item) bool {
	return factory.IsIssueJob(it) && it.Origin == factory.OriginForge && it.Num > 0 &&
		(it.State == factory.StateNew || it.State == factory.StateNeedsYou) &&
		it.DoneHow == "" && it.Starter == "" && !it.PipelineDecided && it.Stream == nil &&
		it.Talk == "" && it.WorktreeMachine == "" && it.PullRequest == nil &&
		!it.Publishing && factory.JobSpent(it) == 0 && it.Failure == nil
}

func finishClosed(st *store.Store, id int) error {
	finished := false
	err := st.Update(id, func(it *factory.Item) error {
		if !untouchedBacklog(*it) {
			return nil
		}
		it.State, it.DoneHow, it.DoneAt = factory.StateShipped, StandingClosed, time.Now().UTC()
		it.Marked, it.Question, it.QKind, it.StopUndo = false, "", "", nil
		finished = true
		return nil
	})
	if err != nil || !finished {
		return err
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		return err
	}
	_, err = history.Append(context.Background(), record.Event{Kind: record.SourceClosed, Verb: StandingClosed, Job: id, Detail: "on GitHub"})
	return err
}

func (s *Source) sweepClosed(ctx context.Context, st *store.Store, repos []string, before, fresh []factory.Item) error {
	meta, err := st.SourceMeta(Name)
	if err != nil {
		return err
	}
	if meta.IssueSweeps == nil {
		meta.IssueSweeps = map[string]store.IssueSweep{}
	}
	seen := map[string]bool{}
	for _, it := range fresh {
		seen[itemKey(it)] = true
	}
	sort.Slice(before, func(i, j int) bool { return before[i].ID < before[j].ID })
	for _, repo := range repos {
		key := strings.ToLower(repo)
		sweep, exists := meta.IssueSweeps[key]
		if sweep.Done {
			continue
		}
		if !exists {
			for _, it := range before {
				if strings.EqualFold(factory.RepoOf(it), repo) {
					sweep.Last = max(sweep.Last, it.ID)
				}
			}
		}
		left := closeSweepBudget
		for _, it := range before {
			if it.ID <= sweep.Through || it.ID > sweep.Last || !strings.EqualFold(factory.RepoOf(it), repo) {
				continue
			}
			if untouchedBacklog(it) && !seen[itemKey(it)] {
				if left == 0 {
					break
				}
				left--
				how, err := Standing(ctx, s.api, it)
				if err != nil {
					return err
				}
				if how == StandingClosed {
					if err := finishClosed(st, it.ID); err != nil {
						return err
					}
				}
			}
			sweep.Through = it.ID
		}
		sweep.Done = sweep.Through >= sweep.Last
		meta.IssueSweeps[key] = sweep
	}
	return st.SetSourceMeta(Name, meta)
}
