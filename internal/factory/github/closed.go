package github

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// closeSweepBudget bounds direct reads of rows the open list lacks, per
// repository per tick. It never lists closed history. Each process sweeps once
// ([store.Store.ClearBusy] starts it over), because each polls from no cursor.
const closeSweepBudget = 40

// untouchedBacklog is a Backlog row that nothing has started or worked on. A
// held start decision with no work is safe to finish just like a new row.
func untouchedBacklog(it factory.Item) bool {
	return closableIssue(it) && (it.State == factory.StateNew || it.State == factory.StateNeedsYou) &&
		it.DoneHow == "" && !it.PipelineDecided
}

func closableIssue(it factory.Item) bool {
	return factory.IsIssueJob(it) && it.Origin == factory.OriginForge && it.Num > 0 && factory.JobUnworked(it)
}

// closedWithoutWork is the row an issue reopened on GitHub takes back to
// Backlog: it left because GitHub closed it, with nothing done on it. A job
// with work on it keeps its ending, and a person chooses Run again.
func closedWithoutWork(it factory.Item) bool {
	return closableIssue(it) && it.State == factory.StateShipped && it.DoneHow == StandingClosed
}

func reopenClosed(st *store.Store, id int) error {
	back := false
	err := st.Update(id, func(it *factory.Item) error {
		if !closedWithoutWork(*it) {
			return nil
		}
		it.State, it.DoneHow, it.DoneAt = factory.StateNew, "", time.Time{}
		back = true
		return nil
	})
	if err != nil || !back {
		return err
	}
	return recordEnding(st, record.Restored, "reopened", id)
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
	return recordEnding(st, record.SourceClosed, StandingClosed, id)
}

func recordEnding(st *store.Store, kind, verb string, id int) error {
	history, err := record.Open(st, record.Options{})
	if err != nil {
		return err
	}
	_, err = history.Append(context.Background(), record.Event{Kind: kind, Verb: verb, Job: id, Detail: "on GitHub"})
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
	var errs []error
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
					var apiErr *forge.APIError
					if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusGone) {
						// A missing issue is unknown, not closed. Leave its row
						// alone, with no trouble on the poll and no block on the
						// rest of the old backlog.
						sweep.Through = it.ID
						continue
					}
					errs = append(errs, err)
					break
				}
				if how == StandingClosed {
					if err := finishClosed(st, it.ID); err != nil {
						errs = append(errs, err)
						break
					}
				}
			}
			sweep.Through = it.ID
		}
		sweep.Done = sweep.Through >= sweep.Last
		meta.IssueSweeps[key] = sweep
	}
	return errors.Join(append(errs, st.SetSourceMeta(Name, meta))...)
}
