package session

// The task tool: how one piece of work leaves the conversation.
//
// A session is a place to think with somebody, and some work does not want to
// be thought about out loud. A long build-and-fix loop, a mechanical sweep over
// forty files, a rewrite whose only interesting moment is the end — run in the
// conversation, each of those buries the discussion under its own output and
// spends the context window on text nobody will read twice. propose_task is the
// model's way of saying "this part is self-contained; let it go and work
// somewhere else", and the machinery under it (task_run.go) is a GRAPH: the
// proposal is a node, the countdown is how the node is admitted, the worktree is
// how it is isolated, and its report is what its dependents will read.
//
// depends_on exists on the wire and is honoured by the executor, so a
// conversation that grooms two pieces of work can say which waits for which.
//
// ── AND IT IS THE ROAD FOR WIDE WORK, WHICH IT DID NOT USED TO BE ──
//
// "Self-contained" once meant "not wide": a goal with several parts was the
// planner's, and the model reached for run_adaptive the moment somebody asked
// for a broad sweep (tools_harness.go). The division road took that away. A
// wide ask is ONE task now, admitted with the road armed, and the worker hands
// the parts out from the material rather than anybody guessing them from the
// request (task_divide.go). `wide` is how the model says so, and it is the
// model's half of the flip the typed `/task` front door already made.
//
// THE FLIP FINISHED: run_adaptive is off the belt outright, so this is not the
// wider of two hands the model chooses between — it is the ONLY hand the model
// has for work with parts in it. No chat door opens a planned graph at all any
// more (loop.go says where the last one stood); width is always this hand.
//
// ── AND A TASK MAY HAND PART OF ITS OWN WORK OUT ──
//
// This tool is on a NODE'S belt too, and the node's proposals join the
// conversation's own graph under the node that made them (session.go's
// Config.tasker). That is the fan-out law: a step with two or three genuinely
// INDEPENDENT parts is faster as three nodes in three worktrees than as one
// model doing them in order, and the coordination — reading the reports,
// folding them into one deliverable — stays with the node that split the work.
// Work that is sequential, or that shares heavy context, is not split at all:
// the parts would each pay for a worktree, an audit and a wait to save nothing.
//
// TWO BOUNDS, AND THEY ARE DIFFERENT KINDS OF THING. The DEPTH cap is absence:
// a node standing on taskDepthLimit is handed no propose_task at all, because a
// capability that cannot work is left off the belt rather than made to refuse
// (tools.go). The FAN cap is a REFUSAL the model reads and acts on
// ([TaskGraph.claimChild]) — it has already been given its slots, and the answer
// to a fourth part is to do it in its own hands.
//
// ── THE PROPOSAL IS THE CONSENT, AND IT HAS A CLOCK ──
//
// The call BLOCKS, exactly as a consent question does (consent.go), and it is
// resolved by one of four things: the person approves, the person redirects
// (approval plus a correction appended to the brief), the person declines (the
// model reads the refusal as grooming feedback and keeps working), or THE CLOCK
// APPROVES. Silence is a yes, because the countdown is a window to redirect
// work the model has already groomed rather than a gate the work waits behind:
// a person who is reading, or away, or on another screen must not be the reason
// nothing happened. A surface that draws no answer box for this event is a
// surface where every task starts after five seconds, which is the right
// behavior for a surface that has not been taught the question yet.
//
// ── WHY A HEADLESS RUN NEVER WAITS ──
//
// With nobody subscribed to the events there is no one to answer, so the
// deadline approves whatever the setting says — including the 0 that means
// "wait for an answer" in a watched session. The alternative is a --once run
// that hangs forever on a question with no reader, which is the same fault
// consent.go refuses for the same reason.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// taskDescription is what the model reads before it calls. THE FAN CAP IS
// INTERPOLATED for taskSchemaJSON's reason: a number a model reasons with must
// be the number the code enforces, and the two drift the moment they are typed
// twice.
//
// AND IT IS WRITTEN FOR DENSITY, WHICH IS NOT HOW THIS CODEBASE WRITES PROSE.
// A Go comment is read once by a person and costs nothing; this string is
// marshalled into the tool block that rides in front of EVERY request of every
// turn, so a sentence here is paid some sixty times over one task and its bytes
// crowd out the conversation the model is actually holding. So it says each rule
// once, in an imperative clause, and it says it in the FIELD DESCRIPTION where
// the rule belongs — this preamble is routing guidance and nothing else. It no
// longer teaches the contract in three parts, because brief, deliverable and
// acceptance each teach their own part below and a preamble that said it again
// was the same paragraph billed twice; it no longer explains what makes work
// wide, because the `wide` field is where that is decided; and it no longer says
// the task cannot ask you anything, because that is the reason the brief must be
// self-contained and it is stated there. What is left here is what has no field
// to live in: when to reach for the tool at all, the countdown, the id that
// returns at once, the fan-out a node may make, and the line about files another
// window is already writing. It is short because it is expensive, never because
// a rule was dropped — the rules all still stand, in one place each.
var taskDescription = "Hand self-contained work to a task outside this conversation. Use it for work that would flood the conversation or wants a clean context, never for work needing back-and-forth. ALSO THE ROAD FOR WIDE WORK, which is still ONE task: set `wide`, never several proposals, and do not reach for a planner. The person may redirect or wave it off during a short countdown; silence starts it. The id returns at once; its report starts a turn here when it lands, so never wait or poll. A task may call this for genuinely independent parts of its own work, up to " + strconv.Itoa(taskFanLimit) + ", one level deep; sequential or context-sharing parts are faster in your own hands. Files another aforge window is already writing come back on their own line: plan around them, nothing is blocked or queued and your task has started."

