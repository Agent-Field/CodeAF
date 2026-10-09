package github

import (
	"context"
	"errors"
	"net/http"
	"reflect"
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

// Progresser is a source that can say, repository by repository, where its
// read is ([Source.SetProgress]).
type Progresser interface {
	SetProgress(fn func(full string, done, of int))
}

// ItemProgresser is a [Progresser] that can also say how many items its read
// has listed so far ([Source.SetProgressItems]). The poll prefers it.
type ItemProgresser interface {
	SetProgressItems(fn func(full string, done, of, items int))
}

// Sinker is a source that can hand over its items batch by batch while a
// read is still going ([Source.SetSink]).
type Sinker interface {
	SetSink(fn func(items []factory.Item))
}

// Poll reads src every tick until ctx ends, folding what it reads into st.
// It is meant to run on its own goroutine, one per process. Before each read
// the watched repositories are read again off the store, so a repository
// watched mid-session is read on the next tick; with none watched the tick
// does nothing at all. After a failure the wait doubles, from every up to ten
// minutes, and the first good read puts it back. A NUDGE ([Nudge]) ENDS THE
// WAIT EARLY, so a repository watched in the picker is read at once.
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
	waker := NewWaker(st)
	for {
		if wait > 0 && !waker.Wait(ctx, wait) {
			return
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
//
// THE ITEMS ARE FOLDED AS THEY COME when the source can hand them over in
// batches ([Sinker]): each batch is merged the moment the source has it, so
// the floor fills repository by repository, and the whole answer is merged
// once more at the end, which skips every item a batch already brought. A
// read that fails on a later repository keeps what the earlier batches
// folded; the cursor is what Read answered, so the failed repository is read
// again from its old mark next tick.
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

	// THE READ IS MARKED IN FLIGHT for the length of it, so the floor can say
	// github is being read; a crash mid-read is cleared by the next process
	// to start (store.ClearBusy).
	// AND WHERE THE READ IS, repository by repository, when the source can
	// say (Reading, Read and Of on the source's record); they are taken off
	// with Polling at the end.
	_ = st.SetPolling(src.Name(), true)
	name := src.Name()
	if p, ok := src.(ItemProgresser); ok {
		p.SetProgressItems(func(full string, done, of, items int) { _ = st.SetReadingItems(name, full, done, of, items) })
		defer p.SetProgressItems(nil)
	} else if p, ok := src.(Progresser); ok {
		p.SetProgress(func(full string, done, of int) { _ = st.SetReading(name, full, done, of) })
		defer p.SetProgress(nil)
	}
	if k, ok := src.(Sinker); ok {
		k.SetSink(func(items []factory.Item) { _ = Merge(st, name, items) })
		defer k.SetSink(nil)
	}
	items, next, readErr := src.Read(ctx, cursor)
	mergeErr := Merge(st, src.Name(), items)
	at := now()
	meta, _ := st.SourceMeta(src.Name())
	meta.Tried = at
	meta.Polling = false
	meta.Reading, meta.Read, meta.Of, meta.Items = "", 0, 0, 0
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
// words brought up to date: title, body, labels, checks, diff, the author's
// standing, its address, its last comments, and a pull request's files and
// check runs. EVERYTHING A PERSON SET IS KEPT: its state, stages, gate and cap
// are never touched, so a poll cannot undo an edit. The one exception is a
// dismissed item that changed on the forge, which comes back as new, because
// dismissing hides an item until it changes. An item not yet on the floor is
// created as new with the default stages for its kind, and its activity
// begins `arrived from github`.
//
// A CHANGED TITLE OR BODY IS A NEW ITEM TO READ: the triage's read is taken
// off ([factory.ClearRead]) so the worker reads it again, and the type is the
// fresh read's (the size, a cheap guess the person may have read by, stays). A change of labels alone keeps the read and
// takes the type again from the labels. Either is `changed on github` in the
// item's activity; a new comment, file or check is not, because the row's own
// words did not move.
//
// WHAT WAS NOT READ YET IS NOT A CHANGE. An item whose Comments are nil
// keeps the comments the floor has, and a pull request whose Files are nil
// keeps its files and line counts: the source handed it over before reading
// them (the per-read budget, [Source.SetSink]), and an empty list is the one
// that says there are none.
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
			at := time.Now()
			errs = append(errs, st.Update(old.ID, func(it *factory.Item) error {
				words := it.Title != in.Title || it.Body != in.Body
				labels := !sameLabels(it.Labels, in.Labels)
				switch {
				case words:
					factory.ClearRead(&it.Triage)
					if in.Triage.Type != "" {
						it.Triage.Type = in.Triage.Type
					}
				case labels && in.Triage.Type != "":
					it.Triage.Type = in.Triage.Type
				}
				if words || labels {
					it.Note(at, factory.EventChanged)
				}
				it.Title, it.Body = in.Title, in.Body
				it.Labels = append([]string(nil), in.Labels...)
				it.Checks = in.Checks
				if in.URL != "" {
					it.URL = in.URL
				}
				if in.Comments != nil {
					it.Comments = in.Comments
				}
				if in.Kind != factory.KindPR || in.Files != nil {
					it.Diff = in.Diff
				}
				if in.Kind == factory.KindPR {
					if in.Files != nil {
						it.Files = in.Files
					}
					it.CheckRuns = in.CheckRuns
				}
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
		if len(in.Places) == 0 && in.Repo != "" {
			in.Places = []string{in.Repo}
		}
		in.Activity = nil
		in.Note(time.Now(), factory.EventArrived)
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
	// A pull request handed over before its files were read says nothing
	// about its files or line counts.
	detailed := in.Kind != factory.KindPR || in.Files != nil
	if old.Title != in.Title || old.Body != in.Body || old.Checks != in.Checks || (detailed && old.Diff != in.Diff) {
		return true
	}
	if in.Tier != "" && old.Tier != in.Tier {
		return true
	}
	if !sameLabels(old.Labels, in.Labels) {
		return true
	}
	if in.URL != "" && old.URL != in.URL {
		return true
	}
	if in.Comments != nil && !reflect.DeepEqual(old.Comments, in.Comments) {
		return true
	}
	if in.Kind == factory.KindPR && ((in.Files != nil && !reflect.DeepEqual(old.Files, in.Files)) || !reflect.DeepEqual(old.CheckRuns, in.CheckRuns)) {
		return true
	}
	return false
}

func sameLabels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Refetcher is the floor's way to read one GitHub item again
// ([factory.WithRefetch]): source answers a source over the person's token
// at the moment of asking (nil when none resolves), the item is read again
// through it ([Source.Refetch]) and folded into st ([Merge]). IT IS BUILT
// FRESH ON EVERY ASK because the window that draws the floor is not the
// process that polls, and a token connected a minute ago is the one to use.
func Refetcher(st *store.Store, source func(ctx context.Context) *Source) func(context.Context, factory.Item) error {
	return func(ctx context.Context, it factory.Item) error {
		src := source(ctx)
		if src == nil {
			return errors.New("github is not connected")
		}
		fresh, err := src.Refetch(ctx, it)
		if err != nil {
			return err
		}
		return Merge(st, src.Name(), []factory.Item{fresh})
	}
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
	return factory.SourceInfo{Name: Name, Repos: repos, Polled: meta.Polled, Trouble: meta.Trouble, Polling: meta.Polling,
		Reading: meta.Reading, Read: meta.Read, Of: meta.Of, Items: meta.Items}, true
}
