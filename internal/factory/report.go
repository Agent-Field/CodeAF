package factory

// RunReport is a worker's incremental write, not a replacement item. Keeping the
// identity and pipeline out of it preserves changes a person made meanwhile.
type RunReport struct {
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
	// Talk is the job's manager conversation, once the worker made it.
	Talk string
	// Notes are sentences the run added to the job's notes, oldest first.
	Notes []string
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
	Name   string
	State  PhaseState
	Round  int
	Result *StageResult
}
