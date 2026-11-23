package factory

// StepProgress is which step a line the runner says into a job's own
// conversation is about, and what happened to it: `review started`, `review
// done · 4m · …`, `asking you: …`, `stopped`. The runner sets it on exactly the
// lines that report one step's round, and on nothing else it says (a landing,
// an answer said back, a steer), so a surface that draws each round of a step
// as a frame of its own can fold those lines into the frame instead of saying
// the same thing twice. The words of the line stay as they were: a surface
// that reads only the words reads what it always did.
//
// IT IS THE PRODUCER'S WORD, NEVER A READING OF THE TEXT. The runner knows the
// step and the round when it speaks; a reader that parsed them back out of
// `review done` would break the day a step is named `done`.
type StepProgress struct {
	Step string `json:"step"`
	// Round is the step's round the line is about, 0 for a step that has no
	// rounds (an approve step).
	Round int           `json:"round,omitempty"`
	Event ProgressEvent `json:"event"`
}

// ProgressEvent is what a [StepProgress] line says happened to its step.
type ProgressEvent string

// The five things a step's line says.
const (
	// ProgressStarted is the step beginning (`review started`).
	ProgressStarted ProgressEvent = "started"
	// ProgressDone is the step finished (`review done · 4m · …`).
	ProgressDone ProgressEvent = "done"
	// ProgressFailed is the run stopped because the step errored.
	ProgressFailed ProgressEvent = "failed"
	// ProgressStopped is the run stopped by a person, or by their no, while the
	// step was under way.
	ProgressStopped ProgressEvent = "stopped"
	// ProgressWaiting is the step waiting on a question put to the person.
	ProgressWaiting ProgressEvent = "waiting"
)
