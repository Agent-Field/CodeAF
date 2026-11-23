package appwire

import (
	"strings"
	"time"
)

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
	// Launch is the record's launch id: the identity of the process this task's
	// program is on, absent when none can be named. ReportFiles are the bare
	// names of the report files the program declares AND THAT LAUNCH ISSUED A
	// RECEIPT FOR when it wrote them; a declared name the launch did not write
	// (a failed rerun, a record from before receipts) is absent. Both are the
	// question tasks.program_report is asked with ({id, launch, name}); neither
	// is a grant, and a name from EndingFacts.Reports is not one of them.
	Launch      string   `json:"launch,omitempty"`
	ReportFiles []string `json:"report_files,omitempty"`
	// CeilingUSD is present only when the run was handed one.
	CeilingUSD float64 `json:"ceiling_usd,omitempty"`
	// Calls is how many of the record's process's calls reached a model,
	// PRESENT ONLY WHEN KNOWN: every one of its calls was read. A present 0 is a
	// process that made none; absent is unknown, never zero.
	Calls  *int     `json:"calls,omitempty"`
	Models []string `json:"models,omitempty"`
	Effort string   `json:"effort,omitempty"`
	// Listens is what the program's declaration says; Listening and InboxClosed
	// are its record's. Copied, never inferred.
	Listens     bool   `json:"listens"`
	Listening   bool   `json:"listening"`
	Started     bool   `json:"started"`
	InboxClosed string `json:"inbox_closed,omitempty"`
	// Read says what the actions and everything about where the process stands
	// were read from (one of the ProgramRead words), and ReadCut why a read is
	// partial (one of the ProgramCut words).
	Read    string `json:"read"`
	ReadCut string `json:"read_cut,omitempty"`
	// Actions are the newest actions the record's own process took, oldest
	// first; an earlier run's are never among them. Absent under a complete read
	// is a process that has shown no action yet; absent otherwise says nothing.
	Actions []ProgramAction `json:"actions,omitempty"`
	// EarlierActions is how many of the process's own actions came before the
	// first carried, and Before the cursor that reads them (tasks.program_actions);
	// both only under a complete read, Before only when there are some.
	EarlierActions int    `json:"earlier_actions,omitempty"`
	Before         string `json:"before,omitempty"`
	// Ending is the process's ending when its own last line is the ending.
	Ending *ProgramEnding `json:"ending,omitempty"`
}

// What a program's actions were read from (TaskProgram.Read).
const (
	// ProgramReadComplete: every line the record's process wrote; the actions,
	// earlier_actions, last_stage and ending are exact.
	ProgramReadComplete = "complete"
	// ProgramReadPartial: not all of them (read_cut says why). Actions are
	// carried only under "damaged", where the read went on to the launch's end;
	// a read stopped early carries none. There is no count, no last stage and
	// no ending, and no claim that there are none.
	ProgramReadPartial = "partial"
	// ProgramReadUnknown: no line can be tied to the record's process (a run
	// recorded before launches were, a relaunch before its hello). Nothing about
	// its work is carried.
	ProgramReadUnknown = "unknown"
)

// Why a program's read is partial (TaskProgram.ReadCut).
const (
	ProgramCutBudget     = "budget"
	ProgramCutDamaged    = "damaged"
	ProgramCutUnreadable = "unreadable"
	ProgramCutDeadline   = "deadline"
)

// ProgramActionsParams asks for the page of a program's actions before a
// cursor a read handed out.
type ProgramActionsParams struct {
	ID     string `json:"id"`
	Before string `json:"before"`
}

// ProgramActionsResult is one older page of a program's actions: its own read
// state, the actions oldest first, and the cursor of the page before it.
type ProgramActionsResult struct {
	Read           string          `json:"read"`
	ReadCut        string          `json:"read_cut,omitempty"`
	Actions        []ProgramAction `json:"actions,omitempty"`
	EarlierActions int             `json:"earlier_actions,omitempty"`
	Before         string          `json:"before,omitempty"`
}

// ProgramPager is the optional half of a Worker that pages a program's older
// actions: tasks.program_actions. An engine without it refuses the method as
// unknown.
type ProgramPager interface {
	// ProgramActions answers the page before the cursor before of the task's
	// program. A cursor of a launch that is no longer the task's is a conflict.
	ProgramActions(id, before string) (ProgramActionsResult, error)
}

func init() {
	methods["tasks.program_actions"] = typed(func(e Engine, p ProgramActionsParams) (*ProgramActionsResult, error) {
		pager, ok := e.(ProgramPager)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve tasks.program_actions")
		}
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Before) == "" {
			return nil, Errorf(CodeBadParams, "tasks.program_actions needs an id and a before")
		}
		page, err := pager.ProgramActions(p.ID, p.Before)
		if err != nil {
			return nil, err
		}
		return &page, nil
	})
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
	// NothingToAudit is true-only: an audit of the changes found no change to
	// audit, so its pass is not the pass of an audit that ran. Absent is not
	// "there was something to audit".
	NothingToAudit *bool `json:"nothing_to_audit,omitempty"`
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
