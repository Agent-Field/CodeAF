package run

// The trajectory is the task's page: one file
// beside the store, one line per step the worker took, and the one place a
// person — or a fresh worker resuming — reads what happened. Nothing in the
// worker's transcript is the record; the file is. A step line carries what
// the worker asked the belt to run, the head of what came back, the path of
// the whole output when the belt filed it, the store writes the step made and
// the child ids it created; the worker's own ending is one more line, because
// a record that says what was done but not what the worker said about it
// would make a person open the transcript to find out how it ended.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// trajectoryName is the file every task's steps are appended to, under the
// task's own record folder ([plandb.TaskDir]).
const trajectoryName = "trajectory.jsonl"

// The two line kinds a trajectory carries. A step line is one command the
// worker ran and what it observed; the ending line is how the worker's turn
// ended and what it said when it did.
const (
	trajectoryStepKind  = "step"
	trajectoryEndKind   = "end"
	trajectoryBeginKind = "begin"
	// trajectoryNotesKind is the line that says which of the task's notes a
	// worker of it has already had: handed over at a step boundary, or written
	// by that worker itself. It is not a step and every step reader skips it,
	// because it records what a worker KNOWS rather than anything it did
	// ([notesAlreadyHad] is its one reader).
	trajectoryNotesKind = "notes"
)

// observationHeadBytes is how much of one step's observation the record
// keeps. The whole output is already filed somewhere durable when the belt
// cut it, and the step line names that file; the head is what a skimming
// reader sees, and two kilobytes is the first screen of it.
const observationHeadBytes = 2048

// Step is one line of a task's trajectory. The step lines carry the command,
// the observation head, the path of the whole output when the belt filed one,
// the plandb verbs the command ran, and the ids of the children it created;
// the ending line carries what the worker said last, how many steps it took
// and why its loop ended. The two shapes share the type and never share a
// line: Kind says which one a line is.
type Step struct {
	Kind string `json:"kind"`
	// Step is the step's number, counted from one over the task across its wakes.
	Step int `json:"step"`
	// Command is what the worker asked the belt to run, as the model spelled
	// it — the command a resumed worker must not repeat blind, and the one
	// address the record has for what this step was.
	Command string `json:"command"`
	// Observation is the head of what came back, cut the way the belt cuts.
	Observation string `json:"observation,omitempty"`
	// FullOutput names the file the whole output was filed in, set only when
	// the step's output was cut — the belt files it beside the worker's
	// transcript, and this is where the next reader finds it.
	FullOutput string `json:"full_output,omitempty"`
	// Writes are the plandb verbs the command ran: the store writes the step
	// made, read off the command's own words, because the verbs are the words
	// the store was addressed by.
	Writes []string `json:"writes,omitempty"`
	// Children are the ids of the tasks this step created under the task, read
	// off the store after the command rather than parsed out of its output.
	Children []string `json:"children,omitempty"`

	// NotRun is established by the engine when the ended tool event is an
	// answer the harness wrote rather than a call the belt ran. The requested
	// command and the answer remain in the record; a surface reads this fact and
	// never the answer's words. False also preserves records written before the
	// field existed.
	NotRun bool `json:"not_run,omitempty"`
	// Refused narrows NotRun to the one kind a person wants to see: AN ACTION THE
	// WORKER ATTEMPTED AND A DOOR REFUSED ([session.Event.Refused]). A NotRun
	// step without it is a correction about the form of the worker's reply, where
	// nothing was attempted on the world. The engine records the difference here
	// because the event is where an attempted action is known; a surface cannot
	// recover it from the record afterwards.
	Refused bool `json:"refused,omitempty"`

	// ExitCode is the command's own exit status when the belt ran one and the
	// recorder knew it: zero for a step that ended, the non-zero code for one
	// that failed. It is a pointer so a step written before this field existed,
	// or one whose exit is unknown, decodes to nil and is never read as a zero a
	// real exit could equal. A holds verdict rests only on a recorded zero exit.
	ExitCode *int `json:"exit_code,omitempty"`

	// The ending line's fields. Steps is the run's whole step count, Result is
	// the worker's own account of the work, and Reason is why the loop ended —
	// a turn that ended, a step cap, a wall.
	Steps  int    `json:"steps,omitempty"`
	Result string `json:"result,omitempty"`
	Reason string `json:"reason,omitempty"`

	// StartedAt and EndedAt are a PROGRAM's own clock on the ending line of the
	// task it was handed: the instant codeaf started its process and the
	// instant that process was gone — the pair the program record carries
	// (delegate.ProgramRecord). They are zero on every other line, on an ending
	// written by a road that never started a process, and on every line a
	// worker of this conversation's own wrote. Step lines never carry them, so
	// the session's mirror of the step line (PlanStep) has no use for them.
	StartedAt time.Time `json:"started_at,omitzero"`
	EndedAt   time.Time `json:"ended_at,omitzero"`

	// ExitsRecorded is stamped true by a build that records each command's
	// exit, on the OPENING line it writes before any step and on the ending
	// line; bashworker.go sets it at both. A reader uses it to tell a record
	// that observed no run, or was cut off before its ending, from one written
	// before exits were recorded: the first refuses a holds verdict that never
	// ran its checks, the second falls back to reading.
	ExitsRecorded bool `json:"exits_recorded,omitempty"`

	// Notes is the notes line's one field: the ids of the task's notes a worker
	// of it has had, handed over or written itself ([trajectoryNotesKind]).
	Notes []string `json:"notes,omitempty"`
}

