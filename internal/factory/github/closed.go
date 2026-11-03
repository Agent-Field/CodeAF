package github

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const closeSweepBudget = 40
const closeSweepEvery = 10 * time.Minute

type closeCheck struct {
	order    uint64
	failures int
	retry    time.Time
}
type closeChecks struct {
	pending  map[int]closeCheck
	sequence uint64
	sweep    time.Time
}

// This is scheduling only. Losing it delays checks until the next sweep;
// neither a list nor process memory is evidence that an issue is closed.
var checksMu sync.Mutex
var checks = map[string]*closeChecks{}

func (s *Source) reconcileClosed(ctx context.Context, st *store.Store, repos []string, before, fresh []factory.Item) error {
	checksMu.Lock()
	q := checks[st.Root()]
	if q == nil {
		q = &closeChecks{pending: map[int]closeCheck{}}
		checks[st.Root()] = q
	}
	checksMu.Unlock()
	now := s.now()
	listed := map[string]bool{}
	closed := map[string]bool{}
	for _, it := range fresh {
		listed[itemKey(it)] = true
		closed[itemKey(it)] = it.DoneHow == StandingClosed
	}
	initial := q.sweep.IsZero()
	sweep := !q.sweep.IsZero() && !now.Before(q.sweep.Add(closeSweepEvery))
	if q.sweep.IsZero() || sweep {
		q.sweep = now
	}
	sort.Slice(before, func(i, j int) bool { return before[i].ID < before[j].ID })
	for _, it := range before {
		if !factory.UntouchedIssue(it) {
			delete(q.pending, it.ID)
			continue
		}
		if it.SourceClosed || closed[itemKey(it)] || (initial && !listed[itemKey(it)]) || sweep {
			if _, ok := q.pending[it.ID]; !ok {
				q.sequence++
				q.pending[it.ID] = closeCheck{order: q.sequence}
			}
		}
	}
	rows := map[int]factory.Item{}
	for _, it := range before {
		rows[it.ID] = it
	}
	ids := make([]int, 0, len(q.pending))
	for id := range q.pending {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return q.pending[ids[i]].order < q.pending[ids[j]].order })
	for _, repo := range repos {
		left := closeSweepBudget
		for _, id := range ids {
			it, ok := rows[id]
			if !ok {
				delete(q.pending, id)
				continue
			}
			if !strings.EqualFold(factory.RepoOf(it), repo) {
				continue
			}
			c := q.pending[id]
			if now.Before(c.retry) {
				continue
			}
			if left == 0 {
				break
			}
			left--
			if err := s.finishIfClosed(ctx, st, it); err != nil {
				q.sequence++
				c.order = q.sequence
				c.failures++
				c.retry = now.Add(Backoff(time.Minute, c.failures))
				q.pending[id] = c
			} else {
				delete(q.pending, id)
			}
		}
	}
	return nil
}

// finishIfClosed is the sole writer of automatic close and reopen state.
// The answer has no remembered time. The row is read again under its lock,
// so a start or a person's edit during the request wins.
func (s *Source) finishIfClosed(ctx context.Context, st *store.Store, row factory.Item) error {
	owner, name, ok := pullRepo(row)
	if !ok {
		return nil
	}
	answer, err := s.api.Issue(ctx, owner, name, row.Num)
	if err != nil {
		return err
	}
	if answer.Redirected || answer.Number != row.Num {
		return nil
	}
	if answer.ID == 0 || (answer.State != "open" && answer.State != "closed") {
		return errors.New("GitHub issue identity or state missing")
	}
	kind, verb, detail := "", "", ""
	err = st.Update(row.ID, func(it *factory.Item) error {
		if itemKey(*it) != itemKey(row) || (it.GitHubIssueID != 0 && it.GitHubIssueID != answer.ID) {
			return errNoCloseChange
		}
		it.GitHubIssueID = answer.ID
		switch {
		case answer.State == "closed" && !it.SourceClosed && factory.UntouchedIssue(*it):
			it.State, it.DoneHow, it.DoneAt = factory.StateShipped, StandingClosed, s.now().UTC()
			it.SourceClosed = true
			it.Marked, it.Question, it.QKind, it.StopUndo = false, "", "", nil
			kind, verb, detail = record.SourceClosed, StandingClosed, "on GitHub"
		case answer.State == "open" && it.SourceClosed && factory.UntouchedIssue(*it):
			it.State, it.DoneHow, it.DoneAt = factory.StateNew, "", time.Time{}
			it.SourceClosed = false
			kind, verb, detail = record.Restored, "brought back", "reopened on GitHub"
		default:
			if row.GitHubIssueID == answer.ID {
				return errNoCloseChange
			}
		}
		return nil
	})
	if errors.Is(err, errNoCloseChange) {
		return nil
	}
	if err != nil || kind == "" {
		return err
	}
	return recordEnding(st, kind, verb, detail, row.ID)
}

var errNoCloseChange = errors.New("issue reconciliation unchanged")

func recordEnding(st *store.Store, kind, verb, detail string, id int) error {
	history, err := record.Open(st, record.Options{})
	if err != nil {
		return err
	}
	_, err = history.Append(context.Background(), record.Event{Kind: kind, Verb: verb, Job: id, Detail: detail})
	return err
}
