package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// mailbox.go is how a window that does not run the floor's items reaches the
// process that does (internal/factory's local.go says why there is only one).
// It is two small files beside the items:
//
//   - <root>/asks.json, the asks a window posted and the owner has not taken
//     yet, in the order they were posted, with the number the next one gets;
//   - <root>/replies.json, the owner's answers, each carrying its ask's number,
//     for the window that is waiting on it.
//
// BOTH ARE WRITTEN TEMP + RENAME UNDER ONE FLOCK, the store's own discipline,
// so two windows posting at once never take the same number and the owner's
// drain never loses an ask posted while it read.
//
// A REPLY NOBODY COLLECTS DOES NOT PILE UP: one older than [replyKeep] is
// dropped the next time a reply is written, and a window collecting its reply
// takes it out. The owner clears both files when it starts ([Mailbox.Clear]),
// because an ask the previous owner never answered was already told
// [factory.ErrRunnerSilent] and must not be carried out late.

// Ask and Reply are internal/factory's, so the window's seam and this file
// speak one shape.
type (
	Ask   = factory.Ask
	Reply = factory.Reply
)

// replyKeep is how long an uncollected reply is kept.
const replyKeep = time.Minute

// mailboxPoll is how often [Mailbox.Wait] looks for its reply. A variable so a
// test can shorten it.
var mailboxPoll = 25 * time.Millisecond

type asksDoc struct {
	Schema int   `json:"schema"`
	Next   int   `json:"next"`
	Asks   []Ask `json:"asks,omitempty"`
}

type repliesDoc struct {
	Schema  int     `json:"schema"`
	Replies []Reply `json:"replies,omitempty"`
}

// Mailbox is the floor's mailbox in one store.
type Mailbox struct{ st *Store }

// Mailbox answers the store's mailbox, or nil for no store.
func (st *Store) Mailbox() *Mailbox {
	if st == nil {
		return nil
	}
	return &Mailbox{st: st}
}

// AsksPath is <root>/asks.json.
func (st *Store) AsksPath() string { return filepath.Join(st.root, "asks.json") }

// RepliesPath is <root>/replies.json.
func (st *Store) RepliesPath() string { return filepath.Join(st.root, "replies.json") }

func (st *Store) mailboxLockPath() string { return filepath.Join(st.root, "mailbox.lock") }

// Post appends ask, stamped with its number and the time when it carries
// none, and answers the number. NUMBERS ONLY EVER GROW, across a [Clear] too,
// so a window still waiting on an ask from before a clear can never collect
// somebody else's reply.
func (m *Mailbox) Post(ask Ask) (int, error) {
	if m == nil || m.st == nil {
		return 0, errors.New("factory store: no store")
	}
	seq := 0
	err := m.under(func(asks *asksDoc, _ *repliesDoc) (bool, bool) {
		asks.Next++
		seq = asks.Next
		ask.Seq = seq
		if ask.At.IsZero() {
			ask.At = m.st.now()
		}
		asks.Asks = append(asks.Asks, ask)
		return true, false
	})
	return seq, err
}

// Take drains the mailbox: every ask posted and not yet taken, oldest first.
func (m *Mailbox) Take() ([]Ask, error) {
	if m == nil || m.st == nil {
		return nil, nil
	}
	var taken []Ask
	err := m.under(func(asks *asksDoc, _ *repliesDoc) (bool, bool) {
		if len(asks.Asks) == 0 {
			return false, false
		}
		taken, asks.Asks = asks.Asks, nil
		return true, false
	})
	return taken, err
}

// Answer writes the reply to the ask numbered seq, and drops every reply older
// than [replyKeep] that nobody collected.
func (m *Mailbox) Answer(seq int, reply Reply) error {
	if m == nil || m.st == nil {
		return errors.New("factory store: no store")
	}
	now := m.st.now()
	reply.Seq = seq
	if reply.At.IsZero() {
		reply.At = now
	}
	return m.under(func(_ *asksDoc, replies *repliesDoc) (bool, bool) {
		kept := replies.Replies[:0]
		for _, r := range replies.Replies {
			if now.Sub(r.At) <= replyKeep && r.Seq != seq {
				kept = append(kept, r)
			}
		}
		replies.Replies = append(kept, reply)
		return false, true
	})
}

// Wait answers the reply to seq, taking it out of the mailbox, or
// [factory.ErrRunnerSilent] when none was written within timeout.
func (m *Mailbox) Wait(seq int, timeout time.Duration) (Reply, error) {
	if m == nil || m.st == nil {
		return Reply{}, errors.New("factory store: no store")
	}
	deadline := time.Now().Add(timeout)
	for {
		var (
			got   Reply
			found bool
		)
		err := m.under(func(_ *asksDoc, replies *repliesDoc) (bool, bool) {
			for i, r := range replies.Replies {
				if r.Seq == seq {
					got, found = r, true
					replies.Replies = append(replies.Replies[:i], replies.Replies[i+1:]...)
					return false, true
				}
			}
			return false, false
		})
		if err != nil {
			return Reply{}, err
		}
		if found {
			return got, nil
		}
		if !time.Now().Before(deadline) {
			return Reply{}, factory.ErrRunnerSilent
		}
		time.Sleep(min(mailboxPoll, time.Until(deadline)))
	}
}

// Clear empties both files, keeping the numbering. The owner calls it when it
// starts.
func (m *Mailbox) Clear() error {
	if m == nil || m.st == nil {
		return nil
	}
	return m.under(func(asks *asksDoc, replies *repliesDoc) (bool, bool) {
		asks.Asks, replies.Replies = nil, nil
		return true, true
	})
}

// under reads both documents under the mailbox's lock, lets change edit them,
// and writes back the ones change says it changed.
func (m *Mailbox) under(change func(*asksDoc, *repliesDoc) (asksChanged, repliesChanged bool)) error {
	st := m.st
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return err
	}
	return st.underLock(st.mailboxLockPath(), func() error {
		asks, err := readMailboxDoc(st.AsksPath(), func(d asksDoc) int { return d.Schema })
		if err != nil {
			return err
		}
		replies, err := readMailboxDoc(st.RepliesPath(), func(d repliesDoc) int { return d.Schema })
		if err != nil {
			return err
		}
		a, r := change(&asks, &replies)
		if a {
			asks.Schema = Schema
			if err := writeMailboxDoc(st.AsksPath(), asks); err != nil {
				return err
			}
		}
		if r {
			replies.Schema = Schema
			if err := writeMailboxDoc(st.RepliesPath(), replies); err != nil {
				return err
			}
		}
		return nil
	})
}

// readMailboxDoc reads one of the two files. A missing file is an empty
// one; A TORN FILE IS AN EMPTY ONE TOO, because refusing it would stop every
// window's verbs for good, and what it lost was at most a few seconds of asks
// whose windows have already said nobody answered.
func readMailboxDoc[T any](path string, schema func(T) int) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, nil
		}
		return zero, err
	}
	var d T
	if err := json.Unmarshal(data, &d); err != nil {
		return zero, nil
	}
	if schema(d) > Schema {
		return zero, fmt.Errorf("%w: %s", ErrNewer, filepath.Base(path))
	}
	return d, nil
}

func writeMailboxDoc(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'))
}