// taskSchemaJSON is the wire schema. depends_on is on it from the first day
// even though a one-node graph can never fill it: the field is the edge, the
// executor already honours it (task_run.go), and a schema that grew the concept
// later would be a second shape for the same idea.
//
// THE TWO THRESHOLD DEFAULTS ARE INTERPOLATED, NEVER TYPED TWICE. They were
// typed twice once, and they drifted: this schema told the model the step
// default was 40 while the executor applied 200 (task_run.go's taskMaxSteps).
// A number in a tool description is not documentation — it is what the model
// reasons with, so a model that wanted room for a long sweep was raising a
// figure that was already five times higher than it believed, and one that
// wanted to keep a small job tight was setting 40 thinking it changed nothing.
// It is a var rather than a const for exactly this reason; the cost is one
// package-level string built at init, and what it buys is a default that
// cannot be wrong.
//
// GROUND AND WHERE ARE TWO QUESTIONS AND TWO FIELDS. `where` is the directory
// the worker types in, which is the person's to name; `ground` is the project
// that directory is a copy OF, which the harness resolves from evidence and the
// model only fills in when it knows better (taskstands.go). They read as one
// question and they are not: the task that made this whole design necessary had
// a perfectly good `where` and was about a repository three levels away from it.
//
// AND TWO RULES LEFT THIS SCHEMA WHEN GROUND ARRIVED, both of them because they
// were being stated twice. "Do not paste or contradict the person's message" is
// prompts/system.md's, where the same line also says the worker follows theirs
// on a disagreement; the class-word rule about `model` came the other way, INTO
// the field that governs it, and left system.md. The prefix is a budget
// (prefixbudget_test.go) and a rule that appears in two places is the cheapest
// thing in it to spend twice.
//
// THE BRIEF AND ACCEPTANCE DESCRIPTIONS CARRY THE SHAPING GUIDE IN MINIATURE.
// prompts/shape.md is the canonical statement of what a worker-ready brief must
// contain, and a person's own /task gets a model call that applies it
// (task_shape.go). A brief the CHAT model writes gets no second call, because
// the chat model has the whole conversation in front of it and a shaper would
// see one sentence — so the guide reaches it here instead, distilled to the
// three moves that survive compression: decide what a worker with nobody to ask
// would have to ask, constrain against the failure THIS kind of work has rather
// than work in general, and make done checkable by somebody else. Anything
// longer belongs in shape.md, and this stays short so the two cannot drift into
// two different accounts of one idea.
//
// AND EVERY FIELD HERE IS WRITTEN FOR DENSITY, for the reason taskDescription
// states about itself: these strings ride in front of every request of every
// turn, so each rule is stated once, in the field it governs, in as few words as
// keep it. The rhetoric is gone and nothing else is.
//
// AND `expects` IS INTERPOLATED, NEVER SPELLED HERE. It is the handoff
// contract's checkable half — what this brief assumes is already true of the
// folder the worker will get, walked before the first model call — and it is
// the SAME text the divider's schema carries, from the one constant that
// spells it (handoffcontract.go). Two doors reading one shape must not come to
// two opinions about it. Its 1,230 bytes were PAID FOR out of prompts/system.md
// rather than taken out of the fixed-prefix budget; the constant's own header
// says what came out and why none of it was a rule stated only there.
//
// AND THE BRIEF IS ALSO WHERE THE DOWRY RIDES. A proposal made from INSIDE an
// answer that has already begun the work holds something no other proposal can:
// what the model has just found out. prompts/system.md teaches the principle —
// the moment you can name the scale in front of you is the moment to hand it
// over, and what you have already learned goes with it — and the brief is where
// it lands, because it is the only part of the contract a finding fits in.
// NOTHING ON THE ROAD CLIPS IT: parseTaskArguments only trims it, [taskSpec]
// carries it whole, and [composeBrief] bounds the person's verbatim ask and
// nothing else — so a findings-rich handoff reaches the worker entire, and this
// sentence is the only thing standing between the model and writing one.
var taskSchemaJSON = `{"type":"object","properties":{` +
	`"title":{"type":"string","description":"One line naming the work as a person would say it"},` +
	`"summary":{"type":"string","description":"Two or three lines the person reads to decide whether to redirect it"},` +
	`"brief":{"type":"string","description":"THE WORK, self-contained: what to do, files and symbols, conventions, constraints, what was tried. It never sees this conversation and cannot ask you anything, so settle here everything it would stop and ask. Constrain THIS job, not work in general: name the lazy but plausible-looking answer here and forbid it — for prose, what reads as machine-written; for code, that \"working\" means having run it; for research, what counts as a source. \"Be accurate\" constrains nothing; every line must be one the worker could disobey. WHERE YOU ARE ALREADY MID-WORK, WHAT YOU HAVE LEARNED IS PART OF THE BRIEF: what you found, what you ruled out and why, what you would have done next — whoever takes this cannot see the calls you already made, so anything left out is learned again from nothing."},` +
	`"deliverable":{"type":"string","description":"WHAT MUST EXIST at the end, and where: the file and its path, the branch, the answer and its shape. Name the thing, not the activity"},` +
	`"where":{"type":"string","description":"Path the person named, or 'in place'; never guess"},` +
	`"ground":{"type":"string","description":"Optional absolute path: the repository or folder THE WORK IS ABOUT, when it is not this conversation's own. Left out, it is resolved from what this conversation read and edited"},` +
	`"acceptance":{"type":"string","description":"DONE WHEN: the observable condition somebody else could check without taking the task's word for it — the command that passes, the output that appears. \"It is finished\" is not this"},` +
	expectsSchemaJSON + `,` +
	`"depends_on":{"type":"array","items":{"type":"integer"},"description":"Ids that must finish first, only ids propose_task itself returned in this session, never a job, adaptive-run or step number. Its brief is given their reports. An unknown or failed id refuses the proposal rather than queueing it"},` +
	`"wide":{"type":"boolean","description":"Optional. Set it when the work is WIDER THAN ONE PAIR OF HANDS: many files, many sources, one change repeating over many independent items. The worker may hand parts out under itself once the material shows the width is real, then fold their reports into one deliverable. Say true whenever you judged the work broad, even with no count in hand: a wrong true costs nothing, the worker being refused unless what it finds names enough items. Leave it out for a linear job"},` +
	`"model":{"type":"string","description":"Optional, ONLY when the person asked for a particular model or class: a catalog id (\"anthropic/claude-opus-5\") or a part of one (\"opus-5\"), never a class word — resolve \"fast\" to a concrete model. Otherwise the configured model is used. A word fitting several is shown to the person to settle, one fitting none returns the nearest ids"},` +
	`"max_steps":{"type":"integer","description":"Optional. Finished tool calls per progress checkpoint (default ` + strconv.Itoa(taskMaxSteps) + `); work still advancing is given further allowances, circling work gets one landing turn and stops. Raise it for a wide sweep, lower it for something small"},` +
	`"no_progress":{"type":"integer","description":"Optional. How many tool calls in a row may add nothing — no new file, no question the work has not asked, no answer it has not been given — before it is stopped as stuck (default ` + strconv.Itoa(taskNoProgress) + `). It fires only on repeats. Raise it when the work needs much reading before its first edit"}` +
	`},"required":["title","summary","brief","deliverable","acceptance"],"additionalProperties":false}`

