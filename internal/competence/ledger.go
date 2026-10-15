// Package competence keeps and reads what each model has PROVEN: the checked
// results of its work, and the undos a person made of it.
//
// ── THE LAW ──
//
// A CHECKED RESULT IS A CHECK THAT RAN. One finished piece of work counts only
// when the verification it declared — its tests, its lint, its build — was
// executed against it and a real exit status came back. A pass is every one of
// those exiting zero; a failure is one of them exiting otherwise. Nothing else
// is a result here: not a checker's verdict, not "done", not a model's account
// of its own work, not a calibration or a rating. The map this package draws
// cannot be argued with by a model, because no model's words reach it.
//
// AN UNDO COUNTS AGAINST. A person taking a change back is the plainest reading
// of it there is, and it is recorded beside the result it reverses rather than
// replacing it, so "passed, then undone" stays both of those facts.
//
// ── WHY A LEDGER OF ITS OWN ──
//
// Nothing written before this file existed carries model, scope and a real
// check outcome together: the project index's verdict basis was never filled,
// the checker's verdict is a model's judgement, and per-task check readings
// lived in memory or in free-text job logs. So history is NO EVIDENCE rather
// than evidence inferred, and the ledger starts empty.
//
// It is one append-only JSONL file under codeaf's home, beside the usage
// ledger, and it is NOT a projection of the task records: deleting a task or a
// whole conversation removes those and leaves this, because what a model proved
// on a Tuesday does not stop being true when the person tidies their list. A
// reader says the source is gone; it never drops the result.
package competence

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// LedgerName is the file, under `<home>/v3/`.
const LedgerName = "checked-results.jsonl"

// LedgerPath is where the ledger lives for this process's home.
func LedgerPath() string { return home.Join("v3", LedgerName) }

// The two kinds of row.
const (
	// KindResult is a piece of work's checks, as they ran. The latest result
	// row for an id is the one that stands: a reopened task settles again.
	KindResult = "result"
	// KindUndo is a person undoing that work. It marks the result with the same
	// id, or stands alone as evidence against when the work had no checks.
	KindUndo = "undo"
)

// Outcome is how one check, or one piece of work, came out.
type Outcome string

const (
	Passed Outcome = "passed"
	Failed Outcome = "failed"
	Undone Outcome = "undone"
)

// The sources a result can come from.
const (
	SourceTask         = "task"
	SourceConversation = "conversation"
)

// The kinds of place work happens in.
const (
	PlaceRepo   = "repo"
	PlaceFolder = "folder"
)

// The bounds one row is held to, so one row never becomes a transcript.
//
// AN IDENTITY THAT DOES NOT FIT IS REFUSED, NEVER CUT. Two long model ids or
// paths cut to the same prefix would be two things counted as one, so the
// fields that say WHOSE result this is and WHERE are refused whole when they
// are too long. Only the words a person reads — a title, a command — are cut.
const (
	// MaxChecks is how many checks one row lists. The row's outcome is decided
	// over every check that ran BEFORE the list is cut, and the check that
	// decided a failure is always one of those kept.
	MaxChecks  = 8
	maxID      = 256
	maxModel   = 256
	maxWork    = 256
	maxAgent   = 128
	maxTask    = 64
	maxPath    = 4096
	maxTitle   = 240
	maxCommand = 400
	// recordVersion is the row format, written on every row.
	recordVersion = 1
)

// CheckRun is one declared check, run, and what its exit said.
type CheckRun struct {
	Command string  `json:"command"`
	Outcome Outcome `json:"outcome"`
}

// Source is where a result came from: a task in a conversation, or (reserved)
// a conversation's own turn. Transcript is the owning conversation's journal
// path, kept so a reader can tell later whether the source is gone; it is
// never shown.
type Source struct {
	Kind       string `json:"kind"`
	Agent      string `json:"agent"`
	Task       string `json:"task,omitempty"`
	Turn       int    `json:"turn,omitempty"`
	Transcript string `json:"transcript,omitempty"`
}

// Record is one row of the ledger.
type Record struct {
	V     int       `json:"v"`
	Kind  string    `json:"kind"`
	ID    string    `json:"id"`
	At    time.Time `json:"at"`
	Model string    `json:"model"`
	Place string    `json:"place"`
	// PlaceKind is "repo" or "folder", read off the disk when the row was written.
	PlaceKind string `json:"place_kind"`
	// Work is the kind of work ([WorkOf] of the title).
	Work   string `json:"work"`
	Title  string `json:"title"`
	Source Source `json:"source"`
	// Outcome is the result's whole outcome, decided over every check that
	// ran: failed when any did, passed when all did. Ran is how many distinct
	// checks that was; Checks lists at most [MaxChecks] of them.
	Outcome Outcome    `json:"outcome,omitempty"`
	Ran     int        `json:"ran,omitempty"`
	Checks  []CheckRun `json:"checks,omitempty"`
}

