package draft

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Store is one build's draft folder.
type Store struct{ dir string }

// Open creates the folder and an empty journal when they are absent.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("draft: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, JournalFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("draft: %w", err)
	}
	return &Store{dir: dir}, f.Close()
}

func (s *Store) Dir() string { return s.dir }

// Append adds one entry: lock, fold, parse and check against the fold, then
// write one line. A refusal leaves the journal unchanged and its text is the
// exact line the CLI prints.
func (s *Store) Append(a Author, verb string, args []string, body string) (Entry, error) {
	v, ok := registry[verb]
	if !ok || v.Parse == nil || v.Check == nil || (v.Internal && a != AuthorCodeaf) {
		return Entry{}, usage("unknown verb %q", verb)
	}
	e, err := v.Parse(args, strings.NewReader(body))
	if err != nil {
		return Entry{}, err
	}
	unlock, err := s.lock()
	if err != nil {
		return Entry{}, err
	}
	defer unlock()
	entries, err := s.readEntries(0)
	if err != nil {
		return Entry{}, err
	}
	st := Fold(entries)
	e.Author = a
	if err := v.Check(st, e); err != nil {
		return Entry{}, err
	}
	applyEntry(&st, e)
	e.Line = v.Describe(st, e)
	e.Seq = len(entries) + 1
	if n := len(entries); n > 0 {
		e.Seq = entries[n-1].Seq + 1
	}
	e.At = time.Now().UTC().Format(time.RFC3339)
	return e, s.write(e)
}

func (s *Store) write(e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path(JournalFile), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	// One Write call keeps the line whole for a reader that does not lock.
	_, werr := f.Write(append(line, '\n'))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// Entries returns the entries with seq greater than since.
func (s *Store) Entries(since int) ([]Entry, error) { return s.readEntries(since) }

// State is the fold of the whole journal.
func (s *Store) State() (State, error) {
	entries, err := s.readEntries(0)
	return Fold(entries), err
}

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

// lock takes the exclusive flock that serialises every writer of this draft.
func (s *Store) lock() (unlock func(), err error) {
	f, err := os.OpenFile(s.path(LockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("draft: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("draft: lock: %w", err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

// readEntries reads complete lines only: a final line with no newline is a
// write still in flight, and the next read will see it whole.
func (s *Store) readEntries(since int) ([]Entry, error) {
	f, err := os.Open(s.path(JournalFile))
	if err != nil {
		return nil, fmt.Errorf("draft: %w", err)
	}
	defer f.Close()
	var out []Entry
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		var e Entry
		if json.Unmarshal(bytes.TrimSpace(line), &e) != nil {
			return out, fmt.Errorf("draft: journal line %d is not valid", len(out)+1)
		}
		if e.Seq > since {
			out = append(out, e)
		}
	}
}
