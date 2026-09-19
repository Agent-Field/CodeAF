package run

// BashWorker is the seat one store task runs in: a session agent on the bash
// belt, the run's one working copy as its ground, the plan shim riding its
// commands, and its steps recorded to the trajectory file that is the task's
// record. The agent is built the way the session builds a bash-belt task
// worker today ([session.NewBeltWorker]); the loop here is what runs it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
// THE TASK'S SPEND ROW IS WRITTEN HERE, on the model the seat was built on
// ([recordSpend]): the run worker's session has no graph node to charge through
// the ordinary plan-spend path, so this is the one writer of a run task's row,
// and the model it names is the model every call the worker made went out on.
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
	// A BUILD THAT RECORDS EXITS SAYS SO ON ITS FIRST LINE, before any step, so a
	// record cut off before its ending (a killed worker, a wall, a window closed
	// mid-run) is still known to be a new build and its silence is not read as a
	// passing run. Written once, when the task first runs.
	if len(past) == 0 {
		// This write is the record's own proof that it comes from a build that
		// records exits; if it fails the record would read as an old one and a
		// declared check could hold unproven, so the run fails here rather than
		// drop the error.
		if err := appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryBeginKind, ExitsRecorded: true}); err != nil {
			return Report{}, fmt.Errorf("stamp the trajectory opening line: %w", err)
		}
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
	rec := stepRecorder{store: w.store, storeDir: storeDir, taskID: task.ID, children: childrenOf(w.store, task.ID)}
	var (
		steps  int
		usd    float64
		inTok  int
		outTok int
	)
	// THE SPEND ROW IS WRITTEN ONCE, WHATEVER THE ENDING. A turn that spent money
	// spent it whether it finished, hit the cap or errored, so the write is
	// deferred rather than kept to the good path: a person reading the ledger sees
	// what the task cost even when the task did not finish.
	defer func() { w.recordSpend(task.ID, usd, inTok, outTok) }()

	// THE BRIEF IS SAID ONCE, on the first round. Every round after it goes out
	// on the harness's own note, because a round only begins again when the last
	// one ended on words with no action.
	brief := session.BeltWorkerBrief(w.store, &task, task.ID == w.store.RootID(), len(past) > 0, WakeClause(runCtx))
	// noAction counts replies in a row that carried no tool call. A reply that
	// did call a tool resets the run to one — its own trailing words are the
	// first of the new run — and the fourth in a row fails the task.
	// sameStep is the law below, held for the whole turn: the identity of the
	// last finished step, how many identical ones have come back in a row, and
	// the moment the store was last read. The words a worker says between its
	// actions leave no step and change nothing, so the count runs over the
	// recorded steps alone.
	noAction := 0
	var (
		lastStep string
		same     int
		since    time.Time
	)
	for {
		events, err := agent.Submit(runCtx, brief)
		if err != nil {
			// A TURN THAT NEVER STARTED RUNS NO COMMAND, so it clears any live step
			// a predecessor left behind on this task rather than claiming a present.
			w.clearLiveStep(task.ID)
			_ = appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, ExitsRecorded: true, Reason: "the turn never started: " + err.Error()})
			return Report{Steps: steps}, err
		}
		brief = noActionNote

		var (
			roundSteps int
			turnErr    error
			capped     bool
			stalled    bool
			ending     storeEnding
		)
		for event := range events {
			switch event.Kind {
			case session.EventToolBegin:
				// A LIVE STEP IS TRUE ONLY WHILE ITS COMMAND RUNS. The begin event is
				// the command the moment before it runs — carrying the tool name and
				// the rendered arguments — and it is the one chance the store is told
				// what this task is doing now, so it is published as the task's live
				// step: the number the step will be recorded under, the command
				// [stepCommand] reads off the event, and the moment the store stamps.
				// The step's own end line clears it, and so does every ending below,
				// so a task that is not running a command never claims a present.
				_ = w.store.SetLive(task.ID, steps+1, stepCommand(event))
			case session.EventToolEnd, session.EventToolFailed:
				// THE CAP IS THE LAST STEP COUNTED. stop() cancels the turn, but
				// the agent's loop notices on its next round, and a round it had
				// already started still ends its tool — under the race detector
				// several do. Those late ends are drained here so the agent can
				// close, and they are neither counted nor recorded: the report
				// says the cap, and the trajectory ends where the cap fell.
				if capped {
					continue
				}
				steps++
				roundSteps++
				if err := rec.record(steps, event); err != nil {
					// A step that could not be recorded left the record shorter
					// than the run was: that is a failure of the record itself,
					// and the honest ending is the task failing on it.
					w.clearLiveStep(task.ID)
					_ = appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, ExitsRecorded: true, Reason: "the record failed: " + err.Error()})
					return Report{Steps: steps}, err
				}
				// THE STEP IS NO LONGER RUNNING, so its live reading goes with it: the
				// end-of-step append clears the live step the begin event published.
				w.clearLiveStep(task.ID)
				if capSteps > 0 && steps >= capSteps {
					// THE CAP IS A BOUND ON SPEND, not a finding about the work:
					// the turn is stopped here rather than judged, and the ending
					// below says where it stopped.
					capped = true
					stop()
					continue
				}
				// THE SAME-ACTION LAW, READ FROM THE RECORD AND NEVER FROM THE
				// WORDS: one step is identified by the command the model spelled
				// and the head of the answer that came back — the same head the
				// trajectory records — and the step before it says whether THIS
				// WORKER'S PART OF THE STORE moved between them ([movedUnder]: the
				// task's own row, a note or a context on it, or any row under it;
				// the supervisor heartbeat deliberately none of them, because
				// refreshing a claim is not work). A different command, a
				// different answer, or a move under the task resets the run to
				// one; the same command with the same answer on a part of the
				// store that stood still counts toward [sameStepLimit].
				identity := event.Tool + "\x00" + stepCommand(event) + "\x00" + observationHead(event.Output)
				moved := movedUnder(w.store, task.ID, since)
				since = time.Now()
				if moved || identity != lastStep {
					same, lastStep = 1, identity
				} else if same++; same >= sameStepLimit {
					// A TASK THE STORE SAYS IS BLOCKED ON ANOTHER TASK goes to
					// the parking the belt already has rather than an ending:
					// `plandb wait` refuses a wait with nothing open, so a park
					// that lands here says the store itself names what the
					// worker is waiting on, and the task wakes when that wait
					// is over instead of failing behind somebody else's work.
					if _, waitErr := w.store.Wait(task.ID, task.ID); waitErr == nil {
						ending = storeEnding{kind: endingWait, reason: fmt.Sprintf("waiting: the same command came back with the same answer %d times in a row", sameStepLimit)}
						stop()
					} else {
						stalled = true
						stop()
					}
				}
				// THE STORE'S OWN ENDING IS DETECTED AFTER THE COMMAND RUNS. A
				// `plandb done`, or a `plandb wait`, that the worker itself just
				// ran is the end of the loop: the shim's verb is already in the
				// step's record, and the store is the one authority on what
				// happened — done with its result, or parked with its claim
				// released. A task ends no other way but these, the cap, the
				// wall, or an errored turn.
				//
				// THE STORE IS READ ONCE THE ENDING IS FOUND, AND EVERY STEP THAT
				// RAN IS STILL COUNTED. The agent runs ahead of this reader: it can
				// call the model again and run the finish command while the step
				// before it is still being recorded here, so the ending is often
				// seen at an earlier step's end than the one that made it. The
				// stop() only asks the turn to end; the ends that still arrive are
				// commands that ran, and a task's record says what ran — unlike the
				// cap, which is a bound and stops counting where it fell.
				if ending.kind != endingNone {
					continue
				}
				if end, ok := w.storeEnding(task.ID); ok {
					ending = end
					stop()
				}
			case session.EventTurnDone:
				usd += event.Usage.CostUSD
				inTok += event.Usage.Input
				outTok += event.Usage.Output
			case session.EventError:
				if turnErr == nil {
					turnErr = event.Err
				}
			}
		}

		// THE ROUND IS OVER, WHATEVER STOPPED IT — a store ending, the step
		// cap, the run's wall, an error, or a reply with no action — so the live
		// step is cleared here too: a task whose loop has stopped, or is about to
		// go round again on the harness's note, is not running a command, and
		// every one of those endings leaves the same emptiness behind.
		w.clearLiveStep(task.ID)
		switch {
		case ending.kind == endingDone:
			if err := appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, ExitsRecorded: true, Steps: steps, Result: ending.result, Reason: "finished in the store"}); err != nil {
				return Report{Steps: steps, USD: usd}, err
			}
			return Report{Result: ending.result, Steps: steps, USD: usd}, nil
		case ending.kind == endingWait:
			reason := "waiting"
			if ending.reason != "" {
				reason = ending.reason
			}
			if err := appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, ExitsRecorded: true, Steps: steps, Reason: reason}); err != nil {
				return Report{Steps: steps, USD: usd}, err
			}
			return Report{Steps: steps, USD: usd, Waiting: true}, nil
		case capped:
			if err := appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, ExitsRecorded: true, Steps: steps, Reason: "stopped at the step cap"}); err != nil {
				return Report{Steps: steps, USD: usd}, err
			}
			return Report{Steps: steps, USD: usd}, fmt.Errorf("stopped at its step cap after %d steps", capSteps)
		case stalled:
			// THE LAW'S OWN ENDING, in the plain words a person reads on the
			// task's page: no machinery, no counts of things they have no
			// name for — the same command, the same answer, the one bound.
			reason := fmt.Sprintf("the same command came back with the same answer %d times in a row: the work was not moving", sameStepLimit)
			if err := appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, ExitsRecorded: true, Steps: steps, Reason: reason}); err != nil {
				return Report{Steps: steps, USD: usd}, err
			}
			return Report{Steps: steps, USD: usd}, errors.New(reason)
		case ctx.Err() != nil:
			if err := appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, ExitsRecorded: true, Steps: steps, Reason: "the run's wall stopped it"}); err != nil {
				return Report{Steps: steps, USD: usd}, err
			}
			return Report{Steps: steps, USD: usd}, fmt.Errorf("the run's wall stopped the worker: %w", ctx.Err())
		case turnErr != nil:
			if err := appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, ExitsRecorded: true, Steps: steps, Reason: "the turn errored: " + turnErr.Error()}); err != nil {
				return Report{Steps: steps, USD: usd}, err
			}
			return Report{Steps: steps, USD: usd}, turnErr
		}

		// A TURN THAT ENDED CLEANLY ENDED ON A REPLY WITH NO TOOL CALL. That
		// reply does not finish the task: the harness says so in its own voice
		// and the loop runs again. A reply that carried a tool call resets the
		// run to one — its own trailing words are the first of the new run —
		// and the fourth reply in a row with no action fails the task, the same
		// cap the belt's own invalid-action rejections use.
		if roundSteps == 0 {
			noAction++
		} else {
			noAction = 1
		}
		if noAction >= noActionLimit {
			reason := fmt.Sprintf("%d replies in a row carried no action", noAction)
			if err := appendTrajectory(storeDir, task.ID, Step{Kind: trajectoryEndKind, ExitsRecorded: true, Steps: steps, Reason: reason}); err != nil {
				return Report{Steps: steps, USD: usd}, err
			}
			return Report{Steps: steps, USD: usd}, errors.New(reason)
		}
	}
}

