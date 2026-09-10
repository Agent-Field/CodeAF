package standing

// occurrence.go is the CAUSE half of a run folder: what admitted one firing,
// written BEFORE the runner starts, and completed with what it came to after.
//
// WHY IT IS A FILE IN THE RUN FOLDER AND NOT A NEW STORE. The run folder is
// already this package's record of one firing — the transcript, the came-to
// word, the ledger row naming it — so the fact of what started it belongs
// beside those, owned by the same owner. A second audit database would be a
// second truth about work this package already owns (the architecture's "no
// generic audit store" rule, docs/design/workspace-foundation).
//
// WHY BEFORE THE RUN. A process can die in the middle of a firing: the laptop
// sleeps, the timer's pass is killed, the person pulls the plug. The item's own
// document is only updated when the firing is recorded, so after a crash the
// same occurrence is due again and fires again — which is the RIGHT answer,
// because the change it was woken for has still not been reported. What was
// missing is the link: the retry did not know an earlier attempt had started,
// and the half-finished folder did not say what started it. The record written
// here is both halves. A later attempt from the SAME item state names the
// interrupted folders it supersedes, and those folders are marked as
// interrupted rather than left looking like work in progress forever.
//
// IT RECORDS OBSERVATIONS AND NEVER EXPLANATIONS. Which revision of the item
// was consumed, what woke it, which files changed, what the run came to and
// what was published — each is a fact the owner observed. Nothing here is a
// model's account of why it did something.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// OccurrenceFile is the record's name inside a run folder.
const OccurrenceFile = "occurrence.json"

// The phases an occurrence record passes through. Admitted is written before
// the runner starts; finished when the firing is recorded; interrupted when a
// later pass finds an admitted record whose process can no longer finish it.
const (
	PhaseAdmitted    = "admitted"
	PhaseFinished    = "finished"
	PhaseInterrupted = "interrupted"
)

// Occurrence is one admitted firing of an item that runs work.
type Occurrence struct {
	// ID is "<item id>/<run folder name>", stable for the life of the folder.
	ID     string `json:"id"`
	ItemID string `json:"item"`
	// Key names the item state the occurrence was admitted FROM: the due moment
	// of a rhythm, or the previous file reading of a watch. Two attempts with
	// one key are the same occurrence tried twice.
	Key string `json:"key"`
	// Spec is the item's [Item.SpecRevision] this firing ran on — which version
	// of the person's instructions — and Revision the document revision it was
	// admitted against. Words, Brief and Report are the effective configuration
	// at that version, retained here because later edits rewrite the document.
	Spec     uint64 `json:"spec"`
	Revision uint64 `json:"revision"`
	Words    string `json:"words"`
	Brief    string `json:"brief,omitempty"`
	Report   string `json:"report,omitempty"`
	// Trigger is what woke it: the item's own When at that revision.
	Trigger When `json:"trigger"`
	// Because is the one plain line the look said.
	Because string `json:"because,omitempty"`
	// Due is the scheduled moment for a rhythm; zero for a watch.
	Due time.Time `json:"due,omitempty"`
	// Changes are the files a watch saw change since its previous reading.
	// ChangesUnknown is set when the previous reading could not be read, so an
	// empty list is never mistaken for "nothing changed".
	Changes        []Change `json:"changes,omitempty"`
	ChangesUnknown bool     `json:"changesUnknown,omitempty"`
	// Reading is the watch's file reading this occurrence was admitted with,
	// which is what the item moves to once the occurrence is recorded.
	Reading string `json:"reading,omitempty"`
	// PreviousRun and PreviousFired are the item's last recorded firing before
	// this one, which is the "since when" a report is written against.
	PreviousRun   string    `json:"previousRun,omitempty"`
	PreviousFired time.Time `json:"previousFired,omitempty"`

	Admitted time.Time `json:"admitted"`
	PID      int       `json:"pid"`
	// Attempt counts tries of this Key; Supersedes lists the run folders of
	// earlier attempts that were interrupted before they finished.
	Attempt    int      `json:"attempt"`
	Supersedes []string `json:"supersedes,omitempty"`

	Phase        string       `json:"phase"`
	Finished     time.Time    `json:"finished,omitempty"`
	Outcome      string       `json:"outcome,omitempty"`
	OutcomeText  string       `json:"outcomeText,omitempty"`
	USD          float64      `json:"usd,omitempty"`
	Published    *Publication `json:"published,omitempty"`
	Error        string       `json:"error,omitempty"`
	SupersededBy string       `json:"supersededBy,omitempty"`

	// RunDir is where the record was read from. It is filled by the reader and
	// never written, because a folder is its own address.
	RunDir string `json:"-"`
}