// taskArguments is the wire form.
type taskArguments struct {
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Brief       string `json:"brief"`
	Deliverable string `json:"deliverable"`
	Where       string `json:"where"`
	Ground      string `json:"ground"`
	Acceptance  string `json:"acceptance"`
	// Expects is what this brief assumes is already true of the folder the
	// worker will get, checked before it is allowed to spend anything
	// (handoffcontract.go). It is optional and the harness never writes one.
	Expects    []Expectation `json:"expects,omitempty"`
	DependsOn  []uint64      `json:"depends_on"`
	Wide       bool          `json:"wide"`
	Model      string        `json:"model"`
	MaxSteps   int           `json:"max_steps"`
	NoProgress int           `json:"no_progress"`
}

// taskSpec is one node's settled instruction: what the person was shown, and
// what the node will be given.
//
// IT IS BUILT ONCE AND AMENDED AT MOST ONCE — by a redirect, BEFORE admission
// (see proposeTask below). After [TaskGraph.admit] takes it, brief and
// acceptance are frozen for the node's whole life: that is the goal contract,
// and the law and the reason for it are written out on [TaskNode]. The one field
// a person may still move afterwards is `model`, and only from inside the node's
// own room — see the field itself.
type taskSpec struct {
	title string
	// named says A MODEL ALREADY WROTE THIS TITLE as a name, rather than the
	// door cutting one out of a sentence it had to hand. It is the whole of what
	// decides whether the work is named again (taskname.go): a name the shaper
	// or the grooming model wrote is what naming this work looks like, and
	// paying a second call to rename it would be the harness disagreeing with
	// its own answer. False is the honest default, so a new door that says
	// nothing gets a name made for it.
	named   bool
	summary string
	// request is THE PERSON'S OWN MESSAGE, captured by the code that admits this
	// proposal rather than asked of the model (task_brief.go). It is the first
	// thing the node reads, and it is the only part of the spec no model wrote.
	request string
	// brief, deliverable and acceptance are the contract the conversation
	// groomed: the work, what must exist at the end, and how anybody checks it.
	// [composeBrief] lays all four out as the node's opening message.
	brief       string
	deliverable string
	// where is empty for the default task-folder worktree, "in place" when the
	// request is deliberately non-code work in this conversation's directory,
	// or the exact path the person named. A worker never infers it from prose.
	where string
	// ground is the repository or folder THE WORK IS ABOUT, absolute, and mode is
	// how the task stands on it (taskstands.go resolves both, and [TaskMode]
	// spells the five modes out). They are settled at the door, before the person
	// is asked anything, so that the card they answer says where their work is
	// going — and frozen from admission like every other field here.
	//
	// WHERE AND GROUND ARE TWO QUESTIONS. `where` is the directory the worker
	// types in; this is the project that directory is a copy of. A task that
	// names neither gets both resolved for it.
	ground     string
	mode       TaskMode
	acceptance string
	// expects is the checkable half of the handoff contract: what this brief
	// assumes is already true of the folder the worker will get
	// (handoffcontract.go). It is written by whoever wrote the brief, never by
	// the harness, and an empty one is the ordinary case.
	expects   []Expectation
	dependsOn []uint64
	// modelWord is the `model` argument as the model wrote it — a word, not an
	// id — and it lives only until [Agent.resolveTaskModel] has answered for it
	// (taskmodel.go). model is that answer: the id this node will actually run
	// on, settled before admission.
	//
	// IT IS FROZEN AGAINST DRIFT AND NOT AGAINST THE PERSON, and it is the ONE
	// field of this spec that is so. Nothing implicit moves it — a `/model` in
	// the conversation reaches the conversation and no work already handed over —
	// and one thing explicit does: a person picking a model inside this node's
	// own room, which moves this node from its next turn on and nothing else in
	// the session ([Agent.RetargetTask], task_room.go). A node that has settled
	// is refused there, so a landed row's model is a fact and stays one.
	//
	// modelOptions is the shortlist a word that fits more than one model raises.
	// It is on the proposal the person is shown and is empty by the time the node
	// is admitted: [settleTaskModel] closes it with their answer, or with the
	// closest match when the clock does.
	modelWord    string
	model        string
	modelOptions []string
	// effort is the rung this node's workers ask the model for, empty when
	// nobody has set one and the ladder's next rung down decides
	// (internal/effort). It travels the same road `model` travels — set at
	// admission or from the node's own room, checkpointed, and read again by
	// [Agent.newTaskAgent] when a worker is prepared — because it answers the
	// same kind of question about the same piece of work.
	effort effort.Rung
	// maxSteps and noProgress are the node's own thresholds, 0 when the model
	// did not name one and the defaults apply (task_run.go).
	maxSteps   int
	noProgress int
	// design is set on the one node this session admits that is NOT a piece of
	// work handed to a child in a worktree: a sub-harness being written
	// (harness_task.go). It is nil on every ordinary task, and where it is set
	// [Agent.runTaskNode] hands the node to the designer's body instead of the
	// worker's — same graph, same room, same stop, a different middle.
	//
	// IT IS NOT IN THE CHECKPOINT AS ITSELF, deliberately, and task_store.go's
	// [interrupt] is what makes that safe rather than a hole: a design restored
	// from disk MID-WRITE settles FAILED before the graph ever holds it, so
	// there is nothing to re-enter. The one design that does come back is one
	// whose page was FINISHED and waiting on a person — its checkpoint carries
	// the page itself ([TaskNode.carryOffer]), and restoreNode rebuilds this
	// field from that record, which is the only record that can. That line and
	// this one are one decision. If any other design were put back on the
	// frontier, the next session would hand it to an ordinary worker in a
	// worktree with the designer's brief as its task — real money spent on work
	// nobody asked for — because this is the only field that says otherwise.
	design *harnessDesignSpec
	// run is set on the other node this session admits that is not a piece of
	// work handed to a child in a worktree: a SUBHARNESS being run
	// (subharness_run.go). It is nil on every ordinary task and on every design,
	// and where it is set [Agent.runTaskNode] hands the node to the run's body —
	// same graph, same room, same stop, a third middle.
	//
	// IT IS NOT IN THE CHECKPOINT EITHER, and for the reason [taskSpec.design]
	// states about itself, arrived at from the other direction: a run's input is
	// the material of one conversation, and a node put back on the frontier
	// without this field would be handed to an ordinary worker in a worktree with
	// the run's brief as its task. task_store.go's [interrupt] is what makes that
	// safe — an interrupted run settles before the graph ever holds it, because
	// nothing about a half-finished program is worth spending money to guess at
	// twice.
	run *subharnessRunSpec
	// parent, depth and owner are THE FAMILY this proposal was made in, and they
	// are the whole of what nesting adds to the spec: 0, 0 and nil for the work
	// a conversation grooms, and the proposing node's id, its depth plus one and
	// its own agent for a sub-task. [TaskNode] carries the same three and says
	// what each is for.
	parent uint64
	depth  int
	owner  *Agent
	// wide is THE PROPOSER'S OWN JUDGEMENT that this work is wider than one pair
	// of hands, and it is one of the three signals [Agent.armDivision] weighs
	// (task_divide.go). It is the model's half of the flip the typed `/task`
	// front door already made: a sizing yes there starts one worker and arms it
	// rather than opening a planner, and a model that has decided in its own
	// words that the work is broad has made the same judgement about the same
	// question, so it arms the same road.
	//
	// IT RIDES ON THE SPEC AND NOT IN [Agent.rememberDivisible]'s bank, which is
	// where the sizing judge's yes lives, and the reason is the batch. A model
	// fanning out emits its propose_task calls together and the loop runs them
	// CONCURRENTLY (loop.go), while the bank is deliberately one entry — three
	// proposals banking three briefs would leave two of them looking up an
	// answer another proposal had overwritten, and each would silently lose the
	// road. A judgement made about one spec belongs on that spec.
	wide bool
	// armed says THIS piece of work may discover that it is wider than one
	// worker and hand the parts out (task_divide.go), AND WHICH READER SAID SO.
	// It is settled at admission by [Agent.armDivision] — the one door every
	// task comes through — and never afterwards, for the reason every other
	// field of this spec is frozen: a road that could be opened under a running
	// node would be a node whose belt changed while it was working.
	//
	// AN EMPTY STRING IS NOT ARMED, and the three words it can otherwise hold
	// are [armedWide], [armedJudged] and [armedCounted]. It carries the reason
	// and not a bare yes for two readers that both need to tell them apart:
	//
	//   - THE TIEBREAK. A division the free text gate refuses on the floor is
	//     reconsidered ONLY where a model's own reading of breadth armed this
	//     work, because that is the one case where two signals disagree — and a
	//     refusal on work the text gate itself armed is that gate agreeing with
	//     itself (task_divide.go's [TaskNode.armedByJudgement]).
	//   - THE RECORD. A row saying a task ran with no parts cannot otherwise be
	//     told from a task that was never allowed any (task_index.go's
	//     [TaskIndexEntry.MaySplit]).
	armed string
	// drawn is A DIVISION SOMEBODY ALREADY WROTE FOR THIS WORK: the shape a
	// checkpoint mark's second reader drew of what was left of a turn, and the
	// account it drew it from (checkpoint.go's [drawnDivision]). It is empty on
	// every other door, and on every mark whose shape said one job.
	//
	// IT IS THE ONE FIELD OF THIS SPEC THAT IS AN INSTRUCTION TO THE HARNESS
	// rather than to the worker. The parts are already at the head of the brief,
	// where they read as a paragraph; this is the same parts in a shape the divide
	// road can be handed, so that the worker starts with them ALREADY handed out
	// instead of being asked to find them again (task_divide_sketch.go).
	//
	// IT IS NOT IN THE CHECKPOINT, deliberately, and the two ways a restored node
	// can be holding one are both answered by leaving it out. A node restored
	// AFTER its division has its parts back as nodes of their own, and a spec that
	// still proposed them would divide the same work twice; a node restored BEFORE
	// it ever ran comes back as one worker, which is what a task whose reviewer
	// refused already is. Neither is a loss worth a second way to spawn work.
	drawn drawnDivision
}

