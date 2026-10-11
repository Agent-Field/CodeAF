package placeorg

import "time"

// The journal is the record of everything an organising run did and why. It
// is append-only JSONL beside the store (places-journal.jsonl) and it is NOT
// the undo: undo is placegraph's receipt for the one commit a Plan makes.
// The journal is what lets a person, or a test, read back every judgment
// the model made, every fallback that was taken and every op that was
// proposed, in order, with cost.

// EntryKind is what one journal line records.
type EntryKind string

const (
	EntryRunStarted EntryKind = "run.started"
	EntryRunEnded   EntryKind = "run.ended"
	EntryStage      EntryKind = "stage"
	EntryJudgment   EntryKind = "judgment"
	EntryFallback   EntryKind = "fallback"
	EntryOp         EntryKind = "op"
	EntryInvariant  EntryKind = "invariant"
	EntryCorrection EntryKind = "correction"
	EntrySteer      EntryKind = "steer"
	EntryFiled      EntryKind = "filed"
)

// Entry is one line. Only the fields the kind needs are set.
type Entry struct {
	At   time.Time `json:"at"`
	Run  string    `json:"run"`
	Kind EntryKind `json:"kind"`
	// Stage names the stage that wrote the line.
	Stage string `json:"stage,omitempty"`
	// Question, Options and Pick record a judgment in full.
	Question *Question `json:"question,omitempty"`
	Pick     *Pick     `json:"pick,omitempty"`
	Op       *Op       `json:"op,omitempty"`
	// Note is one line of prose for anything else.
	Note string `json:"note,omitempty"`
	// Spent is the running total at this line.
	Spent Spent `json:"spent"`
}

// Journal is the sink. The file-backed one lives in placeorg/journal; tests
// use an in-memory one. Write never blocks a run: a journal that cannot be
// written ends the run with an error before anything is applied, because a
// run whose record is lost is a run the person cannot read back.
type Journal interface {
	Write(e Entry) error
}