// noActionLimit is how many replies in a row may carry no tool call before the
// task fails. It is the same four the belt's own envelope uses for consecutive
// invalid actions ([internal/session]'s bashEnvelopeLimit), because the two are
// the same fact: a model that is not driving the belt one action at a time.
const noActionLimit = 4

// sameStepLimit is how many finished steps in a row may be the same command
// coming back with the same answer, with nothing moving in the worker's own
// part of the store between them, before the task ends. THE SAME-ACTION LAW:
// THE SAME COMMAND COMING BACK WITH THE SAME ANSWER sameStepLimit TIMES RUNNING,
// WITH NOTHING MOVING UNDER THE WORKER'S OWN TASK BETWEEN THE LOOKS, IS A WORKER
// THAT HAS STOPPED MAKING PROGRESS, AND ITS TASK ENDS THERE — whatever the tool and whatever
// the cause, because the ending reads no word of the command and no word of
// the error: the identity of the action, the head of the answer, and the
// store's own record of movement are the whole of the evidence.
//
// The count is over FINISHED STEPS and not model calls, so the words a worker
// says between its actions are nothing to it — and the alternation one action
// plus one text reply, which the no-action ending cannot see because every
// action resets that run to one, is bounded here too. Without this law the
// step cap was the only bound on that shape; a real pair of workers spent 820
// model calls and $4.30 inside it in half an hour, the cause unknown because
// the answer every action got was the same every time.
//
// THE THREE RESETS ARE WHAT THE LAW DOES NOT END. A different command is a
// different action tried; a different answer is a changed world, the shape
// of a worker legitimately waiting on something that changes; a move in the
// worker's own part of the store (its task and the rows under it, [movedUnder])
// is work it or its children did, read from the store's own bookkeeping and
// never from the command's words. A worker whose every look answers
// differently, or whose own part of the store moves between looks, is never
// ended here — the step cap bounds it as before. WHAT ITS SIBLINGS DO IS NOT
// ITS PROGRESS: a broken worker in a busy run is ended while the others work. A worker the store says is
// blocked on another task is not ended either: the loop parks it the way
// `plandb wait` would, and it wakes when its wait is over.
//
// The bound is the same four the no-action ending uses, because the two are
// the same fact at both ends of a turn: a belt that has answered the same
// way four times has been told three times what happens.
const sameStepLimit = 4

