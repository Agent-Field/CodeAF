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
	"time"
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
	// Scope is HOW MUCH of the project this reading covered — "whole", or the
	// count of files a reading scoped to the change selected — and Package is
	// WHERE it was taken, which for a monorepo is the package it read rather
	// than the workspace root.
	//
	// They are journaled because a roster of forty checks means two different
	// things and the row could not say which: a small project read whole, or a
	// large one read next to the change. The regression comparison turns on the
	// same distinction (verify.Reading.comparable), so an autopsy that cannot
	// see the scope cannot check the comparison either.
	Scope   string `json:"scope,omitempty"`
	Package string `json:"package,omitempty"`
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
	// Partial says the reading was CUT: the command was killed at its ceiling
	// having already named some of its checks, and Elapsed is how long it ran
	// before that happened.
	//
	// They are journaled because the pair is what an autopsy needs to tell a
	// small suite from a big one that was interrupted, and because Elapsed is
	// the only thing this run ever learns about the PACE of the machine it is
	// on. The budget's arithmetic assumes a native host; these readings are
	// taken in amd64 containers under qemu, where everything is five to ten
	// times slower, and a ceiling derived from a wall knows nothing about that
	// until a reading is cut and says so.
	Partial bool          `json:"partial,omitempty"`
	Elapsed time.Duration `json:"elapsed,omitempty"`
	// Uncollected says the runner produced no test record of its own — a suite
	// that failed to COLLECT rather than one that ran and went red — and Trouble
	// is what it said instead, in its own words.
	//
	// They are journaled because the two were the same row: ofetch's nemotron n1
	// run wrote `named: 1, red: 1` four times over a suite that never ran a
	// check, and an autopsy reading that row had no way to tell it from a suite
	// with one failing test in it.
	Uncollected bool   `json:"uncollected,omitempty"`
	Trouble     string `json:"trouble,omitempty"`
	// Replaced is how many of the checks this roster stopped naming were
	// REWRITTEN rather than removed: a check the after reading no longer holds
	// by name, whose subject a check it does hold still covers.
	//
	// It is journaled because the removal finding it suppresses is invisible
	// otherwise. happy-dom's v4-flash s13 raised `This work removed checks that
	// existed before it: IntersectionObserver observe() Does nothing, …` on
	// four consecutive rounds over four stubs the run had replaced with real
	// checks under the same describe path, and the store held nothing that
	// could tell that from a deletion. A mechanism that declines to convict
	// says so, or an autopsy cannot tell it from one that never ran
	// (FAILSAFE.md clause 4).
	Replaced int `json:"replaced,omitempty"`
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
	reading.Trouble = bounded(strings.TrimSpace(reading.Trouble), MaxDigestBytes)
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

// EventSurface is the symbol-level half of the photograph, journaled beside the
// check-level one.
//
// It is its own kind rather than a field on the reading because the two answer
// different questions from different evidence, and a run can have either without
// the other: a project that declares no verification still has a public surface,
// and a suite that ran fine still says nothing about a name it never touched.
// igel s11's check-level row said the tree got BETTER on the run that deleted
// eight public attributes.
const EventSurface EventKind = "surface"

// SurfaceReading is that comparison as the journal keeps it.
//
// A COUNT PLUS A BOUNDED SAMPLE, exactly as the check roster is kept, and for
// the identical reason: a repository's public surface is tens of thousands of
// short strings, what a reader wants is the difference, and a handful of names
// settles what SHAPE the loss has as well as four hundred would.
type SurfaceReading struct {
	// Compared is how many changed source files the two readings were compared
	// across. Zero with Lost zero is a real answer — the run changed no source
	// this program can read — and it is not the same answer as no row at all.
	Compared int `json:"compared"`
	// Lost is how many public names the finished tree no longer spells.
	Lost  int      `json:"lost"`
	Names []string `json:"names,omitempty"`
}

// RecordSurface journals one symbol-level comparison against a node.
//
// EVERY COMPARISON IS WRITTEN, including one that found nothing. "Sixteen files
// were compared and no public name was lost" and "nobody compared anything" are
// two facts, and the absence of the row was the only spelling either of them
// had.
func (s *Store) RecordSurface(nodeID string, reading SurfaceReading) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record surface: %w: empty node id", ErrInvalid)
	}
	if len(reading.Names) > VerificationSample {
		reading.Names = reading.Names[:VerificationSample]
	}
	for index, name := range reading.Names {
		reading.Names[index] = bounded(strings.TrimSpace(name), MaxDigestBytes)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record surface: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("record surface: %w", err)
	}
	if _, _, err := appendEvent(tx, nodeID, EventSurface, reading); err != nil {
		return fmt.Errorf("record surface: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record surface: %w", err)
	}
	return nil
}