// TaskID is the ledger id of one task in one conversation. Ids restart with
// every conversation, so the conversation is part of the identity.
func TaskID(agent, task string) string { return "task:" + agent + ":" + task }

// ErrIncomplete is a row that names no model, place or work. Such a row is
// refused at the door rather than written for a reader to guess about.
var ErrIncomplete = errors.New("a checked result names its model, its place and its work")

// Append writes one row. It is one write of one whole line with O_APPEND, so
// two processes writing at once interleave rows and never bytes.
func Append(path string, record Record) error {
	record, err := normalized(record)
	if err != nil {
		return err
	}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// normalized is a row held to the ledger's own rules: complete, bounded,
// unambiguous, with no check that is not a pass or a failure and no secret in
// a command.
func normalized(record Record) (Record, error) {
	record.V = recordVersion
	record.ID = strings.TrimSpace(record.ID)
	record.Model = strings.TrimSpace(record.Model)
	record.Place = strings.TrimSpace(record.Place)
	record.Work = strings.TrimSpace(record.Work)
	record.Title = clipString(redact.Secrets(strings.TrimSpace(record.Title)), maxTitle)
	record.Source.Agent = strings.TrimSpace(record.Source.Agent)
	record.Source.Task = strings.TrimSpace(record.Source.Task)
	record.Source.Transcript = strings.TrimSpace(record.Source.Transcript)
	if record.Model == "" || record.Place == "" || record.Work == "" || record.ID == "" ||
		record.Source.Agent == "" || record.At.IsZero() {
		return Record{}, ErrIncomplete
	}
	for _, field := range []struct {
		name, value string
		limit       int
	}{
		{"id", record.ID, maxID}, {"model", record.Model, maxModel}, {"work", record.Work, maxWork},
		{"agent", record.Source.Agent, maxAgent}, {"task", record.Source.Task, maxTask},
		{"place", record.Place, maxPath}, {"transcript", record.Source.Transcript, maxPath},
	} {
		if len(field.value) > field.limit || strings.ContainsAny(field.value, "\x00\n\r") {
			return Record{}, fmt.Errorf("a checked result's %s is not one bounded line", field.name)
		}
	}
	if !filepath.IsAbs(record.Place) {
		return Record{}, fmt.Errorf("a checked result's place is an absolute path, not %q", record.Place)
	}
	record.Place = filepath.Clean(record.Place)
	switch record.Source.Kind {
	case SourceTask, SourceConversation:
	default:
		return Record{}, fmt.Errorf("a checked result's source is a task or a conversation, not %q", record.Source.Kind)
	}
	if record.PlaceKind != PlaceRepo {
		record.PlaceKind = PlaceFolder
	}
	record.At = record.At.UTC()
	switch record.Kind {
	case KindResult:
		checks := lastRunOfEach(record.Checks)
		// A RESULT WITH NO CHECK IS NOT A CHECKED RESULT, and it is refused here
		// so no reader ever has to decide what one would mean.
		if len(checks) == 0 {
			return Record{}, errors.New("a checked result has at least one check that ran")
		}
		record.Outcome, record.Ran = Passed, len(checks)
		for _, check := range checks {
			if check.Outcome == Failed {
				record.Outcome = Failed
				break
			}
		}
		record.Checks = shownChecks(checks)
	case KindUndo:
		record.Outcome, record.Ran, record.Checks = "", 0, nil
	default:
		return Record{}, fmt.Errorf("a ledger row is a result or an undo, not %q", record.Kind)
	}
	return record, nil
}

// lastRunOfEach is every distinct check, in the order each was first run, with
// the outcome of its LAST run: a check run again is a later fact about it.
// Commands are redacted before they are compared, so a secret never reaches
// the file and two runs differing only in a secret are one check.
func lastRunOfEach(runs []CheckRun) []CheckRun {
	out := make([]CheckRun, 0, len(runs))
	at := make(map[string]int, len(runs))
	for _, run := range runs {
		command := clipString(redact.Secrets(strings.Join(strings.Fields(run.Command), " ")), maxCommand)
		if command == "" || run.Outcome != Passed && run.Outcome != Failed {
			continue
		}
		if index, seen := at[command]; seen {
			out[index].Outcome = run.Outcome
			continue
		}
		at[command] = len(out)
		out = append(out, CheckRun{Command: command, Outcome: run.Outcome})
	}
	return out
}

// shownChecks is at most [MaxChecks] of the checks, in the order they ran, and
// ALWAYS the first that failed when one did: the list is cut for size, never
// for news. A decider past the cut takes the last place, which keeps the order
// because it ran after every check before it.
func shownChecks(checks []CheckRun) []CheckRun {
	if len(checks) <= MaxChecks {
		return checks
	}
	shown := append([]CheckRun(nil), checks[:MaxChecks]...)
	for index, check := range checks {
		if check.Outcome != Failed {
			continue
		}
		if index >= MaxChecks {
			shown[MaxChecks-1] = check
		}
		break
	}
	return shown
}

// clipString cuts s to at most limit bytes on a rune boundary.
func clipString(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
