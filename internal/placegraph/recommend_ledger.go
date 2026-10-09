package placegraph

// The recommendation ledger: what codeaf has offered, what the person said, how
// many model calls it has spent today, which chats it has already considered,
// and which places exist because the person accepted an offer.
//
// IT IS A SEPARATE FILE FROM THE GRAPH ON PURPOSE. The graph is the person's
// organisation and its revision is what undo is checked against; an offer being
// made or snoozed is not a change to their organisation, and writing it into the
// graph would invalidate their undo every time codeaf thought about something.
//
// A DAMAGED LEDGER IS SET ASIDE, NOT TRUSTED AND NOT DELETED. Starting empty
// loses snoozes and the call count, which at worst repeats an offer the person
// already declined; it can never move a chat, because nothing in this file
// does.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

const (
	ledgerVersion      = 1
	maxLedgerProposals = 500
	maxLedgerChats     = 20000
	maxLedgerBytes     = 8 << 20
)

type callMark struct {
	Role string    `json:"role"`
	At   time.Time `json:"at"`
}

type ledgerState struct {
	Version   int        `json:"version"`
	Proposals []Proposal `json:"proposals"`
	// Considered is every chat a filing offer has been weighed for, so a
	// chat gets at most one offer however many turns it has.
	Considered map[string]time.Time `json:"considered"`
	Calls      []callMark           `json:"calls"`
	// AIPlaces are the places created by accepting an offer. Only these count
	// against the AI caps; a person's own places never do.
	AIPlaces        []string  `json:"aiPlaces"`
	LastSuggestCall time.Time `json:"lastSuggestCall,omitzero"`
}

// Ledger is the handle. Safe across goroutines and processes on one path.
type Ledger struct {
	path     string
	mu       sync.Mutex
	recovery string
}

// OpenLedger prepares the ledger at path (conventionally beside the graph
// file). The directory is created if missing.
func OpenLedger(path string) (*Ledger, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: empty ledger path", ErrInvalid)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return &Ledger{path: path}, nil
}

// LastRecovery says what the last load did about a damaged file, or "".
func (l *Ledger) LastRecovery() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.recovery
}

func (l *Ledger) acquire() (func(), error) {
	l.mu.Lock()
	f, err := os.OpenFile(l.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		l.mu.Unlock()
		return nil, err
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := filelock.Lock(f, true, true)
		if err == nil {
			break
		}
		if !filelock.IsBusy(err) || time.Now().After(deadline) {
			f.Close()
			l.mu.Unlock()
			if filelock.IsBusy(err) {
				return nil, ErrLocked
			}
			return nil, err
		}
		time.Sleep(lockPoll)
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
		l.mu.Unlock()
	}, nil
}

func emptyLedger() *ledgerState {
	return &ledgerState{Version: ledgerVersion, Proposals: []Proposal{}, Considered: map[string]time.Time{}, Calls: []callMark{}, AIPlaces: []string{}}
}

func (l *Ledger) loadLocked(now time.Time) (*ledgerState, error) {
	f, err := os.Open(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyLedger(), nil
	}
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxLedgerBytes+1))
	f.Close()
	if err != nil {
		return nil, err
	}
	var st ledgerState
	reason := ""
	if len(data) > maxLedgerBytes {
		reason = "file too large"
	} else if err := json.NewDecoder(bytes.NewReader(data)).Decode(&st); err != nil {
		reason = "not valid JSON"
	} else if st.Version > ledgerVersion {
		return nil, fmt.Errorf("%w (ledger version %d)", ErrUnsupportedVersion, st.Version)
	}
	if reason != "" {
		aside := fmt.Sprintf("%s.corrupt-%s", l.path, now.UTC().Format("20060102T150405.000000000Z"))
		if err := os.Rename(l.path, aside); err != nil {
			return nil, fmt.Errorf("placegraph: cannot set aside damaged ledger (%s): %w", reason, err)
		}
		l.recovery = reason + "; the old copy is at " + aside
		return emptyLedger(), nil
	}
	if st.Considered == nil {
		st.Considered = map[string]time.Time{}
	}
	if st.Proposals == nil {
		st.Proposals = []Proposal{}
	}
	if st.AIPlaces == nil {
		st.AIPlaces = []string{}
	}
	return &st, nil
}

func (l *Ledger) writeLocked(st *ledgerState, now time.Time) error {
	st.prune(now)
	data, err := json.MarshalIndent(st, "", " ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(l.path)
	tmp, err := os.CreateTemp(dir, ".places-ai-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, l.path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// view runs fn on a fresh read without writing.
func (l *Ledger) view(now time.Time, fn func(*ledgerState) error) error {
	release, err := l.acquire()
	if err != nil {
		return err
	}
	defer release()
	st, err := l.loadLocked(now)
	if err != nil {
		return err
	}
	return fn(st)
}

// errUnchanged is what an update's fn returns to say it changed nothing, so
// nothing is written. update itself then reports success.
var errUnchanged = errors.New("unchanged")

// update runs fn on a fresh read and writes the result unless fn fails.
func (l *Ledger) update(now time.Time, fn func(*ledgerState) error) error {
	release, err := l.acquire()
	if err != nil {
		return err
	}
	defer release()
	st, err := l.loadLocked(now)
	if err != nil {
		return err
	}
	if err := fn(st); err != nil {
		if errors.Is(err, errUnchanged) {
			return nil
		}
		return err
	}
	return l.writeLocked(st, now)
}

// prune keeps the file bounded: calls older than a day go, decided proposals
// older than a year go (a snooze is at most a year), and past the caps the
// oldest go first. Pending proposals are never pruned.
func (st *ledgerState) prune(now time.Time) {
	calls := st.Calls[:0]
	for _, c := range st.Calls {
		if now.Sub(c.At) < 24*time.Hour {
			calls = append(calls, c)
		}
	}
	st.Calls = calls
	keep := st.Proposals[:0]
	for _, p := range st.Proposals {
		if p.Status == StatusPending || now.Sub(p.DecidedAt) < 366*24*time.Hour {
			keep = append(keep, p)
		}
	}
	st.Proposals = keep
	if over := len(st.Proposals) - maxLedgerProposals; over > 0 {
		sort.SliceStable(st.Proposals, func(i, j int) bool {
			pi, pj := st.Proposals[i].Status == StatusPending, st.Proposals[j].Status == StatusPending
			if pi != pj {
				return !pi
			}
			return st.Proposals[i].CreatedAt.Before(st.Proposals[j].CreatedAt)
		})
		st.Proposals = append([]Proposal(nil), st.Proposals[over:]...)
	}
	if over := len(st.Considered) - maxLedgerChats; over > 0 {
		type kv struct {
			id string
			at time.Time
		}
		all := make([]kv, 0, len(st.Considered))
		for id, at := range st.Considered {
			all = append(all, kv{id, at})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })
		for _, e := range all[:over] {
			delete(st.Considered, e.id)
		}
	}
}

func (st *ledgerState) callsSince(role string, since time.Time) int {
	n := 0
	for _, c := range st.Calls {
		if c.Role == role && c.At.After(since) {
			n++
		}
	}
	return n
}

func (st *ledgerState) find(id string) int {
	for i, p := range st.Proposals {
		if p.ID == id {
			return i
		}
	}
	return -1
}
