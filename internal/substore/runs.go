package substore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// RunNote is the whole of what this store keeps about a run: when, how it ended,
// and what it cost.
//
// IT IS NOT A RUN JOURNAL AND MUST NOT GROW INTO ONE. The journal is the
// runtime's — exec.JournalEntry, written per host call, kept by whoever is
// watching a run — and the door lane decides where a whole trace lands. What
// lives here is the one line a list row needs, and keeping it to that is what
// makes drawing forty rows forty small reads instead of forty directory scans.
//
// It renders NOTHING. The tui lane turns this into the string
// session.SubharnessRow.LastRun carries, because the emptiness law, the word for
// an unfinished run, and how a cost is drawn are all that lane's to decide and
// are all stated in one place there.
type RunNote struct {
	// At is when the run ended, UTC.
	At time.Time `json:"at"`
	// Version is which version ran. A row that says "yesterday" about a
	// subharness somebody has since re-minted is answering about the old program,
	// and this is what lets a surface say so.
	Version int `json:"version,omitempty"`
	// Finished is exec.RunResult.Finished, asked once at the moment it was true
	// and written down. It is a bool and not a word because the word is the tui's.
	Finished bool `json:"finished"`
	// Why is exec.RunResult.Incomplete — why it did not finish, in a person's
	// words — and empty when it did. It is carried verbatim: the sentence was
	// written by whoever knew what ran out, and this store is not entitled to
	// rephrase it.
	Why string `json:"why,omitempty"`
	// CostUSD is the run's ledger, folded to the one figure a row draws. The
	// whole exec.Spend is the journal's; this is the number.
	CostUSD float64 `json:"cost_usd,omitempty"`
}

// RecordRun writes the last-run note for one subharness, replacing whatever was
// there.
//
// ONE NOTE PER NAME, OVERWRITTEN. It is called "last run" and it holds the last
// run; a history would be a different feature with a different door, and
// building the history first and calling it this is how a small file becomes an
// unbounded one nobody pruned. The write is atomic (see [replace]) rather than
// exclusive, because unlike a version this file is MEANT to change — the
// exclusive create that protects a version page would fail on the second run of
// every subharness.
//
// A note for a name with no bundle is still written. That is deliberate: a
// Go-native subharness has no directory here until its first run, and the list
// row for one wants the same "when did I last run this" the stored ones get.
func (s *Store) RecordRun(name string, note RunNote) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("a run note needs the name of the subharness it is about")
	}
	if note.At.IsZero() {
		note.At = s.now()
	}
	note.At = note.At.UTC()
	data, err := json.MarshalIndent(note, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.nameDir(name), 0o755); err != nil {
		return err
	}
	return replace(s.lastRunPath(name), append(data, '\n'))
}

// LastRun answers the note, and false for a subharness nobody has run.
//
// FALSE IS THE ORDINARY ANSWER AND NOT A FAULT. A subharness nobody has run has
// no last run, its row draws nothing under the emptiness law, and a store that
// returned a zero note with a zero time would hand the surface a date in 1970 to
// render. An unreadable note is the same answer with a line in the journal: the
// list is not where somebody learns their disk is broken.
func (s *Store) LastRun(name string) (RunNote, bool) {
	data, err := os.ReadFile(s.lastRunPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return RunNote{}, false
	}
	if err != nil {
		s.notice("%s: its last-run note could not be read: %v", name, err)
		return RunNote{}, false
	}
	var note RunNote
	if err := json.Unmarshal(data, &note); err != nil {
		s.notice("%s: its last-run note is not readable: %v", name, err)
		return RunNote{}, false
	}
	if note.At.IsZero() {
		return RunNote{}, false
	}
	return note, true
}