// taskTools is the belt's task family — one tool, in the conversation and in
// every node that is not standing on the floor of the tree.
//
// A NODE'S PROPOSAL IS NOT A SECOND MACHINE. It reserves an id from the
// conversation's own sequence, admits into the conversation's own graph, and
// runs under the same cap and the same checkpoint; what makes it a sub-task is
// one field, the parent it is registered under. There is nobody in a worktree to
// show a proposal to, so the countdown simply expires and the work starts —
// which is what an unwatched proposal already did before nesting existed
// ([Agent.askTask]).
//
// AT THE FLOOR THE TOOL IS ABSENT, NOT REFUSING. A node at taskDepthLimit has
// nothing left worth handing out, so it is not given the verb — the law every
// conditional family on this belt is built on (tools.go).
func (a *Agent) taskTools() []bare.Tool {
	if !a.mayProposeTask() {
		return nil
	}
	return []bare.Tool{{
		Name:        "propose_task",
		Description: taskDescription,
		Schema:      json.RawMessage(taskSchemaJSON),
		Execute:     a.proposeTask,
	}}
}

// mayProposeTask says whether propose_task belongs on this agent's belt: always
// in a conversation, and in a node only when it was handed the conversation's
// graph to admit into and is not standing on the floor of the tree.
//
// The graph is what the orchestrate run's workers and the auditor are NOT handed
// (task_run.go's newTaskAgent), which is how they end up without the verb
// without anybody writing a second rule about them.
func (a *Agent) mayProposeTask() bool {
	return !a.config.InTask || a.config.mayFanOut()
}