// movedUnder answers whether anything the store records moved, since the
// moment, in the part of the plan THIS worker can move: its own task, or a row
// anywhere under it.
//
// IT IS THE WORKER'S OWN PART AND NOT THE WHOLE STORE, because a run is many
// workers on one store. Read over the whole store, a broken worker would be
// kept alive for as long as its siblings went on working, which is exactly
// when nobody is looking at it: their rows move every few seconds and every
// one of those moves would reset its count. What a sibling does is not this
// worker's progress. A worker that is waiting on a sibling is not failed for
// it either: its look comes back different when the sibling lands, and a
// worker the store says is blocked is parked rather than ended.
//
// The zero moment is the first step of a turn, where there is no earlier look
// to compare with; it answers moved, so the count starts at one.
func movedUnder(store *plandb.Store, id string, since time.Time) bool {
	if since.IsZero() {
		return true
	}
	for _, changed := range store.Changed(since) {
		// Walk the containment chain up from the row that moved. The chain is
		// bounded by the tree's depth, and a row whose parent is gone ends it.
		for at := store.Task(changed); at != nil; at = store.Task(at.ParentID) {
			if at.ID == id {
				return true
			}
			if at.ParentID == "" || at.ParentID == at.ID {
				break
			}
		}
	}
	return false
}

