package factory

import "time"

// RunReport is a worker's incremental write, not a replacement item. Keeping the
// identity and pipeline out of it preserves changes a person made meanwhile.
type RunReport struct {
	FollowUp   *PRFollowUp
	Change     *Change
	ChangeBase string
	HeadCommit string
	Publishing *Publishing
	// Pipeline is the job's steps after the worker's runner reshaped them (the
	// manager's first turn, or a plan's edit, both bounded by the recipe). Nil
	// is no change. It lands before Stages, which may name a new step.
	Pipeline     *PipelineChange
	Stages       []StageReport
	Log          []LogLine
	Cost         float64
	Claims       []Claim
	Question     *Asked
	QuestionKind string
	PR           *PullRequest
	Publish      string
	CommentURL   string
	// Talk is the job's manager conversation, once the worker made it.
	Talk string
	// Notes are sentences the run added to the job's notes, oldest first.
	Notes []string
	// Thread carries spoken lines; Delivered acknowledges pending reply IDs.
	Thread    []Message
	Delivered []string
	// Room is the item team's id the worker's factory-owned stage wrapper
	// recorded on its mirror (Stream.Room). The server keeps the first one it
	// is given and refuses a different one; "" is no change.
	Room string
	// Merge is what the runner's merge did, nil when it carried none out.
	Merge *MergeReport
}

// PipelineChange is the job's stages and the run's phase list as the worker's
// runner left them. A phase already on the job keeps its state on the server;
// only the shape is taken from the report.
type PipelineChange struct {
	Stages []Stage
	Phases []Phase
}

// StageReport addresses a pipeline step by name, so a report cannot invent a
// new step or depend on an index that a person changed between reports.
type StageReport struct {
	Name  string
	Chat  string
	State PhaseState
	Round int
	// Started is when the step's latest round began on the worker
	// ([Phase.Started]); zero leaves the factory's as it is.
	Started time.Time
	Result  *StageResult
	Rounds  []RoundLine
	Failure *Failure
}