// Publication is the owner's receipt for a report it wrote: where, which
// bytes, and when. A firing that published nothing has none.
type Publication struct {
	Path   string    `json:"path"`
	SHA256 string    `json:"sha256"`
	Bytes  int       `json:"bytes"`
	At     time.Time `json:"at"`
}

// Change is one file a watch saw change between two readings.
type Change struct {
	Path string `json:"path"`
	// Kind is added, modified or removed. Modified means its size or its
	// modification time moved; the reading does not hash contents.
	Kind string `json:"kind"`
}

// WriteOccurrence replaces a run folder's record atomically.
func WriteOccurrence(runDir string, occurrence Occurrence) error {
	if strings.TrimSpace(runDir) == "" {
		return errors.New("standing: an occurrence needs its run folder")
	}
	data, err := json.MarshalIndent(occurrence, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(runDir, OccurrenceFile), append(data, '\n'))
}

// ReadOccurrence reads one run folder's record. A folder written before these
// records existed has none, and says so with [os.ErrNotExist].
func ReadOccurrence(runDir string) (Occurrence, error) {
	raw, err := os.ReadFile(filepath.Join(runDir, OccurrenceFile))
	if err != nil {
		return Occurrence{}, err
	}
	var occurrence Occurrence
	if err := json.Unmarshal(raw, &occurrence); err != nil {
		return Occurrence{}, err
	}
	occurrence.RunDir = runDir
	return occurrence, nil
}

// Occurrences reads an item's run records, newest first, at most limit of them
// (zero is all). Folders without a record are skipped, not invented.
func (s *Store) Occurrences(id string, limit int) ([]Occurrence, error) {
	if err := checkID(id); err != nil {
		return nil, ErrNotFound
	}
	entries, err := os.ReadDir(s.RunsDir(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	// The folders are zero-padded numbers (newRunDir), so a reverse string
	// sort is newest first.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	var out []Occurrence
	for _, name := range names {
		occurrence, err := ReadOccurrence(filepath.Join(s.RunsDir(id), name))
		if err != nil {
			continue
		}
		out = append(out, occurrence)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

// occurrenceKey is the item state a firing is admitted from. It is read off
// the document as it was BEFORE the look moved it on, because that is the
// state a crashed attempt leaves behind and the retry finds again.
func occurrenceKey(before Item, now time.Time) string {
	switch before.When.Kind {
	case WhenEvery:
		return "due " + before.NextDue.UTC().Format(time.RFC3339)
	case WhenFile:
		return "files " + before.Fingerprint
	case WhenAt:
		return "at " + before.When.At.UTC().Format(time.RFC3339)
	}
	// A probe or an idle item has no durable before-state that a retry would
	// find again, so each check is its own occurrence.
	return "checked " + now.UTC().Format(time.RFC3339Nano)
}

// finishedOccurrence answers a finished record admitted from this same item
// state. Finding one means the process recorded the occurrence's end but
// stopped before the item's document said so; running it again would deliver
// and publish the same occurrence twice.
func (s *Store) finishedOccurrence(id, key string) (Occurrence, bool) {
	records, err := s.Occurrences(id, 64)
	if err != nil {
		return Occurrence{}, false
	}
	for _, record := range records {
		if record.Key == key && record.Phase == PhaseFinished && record.Error == "" {
			return record, true
		}
	}
	return Occurrence{}, false
}

// interruptedAttempts marks every admitted record of this item that can no
// longer finish as interrupted, and answers the run folders whose key matches
// the occurrence about to be tried, with the highest attempt number among them.
//
// IT RUNS UNDER THE TICK LOCK, so no other pass can be mid-firing: an admitted
// record found here belongs to a process that stopped before recording it. The
// liveness check is the belt-and-braces half, for a process id this one holds.
func (s *Store) interruptedAttempts(id, key, supersededBy string) ([]string, int) {
	records, err := s.Occurrences(id, 64)
	if err != nil {
		return nil, 0
	}
	var matched []string
	highest := 0
	for _, record := range records {
		if record.Phase != PhaseAdmitted {
			if record.Key == key && record.Attempt > highest {
				highest = record.Attempt
			}
			continue
		}
		if record.PID == os.Getpid() || (record.PID > 0 && pidAlive(record.PID) && s.now().Sub(record.Admitted) <= TickWindow) {
			continue
		}
		record.Phase = PhaseInterrupted
		if record.Key == key {
			matched = append(matched, record.RunDir)
			record.SupersededBy = supersededBy
			if record.Attempt > highest {
				highest = record.Attempt
			}
		}
		_ = WriteOccurrence(record.RunDir, record)
	}
	sort.Strings(matched)
	return matched, highest
}
