// Package record keeps the factory's durable account of human decisions and
// outward actions, so publishing can cite consent rather than infer it.
package record

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/publish"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

const (
	Started     = "started"
	Stopped     = "stopped"
	AutoStarted = "auto_started"
	AskedFirst  = "asked_first"
	Approved    = "approved"
	Answered    = "answered"
	Posted      = "posted"
	Took        = "took"
	Dropped     = "dropped"
	// Released is un-take: the person who took the job let it go. It does not
	// withdraw an approval the way a drop does.
	Released       = "released"
	RequestChanges = "request_changes"
	// PRMerged and PRClosed are the poll noticing a pull request this factory
	// opened. The handle, when GitHub named one, is who merged it.
	PRMerged = "pr_merged"
	PRClosed = "pr_closed"
	// StepFailed is a step that errored. It is not a person's decision. The
	// detail is code, step and next, so the audit can show what happened.
	StepFailed = "step_failed"
)

// Event is the persisted and wire shape; approval receipts use these exact ids.
type Event struct {
	ID     string    `json:"id"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Job    int       `json:"job"`
	Handle string    `json:"handle"`
	Cost   float64   `json:"cost"`
	Detail string    `json:"detail"`
	Commit string    `json:"commit,omitempty"`
}

// Options lets tests advance time without waiting on the wall clock.
type Options struct{ Now func() time.Time }

// Record holds no open files or cached history, so separate servers share truth.
type Record struct {
	root string
	now  func() time.Time
}

func Open(st *store.Store, options Options) (*Record, error) {
	if st == nil || st.Root() == "" {
		return nil, errors.New("factory record: no store")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Record{root: st.Root(), now: options.Now}, nil
}

// Root is where this record and its bounded live replay are persisted.
func (r *Record) Root() string {
	if r == nil {
		return ""
	}
	return r.root
}

// Append mints identity and time here, never from an agent's reported fields.
// SYNC BEFORE CONSENT: a returned receipt must already be on disk.
func (r *Record) Append(ctx context.Context, event Event) (Event, error) {
	if err := validate(event); err != nil {
		return Event{}, err
	}
	event.Handle = factory.NormalizeHandle(event.Handle)
	err := r.locked(ctx, true, func() error {
		events, err := r.read(ctx)
		if err != nil {
			return err
		}
		event.ID, err = mint(events)
		if err != nil {
			return err
		}
		event.At = r.now().UTC()
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(r.path(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err = f.Write(append(data, '\n')); err != nil {
			return err
		}
		if err = f.Sync(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		// Accepted decisions reach open pages even when they do not rewrite a job.
		kind := ""
		switch event.Kind {
		case Approved:
			kind = "approved"
		case Took:
			kind = "taken"
		}
		if kind != "" {
			return live.Append(r.root, live.New(kind, event.Job, live.Payload{Job: event.Job, By: event.Handle}))
		}
		return nil
	})
	if err != nil {
		return Event{}, err
	}
	return event, nil
}

func validate(e Event) error {
	switch e.Kind {
	case Started, Stopped, AutoStarted, AskedFirst, Approved, Answered, Posted, Took, Dropped, Released, RequestChanges, PRMerged, PRClosed, StepFailed:
	default:
		return fmt.Errorf("factory record: unknown event kind %q", e.Kind)
	}
	if e.Commit != "" && !publish.ValidCommit(e.Commit) {
		return errors.New("factory record: approval requires a full commit SHA")
	}
	if e.Job <= 0 {
		return errors.New("factory record: job must be positive")
	}
	if e.Cost < 0 {
		return errors.New("factory record: cost must not be negative")
	}
	handle := factory.NormalizeHandle(e.Handle)
	if e.Kind == Approved && handle == "" {
		return errors.New("factory record: approval requires a forge handle")
	}
	for _, c := range handle {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return errors.New("factory record: invalid forge handle")
		}
	}
	return nil
}

func mint(events []Event) (string, error) {
	used := make(map[string]bool, len(events))
	for _, event := range events {
		used[event.ID] = true
	}
	for {
		var bits [4]byte
		if _, err := rand.Read(bits[:]); err != nil {
			return "", err
		}
		id := "evt_" + hex.EncodeToString(bits[:])
		if !used[id] {
			return id, nil
		}
	}
}

// Since returns events strictly after the supplied instant in append order.
func (r *Record) Since(ctx context.Context, since time.Time) ([]Event, error) {
	out := []Event{}
	err := r.locked(ctx, false, func() error {
		events, err := r.read(ctx)
		if err != nil {
			return err
		}
		for _, event := range events {
			if since.IsZero() || event.At.After(since) {
				out = append(out, event)
			}
		}
		return nil
	})
	return out, err
}

// Trailer reads the most recent consent for this job. A fresh run, a drop or
// requested changes withdraws it, so another job or an earlier run cannot attest.
func (r *Record) Trailer(ctx context.Context, id int) (string, error) {
	st, err := store.Open(r.root)
	if err != nil {
		return "", err
	}
	it, err := st.Get(id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	return r.TrailerFor(ctx, id, it.HeadCommit)
}

// TrailerFor refuses consent for a different commit, including older unbound
// receipts when the job now carries a head.
func (r *Record) TrailerFor(ctx context.Context, id int, commit string) (string, error) {
	events, err := r.Since(ctx, time.Time{})
	if err != nil {
		return "", err
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Job != id {
			continue
		}
		switch e.Kind {
		case Approved:
			if err := validate(e); err != nil {
				return "", err
			}
			if len(e.ID) != 12 || !strings.HasPrefix(e.ID, "evt_") {
				return "", errors.New("factory record: invalid approval id")
			}
			if _, err := hex.DecodeString(e.ID[4:]); err != nil {
				return "", err
			}
			if e.Commit != commit {
				return "", publish.ErrCommitChanged
			}
			trailer := "Factory-Approval: " + e.ID + " @" + factory.NormalizeHandle(e.Handle)
			if e.Commit != "" {
				trailer += " " + e.Commit
			}
			return trailer, nil
		case Started, AutoStarted, Dropped, RequestChanges:
			return "", errors.New("job requires a recorded approval before publishing")
		}
	}
	return "", errors.New("job requires a recorded approval before publishing")
}

func (r *Record) path() string { return filepath.Join(r.root, "events.jsonl") }

func (r *Record) locked(ctx context.Context, exclusive bool, action func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(r.root, "events.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := filelock.Lock(f, exclusive, false); err != nil {
		return err
	}
	defer func() { _ = filelock.Unlock(f) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return action()
}

// A malformed or unfinished tail refuses new writes: silently appending would
// erase the distinction between a complete decision and an interrupted write.
func (r *Record) read(ctx context.Context) ([]Event, error) {
	data, err := os.ReadFile(r.path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		return nil, errors.New("factory record: incomplete events.jsonl tail")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	var events []Event
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var event Event
		err := decoder.Decode(&event)
		if err == io.EOF {
			return events, nil
		}
		if err != nil {
			return nil, fmt.Errorf("factory record: events.jsonl: %w", err)
		}
		events = append(events, event)
	}
}
