//go:build !windows

// This file is where the run's records go: the stage and step records the
// run reports to codeaf, and the run's own log of every record and bus
// payload for the tests that read one.
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
)

type event struct {
	Type      string         `json:"type"`
	Stage     string         `json:"stage,omitempty"`
	Status    string         `json:"status,omitempty"`
	Message   string         `json:"message,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp int64          `json:"ts"`
	// `spend` only. A pointer because a run that has cost nothing yet still
	// reports a figure, and omitempty would drop a real zero.
	CostUSD *float64 `json:"cost_usd,omitempty"`
	// `step` only: what was run, and what came back; the tool that ran it, the
	// step of senior-dev's process it served (step_ids.go), and a command's
	// exit code when it has one.
	Command     string `json:"command,omitempty"`
	Observation string `json:"observation,omitempty"`
	Tool        string `json:"tool,omitempty"`
	Step        string `json:"step,omitempty"`
	Exit        *int   `json:"exit,omitempty"`
}

// recordSink is where the run's protocol records go: codeaf, through the
// delegate.Host the run command was handed. It takes the two records the run
// writes as it goes; the first (hello) and the last (terminal) are the run
// command's own, because it is the one place that sees every ending.
type recordSink interface {
	Stage(stage delegate.StageRecord)
	Step(step delegate.StepRecord)
}

// eventWriter is the run's one outlet for what it has to say.
//
// STDOUT CARRIES THE PROTOCOL'S RECORDS AND NOTHING ELSE (docs/design/delegate/
// PROTOCOL.md), and it is codeaf's: a run codeaf hosts reports its stages and
// its finished steps through the host, and those are the only two records it
// writes as it goes. The instance bus's payloads — sessions, messages, parts,
// questions, model requests — and the `spend` record stay inside the process.
// The bus still carries them, and this writer still reads them: a finished
// tool part is a step, and the assistant messages are what the agent summary
// is added up from. Money is not reported here at all, because codeaf's model
// API meters every call itself.
//
// A test that wants to read the run the way senior-dev's own stream used to
// show it hands newEventWriter a writer instead, and gets every record and
// every bus payload on it, one JSON object per line.
type eventWriter struct {
	mu sync.Mutex
	// records is the host, in a run codeaf started. Nil in the tests that read
	// the log instead.
	records recordSink
	// encoder is the log: every record and bus payload, for a test. Nil in a
	// run codeaf started, where nothing but the host's records may reach stdout.
	encoder *json.Encoder
	// notes is where a stage's data goes for a person: one line per stage, on
	// stderr, which codeaf keeps in a file beside the task. The protocol's
	// stage record carries a curated copy of it (stage_data.go).
	notes   io.Writer
	summary *agentSummary
	// steps deduplicates `step` records: a tool part is republished as its
	// state moves, so the same finished call arrives more than once.
	steps map[string]struct{}
	// progress is what the step classifier knows of the run so far
	// (step_ids.go): whether a project file has changed, whether a submit was
	// accepted. It is read and moved under mu, in the order the calls finish
	// and the freeze's stage record is written.
	progress stepProgress
}

// newEventWriter is a writer whose only outlet is output: every record and
// every bus payload, one JSON object per line. It is the tests' view of a run.
func newEventWriter(output io.Writer) *eventWriter {
	return &eventWriter{
		encoder: json.NewEncoder(output),
		summary: newAgentSummary(),
		steps:   map[string]struct{}{},
	}
}

// newRecordWriter is the writer of a run codeaf hosts: stages and steps to
// records, and each stage's data as one line on notes.
func newRecordWriter(records recordSink, notes io.Writer) *eventWriter {
	return &eventWriter{
		records: records,
		notes:   notes,
		summary: newAgentSummary(),
		steps:   map[string]struct{}{},
	}
}

func (writer *eventWriter) emit(value event) {
	if writer == nil {
		return
	}
	if value.Timestamp == 0 {
		value.Timestamp = time.Now().UnixMilli()
	}
	// ONE LOCK, SO THE RECORDS KEEP THE ORDER THE RUN MADE THEM IN. Two
	// goroutines of the run can report at once (a tool finishing while the
	// stage machine moves on), and codeaf reads the order as the order things
	// happened in.
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.encoder != nil {
		_ = writer.encoder.Encode(value)
	}
	switch value.Type {
	case "stage":
		// The freeze's stage record is what says a submit was accepted, so it
		// moves the step classifier's progress here, under the same lock as the
		// steps and in the order the run wrote them: it is written inside the
		// submit call, before that call's own step.
		writer.progress = writer.progress.afterStage(value.Stage, value.Status)
		if writer.records != nil {
			writer.records.Stage(delegate.StageRecord{
				Stage: value.Stage, Status: value.Status, Data: stageRecordData(value.Data),
			})
		}
		writer.noteStage(value)
	case "step":
		if writer.records != nil {
			writer.records.Step(delegate.StepRecord{
				Command: value.Command, Observation: value.Observation,
				Tool: value.Tool, Step: value.Step, Exit: value.Exit,
			})
		}
	}
}

// noteStage writes a stage and its data as one line for a person reading the
// run's stderr: what the protocol's record has no field for, which is most of
// what senior-dev knows about why it did what it did.
func (writer *eventWriter) noteStage(value event) {
	if writer.notes == nil {
		return
	}
	line := "[senior-dev] " + value.Stage + " · " + value.Status
	if len(value.Data) > 0 {
		if data, err := json.Marshal(value.Data); err == nil {
			line += " " + string(data)
		}
	}
	_, _ = fmt.Fprintln(writer.notes, line)
}

// busEvent reads one instance-bus payload for what the run reports from it: a
// finished tool call is a step, and a completed assistant message moves the
// agent summary. The payload itself reaches only the log.
func (writer *eventWriter) busEvent(value bus.Payload) {
	if writer == nil {
		return
	}
	// Observed OUTSIDE the writer lock: the summary keeps its own mutex, so
	// aggregation never extends the encode critical section.
	spend, completed := writer.summary.observeBus(value)
	step, isStep := toolStepRecord(value)
	writer.mu.Lock()
	if writer.encoder != nil {
		_ = writer.encoder.Encode(value)
	}
	if isStep {
		if _, seen := writer.steps[step.key]; seen {
			isStep = false
		} else {
			writer.steps[step.key] = struct{}{}
			// THE STEP IS NAMED IN THE ORDER THE CALLS FINISHED, under the
			// same lock that deduplicates them, so the progress it reads is the
			// run's as of this call and no other.
			step.step, writer.progress = stepOf(step.action, writer.progress)
		}
	}
	writer.mu.Unlock()
	// Both are emitted outside the lock, because emit takes the same one.
	// Neither reaches the model: they are written after the fact, from state
	// the bus already published.
	if isStep {
		writer.emit(event{
			Type: "step", Command: step.command, Observation: step.observation,
			Tool: step.action.tool, Step: step.step, Exit: step.exit,
		})
	}
	// The running total, after the message that moved it, for the log only:
	// codeaf's model API meters every call itself, so a run it hosts never
	// reports money.
	if completed && writer.encoder != nil {
		total := spend
		writer.emit(event{Type: "spend", CostUSD: &total})
	}
}

func (writer *eventWriter) stage(stage, status string, data map[string]any) {
	writer.emit(event{Type: "stage", Stage: stage, Status: status, Data: data})
}

// verifyStep reports one command senior-dev itself ran on the tree — the
// project's own build or tests, with no model — as a step of its own: the
// command, its exit code (nil for one that hung or was cut, which has none),
// and the tail of what it printed.
//
// IT REPORTS WHAT RAN, AND CHANGES NOTHING ABOUT IT. The command, its ceiling
// and how its result is judged are the verification's own
// (full_verification_run.go); this is written after the command has exited,
// from the observation the verification already made.
func (writer *eventWriter) verifyStep(command, tail string, exit *int) {
	writer.emit(event{
		Type: "step", Command: "bash: " + oneLine(command),
		Observation: clipBytes(tail, stepObservationMax),
		Tool:        "bash", Step: StepVerify, Exit: exit,
	})
}
