package api

// ── AN EXTRA TASK ON THE WIRE ──────────────────────────────────────────────
//
// POST /v1/jobs/{id}/task sends a delegate into the job's checkout with plain
// instructions, as an extra step named task (task2, …), whatever step the job
// is on (internal/factory's task.go). It answers [TaskSent]; a job that takes
// no task now is refused with 409 and the reason in a sentence, and a body
// with no instructions with 422. A factory older than the verb answers 404.
//
// The step shows in the job's steps like any step the run holds that the
// pipeline does not: in_run true, in_pipeline false, its instructions as ask
// and who asked as by.

// Who asked for a task ([Task.By]).
const (
	TaskByMainAgent = "main_agent"
	TaskByYou       = "you"
)

// MaxTaskWords is the most a task's instructions may be, in bytes.
const MaxTaskWords = 16000

// Task is the body of POST /v1/jobs/{id}/task.
type Task struct {
	// Words is the instructions, required.
	Words string `json:"words"`
	// Now runs the task at once: the round running now is ended, its work
	// kept in the checkout, and the step it was in carries on after the task.
	// False runs the task when that round ends.
	Now bool `json:"now,omitempty"`
	// By is who asked: main_agent, or you (the default). The factory's local
	// service runs with one credential, so the caller says which.
	By     string  `json:"by,omitempty"`
	Expect *Expect `json:"expect,omitempty"`
}

// TaskSent is POST /v1/jobs/{id}/task's answer.
type TaskSent struct {
	Factory Factory `json:"factory"`
	// Step is the step the task runs as: task, task2, …, or followpr when the
	// job's pull request is open and the task is a Follow PR round's request.
	Step string `json:"step"`
	// Runs says when: now (it is the next thing to run), after_round (when
	// the round running now ends).
	Runs string `json:"runs"`
}

// When a task runs ([TaskSent.Runs]).
const (
	TaskRunsNow        = "now"
	TaskRunsAfterRound = "after_round"
)
