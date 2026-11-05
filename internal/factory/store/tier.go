package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// TierResolver looks up the author's access using the factory's read credential.
// retryAt may extend the store's backoff after a failed lookup.
type TierResolver func(context.Context, factory.Item) (tier factory.Tier, retryAt time.Time, err error)

type tierRead struct {
	author, repo string
	done         chan struct{}
	attempts     int
	reason       string
	retryAt      time.Time
}

// SetTierResolver installs read-time author lookup if none is registered.
// Composition owns the resolver; later polls cannot replace its read credential.
// Retry state stays in memory.
func (st *Store) SetTierResolver(resolve TierResolver) {
	st.tierMu.Lock()
	defer st.tierMu.Unlock()
	if st.tierResolver == nil {
		st.tierResolver = resolve
	}
}

func sameTierAuthor(it factory.Item, r *tierRead) bool {
	return it.Author == r.author && factory.RepoOf(it) == r.repo
}

func (st *Store) readHydrated(it factory.Item) (factory.Item, error) {
	st.hydrateTier(it)
	// Lookup runs without the item lock. Atomic document replacement lets
	// us reload the latest saved snapshot without acquiring that lock.
	d, err := st.read(st.ItemPath(it.ID))
	if err != nil {
		return factory.Item{}, err
	}
	it = d.Item
	st.tierDetail(&it)
	st.retainOutput(&it)
	return it, nil
}

func (st *Store) hydrateTier(it factory.Item) {
	if it.Tier != "" || !it.RefusesWrite() || it.Author == "" || factory.RepoOf(it) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	st.tierMu.Lock()
	resolve := st.tierResolver
	if resolve == nil {
		st.tierMu.Unlock()
		return
	}
	if st.tierReads == nil {
		st.tierReads = make(map[int]*tierRead)
	}
	r := st.tierReads[it.ID]
	if r != nil && sameTierAuthor(it, r) {
		if r.done != nil {
			done := r.done
			st.tierMu.Unlock()
			select {
			case <-done:
			case <-ctx.Done():
			}
			return
		}
		if st.now().Before(r.retryAt) {
			st.tierMu.Unlock()
			return
		}
	} else {
		r = &tierRead{author: it.Author, repo: factory.RepoOf(it)}
		st.tierReads[it.ID] = r
	}
	// Another reader may have completed between our initial decode and arrival.
	current, err := st.read(st.ItemPath(it.ID))
	if err != nil || current.Item.Tier != "" || !sameTierAuthor(current.Item, r) {
		st.tierMu.Unlock()
		return
	}
	r.done = make(chan struct{})
	done := r.done
	st.tierMu.Unlock()
	tier, retryAt, err := resolve(ctx, it)
	if err == nil && tier == "" {
		err = fmt.Errorf("permission lookup returned no tier")
	}
	if err == nil {
		err = st.Annotate(it.ID, func(cur *factory.Item) error {
			if cur.Tier != "" || !sameTierAuthor(*cur, r) {
				return errUnchanged
			}
			cur.Tier = tier
			return nil
		})
		if err == errUnchanged {
			err = nil
		}
	}
	st.tierMu.Lock()
	defer st.tierMu.Unlock()
	if err != nil {
		r.attempts++
		delay := time.Minute
		for n := 1; n < r.attempts && delay < 10*time.Minute; n++ {
			delay *= 2
		}
		r.retryAt = st.now().Add(min(delay, 10*time.Minute))
		if retryAt.After(r.retryAt) {
			r.retryAt = retryAt
		}
		r.reason = redact.Secrets(err.Error())
	} else if st.tierReads[it.ID] == r {
		delete(st.tierReads, it.ID)
	}
	r.done = nil
	close(done)
}

// tierDetail projects transient lookup state through the existing failure view.
// The projection is never a saved job field or a saved retry record.
func (st *Store) tierDetail(it *factory.Item) {
	if it.Tier != "" || it.Author == "" || factory.RepoOf(*it) == "" {
		return
	}
	st.tierMu.Lock()
	defer st.tierMu.Unlock()
	r := st.tierReads[it.ID]
	if r == nil || !sameTierAuthor(*it, r) {
		return
	}
	reason := r.reason
	retryAt := r.retryAt
	if r.done != nil {
		reason = "lookup in progress"
		retryAt = st.now().Add(15 * time.Second)
	}
	if reason == "" {
		return
	}
	f := factory.Failure{Code: factory.CodeToolError, Next: factory.FailureStopped}
	if it.Failure != nil {
		f = *it.Failure
	}
	f.Detail = fmt.Sprintf("%s%s; retry after %s", factory.AuthorAccessUnknownPrefix, reason, retryAt.UTC().Format(time.RFC3339))
	it.Failure = &f
}

func isTierProjection(f *factory.Failure) bool {
	return f != nil && strings.HasPrefix(f.Detail, factory.AuthorAccessUnknownPrefix)
}
