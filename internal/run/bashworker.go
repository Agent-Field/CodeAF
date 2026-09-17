package run

// BashWorker is the seat one store task runs in: a session agent on the bash
// belt, the run's one working copy as its ground, the plan shim riding its
// commands, and its steps recorded to the trajectory file that is the task's
// record. The agent is built the way the session builds a bash-belt task
// worker today ([session.NewBeltWorker]); the loop here is what runs it.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// BashWorker implements Worker by hosting one session agent on the bash belt
// for one store task. The agent is built the way the session builds a
// bash-belt task worker today ([session.NewBeltWorker], with the task's own id
// as the agent name — the supervisor's claim trick, so the ownership check the
// finish command answers is taken against the exact name the task was claimed
// with); its turn loop runs until the agent ends its turn or the step cap on
// the context is reached, and then a Report comes back.
//
// EVERY STEP IS RECORDED, and the trajectory is the record — nothing in the
// worker's transcript is. A step line is appended when the step finishes, and
// the worker's own ending is one more line, so a person opening the task's
// folder reads the run the way it happened and how it ended.
//
// THE RESUME CLAUSE IS THE ONLY THING PORTED OF CHECKPOINT REPLAY. A worker
// opened on a task whose trajectory already has steps opens with the
// sentence the fresh worker needs — a predecessor was interrupted mid-work,
// and the effects it left are unannounced facts about the tree — and never
// replays a recorded command: the file is a record of what ran, not a
// checkpoint of what to run again.
//
// THE FLAG IS THE DOOR'S. CODEAF_TASK_BELT=bash is what makes a run wire this
// worker at all; the seat's constructor reads the switch once and refuses
// without it, so with the flag unset not one byte of any prompt, belt or
// landing changes — nothing constructs this worker.
type BashWorker struct {
	store     *plandb.Store
	workspace string
	model     string
	completer session.Completer
}

// NewBashWorker builds the seat the run's factory hands each claimed task to.
// The store is the run's own — the trajectory is appended beside it and the
// plandb shim is armed against it — the workspace is the run's one working
// copy every worker of the run shares, and the model is the seat's. The
// completer is the seat's provider: a test scripts it, a run hands the door's
// own.
func NewBashWorker(store *plandb.Store, workspace, model string, completer session.Completer) *BashWorker {
	return &BashWorker{store: store, workspace: workspace, model: model, completer: completer}
}

// Run hosts one agent's turn loop for the task until the agent ends its turn
// or the step cap on the context is reached, and returns a Report either way.
// A turn that ended cleanly reports the agent's own account of the work; a
// loop the cap stopped reports the steps it took and an error, because a task
// that ran out of steps did not finish; and a wall or a provider ending the
// turn ends the task with that reason.
func (w *BashWorker) Run(ctx context.Context, task plandb.Task) (Report, error) {
	capSteps := StepsPerTask(ctx)
	storeDir := filepath.Dir(w.store.Path())
	// THE RESUME READING COMES FIRST, because the opening message carries the
	// clause when there are steps to read: a predecessor's recorded commands
	// are effects already in the tree, and this worker opens on that fact.
	past, err := Trajectory(storeDir, task.ID)
	if err != nil {
		return Report{}, fmt.Errorf("read the task's trajectory: %w", err)
	}
	agent, err := session.NewBeltWorker(session.Config{
		Workspace: w.workspace,
		Model:     w.model,
	}, w.completer, &task, w.store.Path())
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = agent.Close(); agent.SettleWrites() }()
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	events, err := agent.Submit(runCtx, session.BeltWorkerBrief(&task, task.ID == w.store.RootID(), len(past) > 0))
	if err != nil {
		_ = appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, Reason: "the turn never started: " + err.Error()})
		return Report{}, err
	}
	rec := stepRecorder{store: w.store, storeDir: storeDir, taskID: task.ID, children: childrenOf(w.store, task.ID)}
	var (
		steps   int
		usd     float64
		turnErr error
		capped  bool
	)
	for event := range events {
		switch event.Kind {
		case session.EventToolEnd, session.EventToolFailed:
			steps++
			if err := rec.record(steps, event); err != nil {
				// A step that could not be recorded left the record shorter
				// than the run was: that is a failure of the record itself,
				// and the honest ending is the task failing on it.
				_ = appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, Reason: "the record failed: " + err.Error()})
				return Report{Steps: steps}, err
			}
			if capSteps > 0 && steps >= capSteps {
				// THE CAP IS A BOUND ON SPEND, not a finding about the work:
				// the turn is stopped here rather than judged, and the ending
				// below says where it stopped.
				capped = true
				stop()
			}
		case session.EventTurnDone:
			usd += event.Usage.CostUSD
		case session.EventError:
			if turnErr == nil {
				turnErr = event.Err
			}
		}
	}
	result := agent.TaskReport()
	reason := "turn ended"
	switch {
	case capped:
		reason = "stopped at the step cap"
		err = fmt.Errorf("stopped at its step cap after %d steps", capSteps)
	case ctx.Err() != nil:
		reason = "the run's wall stopped it"
		err = fmt.Errorf("the run's wall stopped the worker: %w", ctx.Err())
	case turnErr != nil:
		reason = "the turn errored: " + turnErr.Error()
		err = turnErr
	}
	if err := appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, Steps: steps, Result: result, Reason: reason}); err != nil {
		return Report{Steps: steps}, err
	}
	return Report{Result: result, Steps: steps, USD: usd}, err
}