// noActionNote is the harness's own voice, sent as a user message after a reply
// that executed no action. It is not the person's and it is not a step: it is
// the belt saying what a worker already knows from its page, at the one moment
// the loop can be sure it has stopped acting.
const noActionNote = "no action executed: answer with one bash call; finish with plandb done <your id> --result '…' when the acceptance holds; wait with plandb wait when you are blocked on another task"

// storeEndingKind is which of the two store endings a task reached.
type storeEndingKind int

const (
	endingNone storeEndingKind = iota
	endingDone
	endingWait
)

// storeEnding is the task's store row read after a step, answering whether the
// task has ended in the store: `plandb done` marked it done (with the result
// the worker passed), or `plandb wait` parked it ([Task.Waiting]).
type storeEnding struct {
	kind   storeEndingKind
	result string
	// reason is the ending line's own words where an ending carries more
	// than its kind: the wait the same-action law sends a blocked worker to
	// says beside its "waiting" what the worker was doing, so a person reading
	// the record asks no second question.
	reason string
}

// storeEnding reads the task's row and answers the ending it carries, if any.
// A task with no row has no ending; a parked task ends as a wait; a done task
// ends with its own result.
func (w *BashWorker) storeEnding(id string) (storeEnding, bool) {
	task := w.store.Task(id)
	if task == nil {
		return storeEnding{}, false
	}
	if task.Waiting {
		return storeEnding{kind: endingWait}, true
	}
	if task.Status == plandb.StatusDone {
		return storeEnding{kind: endingDone, result: task.Result}, true
	}
	return storeEnding{}, false
}

