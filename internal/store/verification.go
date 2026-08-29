package store

// The verification reading, journaled.
//
// A fail-safe leaves a record that can be autopsied (FAILSAFE.md clause 4), and
// this one did not. The photograph of a project's own checks lived entirely in
// memory: the worker took it, the gate weighed it, and the run's journal held no
// row saying a reading had happened at all. Five graded runs of the 2026-08-29
// sweep were autopsied with no way to tell a project that declares no
// verification from a reading that ran and named nothing — which are the two
// opposite diagnoses, and the whole sweep turned on which of them it was.
//
// So the reading is an event. It says which command actually ran, how it was
// read, what it exited with, and how many identities it named — the four facts
// that separate "nobody looked", "the runner was asked the wrong way", "the
// suite is red" and "the suite is green".

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// EventVerification is one reading of a project's own checks, journaled against
// the node whose work it is a reading of.
const EventVerification EventKind = "verification"

// VerificationReading is that reading as the journal keeps it.
//
// The roster is kept as a COUNT plus a bounded sample rather than whole. The
// count is what every question an autopsy asks is actually about — did this
// reader name anything — and a suite with two thousand checks would otherwise
// write a megabyte into the journal on every round of every job.
type VerificationReading struct {
	// When says which half of the photograph this is: the tree before the
	// job's first change, or the tree as it was handed over.
	When string `json:"when"`
	// Command is what actually ran, which is not always what the project
	// declared — a lifecycle script that lints before it tests is read through
	// the runner underneath it. Declared keeps the project's own spelling.
	Command  string `json:"command"`
	Declared string `json:"declared,omitempty"`
	// Read says a reading EXISTS. False is the row this type was extended for:
	// a reading that was not taken is still an event, because "nobody looked"
	// and "this project declares no verification" and "the command was killed
	// at its ceiling" are three different facts that cost three different
	// amounts, and a run that journals none of them is a run whose autopsy
	// cannot tell them apart. Why says which, in one sentence.
	Read bool   `json:"read"`
	Why  string `json:"why,omitempty"`
	// Runner and Format are the strategy: which program was asked, and how its
	// answer was read. ReadAsPlain says the strategy's own reader found nothing
	// and the shared vocabulary read the same bytes instead.
	Runner      string `json:"runner,omitempty"`
	Format      string `json:"format,omitempty"`
	Source      string `json:"source,omitempty"`
	ReadAsPlain bool   `json:"read_as_plain,omitempty"`
	// Exit is the command's own status, and -1 is a command that never got far
	// enough to have one. TimedOut says the ceiling fired, which is an
	// INCOMPLETE OBSERVATION and not a red one.
	Exit     int  `json:"exit"`
	TimedOut bool `json:"timed_out,omitempty"`
	// Named is the size of the roster and Red the size of its failing half.
	Named int `json:"named"`
	Red   int `json:"red"`
	// Sample is a bounded handful of the identities, so an autopsy can see what
	// shape the names came out in — a file path means the reader read a
	// file-level summary, a test name means it read the checks.
	Sample []string `json:"sample,omitempty"`
	// Inherited says this reading was not taken here: it is the baseline this
	// job took before its first change, carried forward into a later round.
	Inherited bool `json:"inherited,omitempty"`
}

// VerificationSample bounds how many identities one journaled reading names.
//
// Eight is the same bound regressionsNamed spells for a finding and describeChecks
// spells for an outcome sentence, and for the same reason: a list of names is
// read to learn what SHAPE the names have, and eight settles that as well as
// eight hundred.
const VerificationSample = 8

// RecordVerification journals one reading, or one reading that could not be
// taken, against a node.
//
// A ROW THAT SAYS NOTHING IS THE ONLY ONE NOT WRITTEN. A reading naming neither
// a command nor a reason is the zero value — nobody called this — and a row for
// it would be one every reader has to learn to ignore. Everything else is
// written, including every refusal: the absence of the event used to be the only
// spelling of four different facts.
func (s *Store) RecordVerification(nodeID string, reading VerificationReading) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record verification: %w: empty node id", ErrInvalid)
	}
	reading.Command = bounded(strings.TrimSpace(reading.Command), MaxDigestBytes)
	reading.Declared = bounded(strings.TrimSpace(reading.Declared), MaxDigestBytes)
	reading.Why = bounded(strings.TrimSpace(reading.Why), MaxDigestBytes)
	if reading.Command == "" && reading.Why == "" {
		return nil
	}
	if len(reading.Sample) > VerificationSample {
		reading.Sample = reading.Sample[:VerificationSample]
	}
	for index, name := range reading.Sample {
		reading.Sample[index] = bounded(strings.TrimSpace(name), MaxDigestBytes)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record verification: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("record verification: %w", err)
	}
	if _, _, err := appendEvent(tx, nodeID, EventVerification, reading); err != nil {
		return fmt.Errorf("record verification: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record verification: %w", err)
	}
	return nil
}

// VerificationsFor returns every reading journaled for a node, oldest first. It
// reads the events directly, exactly as AcceptanceFor does and for the same
// reason: the payload is sparse, looked up by id, and has no query anyone would
// run across it.
func (s *Store) VerificationsFor(nodeID string) ([]VerificationReading, error) {
	rows, err := s.db.Query(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq ASC`, nodeID, EventVerification)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("read verification readings %q: %w", nodeID, err)
	}
	defer rows.Close()
	var readings []VerificationReading
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("read verification readings %q: %w", nodeID, err)
		}
		var reading VerificationReading
		if err := json.Unmarshal([]byte(payload), &reading); err != nil {
			continue
		}
		readings = append(readings, reading)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read verification readings %q: %w", nodeID, err)
	}
	return readings, nil
}
