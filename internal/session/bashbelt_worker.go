package session

// The bash-belt worker seat the run's supervisor hosts each store task in,
// and the opening document it composes for one. The run engine (internal/run)
// claims a ready task, and this file is what a claimed task runs in: the seat
// is [Agent.newTaskAgentOn] stated for a worker built outside a session's own
// task tree — the run carries no parent config to copy a worker's posture
// from, so the posture is set here — and the belt is the switch's, read
// through [bashBeltAsked] the way every reader of it does.
//
// THE SHIM IS ARMED ON THE WORKER'S OWN GRAPH. [Agent.planCommandArgs]
// prefixes every bash command with the shim's directory, and that road reads
// the plan armed on [Agent.graph] — which mints the agent's own graph for an
// agent with no tasker, so arming is local to the worker and touches no
// conversation. The pulse is left behind on purpose: it fires only where
// Config.taskID names a node, and a run worker has none — dispatch is the
// run supervisor's own pass, not the session graph's.
//
// THE REFUSAL IS THE SWITCH'S. CODEAF_TASK_BELT=bash is what makes a run
// wire this seat at all, and the constructor reads the switch once and
// refuses without it: a seat built without the switch would wear a belt
// nobody composed and reach a `plandb` that is not this run's. With the
// switch unset not one byte of any prompt, belt or landing changes, because
// nothing constructs one.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// NewBeltWorker builds one bash-belt worker agent for one store task. The
// config carries the door's facts about the seat — the model, its key and
// window, the workspace the run's workers share — and the constructor sets
// the posture a worker has no parent to inherit: InTask, the belt, the
// unwatched node's approval floor, and the folder the worker's transcript and
// spill files land in, which is the task's own record folder beside the store
// ([plandb.TaskDir]), where the trajectory the run records lives too.
//
// THE COMPLETER IS THE SEAT'S PROVIDER, carried into the worker through the
// public door ([Config.completer], read by [New]). A run hands the
// conversation's account-aware view ([Agent.beltRunCompleter]) and a test hands
// a scripted one; nil is the road where nobody handed one and [New] builds the
// real client itself.
func NewBeltWorker(config Config, completer Completer, task *plandb.Task, storePath string) (*Agent, error) {
	if !bashBeltAsked() {
		return nil, errors.New("the bash belt is off: CODEAF_TASK_BELT is not bash")
	}
	if task == nil {
		return nil, errors.New("no store task for the worker seat")
	}
	// THE BELT IS THE SEAT'S, and InTask is the predicate's other half
	// ([Config.mayBashBelt]): a run worker is a task, and the switch above is
	// what said a run may build one.
	config.InTask = true
	config.bashBelt = true
	// ALLOW EVERYTHING EXCEPT THE FLOOR. The approval table still turns an
	// allow into a prompt for the shapes that destroy a disk or drop the
	// machine, and a prompt in a worker is a refusal it can read — never a
	// question and never a hang.
	config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	config.AskConsent = false
	// THE WORKER'S OWN RECORD FOLDER. The transcript this agent writes and the
	// whole outputs the belt's cut files land in the task's own folder beside
	// the store — the same folder the trajectory is appended to, so one task's
	// page is one folder a person can open — and nowhere in the working copy,
	// which is [Config.droppings]' own law applied to a seat with no family
	// place to inherit.
	taskDir := plandb.TaskDir(filepath.Dir(storePath), task.ID)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		return nil, err
	}
	config.SessionFile = filepath.Join(taskDir, workerJournalName())
	config.droppings = Place{Dir: taskDir}
	// AND THE WORKER IS BORN THROUGH THE PUBLIC DOOR, on the seat's provider. A
	// run worker stands alone — the run builds it, and it is not a node of this
	// conversation's own tree — so it takes the door a standalone seat takes
	// ([New]) rather than the scripted-completer seam the tests keep for
	// themselves. Handing New the seat's provider ([Agent.beltRunCompleter], the
	// conversation's account-aware view) is what gives the worker a managed
	// account pool the same way every other production agent gets one; a nil
	// provider builds the real client the way New always does.
	config.completer = completer
	agent, err := New(config)
	if err != nil {
		return nil, err
	}
	// THE SHIM, ARMED ON THE WORKER'S OWN GRAPH, before the seat is handed
	// back — and a shim that never landed is a seat that cannot run, because
	// every `plandb` its worker runs would resolve to whatever shares the
	// machine's PATH and write a plan this run would never read.
	plan := &planState{path: storePath}
	if err := plan.armShim(); err != nil {
		_ = agent.Close()
		return nil, fmt.Errorf("arm the plandb shim: %w", err)
	}
	g := agent.graph()
	g.planMu.Lock()
	g.plan = plan
	g.planMu.Unlock()
	return agent, nil
}

// workerJournalName mints the transcript's file name the way
// [taskJournalPath] names a node's, stamped by [journalMoment] so a resumed
// worker's transcript lands beside its predecessor's instead of over it. The
// transcript is not the record — the trajectory beside it is — but the belt's
// spill files land beside it, and the trajectory names those files, so it has
// to be somewhere the next reader can find.
func workerJournalName() string {
	return fmt.Sprintf("%s_worker.jsonl", journalMoment().Format("20060102-150405.000000"))
}

// BeltWorkerBrief composes a run worker's opening document for one store
// task: the plan-born road ([composeBriefScoped] with the task's id as the
// scope, so the document OPENS on the task it owns), the work order read FROM
// the store ([planBrief]) as THE WORK, and the store's deliverables and
// acceptance as the two sections under it.
//
// THE PERSON'S WORDS DO NOT RIDE A LEAF'S DOCUMENT. A store task's description
// is the work order, and the run's own ask reached the store as the root
// task's description — the root worker reads it the same way. There is no
// request to quote, so the section the ordinary worker opens on is absent
// here, which is the emptiness law applied to a document.
//
// THE RESUME CLAUSE IS ADDED, NOT DUPLICATED, the way [childRun.open] adds it
// to a resumed node's opening: one sentence, appended, when the task's
// trajectory already has steps — a predecessor was interrupted mid-work, and
// the effects it left are unannounced facts about the tree this worker is
// about to act in.
// THE WAKE CLAUSE, WHEN THERE IS ONE, IS WHAT THE OPENING CARRIES. A worker
// launched to integrate its children's landings opens on the supervisor's list
// of what they did — every child's title, status and result — in place of the
// interrupted-predecessor sentence, which is a fact about a different worker
// and not about this one. The resume flag still rides the trajectory's steps.
func BeltWorkerBrief(task *plandb.Task, root, resume bool, wake string) string {
	role := planIsTask
	if root {
		role = planIsRoot
	}
	doc := composeBriefScoped(briefScopeFor(task.ID, true), briefPiece, "",
		planBrief(task, task.ID, role),
		strings.Join(task.Deliverables, "\n"),
		task.Acceptance,
		"", AdmissionContext{}, taskOrigin{}, taskCopy{})
	switch {
	case strings.TrimSpace(wake) != "":
		doc = withReport(doc, wake)
	case resume:
		doc = withReport(doc, taskResumeClause)
	}
	return doc
}

// TaskReport is the agent's own account of its work, composed the way a
// node's landing composes its report: the final assistant message, read off
// the transcript rather than accumulated from the deltas, because a
// non-streaming provider emits no deltas and both roads end with the same
// recorded message ([taskReport] says why at length). It is what the run's
// supervisor writes when the worker's own `plandb done` has not already
// ended the task.
func (a *Agent) TaskReport() string { return taskReport(a) }
