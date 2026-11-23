package inbox

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// Feed is a connector that pulls outside work: a ticket tracker, an alert
// service, a mail folder. A feed only translates; it never decides policy
// (that is internal/factory/route) and never writes the store (that is
// [Add]). [Poll] drives every feed the same way, so a new connector is one
// small type and no new loop.
type Feed interface {
	// Name is the source every entry it returns carries, such as "jira".
	Name() string
	// Fetch returns what changed since cursor and the next cursor. A feed that
	// cannot say "since" returns everything and the same cursor; [Add] makes
	// the repeat harmless.
	Fetch(ctx context.Context, cursor string) (entries []Entry, next string, err error)
}

// maxBackoff is the longest a failing feed waits between passes.
const maxBackoff = 10 * time.Minute

// logf is where the inbox's background work says what went wrong: the
// factory process's own log. A test replaces it.
var logf = defaultLogf

var defaultLogf = log.Printf

// Poll runs f every interval until ctx ends, filing each entry through [Add]
// and calling after (the intake pass) once a pass. A failing pass waits twice
// as long each time, up to ten minutes, and is logged with secrets redacted.
//
// THE CURSOR LIVES IN THIS LOOP, as the GitHub poll's does: a restart reads
// from the beginning once, and [Add] makes that harmless. What the floor
// draws about the feed (last read, last try, the trouble) is its record in
// the store's source meta, the same record the GitHub poll writes.
func Poll(ctx context.Context, f Feed, st *store.Store, every time.Duration, after func(context.Context) error, opts ...Option) {
	if f == nil || st == nil || every <= 0 {
		return
	}
	cursor := ""
	failed := 0
	for {
		next, err := PollOnce(ctx, f, st, cursor, time.Now, opts...)
		if after != nil && ctx.Err() == nil {
			err = errors.Join(err, after(ctx))
		}
		cursor = next
		if err != nil {
			failed++
			logf("factory: %s poll failed: %s", f.Name(), redact.Secrets(err.Error()))
		} else {
			failed = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff(every, failed)):
		}
	}
}

// PollOnce is one pass: take the floor's poller lock (and skip quietly when
// another poll holds it), fetch since cursor, file every entry, and write the
// feed's record. It answers the next cursor and the pass's error. AN ENTRY
// THAT IS REFUSED DOES NOT STOP THE REST: each is filed on its own, and the
// refusals come back joined. A fetch that failed keeps the old cursor, so the
// same window is fetched again next pass.
func PollOnce(ctx context.Context, f Feed, st *store.Store, cursor string, now func() time.Time, opts ...Option) (string, error) {
	release, ok := st.TryPoller()
	if !ok {
		return cursor, nil
	}
	defer release()
	name := f.Name()
	_ = st.SetPolling(name, true)
	defer func() { _ = st.SetPolling(name, false) }()

	entries, next, fetchErr := f.Fetch(ctx, cursor)
	var errs []error
	for _, e := range entries {
		if e.Source == "" {
			e.Source = name
		}
		if e.Source != name {
			errs = append(errs, fmt.Errorf("%s handed over an entry from %q", name, e.Source))
			continue
		}
		if _, err := Add(ctx, st, e, now(), opts...); err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", name, e.ExternalID, err))
		}
	}
	if fetchErr != nil {
		next = cursor
		errs = append(errs, fetchErr)
	}
	err := errors.Join(errs...)
	at := now()
	meta, _ := st.SourceMeta(name)
	meta.Tried = at
	if fetchErr == nil {
		meta.Polled = at
	}
	meta.Trouble = ""
	if err != nil {
		meta.Trouble = redact.Secrets(err.Error())
	}
	return next, errors.Join(err, st.SetSourceMeta(name, meta))
}

// backoff is the wait after failed failures in a row: every when none, then
// twice, four times and so on, up to [maxBackoff].
func backoff(every time.Duration, failed int) time.Duration {
	d := every
	for i := 0; i < failed && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, max(every, maxBackoff))
}