// mayFanOut is the same question asked of a CONFIG, before there is an agent to
// ask: [renderSystem] decides whether to tell this worker how to split its work
// at construction, and the belt and the prompt must not disagree about whether
// it can.
func (c Config) mayFanOut() bool {
	return c.InTask && c.tasker != nil && c.taskDepth < taskDepthLimit
}

// proposeTask is the tool's whole life: validate, ask, and admit.
//
// Everything it can answer badly is an ordinary tool result rather than a Go
// error, the way every other tool on this belt answers: a brief the model
// forgot to write is a call it can make again, and an error would end the turn
// over a missing field.
func (a *Agent) proposeTask(ctx context.Context, args json.RawMessage) (string, bool, error) {
	spec, problem := parseTaskArguments(args)
	if problem != "" {
		return problem, true, nil
	}
	// THE DEPENDENCIES ARE CHECKED AT THE DOOR, not on the frontier. A number
	// that names no task — a job id, an adaptive run, a step count the model
	// mistook for one — used to sail through here, be shown to the person,
	// admitted, and then failed on the very next frontier turn as a wait that
	// could never resolve. A refusal now costs nothing and names the fix; the
	// frontier's own check stays, as the backstop for a prerequisite that
	// fails after admission.
	if missing, failed := a.graph().doomedDependencies(spec.dependsOn); len(missing)+len(failed) > 0 {
		return dependencyRefusal(missing, failed), true, nil
	}
	// WHICH HANDS THE WORK LEAVES ON, settled before anybody is asked anything
	// (taskmodel.go). A word that names no model this install has is a refusal
	// the model can act on — it names the nearest ids — and one that names
	// several is not refused at all: the shortlist rides on the proposal, and the
	// person settles it in the same breath as the work.
	choice := a.resolveTaskModel(spec.modelWord)
	if choice.problem != "" {
		return choice.problem, true, nil
	}
	spec.model, spec.modelOptions = choice.model, choice.options

	// WHOSE WORK THIS IS. In a conversation the three are zero and this is a
	// root; in a node they are the node, its depth and its own agent, and they
	// are what make the proposal a sub-task rather than a second root
	// (task_run.go's [TaskNode]).
	spec.parent, spec.depth, spec.owner = a.config.taskID, a.config.taskDepth+1, a
	// AND WHAT THE PERSON ACTUALLY ASKED FOR, taken here rather than asked of the
	// model. The message that caused this call is known at this moment — it is the
	// one the turn opened on, or the newest thing typed into it — so the node
	// opens on their sentence, unedited, above the contract the model groomed out
	// of it (task_brief.go). A node proposing a sub-task inherits the same
	// sentence; there is nobody in a worktree to type a new one.
	spec.request = a.taskRequest()
	// AND WHERE THE WORK STANDS, resolved from the evidence this conversation
	// already holds (taskstands.go) before anybody is asked anything, so the card
	// the person answers names the project rather than a folder under a session.
	// Two places with real weight in the evidence are a QUESTION and never a coin
	// toss: the model is handed the person's sentence to put to them, and nothing
	// is admitted until it comes back with an answer.
	stand := a.resolveTaskGround(spec)
	switch {
	case stand.refusal != "":
		return stand.refusal, true, nil
	case stand.ask != "":
		return stand.ask + "\nAsk the person which, then propose this again with `ground` set to their answer.", true, nil
	}
	spec.ground, spec.mode = stand.dir, stand.mode
	// AND THE CONVERSATION REMEMBERS WHERE IT IS ABOUT. A ground resolved here —
	// the model's own `ground`, the person's answer to the two-places question
	// arriving as the next proposal's argument, or the repository this
	// conversation has plainly been working in — is written onto the session as a
	// referred place (places.go), so the ladder's next climb finds it at SAID and
	// nobody is asked the same question twice.
	a.keepGround(stand)
	graph := a.graph()
	// THE SLOT IS TAKEN BEFORE THE QUESTION and handed back by everything that
	// is not an admission, so a batch of proposals cannot walk through the fan
	// cap together ([TaskGraph.claimChild]).
	if refusal := graph.claimChild(spec.parent); refusal != "" {
		return refusal, true, nil
	}
	admitted := false
	defer func() {
		if !admitted {
			graph.releaseChild(spec.parent)
		}
	}()

	id := graph.reserve()
	// WHO ELSE IS ALREADY IN THESE FILES, ASKED BEFORE THE MONEY. It is one line
	// or nothing at all (taskpreflight.go), it rides on the proposal so the person
	// reads it on the card while the countdown is still running, and it comes back
	// on the result so the model can sequence its next proposal around it. NOTHING
	// IS PREVENTED BY IT: the work starts on the same answer it would have started
	// on, because a claim another window wrote is evidence and never an
	// instruction.
	elsewhere := a.taskPreflight(spec.title, spec.summary, spec.brief, spec.deliverable, spec.acceptance)
	answer, err := a.askTask(ctx, id, spec, elsewhere)
	if err != nil {
		// The turn ended under the question. Nothing was admitted, so there is
		// no node to cancel and nothing to clean up — the proposal simply never
		// became one.
		return "the task was never answered: the turn ended first", true, nil
	}
	if !answer.Approved {
		// A DECLINE IS A RESULT, NOT AN ERROR. The model asked a reasonable
		// question and got a plain no; handing it a failure would put a red row
		// in the transcript for a conversation working exactly as intended.
		if reason := strings.TrimSpace(answer.Redirect); reason != "" {
			return withElsewhere("the person declined this task: "+reason, elsewhere), false, nil
		}
		return withElsewhere("the person declined this task", elsewhere), false, nil
	}
	if redirect := strings.TrimSpace(answer.Redirect); redirect != "" {
		// APPENDED, never merged into the brief's prose. The person's words
		// arrive last and in their own voice, so the node reads them as the
		// correction they are rather than as one more paragraph the model wrote.
		//
		// AND IT HAPPENS HERE, BEFORE admit — this line is the last moment in
		// the node's life at which its brief may change. Everything after
		// admission reads a frozen spec, including the auditor that decides
		// whether the work is done ([TaskNode]'s goal contract): a target that
		// can move while the work runs is a target the work can always be made
		// to hit. A correction that arrives later is a new proposal, which is
		// the person exercising the same authority a second time.
		spec.brief = strings.TrimRight(spec.brief, "\n") +
			"\n\nThe person redirecting this task says: " + redirect
	}
	// THE SHORTLIST IS CLOSED HERE, in the same breath the brief is: a node is
	// admitted with one model and never a set of them. An answer that named one
	// of the options takes it; an answer that named nothing — including the
	// clock's silence — takes the closest match, which is the one the proposal
	// showed (taskmodel.go).
	if len(spec.modelOptions) > 0 {
		spec.model, spec.modelOptions = settleTaskModel(spec.modelOptions, answer.Model), nil
	}

	state := graph.admit(id, spec)
	admitted = true
	// THE MODEL IS NAMED BACK ONLY WHEN IT WAS ASKED FOR. A word resolves to an
	// id and a shortlist is settled by somebody else, so the one thing the model
	// cannot know after this call is what its own argument came to; a task that
	// named no model has nothing to be told, and a receipt reciting the default
	// every time would be a line nobody reads.
	on := ""
	if spec.modelWord != "" && spec.model != "" {
		on = " on " + spec.model
	}
	if state == TaskQueued {
		return withElsewhere(fmt.Sprintf("task %d queued%s: %s\nIt starts when the work it waits on has finished and a slot is free. %s", id, on, spec.title, taskHandoffWakeSentence), elsewhere), false, nil
	}
	return withElsewhere(fmt.Sprintf("task %d started%s: %s\nIt works from the brief alone, in its own copy of the repository. %s", id, on, spec.title, taskHandoffWakeSentence), elsewhere), false, nil
}

