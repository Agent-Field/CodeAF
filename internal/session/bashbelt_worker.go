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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/effort"
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
//
// rootID IS THE RUN THE WORKER BELONGS TO, read off the run's own open handle
// and never off the file at storePath. The path is where the run's store WAS
// when the run opened it; the root is which run it is, and a worker's
// `plandb` refuses a store at that path whose root is another run's
// ([plandb.RunEnv]). Empty binds the path alone.
func NewBeltWorker(config Config, completer Completer, task *plandb.Task, storePath, rootID string) (*Agent, error) {
	if !bashBeltAsked() {
		return nil, errors.New("the bash belt is off: CODEAF_TASK_BELT names the node belt")
	}
	if task == nil {
		return nil, errors.New("no store task for the worker seat")
	}
	// THE BELT IS THE SEAT'S, and InTask is the predicate's other half
	// ([Config.mayBashBelt]): a run worker is a task, and the switch above is
	// what said a run may build one.
	config.InTask = true
	config.bashBelt = true
	// A RUN WORKER THINKS AT THE WORK SEAT, ONE ACTION PER ROUND. The belt's
	// one call per response would pay the person's depth again on every round
	// of the run, so the seat is work (effort.RoleWork) — the same seat the
	// /task road chooses when its belt is on (task_run.go's workerSeat) — which
	// answers low when nothing above it spoke. A RUNG SET HIGHER STILL WINS: the
	// task's rung, the conversation's and the turn's all outrank the role,
	// because the rung is resolved by [effort.Resolve]'s own order (turn beats
	// conversation beats task beats role beats the install's default) and
	// nothing here tests those scopes itself.
	config.EffortRole = effort.RoleWork
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
	plan := &planState{path: storePath, root: rootID}
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
// A LEAF OWNS A PART OF THE ASK, AND READS THE ASK. A store task's description
// is the work order — the planning seat's account of one part of the run — and
// the run's own ask reached the store as the ROOT task's description. A leaf
// handed only the paraphrase inherits its omissions and cannot notice: on two
// runs of one objective the planner dropped the same bullet, and only the leaf
// that read the person's own sentence carried it. So one section of a non-root
// worker's document carries that sentence VERBATIM — the leaf reads the whole
// ask even though it owns one part, and where the ask and its work order
// disagree about a requirement it owns, the ask wins and the leaf says so in
// its report.
//
// THE ROOT'S OWN DOCUMENT IS UNCHANGED: its work order IS the ask, so there is
// nothing to put beside it. The section is absent there, and absent when the
// store carries no root row to read it from — the emptiness law applied to a
// document, the same reason a request equal to the work is printed once.
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
func BeltWorkerBrief(store *plandb.Store, task *plandb.Task, root, resume bool, wake string) string {
	role := planIsTask
	if root {
		role = planIsRoot
	}
	doc := composeBriefScoped(briefScopeFor(task.ID, true), briefPiece, "",
		planBrief(task, task.ID, role),
		strings.Join(task.Deliverables, "\n"),
		task.Acceptance,
		"", AdmissionContext{}, taskOrigin{}, taskCopy{})
	// THE ASK, FOR EVERY LEAF AND ONLY A LEAF. The section is absent on the
	// root's own document (its work order is the ask) and absent when the store
	// holds no root row to read it from, which is the emptiness law and not a
	// special case.
	if !root {
		if ask := runRootAsk(store); ask != "" {
			doc = withReport(doc, askSection(ask))
		}
	}
	// THE CHECK'S OWN SECTION. The review round dispatches a check as a leaf
	// whose work order already carries the checked leaf's acceptance and its own
	// result; this section says what a check does and the one shape its answer
	// takes. It is absent on every other task, so a doer never reads it.
	if !root && task.Role == plandb.RoleCheck {
		doc = withReport(doc, checkSection)
	}
	switch {
	case strings.TrimSpace(wake) != "":
		doc = withReport(doc, wake)
	case resume:
		doc = withReport(doc, taskResumeClause)
	}
	return doc
}

// The heading, the rule and the bound over the run root's own words, carried
// on every leaf's document ([BeltWorkerBrief]). The heading is the belt pages'
// own Markdown voice; the rule is the one sentence that says what the section
// is for.
const (
	askSectionHeading = "## The ask this run serves"
	askSectionRule    = "Your work order above is your part; where it and the ask disagree on a requirement you own, the ask wins, and you say so in your report."
	// askSectionLimit is the hard cap on the verbatim ask. The whole value of
	// carrying it is that nothing was edited out, and the bound is for the
	// pasted-log case, where an unbounded copy would put megabytes into every
	// leaf's prompt. The cut is marked ([clip]), so a leaf given a truncated ask
	// can see that it was.
	askSectionLimit = 16 << 10
)

// askSection lays out the run root's own words: the heading, the one sentence
// saying what they are for, and the description VERBATIM — unedited and
// untrimmed, bounded only by [askSectionLimit], which marks its cut.
func askSection(ask string) string {
	return askSectionHeading + "\n" + askSectionRule + "\n\n" + clip(ask, askSectionLimit)
}

// checkSection is the document the review round's check worker opens on, added
// to its brief by [BeltWorkerBrief] and read by no other task. THE CHECK IS NOT
// A DOER: it reads the acceptance above against the result above, proves each
// sentence with the leaf's own tests or one probe, and answers in one of the two
// shapes the finding is read from ([internal/run]'s recordCheckFinding reads
// "does not hold:"). It is written to stay under 120 words, because the whole
// job is one comparison and a wall of instruction is the drift it exists to stop.
const checkSection = `## Who checks this work

You are the check, not the doer: you read the acceptance above against the result above, and you do not redo the work.

Read the acceptance sentence by sentence. First run every command declared under Checks:, in order and exactly as spelled. Then, for every acceptance sentence those checks do not cover, run one probe — the smallest command that would fail were that sentence not met.

Answer with exactly one of these, as your whole result:

- "holds: <one sentence saying why>" only when every declared check exits 0 and every sentence holds.
- "does not hold: <the one unmet requirement, and the command that showed it>" otherwise.

If no declared command can be run, answer "check: <one sentence>" instead.

One line. A requirement you could not test is one you did not prove.
`

// runRootAsk answers the run root's description as the store keeps it — the
// person's own ask, seeded on the root row — or "" when there is no root to
// read (a store that never seeded one, a lost root, a nil handle). The empty
// answer is what makes the section absent rather than empty on a document
// ([BeltWorkerBrief]).
func runRootAsk(store *plandb.Store) string {
	if store == nil {
		return ""
	}
	root := store.Task(store.RootID())
	if root == nil || strings.TrimSpace(root.Description) == "" {
		return ""
	}
	return root.Description
}

// TaskReport is the agent's own account of its work, composed the way a
// node's landing composes its report: the final assistant message, read off
// the transcript rather than accumulated from the deltas, because a
// non-streaming provider emits no deltas and both roads end with the same
// recorded message ([taskReport] says why at length). It is what the run's
// supervisor writes when the worker's own `plandb done` has not already
// ended the task.
func (a *Agent) TaskReport() string { return taskReport(a) }

// sendBeltStep publishes a completed action and, for the run worker alone,
// keeps the next action behind the run's recording, limits and note delivery.
// The shared event hub stays asynchronous; only this producer waits, outside
// every agent and hub lock. Cancellation also releases a failed event reader.
func (a *Agent) sendBeltStep(ctx context.Context, hub *eventHub, event Event) {
	if !a.config.WaitForBeltSteps {
		hub.send(event)
		return
	}
	handled := make(chan struct{})
	event.BeltStepHandled = handled
	if !hub.send(event) {
		return
	}
	select {
	case <-handled:
	case <-ctx.Done():
	}
}
