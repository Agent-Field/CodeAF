package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// HostedRemoval records when a person was removed so a retry can leave later chats alone.
type HostedRemoval struct {
	Since time.Time `json:"since"`
}

const (
	hostedRemovalsFile = "hosted-removals.json"
	hostedRemovalsLock = "hosted-removals.lock"
)

// ErrHostedRemovalsUnreadable keeps damaged file contents out of errors and logs.
var ErrHostedRemovalsUnreadable = errors.New("the saved removals of hosted chats cannot be read")

// HostedRemovals returns the removals still owed after a restart. Before the
// first removal there is no file, so there is nothing to retry.
func (st *Store) HostedRemovals() (map[string]HostedRemoval, error) {
	raw, err := os.ReadFile(filepath.Join(st.root, hostedRemovalsFile))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]HostedRemoval{}, nil
	}
	if err != nil {
		return nil, err
	}
	// A pointer lets the reader distinguish an absent timestamp from a recorded zero time.
	var doc struct {
		Rows map[string]struct {
			Since *time.Time `json:"since"`
		} `json:"rows"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.Rows == nil {
		return nil, ErrHostedRemovalsUnreadable
	}
	rows := make(map[string]HostedRemoval, len(doc.Rows))
	for person, row := range doc.Rows {
		if row.Since == nil {
			return nil, ErrHostedRemovalsUnreadable
		}
		rows[person] = HostedRemoval{Since: *row.Since}
	}
	return rows, nil
}

// MarkHostedRemoval keeps the later cutoff because it also covers the chats
// that were owed at the earlier cutoff.
func (st *Store) MarkHostedRemoval(person string, since time.Time) error {
	return st.underLock(filepath.Join(st.root, hostedRemovalsLock), func() error {
		rows, err := st.HostedRemovals()
		if errors.Is(err, ErrHostedRemovalsUnreadable) {
			rows = map[string]HostedRemoval{}
		} else if err != nil {
			return err
		}
		if mark, ok := rows[person]; ok && !since.After(mark.Since) {
			return nil
		}
		rows[person] = HostedRemoval{Since: since}
		return st.writeHostedRemovals(rows)
	})
}

// ClearHostedRemoval clears only the cutoff that was finished because another
// removal may have included more chats while the host was being asked.
func (st *Store) ClearHostedRemoval(person string, since time.Time) error {
	return st.underLock(filepath.Join(st.root, hostedRemovalsLock), func() error {
		rows, err := st.HostedRemovals()
		if err != nil {
			return err
		}
		if mark, ok := rows[person]; !ok || !mark.Since.Equal(since) {
			return nil
		}
		delete(rows, person)
		return st.writeHostedRemovals(rows)
	})
}

func (st *Store) writeHostedRemovals(rows map[string]HostedRemoval) error {
	raw, err := json.Marshal(struct {
		Rows map[string]HostedRemoval `json:"rows"`
	}{Rows: rows})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(st.root, hostedRemovalsFile), append(raw, '\n'))
}