// taskHandoffWakeSentence is what EVERY handoff receipt ends with, and it is one
// sentence because it answers one question: what does the model do now?
//
// IT IS HERE BECAUSE A MODEL POLLED FOR WORK IT WAS GOING TO BE TOLD ABOUT. The
// landing already wakes this session with the node's report (task_run.go's
// [Agent.reportTaskNode] and [Agent.deliverTaskNote]) — a turn starts for it,
// with nobody having typed — and a model that does not know this reads "its
// report arrives here" as something it might have to go and collect. One did:
// eleven `tasks` calls in a single turn, waiting. So the receipt now says the
// news comes to it and there is nothing to check, and `tasks` says the same in
// its own description (tools_tasks.go), and the answer says it a third time when
// it has not moved (tasklook.go). The three are one fact stated where a model
// mid-poll will actually meet it.
const taskHandoffWakeSentence = "You are told the moment it lands — its report starts a turn here on its own — so there is nothing to check and nothing to poll: carry on with other work, or end your turn."

// dependencyRefusal is the sentence a doomed depends_on gets back: which ids
// are wrong, what they probably were instead, and what to do — in the model's
// own terms, so the next call is the corrected one rather than a guess.
func dependencyRefusal(missing, failed []uint64) string {
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf(
			"depends_on names %s — no task in this session has that id. depends_on takes only ids propose_task itself returned; a job, adaptive-run or step number is a different kind of work and cannot gate a task",
			numberedTasks(missing)))
	}
	if len(failed) > 0 {
		parts = append(parts, fmt.Sprintf(
			"depends_on names %s, which already failed and will never finish — re-run that work first, or drop the dependency",
			numberedTasks(failed)))
	}
	return "Invalid arguments: " + strings.Join(parts, "; ") +
		". Propose it again with depends_on corrected, or left out if nothing must finish first."
}

// numberedTasks says one or several ids the way a sentence would.
func numberedTasks(ids []uint64) string {
	words := make([]string, len(ids))
	for i, id := range ids {
		words[i] = strconv.FormatUint(id, 10)
	}
	if len(words) == 1 {
		return "task " + words[0]
	}
	return "tasks " + strings.Join(words, ", ")
}