// SurfacesFor returns every symbol-level comparison journaled for a node,
// oldest first.
func (s *Store) SurfacesFor(nodeID string) ([]SurfaceReading, error) {
	rows, err := s.db.Query(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq ASC`, nodeID, EventSurface)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("surfaces for %s: %w", nodeID, err)
	}
	defer rows.Close()
	var readings []SurfaceReading
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("surfaces for %s: %w", nodeID, err)
		}
		var reading SurfaceReading
		if err := json.Unmarshal(payload, &reading); err != nil {
			continue
		}
		readings = append(readings, reading)
	}
	return readings, rows.Err()
}

// EventConsumers is the third reading of the same tree, journaled beside the
// other two: the definitions this run's own diff touched, and what the rest of
// the project still does with them.
//
// It is its own kind for the reason EventSurface is. The check-level reading
// answers what a suite says, the surface reading answers whether a name is still
// there, and neither can see a definition that kept its name and changed its
// SHAPE — igel s12 rebound `configs` from a dict to an instance of a class it
// wrote, the surface row read `compared: 8, lost: 0`, and twenty-four hidden
// tests failed on `'Configs' object does not support item assignment`.
const EventConsumers EventKind = "consumers"

// ConsumersReading is that reading as the journal keeps it: one row per changed
// definition, with its consumers counted by shape.
//
// COUNTS PLUS A BOUNDED SAMPLE, the same shape SurfaceReading keeps and for the
// same reason: what a reader wants from a hundred usage sites is how many there
// are and what kind they are.
type ConsumersReading struct {
	// Weighed is how many changed definitions were read. Zero with no rows is a
	// real answer — the run touched no declaration this program can read — and
	// it is not the same answer as no row at all.
	Weighed int                 `json:"weighed"`
	Changed []ChangedDefinition `json:"changed,omitempty"`
}

// ChangedDefinition is one definition the run's diff overlapped, and what still
// uses it.
type ChangedDefinition struct {
	Name string `json:"name"`
	File string `json:"file,omitempty"`
	// Sites is how many usage sites were found outside the lines this run
	// changed, and Shapes is those sites grouped by what they DO with the name.
	Sites  int             `json:"sites"`
	Shapes []ConsumerShape `json:"shapes,omitempty"`
}

// ConsumerShape is one syntactic shape of use, how many sites have it, and a
// bounded sample of where they are.
type ConsumerShape struct {
	Shape  string   `json:"shape"`
	Count  int      `json:"count"`
	Sample []string `json:"sample,omitempty"`
}

// RecordConsumers journals one reading of the changed definitions against a node.
//
// EVERY READING IS WRITTEN, including one that found nothing, for the reason
// RecordSurface's is: "the diff overlapped no declaration" and "nobody looked"
// are two facts and the absence of the row is the only spelling either has.
func (s *Store) RecordConsumers(nodeID string, reading ConsumersReading) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record consumers: %w: empty node id", ErrInvalid)
	}
	if len(reading.Changed) > VerificationSample {
		reading.Changed = reading.Changed[:VerificationSample]
	}
	for index := range reading.Changed {
		reading.Changed[index].Name = bounded(strings.TrimSpace(reading.Changed[index].Name), MaxDigestBytes)
		reading.Changed[index].File = bounded(strings.TrimSpace(reading.Changed[index].File), MaxDigestBytes)
		if len(reading.Changed[index].Shapes) > VerificationSample {
			reading.Changed[index].Shapes = reading.Changed[index].Shapes[:VerificationSample]
		}
		for shape := range reading.Changed[index].Shapes {
			held := reading.Changed[index].Shapes[shape]
			held.Shape = bounded(strings.TrimSpace(held.Shape), MaxDigestBytes)
			if len(held.Sample) > VerificationSample {
				held.Sample = held.Sample[:VerificationSample]
			}
			for at, where := range held.Sample {
				held.Sample[at] = bounded(strings.TrimSpace(where), MaxDigestBytes)
			}
			reading.Changed[index].Shapes[shape] = held
		}
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record consumers: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("record consumers: %w", err)
	}
	if _, _, err := appendEvent(tx, nodeID, EventConsumers, reading); err != nil {
		return fmt.Errorf("record consumers: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record consumers: %w", err)
	}
	return nil
}

// ConsumersFor returns every changed-definition reading journaled for a node,
// oldest first.
func (s *Store) ConsumersFor(nodeID string) ([]ConsumersReading, error) {
	rows, err := s.db.Query(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq ASC`, nodeID, EventConsumers)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("consumers for %s: %w", nodeID, err)
	}
	defer rows.Close()
	var readings []ConsumersReading
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("consumers for %s: %w", nodeID, err)
		}
		var reading ConsumersReading
		if err := json.Unmarshal(payload, &reading); err != nil {
			continue
		}
		readings = append(readings, reading)
	}
	return readings, rows.Err()
}
