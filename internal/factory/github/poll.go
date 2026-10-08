package github

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// maxBackoff is the longest the poll waits after failures in a row.
const maxBackoff = 10 * time.Minute

// FactsFresh is how recently a poll must have tried for the floor to name the
// source at all. It is longer than the longest backoff, so a poll that is
// alive and failing is still named, and a record left by a window that has
// since closed is not: A SOURCE NOTHING IS POLLING IS NOT CONNECTED.
const FactsFresh = maxBackoff + 5*time.Minute

// Watcher is a source whose watched repositories can change while it polls.
type Watcher interface {
	Watch(repos []string)
}

// Poll reads src every tick until ctx ends, folding what it reads into st.
// It is meant to run on its own goroutine, one per process. Before each read
// the watched repositories are read again off the store, so a repository
// watched mid-session is read on the next tick; with none watched the tick
// does nothing at all. After a failure the wait doubles, from every up to ten
// minutes, and the first good read puts it back.
//
// NOTHING IS POSTED BY THE POLL. It calls Read and never Write. log, when not
// nil, is told about a failure in words with every secret taken out.
func Poll(ctx context.Context, src factory.Source, st *store.Store, every time.Duration, log func(string)) {
	if src == nil || st == nil || every <= 0 {
		return
	}
	cursor := ""
	wait := time.Duration(0)
	failed := 0
	for {
		if wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
		if ctx.Err() != nil {
			return
		}
		next, err := PollOnce(ctx, src, st, cursor, time.Now)
		cursor = next
		if err != nil {
			failed++
			if log != nil {
				log("factory: " + src.Name() + " poll failed: " + redact.Secrets(err.Error()))
			}
		} else {
			failed = 0
		}
		wait = Backoff(every, failed)
	}
}

// Backoff is the wait before the next tick after failed failures in a row:
// every when none, then twice, four times, and so on up to ten minutes.
func Backoff(every time.Duration, failed int) time.Duration {
	if failed <= 0 {
		return every
	}
	d := every
	for i := 0; i < failed && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

// PollOnce is one tick: refresh the watched repositories, take the floor's
// poller lock (and skip quietly when another window holds it), read since
// cursor, fold the items in, and write the source's record. It answers the
// next cursor and the read's error. Items a partly failed read did bring are
// still folded in.
func PollOnce(ctx context.Context, src factory.Source, st *store.Store, cursor string, now func() time.Time) (string, error) {
	repos, err := st.Repos()
	if err != nil {
		return cursor, err
	}
	if len(repos) == 0 {
		return cursor, nil
	}
	if w, ok := src.(Watcher); ok {
		w.Watch(repos)
	}
	release, ok := st.TryPoller()
	if !ok {
		return cursor, nil
	}
	defer release()

	items, next, readErr := src.Read(ctx, cursor)
	mergeErr := Merge(st, src.Name(), items)
	at := now()
	meta, _ := st.SourceMeta(src.Name())
	meta.Tried = at
	if readErr == nil {
		meta.Polled, meta.Trouble = at, ""
	} else {
		meta.Trouble = TroubleWords(readErr)
	}
	_ = st.SetSourceMeta(src.Name(), meta)
	if readErr != nil {
		return next, readErr
	}
	return next, mergeErr
}

// TroubleWords is a failed read in the few words the floor's facts line says
// after the source's name.
func TroubleWords(err error) string {
	var apiErr *forge.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			return "token refused"
		case http.StatusNotFound:
			return "repository not found"
		}
	}
	return "not reachable"
}