// parseTaskArguments reads one call and says, in plain words, what is missing.
//
// Every field is required because every field is load-bearing: the title and
// summary are what the person decides on, the brief is the work, the
// deliverable is what must exist at the end, and the acceptance is what the
// node is finished against. A task groomed without one of them is not a task
// that was groomed — and a deliverable nobody named is how work comes back
// having thought about something rather than having produced it.
//
// THE PERSON'S REQUEST IS NOT ON THIS LIST because it is not asked for: the
// message that triggered the proposal is already in hand, and [Agent.proposeTask]
// puts it on the spec itself (task_brief.go). A field the model must remember to
// fill is a field the model will one day fill with its own words.
func parseTaskArguments(args json.RawMessage) (taskSpec, string) {
	var parsed taskArguments
	if err := decodeToolArguments(args, &parsed); err != nil {
		return taskSpec{}, "Invalid arguments: " + err.Error()
	}
	spec := taskSpec{
		title: strings.TrimSpace(parsed.Title),
		// THE MODEL GROOMED THIS PROPOSAL AND NAMED IT IN THE SAME BREATH, so the
		// namer leaves it alone (taskname.go).
		named:       strings.TrimSpace(parsed.Title) != "",
		summary:     strings.TrimSpace(parsed.Summary),
		brief:       strings.TrimSpace(parsed.Brief),
		deliverable: strings.TrimSpace(parsed.Deliverable),
		where:       strings.TrimSpace(parsed.Where),
		ground:      strings.TrimSpace(parsed.Ground),
		acceptance:  strings.TrimSpace(parsed.Acceptance),
		dependsOn:   parsed.DependsOn,
		// THE MODEL'S OWN "THIS IS WIDE", carried to [TaskGraph.admit] where the
		// road is armed. Absent is false, which is the honest default: a model
		// that has never heard of this argument proposes exactly the task it
		// proposed before it existed.
		wide:       parsed.Wide,
		modelWord:  strings.TrimSpace(parsed.Model),
		maxSteps:   parsed.MaxSteps,
		noProgress: parsed.NoProgress,
	}
	// A NEGATIVE THRESHOLD IS A MISTAKE WORTH SAYING OUT LOUD, where an absent
	// one is not: omitting the field means "use the default" and is the ordinary
	// case, but a model that asked for -1 steps meant something it did not say,
	// and silently running that node for forty would be the harness inventing an
	// answer to a question the model got wrong.
	for _, threshold := range []struct {
		value int
		field string
	}{
		{spec.maxSteps, "max_steps"},
		{spec.noProgress, "no_progress"},
	} {
		if threshold.value < 0 {
			return spec, "Invalid arguments: " + threshold.field + " cannot be negative"
		}
	}
	for _, missing := range []struct {
		value string
		field string
	}{
		{spec.title, "title"},
		{spec.summary, "summary"},
		{spec.brief, "brief"},
		{spec.deliverable, "deliverable"},
		{spec.acceptance, "acceptance"},
	} {
		if missing.value == "" {
			return spec, "Invalid arguments: " + missing.field + " is required"
		}
	}
	// THE MANIFEST IS READ LAST BECAUSE IT IS THE ONLY OPTIONAL HALF OF THE
	// CONTRACT. A proposal missing its brief is told about the brief; a
	// proposal that named an assumption it could not shape is told about that,
	// in the same words the divider's door uses, because one shape checked in
	// two places would drift into two accounts of what an expectation is
	// (handoffcontract.go owns both).
	expects, problem := parseExpectations(parsed.Expects)
	if problem != "" {
		return spec, problem
	}
	spec.expects = expects
	return spec, ""
}

// ── the proposal's admission ────────────────────────────────────────────────

// ResolveTask answers one EventTaskProposal. A surface hands back the id the
// event carried and what the person said about it.
//
// An id nobody is waiting on — a proposal the clock already approved, a second
// click, a turn that was interrupted — is IGNORED rather than reported, exactly
// as [Agent.ResolveConsent] ignores a late answer. The answer is simply late,
// and the surface has already seen the node start or the turn end.
func (a *Agent) ResolveTask(id uint64, answer TaskAnswer) {
	a.mu.Lock()
	answers, waiting := a.taskAnswers[id]
	if waiting {
		delete(a.taskAnswers, id)
	}
	a.mu.Unlock()
	if !waiting {
		return
	}
	// Buffered to one and read at most once, so this never blocks and never
	// needs the lock held across it.
	answers <- answer
}

// askTask emits one proposal and waits for the person, the clock, or the end of
// the turn.
//
// THE CLOCK IS THE DIFFERENCE from consent's ask, and where it applies is the
// whole law:
//
//   - WATCHED, countdown > 0: the deadline is real and approves on expiry.
//   - WATCHED, countdown 0: no clock at all. Somebody is there, and they said
//     they want to answer every time; the Deadline field goes out zero so the
//     surface draws no bar.
//   - UNWATCHED: the deadline approves whatever the countdown says, zero
//     included. There is no one to wait for, and a headless run blocked on a
//     question nobody can see is a hang, not a safeguard.
//
// elsewhere is the preflight's one line about other windows already in these
// files, or "" — a fact the card draws beside the work, not a reason to wait.
func (a *Agent) askTask(ctx context.Context, id uint64, spec taskSpec, elsewhere string) (TaskAnswer, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return TaskAnswer{}, errAgentClosed
	}
	answers := make(chan TaskAnswer, 1)
	if a.taskAnswers == nil {
		a.taskAnswers = make(map[uint64]chan TaskAnswer, 1)
	}
	a.taskAnswers[id] = answers
	// The turn's hub, read under the same lock that registers the wait: a tool
	// runs inside a turn, and the turn's fan-out is where its question is seen.
	hub := a.hub
	watched := a.config.AskConsent && hub != nil
	countdown := time.Duration(a.config.TaskAutoApproveSeconds) * time.Second
	if countdown < 0 {
		countdown = 0
	}
	a.mu.Unlock()

	clock := watched && countdown > 0 || !watched
	var deadline time.Time
	if clock {
		deadline = time.Now().Add(countdown)
	}

	// AND ANOTHER WINDOW LEARNS WHAT THIS ONE IS STOPPED ON (taskpresence.go).
	// The line is the title, because the title is what the person on the card is
	// deciding about; a proposal has a summary and a brief as well, and neither
	// belongs in a file every window re-reads every few seconds.
	//
	// IT IS BANKED EVEN WHEN THE CLOCK IS RUNNING. An answer that arrives after
	// the countdown approved is late, and late answers are dropped by
	// [Agent.ResolveTask] exactly as they are when a click lands a moment too
	// slowly in the card's own window.
	defer a.presenceAsking(QuestionTask, id, "wants to start a task: "+strings.TrimSpace(spec.title))()

	if hub != nil {
		// AND THE WARM LINE, WHERE THE HANDOFF CAME OUT OF THE WORK ITSELF. It
		// goes ABOVE the card because it is the reason the card is there, and it
		// is [EventNotice] — the dim one-liner a surface already draws for
		// something it did not stop to ask about — rather than a kind of its own.
		// THE CARD IS NOT REPLACED BY IT: this work was groomed by the model, and
		// the countdown is the consent for exactly that (route_judge.go's
		// [Agent.launchRouteTask] states the other half of the same law, for work
		// nobody groomed). Silence still starts it, so the person is told and the
		// work opens; what the card adds is a window to redirect, which is more
		// than an auto-start could give them and not less.
		if a.alreadyWorking() {
			hub.send(Event{Kind: EventNotice, Text: taskEscalationNote})
		}
		hub.send(Event{
			Kind: EventTaskProposal,
			Tool: "propose_task",
			Task: &TaskNotice{
				ID:         id,
				Title:      spec.title,
				Summary:    spec.summary,
				Brief:      spec.brief,
				Acceptance: spec.acceptance,
				Where:      taskWhereNotice(a.config.Place, a.config.Workspace, id, spec.where),
				Ground:     spec.ground,
				Mode:       spec.mode,
				DependsOn:  spec.dependsOn,
				Deadline:   deadline,
				// What it will run on, and — when one word fit more than one model
				// — what it could run on instead. A surface draws the first as a
				// fact and offers the second as a choice; both are settled by the
				// answer this select is waiting for.
				Model:        firstTaskModel(spec.modelOptions, spec.model),
				ModelOptions: append([]string(nil), spec.modelOptions...),
				Elsewhere:    elsewhere,
			},
		})
	}

	var expiry <-chan time.Time
	if clock {
		timer := time.NewTimer(countdown)
		defer timer.Stop()
		expiry = timer.C
	}

	select {
	case answer := <-answers:
		return answer, nil
	case <-expiry:
		a.forgetTask(id)
		// Silence is a yes, and it is a yes with no redirect: the person said
		// nothing, so nothing is appended to the brief.
		return TaskAnswer{Approved: true}, nil
	case <-ctx.Done():
		a.forgetTask(id)
		return TaskAnswer{}, ctx.Err()
	}
}

