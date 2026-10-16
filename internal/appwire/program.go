package appwire

import "time"

// A SPECIALIST'S TASK, AS ITS PAGE READS IT. [TaskPage.Program] is present only
// for a task a program (senior-dev, sec, review) was handed, and carries what
// the engine already reads of it: the stages it said it would move through with
// the person's word for each, the newest actions it took, and the facts of its
// ending. Every field is a copy of a record; nothing is derived from a word.
//
// WHAT IS ABSENT IS UNKNOWN, NEVER ZERO. There is no attempt number, no "auto",
// no earlier run (the record is the latest process, not a history), no percent
// and no stage index: [TaskProgram.LastStage] is the program's own name for the
// newest stage it reported, and the reader finds it in [TaskProgram.Stages].
// Actions are the newest [ProgramActionsMax] and [TaskProgram.EarlierActions]
// says how many came before, so a short list is never taken for the history.

// ProgramActionsMax is the most actions one page carries.
const ProgramActionsMax = 60

// TaskProgram is the program a task was handed to.
type TaskProgram struct {
	Name string `json:"name"`
	// StartedAt and EndedAt are the latest process's clock on this task.
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	// Stages are the stages the program's hello named, in its order. The reader
	// groups consecutive equal words; the count is the program's own.
	Stages    []ProgramStage `json:"stages,omitempty"`
	LastStage string         `json:"last_stage,omitempty"`
	// CeilingUSD is present only when the run was handed one.
	CeilingUSD float64  `json:"ceiling_usd,omitempty"`
	Calls      int      `json:"calls,omitempty"`
	Models     []string `json:"models,omitempty"`
	Effort     string   `json:"effort,omitempty"`
	// Listens is what the program's declaration says; Listening and InboxClosed
	// are its record's. Copied, never inferred.
	Listens     bool   `json:"listens"`
	Listening   bool   `json:"listening"`
	Started     bool   `json:"started"`
	InboxClosed string `json:"inbox_closed,omitempty"`
	// Actions are the newest actions the program took, oldest first.
	Actions        []ProgramAction `json:"actions"`
	EarlierActions int             `json:"earlier_actions,omitempty"`
	// Ending is the program's ending when its log's last line is the ending.
	Ending *ProgramEnding `json:"ending,omitempty"`
}

// ProgramStage is one stage of the program's hello and the word a person reads
// for it. An empty word is a stage with no word and none standing before it.
type ProgramStage struct {
	Name string `json:"name"`
	Word string `json:"word,omitempty"`
}

// ProgramAction is one action the program took, in its own vocabulary.
type ProgramAction struct {
	At      time.Time `json:"at"`
	Step    string    `json:"step,omitempty"`
	Text    string    `json:"text"`
	Outcome string    `json:"outcome,omitempty"`
	Added   *int      `json:"added,omitempty"`
	Removed *int      `json:"removed,omitempty"`
	Steer   bool      `json:"steer,omitempty"`
	Memory  bool      `json:"memory,omitempty"`
	Model   string    `json:"model,omitempty"`
	Reason  string    `json:"reason,omitempty"`
}

// ProgramEnding is how the program ended. Status is the line's own word.
type ProgramEnding struct {
	At      time.Time    `json:"at"`
	Status  string       `json:"status,omitempty"`
	Text    string       `json:"text"`
	Outcome string       `json:"outcome,omitempty"`
	Facts   *EndingFacts `json:"facts,omitempty"`
}

// EndingFacts are the typed facts of an ending: an allow-list the engine
// bounded and checked. A count nobody said is absent, not 0. Reports are the
// names the program wrote, for display only: not a file handle, and not
// something the renderer may open. InnerStatus is the program's word on its own
// work and is not the run's status.
type EndingFacts struct {
	Claim                string        `json:"claim,omitempty"`
	Observed             string        `json:"observed,omitempty"`
	Submitted            *bool         `json:"submitted,omitempty"`
	SubmissionReason     string        `json:"submission_reason,omitempty"`
	InnerStatus          string        `json:"status,omitempty"`
	VerificationFailing  *int          `json:"verification_failing,omitempty"`
	VerificationCommands *int          `json:"verification_commands,omitempty"`
	Reports              []string      `json:"reports,omitempty"`
	Findings             *int          `json:"findings,omitempty"`
	Blocking             *int          `json:"blocking,omitempty"`
	Confirmed            *int          `json:"confirmed,omitempty"`
	Likely               *int          `json:"likely,omitempty"`
	Inconclusive         *int          `json:"inconclusive,omitempty"`
	RuledOut             *int          `json:"ruled_out,omitempty"`
	Partial              *bool         `json:"partial,omitempty"`
	AnalysisIncomplete   string        `json:"analysis_incomplete,omitempty"`
	HuntCoverage         *HuntCoverage `json:"hunt_coverage,omitempty"`
}

// HuntCoverage is an audit's account of the hunts it attempted and the ones
// that failed.
type HuntCoverage struct {
	Attempted *int          `json:"attempted,omitempty"`
	Failures  []HuntFailure `json:"failures,omitempty"`
	Unknown   bool          `json:"unknown,omitempty"`
}

// HuntFailure is one hunt that failed and the reason it gave.
type HuntFailure struct {
	Strategy string `json:"strategy"`
	Reason   string `json:"reason"`
}