// Merge folds items read from one source into the store. An item already on
// the floor (the same source, owner, repository and number) has its forge
// words brought up to date: title, body, labels, checks, diff and the
// author's standing. EVERYTHING A PERSON SET IS KEPT: its state, stages, gate,
// cap and triage are never touched, so a poll cannot undo an edit. The one
// exception is a dismissed item that changed on the forge, which comes back
// as new, because dismissing hides an item until it changes. An item not yet
// on the floor is created as new with the default stages for its kind.
func Merge(st *store.Store, origin string, items []factory.Item) error {
	if len(items) == 0 {
		return nil
	}
	have, err := st.List()
	if err != nil {
		return err
	}
	index := map[string]factory.Item{}
	for _, it := range have {
		if string(it.Origin) == origin && it.Num > 0 {
			index[itemKey(it)] = it
		}
	}
	var errs []error
	for _, in := range items {
		if old, ok := index[itemKey(in)]; ok {
			if !forgeChanged(old, in) {
				continue
			}
			errs = append(errs, st.Update(old.ID, func(it *factory.Item) error {
				it.Title, it.Body = in.Title, in.Body
				it.Labels = append([]string(nil), in.Labels...)
				it.Checks, it.Diff = in.Checks, in.Diff
				if in.Tier != "" {
					it.Tier = in.Tier
				}
				if it.State == factory.StateDismissed {
					it.State = factory.StateNew
				}
				return nil
			}))
			continue
		}
		in.ID = 0
		in.State = factory.StateNew
		if len(in.Stages) == 0 {
			in.Stages = factory.CopyStages(factory.DefaultRecipe().For(in.Kind))
		}
		if in.Gate == "" {
			in.Gate = factory.GateShip
		}
		if len(in.Places) == 0 && in.Repo != "" {
			in.Places = []string{in.Repo}
		}
		made, err := st.Create(in)
		if err == nil {
			index[itemKey(made)] = made
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func itemKey(it factory.Item) string {
	return strings.ToLower(it.Product + "/" + it.Repo + "#" + string(it.Kind) + strconv.Itoa(it.Num))
}

// forgeChanged says whether any word the forge owns differs.
func forgeChanged(old, in factory.Item) bool {
	if old.Title != in.Title || old.Body != in.Body || old.Checks != in.Checks || old.Diff != in.Diff {
		return true
	}
	if in.Tier != "" && old.Tier != in.Tier {
		return true
	}
	if len(old.Labels) != len(in.Labels) {
		return true
	}
	for i := range old.Labels {
		if old.Labels[i] != in.Labels[i] {
			return true
		}
	}
	return false
}

// Facts wraps a seam's Load so the floor's facts line names this source: its
// name, the watched repositories, when it last read well, and the trouble when
// its last read failed. A FLOOR WITH NO REPOSITORIES WATCHED, OR WHOSE RECORD
// NOBODY HAS WRITTEN FOR [FactsFresh], NAMES NO GITHUB AT ALL: no token, no
// repositories and a closed window all leave the facts line saying only
// `terminal · chat`.
func Facts(seam factory.Seam, st *store.Store, now func() time.Time) factory.Seam {
	if seam.Load == nil || st == nil {
		return seam
	}
	if now == nil {
		now = time.Now
	}
	load := seam.Load
	seam.Load = func() (factory.Snapshot, error) {
		snap, err := load()
		if err != nil {
			return snap, err
		}
		if info, ok := SourceFacts(st, now()); ok {
			snap.Sources = append(snap.Sources, info)
		}
		return snap, nil
	}
	return seam
}

// SourceFacts is the github row of the floor's sources, and whether there is
// one.
func SourceFacts(st *store.Store, now time.Time) (factory.SourceInfo, bool) {
	repos, err := st.Repos()
	if err != nil || len(repos) == 0 {
		return factory.SourceInfo{}, false
	}
	meta, err := st.SourceMeta(Name)
	if err != nil || meta.Tried.IsZero() || now.Sub(meta.Tried) > FactsFresh {
		return factory.SourceInfo{}, false
	}
	// Writes stays false: Write exists, but only a post stage would call it,
	// and none runs yet, so the floor offers nothing that would post.
	return factory.SourceInfo{Name: Name, Repos: repos, Polled: meta.Polled, Trouble: meta.Trouble}, true
}