// notesAlreadyHad reads back every note id a worker of this task has already
// had, across every launch of it, so a worker woken for the same task starts
// with them marked.
//
// A NOTE IS HANDED TO A TASK ONCE, NOT ONCE PER LAUNCH. The mark that stops a
// second delivery lived in one worker's memory, so a parent woken to integrate
// its children, or a task parked and woken, was handed the same older notes
// again at its first boundaries — up to a delivery's worth of words it had
// already been told, crowding out the one note that was new. The record is
// where a task's history lives, so that is where the mark is kept.
func notesAlreadyHad(storeDir, id string) map[string]bool {
	had := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(plandb.TaskDir(storeDir, id), trajectoryName))
	if err != nil {
		return had
	}
	for _, line := range strings.Split(string(data), "\n") {
		var step Step
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &step) != nil || step.Kind != trajectoryNotesKind {
			continue
		}
		for _, note := range step.Notes {
			had[note] = true
		}
	}
	return had
}

// Trajectory reads one task's recorded steps back, in the order they were
// appended. A task that has never run has no file and answers with no steps
// and no error — the resume road reads it as "no predecessor left anything
// behind". A line that will not parse is skipped, not reported: a run
// interrupted mid-append left a half-written last line, and the record is
// about effects, not about the byte the process died on.
func Trajectory(storeDir, id string) ([]Step, error) {
	data, err := os.ReadFile(filepath.Join(plandb.TaskDir(storeDir, id), trajectoryName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var steps []Step
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var step Step
		if json.Unmarshal([]byte(line), &step) != nil || step.Kind != trajectoryStepKind {
			continue
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// appendTrajectory appends one line to the task's trajectory file, creating
// the task's record folder when the first line arrives. Appends are one
// write: the record grows in the order the worker took its steps, and a
// reader walking the file sees the run the way it happened.
func appendTrajectory(storeDir, id string, step Step) error {
	dir := plandb.TaskDir(storeDir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	line, err := json.Marshal(step)
	if err != nil {
		return err
	}
	file := filepath.Join(dir, trajectoryName)
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// observationHead is one observation's head, cut on a rune boundary the way
// the belt's own cut is — a record that opens with half a character is a
// record a person has to squint at.
func observationHead(text string) string {
	if len(text) <= observationHeadBytes {
		return text
	}
	cut := observationHeadBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
