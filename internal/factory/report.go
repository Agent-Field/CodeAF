package factory

// RunReport is a worker's incremental write, not a replacement item. Keeping the
// identity and pipeline out of it preserves changes a person made meanwhile.
type RunReport struct {
	Stages       []StageReport
	Log          []LogLine
	Cost         float64
	Claims       []Claim
	Question     *Asked
	QuestionKind string
	PR           *PullRequest
}

// StageReport addresses a pipeline step by name, so a report cannot invent a
// new step or depend on an index that a person changed between reports.
type StageReport struct {
	Name   string
	State  PhaseState
	Round  int
	Result *StageResult
}