// childrenOf reads the ids of the tasks that stand under the task when the
// worker opens, the before side of the diff each step's line records: the
// children a step created are the ones the store did not hold a moment
// before it ran.
func (w *BashWorker) childrenOf(id string) map[string]bool {
	set := map[string]bool{}
	for _, task := range w.store.Tasks() {
		if task.ParentID == id {
			set[task.ID] = true
		}
	}
	return set
}

// stepRecorder appends one worker's step lines, and keeps the two readings
// every step's record is a diff against: the children the task had when the
// last step ended, and the highest spill number the belt had filed when it
// did. Both belong to the run, not to the worker — a resumed worker inherits
// a folder its predecessor left numbers in.
type stepRecorder struct {
	store    *plandb.Store
	storeDir string
	taskID   string
	children map[string]bool
	spilled  int
}

// record appends one step line: the command the model spelled, the head of
// what came back, the whole output's file when the belt filed one, the plandb
// verbs the command ran, and the children the step created.
func (r *stepRecorder) record(n int, event session.Event) error {
	command := stepCommand(event)
	step := Step{
		Kind:        trajectoryStepKind,
		Step:        n,
		Command:     command,
		Observation: observationHead(event.Output),
		FullOutput:  r.takeSpill(),
		Writes:      planVerbs(command),
		Children:    r.takeChildren(),
	}
	return appendTrajectory(r.storeDir, r.taskID, step)
}

// stepCommand reads one step's command off the event's display arguments. For
// the belt's one bash hand the arguments are one command, and that command is
// what the record keeps; for the kept hands — the billed parse, a background
// job's own reading — the whole argument object is the step.
func stepCommand(event session.Event) string {
	if event.Tool != "bash" {
		return event.Args
	}
	var parsed struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(event.Args), &parsed) == nil && parsed.Command != "" {
		return parsed.Command
	}
	return event.Args
}

// takeSpill answers the file the belt filed this step's whole output in, or
// "" when it filed none. The spill is detected by the folder, not by parsing
// the result: the belt writes one numbered file beside the transcript exactly
// when it cuts, so a number that grew past the last one recorded is this
// step's whole output.
func (r *stepRecorder) takeSpill() string {
	highest := spillHigh(r.storeDir, r.taskID)
	if highest <= r.spilled {
		return ""
	}
	r.spilled = highest
	return filepath.Join(plandb.TaskDir(r.storeDir, r.taskID), fmt.Sprintf("action-%06d.txt", highest))
}

// spillHigh is the highest spill number the task's record folder holds, and
// zero when it holds none — the same scan the belt's own spiller walks, so
// the two readings of "what is already there" cannot disagree.
func spillHigh(storeDir, id string) int {
	highest := 0
	entries, _ := os.ReadDir(plandb.TaskDir(storeDir, id))
	for _, entry := range entries {
		var n int
		if _, err := fmt.Sscanf(entry.Name(), "action-%06d.txt", &n); err == nil && n > highest {
			highest = n
		}
	}
	return highest
}

// takeChildren answers the ids of the tasks this step created under the
// task — the store read after the command against the reading the last step
// left — and refreshes the reading for the step after it. Sorted, so the
// record reads the same way twice.
func (r *stepRecorder) takeChildren() []string {
	now := childrenOf(r.store, r.taskID)
	var created []string
	for id := range now {
		if !r.children[id] {
			created = append(created, id)
		}
	}
	sort.Strings(created)
	r.children = now
	return created
}

// childrenOf is the set of tasks standing under one task right now.
func childrenOf(store *plandb.Store, id string) map[string]bool {
	set := map[string]bool{}
	for _, task := range store.Tasks() {
		if task.ParentID == id {
			set[task.ID] = true
		}
	}
	return set
}

// planVerbs reads the plandb verbs one command spells, in the order the
// command spells them. The record says what the store was addressed by: the
// word after `plandb`, and for the task family the subverb with it. A command
// that only mentions the CLI inside a quoted echo would be recorded too — a
// false line in a file, never an effect in the world.
func planVerbs(command string) []string {
	fields := strings.Fields(command)
	var verbs []string
	for i, word := range fields {
		if word != "plandb" || i+1 >= len(fields) {
			continue
		}
		verb := fields[i+1]
		if verb == "task" && i+2 < len(fields) {
			verb = verb + " " + fields[i+2]
		}
		known := false
		for _, seen := range verbs {
			if seen == verb {
				known = true
				break
			}
		}
		if !known {
			verbs = append(verbs, verb)
		}
	}
	return verbs
}