func taskWhereNotice(place Place, workspace string, id uint64, where string) string {
	where = strings.TrimSpace(where)
	if strings.EqualFold(where, "in place") {
		return workspace
	}
	if where != "" {
		if resolved, err := resolveTaskWhere(where, workspace); err == nil {
			return resolved
		}
		return where
	}
	if trees := place.Trees(); trees != "" {
		return filepath.Join(trees, strconv.FormatUint(id, 10))
	}
	return "task folder"
}

// ── the handoff made from inside the work ───────────────────────────────────

// taskEscalationNote is the ONE line a person reads when work leaves an answer
// that had already begun it.
//
// IT IS WRITTEN ONCE AND IS NOT A ROTATION. A line somebody sees on their good
// days is furniture, and furniture that changes its wording every time reads as
// a machine trying to sound spontaneous. This is the register the surface
// already speaks in for a fact it did not stop to ask about (internal/tui3's
// taskWideNote: an observation, a middle dot, a promise), and it says the two
// things that are true at the moment it is written — the work turned out to
// want more than one pair of hands, and what has already been found goes with
// it. Nothing about machinery, nothing about a graph, no capital letter.
//
// IT PROMISES ONLY THE DOWRY, never a shape. Whether the worker splits is the
// worker's own discovery ([Agent.armDivision], task_divide.go) and the roster
// says it when it happens, so a line written before the work starts must not
// spend a promise the work has not made yet.
const taskEscalationNote = "this one wants more hands · handing it over with everything found so far"

// alreadyWorking reports whether THIS answer has already done work: a tool
// result stands in the transcript below the last thing anybody said to this
// agent.
//
// IT IS THE WHOLE OF WHAT MAKES A HANDOFF "MID-TURN", and it is read from the
// transcript rather than kept as a flag because the transcript is the only
// record that cannot fall out of step with itself. A counter reset at the top
// of a turn is a second account of the same fact, and the turn loop has enough
// exits (an interrupt, an overflow retry, a truncation continuation) that one
// of them would eventually leave it saying the wrong thing.
//
// THE CALL'S OWN RESULT IS NOT RECORDED YET when this is asked: a tool runs
// between its assistant message and the tool message that answers it, so
// propose_task on the first step of a turn correctly sees nothing, and a batch
// that pairs a read with a proposal sees nothing either — that batch has not
// learned anything yet. Only a step that FOLLOWS a finished batch is mid-work.
//
// EVERY USER MESSAGE ENDS THE ANSWER, WITH NO EXCEPTIONS. There used to be one:
// the harness's own checkpoint rode this lane as plain user-role text, and a
// reader that took it for somebody speaking would have silenced the mid-answer
// line on exactly the handoffs it caused. The checkpoint does not write into a
// running turn at all any more — it is read beside the turn by a sidecar
// (checkpoint.go) — so the only user-role text below the last thing said is a
// person's own steering, which IS them speaking again.
func (a *Agent) alreadyWorking() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for index := len(a.messages) - 1; index >= 0; index-- {
		switch a.messages[index].Role {
		case "tool":
			return true
		case "user":
			return false
		}
	}
	return false
}

// forgetTask drops a proposal nobody will answer, so a late resolve does not
// deliver into a channel with no reader and the map does not grow for the life
// of the session.
func (a *Agent) forgetTask(id uint64) {
	a.mu.Lock()
	delete(a.taskAnswers, id)
	a.mu.Unlock()
}

// PendingTasks lists the proposals still waiting for an answer, oldest id
// first. It is [Agent.PendingConsent] for the other question: a surface
// redrawing itself mid-turn — a resize, a reattach — needs to know a proposal
// is outstanding without having kept the event.
func (a *Agent) PendingTasks() []uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]uint64, 0, len(a.taskAnswers))
	for id := range a.taskAnswers {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}