// recordSpend writes the task's one spend row: the model this seat was built
// on, the role its shape gave it at the moment the turn ended, and the dollars
// and tokens its calls cost. It is best-effort whole and last, because the money
// is already in the Report the supervisor absorbs and in the session's usage
// ledger; a store that refuses this write leaves the run's own counters true,
// and a row here is a reading of the run rather than the run's book. A turn that
// spent nothing writes nothing rather than a zero row somebody reads as a
// figure.
func (w *BashWorker) recordSpend(taskID string, usd float64, inTokens, outTokens int) {
	if usd == 0 && inTokens == 0 && outTokens == 0 {
		return
	}
	role, err := w.store.RoleOf(taskID)
	if err != nil {
		role = plandb.RoleWork
	}
	_ = w.store.AddSpend(taskID, w.model, role, usd, inTokens, outTokens)
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
		NotRun:      event.HarnessMade,
		Refused:     event.HarnessMade && event.Refused,
	}
	// THE COMMAND'S OWN EXIT IS RECORDED FROM THE EVENT, and only for a bash
	// command the belt actually ran: an ended bash tool exited zero, a failed one
	// exited non-zero (the belt appends "Command exited with code N", read back
	// here when it survived the output cap, otherwise a plain non-zero). A harness
	// refusal never ran the command and a non-bash tool has no exit, so both leave
	// the field nil, which a reader treats as unknown rather than as a zero.
	if event.Tool == "bash" {
		switch event.Kind {
		case session.EventToolEnd:
			zero := 0
			step.ExitCode = &zero
		case session.EventToolFailed:
			if !event.HarnessMade {
				code := 1
				if parsed, ok := exitStatusFromOutput(event.Output); ok {
					code = parsed
				}
				step.ExitCode = &code
			}
		}
	}
	return appendTrajectory(r.storeDir, r.taskID, step)
}

// exitStatusFromOutput reads the exit code the belt appended to a failed
// command's output ("Command exited with code N"), taking the last such line
// so a command whose own output quoted the phrase does not mislead it. It
// answers false when the phrase is absent, for instance when the output was cut
// before it, and the caller then records a plain non-zero.
func exitStatusFromOutput(output string) (int, bool) {
	const marker = "Command exited with code "
	idx := strings.LastIndex(output, marker)
	if idx < 0 {
		return 0, false
	}
	rest := output[idx+len(marker):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	code, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0, false
	}
	return code, true
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

// clearLiveStep forgets the task's live step: the one present-tense reading
// the store holds of this worker. It is called by the step's own end line and
// by every ending of the loop — a turn that ended, a step cap, a wall, an
// error, a turn that never started — so A LIVE STEP IS TRUE ONLY WHILE ITS
// COMMAND RUNS and a stopped task never keeps claiming a present it is not in.
//
// EVERY REFUSAL IS DROPPED ALIKE, the one a worker the run outlived meets being
// plandb.ErrClosed: the supervisor closes the run's store the moment the run is
// over, and this is a worker's last act. A live row that outlives its command
// is the one thing this must not leave, and a refusal to clear means the store
// is already gone — nobody reads it again — so the miss is the safe answer and
// never a second attempt at a handle that is closed.
func (w *BashWorker) clearLiveStep(taskID string) {
	_ = w.store.ClearLive(taskID)
}
