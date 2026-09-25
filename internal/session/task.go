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
// Config.tasker). That is the fan-out law: a step with genuinely INDEPENDENT
// parts is faster as one node per part, each in its own worktree, than as one
// model doing them in order, because the whole then costs the longest part
// rather than their sum however many parts there are. The coordination —
// reading the reports, folding them into one deliverable — stays with the node
// that split the work.
// Work that is sequential, or that shares heavy context, is not split at all:
// the parts would each pay for a worktree, an audit and a wait to save nothing.
//
// TWO BOUNDS, AND THEY ARE DIFFERENT KINDS OF THING. The DEPTH cap is absence:
// a node standing on taskDepthLimit is handed no propose_task at all, because a
// capability that cannot work is left off the belt rather than made to refuse
// (tools.go). The FAN cap is a REFUSAL the model reads and acts on
// ([TaskGraph.claimChild]) — it has already been given its slots, and the answer
// to a part past them is to do it in its own hands. Why the two numbers are
// what they are is written beside them (task_run.go).
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
// surface where every task starts after fifteen seconds, which is the right
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
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
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
var taskDescription = "Hand self-contained work to a task outside this conversation: work that would flood it or wants a clean context, never work needing back-and-forth. A WIDE CHANGE IS ONE PROPOSAL with `wide`, never several, and do not reach for a planner. The person may redirect or wave it off during a short countdown; silence starts it. The id returns at once; its report starts a turn here when it lands, so never wait or poll. A task may call this for genuinely independent parts of its own work, up to " + strconv.Itoa(taskFanLimit) + " each, with tasks nested at most " + strconv.Itoa(taskDepthLimit) + " deep; sequential or context-sharing parts are faster in your own hands. Files another codeaf window is already writing come back on their own line: nothing is blocked, so plan around them. If you will read the result yourself and carry on, and it does not need its own check or its own branch, use quick_task instead — it starts now and costs nothing to land."

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
//
// AND `via` SAYS WHEN IT IS SET, NOT ONLY WHAT IT IS. It opened on "Optional",
// and a model already writing a proposal for an issue in a mature project read
// that as the field to leave out. The hand-off page says codeaf prefers a
// program for the work its guide claims and uses one the person asks for
// (delegate_door.go's [delegateFact]); this is that same preference at the
// moment the field is being filled, in as few bytes as say it. The page stays
// the rule's home: on the lean belt this schema is fetched on demand, and the
// page is all that is read before the model decides to propose at all.
const taskViaSchemaJSON = `"via":{"type":"string","description":"A program your instructions list, to do the whole task alone in ground (or this conversation's folder): set it for work one is for, and when the person names one"},`

var taskSchemaJSON = `{"type":"object","properties":{` +
	`"title":{"type":"string","description":"One line naming the work as a person would say it"},` +
	`"summary":{"type":"string","description":"Two or three lines the person reads to decide whether to redirect it"},` +
	`"brief":{"type":"string","description":"THE WORK, self-contained: what to do, the material and the names in it, constraints, and what you have already found and ruled out. It cannot ask you anything, so settle here everything it would stop and ask. Name the plausible-looking wrong answer and forbid it; every line must be one the worker could disobey. Replacing a failed task, carry its findings here: the new worker inherits neither its transcript nor its report."},` +
	`"deliverable":{"type":"string","description":"What must exist at the end, and where. Name the thing, not the activity"},` +
	`"where":{"type":"string","description":"Path the person named, or 'in place'; never guess"},` +
	`"ground":{"type":"string","description":"Optional absolute path: the repository or folder the work is about, when it is not this conversation's own"},` +
	`"acceptance":{"type":"string","description":"Done when: the observable condition somebody else could check without taking the task's word for it"},` +
	expectsSchemaJSON + `,` +
	checksSchemaJSON + `,` +
	`"depends_on":{"type":"array","items":{"type":"integer"},"description":"Ids that must finish first, only ones propose_task returned in this session. Its brief is given their reports; an unknown or failed id refuses the proposal"},` +
	`"wide":{"type":"boolean","description":"Optional. True when the work is wider than one pair of hands. Say true whenever you judged it broad; a wrong true costs nothing"},` +
	`"model":{"type":"string","description":"Optional, only where the person asked for one: a catalog id or part of one, never a class word, so resolve \"fast\" to a concrete model. A word fitting several is shown to the person to settle"},` +
	`"effort":{"type":"string","enum":["best","cheap"],"description":"Only when the person asked"},` +
	taskViaSchemaJSON +
	`"max_steps":{"type":"integer","description":"Optional. Finished tool calls per progress checkpoint (default ` + strconv.Itoa(taskMaxSteps) + `); work still advancing is given more."},` +
	`"no_progress":{"type":"integer","description":"Optional. Tool calls in a row that may add nothing before it is stopped as stuck (default ` + strconv.Itoa(taskNoProgress) + `). Raise it for work that must read a great deal first"}` +
	`},"required":["title","summary","brief","deliverable","acceptance"],"additionalProperties":false}`

// A build with no program has no usable via argument. Keep the full schema's
// bytes unchanged where the launch registry carries one, and omit this field
// on the same Config.mayDelegate predicate that controls the prompt.
var taskSchemaWithoutViaJSON = strings.Replace(taskSchemaJSON, taskViaSchemaJSON, "", 1)

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
	Expects []Expectation `json:"expects,omitempty"`
	// Checks is the repeatable verification this proposal puts the work under:
	// the only commands its independent checker will be allowed to run
	// (task_checks.go). Optional, and the harness never writes one either.
	Checks    []string `json:"checks,omitempty"`
	DependsOn []uint64 `json:"depends_on"`
	Wide      bool     `json:"wide"`
	Model     string   `json:"model"`
	Via       string   `json:"via"`
	// Effort is the person's one-task word for how hard to try — best or
	// cheap — which moves this task's crew and nothing after it (taskcrew.go).
	Effort     string `json:"effort"`
	MaxSteps   int    `json:"max_steps"`
	NoProgress int    `json:"no_progress"`
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
	named bool
	// ahead is a name that was asked for before this node existed and has not
	// landed yet (taskname.go's [nameAhead]). A road that starts work on its own
	// asks at the moment it decides to, and hands the call over here so that
	// [TaskGraph.nameNode] waits for its answer instead of asking a second time.
	// It is never written to the checkpoint: a name that lands is written into
	// title like any other, and one that never lands leaves the title as it was.
	ahead   *nameAhead
	summary string
	// request is THE PERSON'S OWN MESSAGE, captured by the code that admits this
	// proposal rather than asked of the model (task_brief.go). It is the first
	// thing the node reads, and it is the only part of the spec no model wrote.
	request string
	// origin is an ADDRESS, not inherited context. THE POINTER IS NOT THE
	// SESSION: the node never reads this conversation, and the brief remains
	// the contract. What this names is the filesystem path of the session
	// journal and the line where the person's turn began, so a worker whose
	// restatement was clipped can read the original words itself with the
	// tools it already has. Empty is ordinary — a standing firing, a restored
	// checkpoint written before origins were carried, a test that never set
	// one — and [composeBrief] draws nothing for it.
	origin taskOrigin
	// admission is the WORKING CONTEXT this node was admitted with: bounded
	// quotations of what was said around the work, with who said them, and
	// handles to the calls that already ran (admission.go). Every door compiles
	// it the same way, through [Agent.admissionContext], and no door composes
	// its own.
	//
	// IT IS QUOTATION AND NOT FACT, which is the difference between it and every
	// other field here. The brief is the contract; this is the record the
	// contract came out of, and the document says so where the worker reads it.
	admission AdmissionContext
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
	ground string
	mode   TaskMode
	// frozen is THE WORLD THIS PART IS TO START FROM, set only by the division
	// that admits it (task_divide_wip.go): the commit its parent put the family
	// tree at before any part of it existed. Every other door leaves it empty,
	// which is what "this task is nobody's part" means to the ground ladder.
	frozen     string
	acceptance string
	// expects is the checkable half of the handoff contract: what this brief
	// assumes is already true of the folder the worker will get
	// (handoffcontract.go). It is written by whoever wrote the brief, never by
	// the harness, and an empty one is the ordinary case.
	expects []Expectation
	// checks is the REPEATABLE VERIFICATION this work is put under contract with:
	// the commands anybody could run again to re-establish that it is done. It is
	// the only thing the node's independent checker may run (task_checks.go), it
	// is written by whoever wrote the brief and never by the harness, and an empty
	// one — the ordinary case — means the checker judges by reading rather than by
	// repeating anything the worker happened to do.
	checks    []string
	dependsOn []uint64
	// planID is THE TASK'S ID IN THE PLAN STORE (internal/plandb), set only on
	// a node the bash belt's plandb loop drives (docs/design/plandb-cli/
	// DESIGN.md, the wiring section). It is what makes a node and its plan
	// task one thing: the runtime claims the store task under this id when it
	// dispatches the node, the node's work order is composed FROM the store
	// read, and the pulse writes the node's ending back to the store task.
	// Empty is the ordinary case — every node outside the experiment, and a
	// quick or design or run node under it — and an empty one touches nothing:
	// no seed, no pulse, no plan lines in the worker document.
	planID string
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
	// the session ([Agent.RetargetTask], task_room.go). A settled node saves its
	// continuation choice separately, so its last model remains a historical fact.
	//
	// modelOptions is the shortlist a word that fits more than one model raises.
	// It is on the proposal the person is shown and is empty by the time the node
	// is admitted: [settleTaskModel] closes it with their answer, or with the
	// closest match when the clock does.
	modelWord    string
	model        string
	modelOptions []string
	// via is the delegate this work is proposed for, empty for the conversation's
	// own worker (delegate_door.go). It is resolved at staging, so a name this
	// machine has no delegate for is a refusal before any card goes up.
	via string
	// crewEffort is the one-task crew word the proposal carried — best, cheap,
	// or empty for the router's own knee (taskcrew.go). It is not `effort`
	// below, which is how hard the model thinks, not which models the crew is.
	crewEffort crewroute.Effort
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
	// quick is set on a node that runs WHERE ITS CALLER WORKS: no worktree, no
	// check and no landing, its last message its result (task_quick.go). It is
	// nil on every ordinary task, on every design and on every run, and where it
	// is set [Agent.runTaskNode] hands the node to the quick body — same graph,
	// same room, same stop, a fourth middle.
	//
	// IT IS IN THE CHECKPOINT, unlike [taskSpec.design] and [taskSpec.run]: the
	// list is the node's graph, so the record carries it whole with its ticks
	// (task_store.go's [taskRecord.Quick]) and a node read back is the quick node
	// it was. What a restart does NOT do is run it again — task_store.go's
	// [interrupt] settles every quick node the close caught, because what died
	// with the process is its worker's context and its caller's turn.
	//
	// IT IS NEVER SET BESIDE [taskSpec.drawn]. A drawing is an instruction to
	// divide this work into children; the items ARE the division, done in order
	// by one worker, so a spec carrying both would hand the same parts out twice
	// (checkpoint_quick.go).
	quick *quickTaskSpec
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
	// unshaped says THE BRIEF IS THE PERSON'S SENTENCE STANDING IN for the one
	// the shaper has not written yet, and it is set by exactly one door: a
	// person's own `/task` ([Agent.StartTask]). The shaper is asked beside the
	// node's first worker and the flag is cleared by the one write that replaces
	// the stand-in ([TaskNode.writeBrief], task_shape.go). Every other door
	// admits a brief somebody already wrote and leaves it false.
	unshaped bool
	// unsized says NOBODY HAS READ THIS REQUEST FOR WIDTH, and the sizing judge
	// is to read it beside the node's first worker rather than in front of it
	// (task_divide_sketch.go's [Agent.proposalBeside]). The same one door sets
	// it, unless the person said the work is one worker's — `/task solo`, or a
	// standing answer of `single` — and it is spent the moment the judge is
	// asked, so a node is read for width once whatever happens to it after.
	//
	// BOTH FLAGS ARE IN THE CHECKPOINT ([taskRecord.Unshaped]), and they have to
	// be. A `/task` admitted while the lanes are full sits QUEUED with no worker,
	// so neither reading has happened yet; an engine restart in that window would
	// bring the node back with both flags false and no road left to write its
	// brief, and the work would run on the raw sentence and the canned
	// done-condition for ever, silently. Carrying them is the only thing that
	// makes "beside the worker" survive a restart, and it says exactly what was
	// true when the file was written: what has been read, and what has not.
	unsized bool
}

// taskOrigin is the pointer a worker is handed so it can find the person's
// original words. THE POINTER IS AN ADDRESS, NOT INHERITED CONTEXT: it names
// a file and a line the worker may read, and it does not grant the
// conversation. THE BRIEF REMAINS THE CONTRACT — this is where the words
// live, not a second brief.
type taskOrigin struct {
	journal string
	line    int
}

// empty is the emptiness law: a pointer with no path is not a pointer, and a
// heading over nothing is not written. A path with no line still stands —
// the file is somewhere to look, and a made-up line number is not.
func (o taskOrigin) empty() bool {
	return strings.TrimSpace(o.journal) == ""
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
// ([Agent.openTask]).
//
// AT THE FLOOR THE TOOL IS ABSENT, NOT REFUSING. A node at taskDepthLimit has
// nothing left worth handing out, so it is not given the verb — the law every
// conditional family on this belt is built on (tools.go).
//
// AND IT IS A STAGED TOOL (internal/exec/bare's stage.go): the card and its clock
// are the half of a proposal that can be taken back, and the admission is the
// half that cannot, so a proposal may be put in front of the person while the
// rest of the message that carries it is still arriving ([Agent.stageTask]).
func (a *Agent) taskTools() []bare.Tool {
	if !a.mayProposeTask() {
		return nil
	}
	schema := taskSchemaWithoutViaJSON
	if a.config.mayDelegate() {
		schema = taskSchemaJSON
	}
	return []bare.Tool{bare.StagedTool("propose_task", taskDescription, json.RawMessage(schema), a.stageTask)}
}

// mayProposeTask says whether propose_task belongs on this agent's belt: always
// in a conversation, and in a node only when it was handed the conversation's
// graph to admit into and is not standing on the floor of the tree.
//
// The graph is what the orchestrate run's workers and the auditor are NOT handed
// (task_run.go's newTaskAgent), which is how they end up without the verb
// without anybody writing a second rule about them.
// It is [Config.mayProposeTask] asked of a live agent (beltfacts.go), and it is
// written that way round rather than repeated here because the render step asks
// the CONFIG the same question before there is an agent to ask: the page's
// `tasks` sentences are composed from it, so a floor node is never told to call
// a verb this line has just kept off its belt.
func (a *Agent) mayProposeTask() bool { return a.config.mayProposeTask() }

// mayFanOut is the same question asked of a CONFIG, before there is an agent to
// ask: [renderSystem] decides whether to tell this worker how to split its work
// at construction, and the belt and the prompt must not disagree about whether
// it can.
func (c Config) mayFanOut() bool {
	return c.InTask && c.tasker != nil && fansOutAt(c.taskDepth)
}

// fansOutAt says whether a worker standing this many tasks deep may hand work
// out, and it is the ONE reading of [taskDepthLimit]. The belt asks it of the
// worker itself ([Config.mayFanOut]); the fan-out page asks it of the depth one
// below, so a worker is told whether the pieces it hands out may split in turn
// ([fanoutPage]) by the same line that will build their belts.
func fansOutAt(depth int) bool { return depth < taskDepthLimit }

// proposeTask is the tool's whole life — validate, ask, and admit — for a call
// nobody started early: the two halves ([Agent.stageTask] and
// [stagedProposal.Commit]) run back to back through the one seam every staged
// call passes ([bare.RunStaged]). It is what the belt's own Execute does, named
// for the callers that hold an agent rather than a belt.
//
// Everything it can answer badly is an ordinary tool result rather than a Go
// error, the way every other tool on this belt answers: a brief the model
// forgot to write is a call it can make again, and an error would end the turn
// over a missing field.
func (a *Agent) proposeTask(ctx context.Context, args json.RawMessage) (string, bool, error) {
	return bare.RunStaged(ctx, a.stageTask(ctx, args))
}

// taskHasPlacementContract reports that parsing got far enough to ask where the
// proposed work stands. Missing task fields remain their single, established
// refusal rather than manufacturing a placement question for something that is
// not yet a task.
func taskHasPlacementContract(spec taskSpec) bool {
	return spec.title != "" && spec.summary != "" && spec.brief != "" &&
		spec.deliverable != "" && spec.acceptance != ""
}

// proposalProblems joins independently repairable contract and placement
// problems as sentences. Contract comes first because its corrected words may
// change the placement the proposer chooses.
func proposalProblems(contract string, stand taskStand) string {
	parts := []string{strings.TrimSuffix(strings.TrimSpace(contract), ".")}
	if stand.refusal != "" {
		parts = append(parts, strings.TrimSuffix(strings.TrimSpace(stand.refusal), "."))
	}
	if stand.ask != "" {
		parts = append(parts, strings.TrimSuffix(strings.TrimSpace(stand.ask), "."),
			"Ask the person which, then propose this again with `ground` set to their answer")
	}
	return strings.Join(parts, ". ") + "."
}

// stageTask is the half of a proposal that can be taken back: the arguments
// read, the door refusals asked, the ground resolved, a slot and an id taken,
// and the card put in front of the person with its clock running.
//
// ── WHY THE CARD MAY GO UP BEFORE THE MESSAGE IS WHOLE ──
//
// THE CONSENT IS THE COMMIT. Nothing in this half starts any work: it asks, and
// the person (or the clock) answers, and only [stagedProposal.Commit] admits the
// node on that answer. So when the turn loop sees this call's arguments close
// while the model is still writing the rest of its message — the second and
// third proposals of a batch, typically, a brief of several paragraphs each —
// it starts this half then, and the countdown the person reads runs beside the
// stream instead of after it. If the message then fails to arrive whole, the
// loop withdraws the call ([stagedProposal.Withdraw]) and every trace of it
// goes: the card settles as withdrawn, the question comes off the block, the
// slot is handed back. The id stays spent, as a declined proposal's does, and
// the place the ground ladder resolved stays known (the note at
// [Agent.keepGround]'s call below says why).
//
// WHAT THIS HALF MAY NOT READ is anything the finished message changes. The
// transcript is one of those — the assistant message carrying this call is
// recorded only once it is whole — so what was said around the work is compiled
// in the other half, where it reads exactly what it would have read had nothing
// started early (the quality law of an early start: the same brief, sooner).
func (a *Agent) stageTask(ctx context.Context, args json.RawMessage) bare.Staged {
	spec, problem := parseTaskArguments(args)
	if problem != "" {
		// A CONTRACT PROBLEM DOES NOT HIDE A PLACEMENT PROBLEM. Once the call has
		// enough of a task to place, both readings are already available and each
		// is returned as its own sentence in the order the proposer can repair them.
		if taskHasPlacementContract(spec) {
			problem = proposalProblems(problem, a.resolveTaskGround(spec))
		}
		return bare.Settled(problem, true)
	}
	// THE DOOR REFUSALS, before a card or a slot. A proposal that left out the
	// program the person named, a trivial ask and a depends_on that can never
	// resolve are all "do not start this"; they live in one helper so this
	// road does not grow another ending (complexity_test.go's ratchet on this
	// function).
	if refusal := a.refuseProposedTask(spec); refusal != nil {
		return refusal
	}
	// A PROGRAM'S RUN THAT ENDED DONE IS A DEPENDENCY MET, and the graph knows
	// no node by its id (program_depends.go).
	spec.dependsOn = a.withoutEndedProgramRuns(spec.dependsOn)
	// A DELEGATE IS RESOLVED BEFORE THE CARD, so a name this machine has no
	// delegate for is answered with the names it has and nobody is asked to
	// approve work that could not start (delegate_door.go).
	if spec.via != "" {
		if _, err := a.delegateFor(spec.via); err != nil {
			return bare.Settled(err.Error(), true)
		}
		if !a.mayHandToProgram() {
			return bare.Settled(spec.via+" can only be given work from the conversation, and only where the run road is linked", true)
		}
	}
	// WHICH HANDS THE WORK LEAVES ON, settled before anybody is asked anything
	// (taskmodel.go). A word that names no model this install has is a refusal
	// the model can act on — it names the nearest ids — and one that names
	// several is not refused at all: the shortlist rides on the proposal, and the
	// person settles it in the same breath as the work.
	choice := a.resolveTaskModel(spec.modelWord)
	if spec.via != "" {
		choice = a.resolveProgramModels(spec.modelWord)
	}
	if choice.problem != "" {
		return bare.Settled(choice.problem, true)
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
	// AND WHERE THOSE WORDS LIVE, an address rather than a second copy
	// (task_brief.go). A node proposing a sub-task inherits the same pointer
	// so a nested worker still finds the person's turn, not its parent's
	// journal.
	spec.origin = a.taskOriginRef()
	// AND WHERE THE WORK STANDS, resolved from the evidence this conversation
	// already holds (taskstands.go) before anybody is asked anything, so the card
	// the person answers names the project rather than a folder under a session.
	// Two places with real weight in the evidence are a QUESTION and never a coin
	// toss: the model is handed the person's sentence to put to them, and nothing
	// is admitted until it comes back with an answer.
	stand := a.resolveTaskGround(spec)
	switch {
	case stand.refusal != "":
		return bare.Settled(stand.refusal, true)
	case stand.ask != "":
		return bare.Settled(stand.ask+"\nAsk the person which, then propose this again with `ground` set to their answer.", true)
	}
	spec.ground, spec.mode = stand.dir, stand.mode
	// AND THE CONVERSATION REMEMBERS WHERE IT IS ABOUT. A ground resolved here —
	// the model's own `ground`, the person's answer to the two-places question
	// arriving as the next proposal's argument, or the repository this
	// conversation has plainly been working in — is written onto the session as a
	// referred place (places.go), so the ladder's next climb finds it at SAID and
	// nobody is asked the same question twice.
	//
	// A WITHDRAWN PROPOSAL LEAVES IT, and that is the one thing this half does
	// that its withdrawal does not undo. The place is a fact about the
	// conversation that the call merely noticed — it names where the work is
	// about, never that any work was handed out — and the call that replaces a
	// withdrawn one resolves the same ground from the same evidence.
	a.keepGround(stand)
	graph := a.graph()
	// THE SLOT IS TAKEN BEFORE THE QUESTION and handed back by everything that
	// is not an admission, so a batch of proposals cannot walk through the fan
	// cap together ([TaskGraph.claimChild]).
	if refusal := graph.claimChild(spec.parent); refusal != "" {
		return bare.Settled(refusal, true)
	}
	id := graph.reserve()
	// WHO ELSE IS ALREADY IN THESE FILES, ASKED BEFORE THE MONEY. It is one line
	// or nothing at all (taskpreflight.go), it rides on the proposal so the person
	// reads it on the card while the countdown is still running, and it comes back
	// on the result so the model can sequence its next proposal around it. NOTHING
	// IS PREVENTED BY IT: the work starts on the same answer it would have started
	// on, because a claim another window wrote is evidence and never an
	// instruction.
	elsewhere := a.taskPreflight(spec.title, spec.summary, spec.brief, spec.deliverable, spec.acceptance)
	wait, err := a.openTask(ctx, id, spec, elsewhere)
	if err != nil {
		// The session closed under the question. Nothing was asked and nothing
		// admitted, so the proposal simply never became one.
		graph.releaseChild(spec.parent)
		return bare.Settled(taskNeverAnswered, true)
	}
	return &stagedProposal{agent: a, graph: graph, id: id, spec: spec, stand: stand, elsewhere: elsewhere, wait: wait}
}

// taskNeverAnswered is the result of a proposal whose turn ended under its
// question: nothing was admitted, so there is no node to cancel.
const taskNeverAnswered = "the task was never answered: the turn ended first"

// stagedProposal is one proposal on the card with its clock running, and
// everything its admission will need: the half [Agent.stageTask] built and the
// half [stagedProposal.Commit] finishes.
type stagedProposal struct {
	agent     *Agent
	graph     *TaskGraph
	id        uint64
	spec      taskSpec
	stand     taskStand
	elsewhere string
	wait      *taskWait
}

// Withdraw takes the proposal back before anybody's answer admitted it: the card
// and its question go with the reason, and the slot is handed back. It is the
// other ending of [stagedProposal.Commit], and exactly one of the two runs.
func (p *stagedProposal) Withdraw() {
	p.wait.withdraw()
	p.graph.releaseChild(p.spec.parent)
}

// Commit reads what the wait came to and admits on it. It runs once the message
// that carried the call is whole and recorded — at once, for a call nobody
// started early — so everything it reads about the conversation is what the
// batch would have read. The answer may already be in: a person who said no, or
// a clock that ran out, while the message was still arriving, is read here in
// the order it happened ([Agent.openTask]).
func (p *stagedProposal) Commit(ctx context.Context) (string, bool, error) {
	a, spec, graph, elsewhere := p.agent, p.spec, p.graph, p.elsewhere
	admitted := false
	defer func() {
		if !admitted {
			graph.releaseChild(spec.parent)
		}
	}()
	answer, err := p.wait.answer()
	if err != nil {
		// The turn ended under the question. Nothing was admitted, so there is
		// no node to cancel and nothing to clean up — the proposal simply never
		// became one.
		return taskNeverAnswered, true, nil
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
	// AND WHAT WAS SAID AROUND THE WORK, compiled by the one compiler every door
	// uses (admission.go), and compiled HERE, in the half that cannot run before
	// the message is whole. A proposal made mid-answer is where this matters most:
	// the calls this turn has already made, the words the model wrote above this
	// very call, and the constraint the person typed two turns ago are all in
	// the transcript by now and in neither the brief nor the request.
	spec.admission = a.admissionContext()

	// AN APPROVED HAND-OFF UNDER THE BASH BELT IS A RUN, NEVER A SESSION-TREE
	// NODE. It keeps the id the card showed, carries its acceptance in the brief
	// and its depends_on as the store's own dependencies, and takes the person's
	// ask with it when this turn owes one (CHAT-ROLE.md, "A landing speaks only
	// when an answer is owed"). A task about ANOTHER FOLDER than the work
	// already underway is refused here ([standsElsewhereError]).
	//
	// AND A RUN ROAD THAT FAILS IS SAID, NOT HIDDEN. Any other failure of the run
	// road used to fall through to the older engine's tree, which is the one
	// thing this comment's first line says an approved hand-off never becomes:
	// a batch of eight approved at once raced to one store and six of them
	// quietly became old-tree nodes. The receipt now says the task did not start
	// and why ([runDidNotStart]), and reads as a failure. Only a run road that
	// is not there at all (no engine linked, no place for a store) leaves this
	// door for the older one ([Agent.commitProposalToRun]).
	ctx = withCrewWish(ctx, crewWish{effort: spec.crewEffort, worker: namedModel(spec)})
	if answer, refused, handled := a.commitProposalToRun(ctx, p, spec, elsewhere); handled {
		return answer, refused, nil
	}
	state := graph.admit(p.id, spec)
	admitted = true
	return taskReceipt(p.id, spec, state, p.stand, elsewhere), false, nil
}

// commitProposalToRun is the run road of an approved proposal: an approved
// hand-off under the bash belt, and every hand-off that names a delegate, is a
// RUN and never a session-tree node. It keeps the id the card showed, carries
// its acceptance in the brief and its depends_on as the store's own
// dependencies, and takes the person's ask with it when this turn owes one
// (CHAT-ROLE.md, "A landing speaks only when an answer is owed"). It answers
// handled=false when this proposal is not the run road's — the shipped engine
// admits it then — and handled=true with the model's answer otherwise.
//
// A task about ANOTHER FOLDER than the work already underway is refused here
// ([standsElsewhereError]). Any other failure ON the run road is said, as a
// task that did not start ([runDidNotStart]); only a run road that is not there
// at all ([errRunRoadUnavailable]) leaves an ordinary hand-off to the shipped
// engine. A DELEGATE HAS NO OTHER ROAD: the shipped engine would seat a worker
// of its own on the brief, which is not what was asked for, so its failure is
// answered as a refusal.
func (a *Agent) commitProposalToRun(ctx context.Context, p *stagedProposal, spec taskSpec, elsewhere string) (string, bool, bool) {
	if !(bashBeltAsked() || spec.via != "") || chatRunEngine == nil || a.config.InTask {
		return "", false, false
	}
	a.mu.Lock()
	question := questionAtTaskHandoff(a.owedAsks)
	a.mu.Unlock()
	var via *delegate.Delegate
	if spec.via != "" {
		m, err := a.delegateFor(spec.via)
		if err != nil {
			return err.Error(), true, true
		}
		via = &m
	}
	description := composeBrief(briefWhole, spec.request, spec.brief, spec.deliverable, spec.acceptance, "", spec.admission, spec.origin, taskCopy{})
	// THE RUN OUTLIVES THE TURN THAT LAUNCHED IT, AND NOT THE CONVERSATION.
	// This context is the turn's, and the turn cancels it on its way out
	// (agent.go, `defer cancel(nil)`); a run driven under it would be stopped
	// the moment the model finished its sentence. The values ride along, the
	// cancellation does not. What ends it instead is the conversation: the
	// person's stop (stoprun.go) or the room closing ([Agent.cutBeltRun]).
	//
	// WHETHER IT JOINED is the start door's answer and not a look taken
	// before it: in a batch committed at one moment none of the hand-offs
	// could see a live run beforehand, and the one that opened the run is
	// decided under the start lock ([Agent.startOrJoinTaskRunVia]).
	stand := p.stand
	if via != nil {
		stand = delegateStand(stand.dir)
	}
	asked := programAsked(spec)
	var prior *programOutcome
	if via != nil {
		prior = a.keepProgramAttempt(p.id, a.programAttemptOf())
	}
	joined, err := a.startOrJoinTaskRunVia(context.WithoutCancel(ctx), p.id, spec.title, description, spec.dependsOn, stand, question, via, asked...)
	a.rollbackFailedProgramStart(via, p.id, prior, err)
	if refusal := (standsElsewhereError{}); errors.As(err, &refusal) {
		return refusal.Error(), true, true
	}
	if err == nil {
		receipt := taskReceipt(p.id, spec, TaskRunning, p.stand, elsewhere)
		switch {
		case via != nil:
			receipt = delegateStartedReceipt(p.id, spec.title, strings.Join(asked, ", "), delegateReceipt(canonicalPath(stand.dir), *via, a.runRowCopy(p.id)), elsewhere)
		case joined:
			receipt = withReport(receipt, "It joined the work already underway and shares its copy.")
		}
		return receipt, false, true
	}
	if via != nil {
		return via.Name + " could not start: " + err.Error(), true, true
	}
	if !errors.Is(err, errRunRoadUnavailable) {
		return withElsewhere(runDidNotStart(p.id, err), elsewhere), true, true
	}
	return "", false, false
}

// namedModel is the model a hand-off named for its work, and nothing when it
// named none — a task that names nothing is the crew's to seat.
func namedModel(spec taskSpec) string {
	if spec.modelWord == "" {
		return ""
	}
	return spec.model
}

// taskReceipt is what an admitted proposal hands back to the model.
//
// THE MODEL IS NAMED BACK ONLY WHEN IT WAS ASKED FOR. A word resolves to an id
// and a shortlist is settled by somebody else, so the one thing the model cannot
// know after this call is what its own argument came to; a task that named no
// model has nothing to be told, and a receipt reciting the default every time
// would be a line nobody reads.
func taskReceipt(id uint64, spec taskSpec, state TaskState, stand taskStand, elsewhere string) string {
	on := ""
	if spec.modelWord != "" && spec.model != "" {
		on = " on " + spec.model
	}
	if state == TaskQueued {
		result := fmt.Sprintf("task %d queued%s: %s\nIt starts when the work it waits on has finished and a slot is free. %s", id, on, spec.title, taskHandoffWakeSentence)
		return withElsewhere(withReport(withReport(result, taskStandSentence(stand)), stand.redirect), elsewhere)
	}
	result := fmt.Sprintf("task %d started%s: %s\nIt works from the brief alone, in a copy of its own. %s", id, on, spec.title, taskHandoffWakeSentence)
	return withElsewhere(withReport(withReport(result, taskStandSentence(stand)), stand.redirect), elsewhere)
}

// taskStandSentence says WHERE the task works whenever that was read from the
// proposal itself, and says nothing otherwise.
//
// IT IS SAID IN A PERSON'S WORDS AND NEVER AS A RUNG'S NAME. `brief` and `said`
// are how a log spells which step of the ladder answered; the receipt is read
// by the model in front of the person and is theirs to open, so it names the
// folder and whose word put the work there. A `ground` the refusal never
// offered is accepted (it is somebody saying where the work is), and this line
// is what makes a wrong one visible in the same breath rather than when the
// work lands somewhere nobody looked. The rungs that read the conversation
// instead are silent here, as they always were: the work went where the
// conversation already is.
func taskStandSentence(stand taskStand) string {
	switch {
	case stand.dir == "":
		return ""
	case stand.rung == taskGroundBrief:
		return "It works in " + stand.dir + ", the one folder its brief names the work in."
	case stand.rung == taskGroundSaid && !stand.kept:
		return "It works in " + stand.dir + ", the folder this proposal gave as its ground."
	}
	return ""
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
		via:        strings.TrimSpace(parsed.Via),
		maxSteps:   parsed.MaxSteps,
		noProgress: parsed.NoProgress,
	}
	if word := strings.TrimSpace(parsed.Effort); word != "" {
		effort, ok := crewroute.ParseEffort(word)
		if !ok {
			return spec, "Invalid arguments: effort is best or cheap, or left out"
		}
		spec.crewEffort = effort
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
	expects, expectationProblem := parseExpectations(parsed.Expects)
	spec.expects = expects
	// AND THE VERIFICATION, on the same terms and for the same reason: it is the
	// other optional half of the contract, and a check nobody could run is worth
	// saying out loud here where the model can still fix it (task_checks.go's
	// [declaredCheckList]). Reading it even when an expectation is malformed lets
	// one refusal carry every independently repairable contract problem.
	checks, checkProblem := declaredCheckList(parsed.Checks)
	spec.checks = checks
	problems := make([]string, 0, 2)
	if expectationProblem != "" {
		problems = append(problems, expectationProblem)
	}
	if checkProblem != "" {
		problems = append(problems, checkProblem)
	}
	if len(problems) == 1 {
		return spec, problems[0]
	}
	if len(problems) > 1 {
		for i := range problems {
			problems[i] = strings.TrimSuffix(strings.TrimSpace(problems[i]), ".")
		}
		return spec, strings.Join(problems, ". ") + "."
	}
	return spec, ""
}

// ── the proposal's admission ────────────────────────────────────────────────

// taskQuestion owns both ways a pending proposal can change while its
// [taskWait] is waiting. Holding closes hold exactly once but keeps answer alive, because
// typing removes the clock rather than answering the question.
type taskQuestion struct {
	answer chan TaskAnswer
	hold   chan struct{}
	held   bool
	notice TaskNotice
}

// ResolveTask answers one EventTaskProposal. A surface hands back the id the
// event carried and what the person said about it.
//
// An id nobody is waiting on — a proposal the clock already approved, a second
// click, a turn that was interrupted — is IGNORED rather than reported, exactly
// as [Agent.ResolveConsent] ignores a late answer. The answer is simply late,
// and the surface has already seen the node start or the turn end.
func (a *Agent) ResolveTask(id uint64, answer TaskAnswer) {
	a.mu.Lock()
	question, waiting := a.taskAnswers[id]
	if waiting {
		delete(a.taskAnswers, id)
	}
	a.mu.Unlock()
	if !waiting {
		return
	}
	// Buffered to one and read at most once, so this never blocks and never
	// needs the lock held across it.
	question.answer <- answer
}

// HoldTask removes the admission clock from one pending proposal without
// answering it. The updated proposal is broadcast through the turn's ordinary
// event lane so every watching surface clears the same deadline.
func (a *Agent) HoldTask(id uint64) {
	a.mu.Lock()
	question, waiting := a.taskAnswers[id]
	if !waiting || question.held {
		a.mu.Unlock()
		return
	}
	question.held = true
	question.notice.Deadline = time.Time{}
	notice := question.notice
	hub := a.hub
	close(question.hold)
	a.mu.Unlock()

	if hub != nil {
		hub.send(Event{Kind: EventTaskProposal, Tool: "propose_task", Task: &notice})
	}
}

// openTask puts one proposal in front of the person, starts its clock, and
// starts waiting for its answer ([taskWait]). It is the half of a proposal that
// can still be taken back: the admission, under the lock — the clock's law, the
// card, and the wait registered where [Agent.ResolveTask] and [Agent.HoldTask]
// will find it — the announcement, to the other windows and to this one, and
// the wait itself, which runs from here. Reading what the wait came to is
// [taskWait.answer], and taking it all back is [taskWait.withdraw].
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
// THE WAIT RUNS FROM THE MOMENT THE CARD GOES UP, and never from the moment
// somebody reads its answer. The card can go up while the message carrying the
// call is still arriving ([Agent.stageTask]), and the deadline on it is the one
// the engine keeps: a wait that began only when the message was whole would
// find the person's answer and the expired clock both waiting for it, and would
// choose between them by chance rather than by which came first. So the order
// the person acted in is the order the engine reads, exactly as it was when
// nothing could start early.
//
// elsewhere is the preflight's one line about other windows already in these
// files, or "" — a fact the card draws beside the work, not a reason to wait.
func (a *Agent) openTask(ctx context.Context, id uint64, spec taskSpec, elsewhere string) (*taskWait, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, errAgentClosed
	}
	// The turn's hub, read under the same lock that registers the wait: a tool
	// runs inside a turn, and the turn's fan-out is where its question is seen.
	hub := a.hub
	clock, countdown := a.taskClock(hub)
	var deadline time.Time
	if clock {
		deadline = a.taskClockNow().Add(countdown)
	}
	question := newTaskQuestion(id, spec, elsewhere, deadline, a.config)
	if spec.via == "senior-dev" {
		question.notice.Ceiling = a.seniorDevCeilings(a.usage.CostUSD).Summary()
	}
	if a.taskAnswers == nil {
		a.taskAnswers = make(map[uint64]*taskQuestion, 1)
	}
	a.taskAnswers[id] = question
	a.mu.Unlock()

	// AND ANOTHER WINDOW LEARNS WHAT THIS ONE IS STOPPED ON (taskpresence.go).
	// The line is the title, because the title is what the person on the card is
	// deciding about; a proposal has a summary and a brief as well, and neither
	// belongs in a file every window re-reads every few seconds.
	//
	// IT IS BANKED EVEN WHEN THE CLOCK IS RUNNING. An answer that arrives after
	// the countdown approved is late, and late answers are dropped by
	// [Agent.ResolveTask] exactly as they are when a click lands a moment too
	// slowly in the card's own window.
	//
	// AND IT IS BANKED WHOLE (question.go). The proposal is the one question in
	// this engine with a clock, and the clock APPROVES; a window drawing only
	// the line and two chips could not say so, and a person who left it alone
	// was told nothing about what leaving it alone would do.
	proposed := a.proposalAsk(id, question.notice)
	// The card that carries the brief is the lane's own announcement, and the
	// question goes out after it on EventQuestion's own ordering law
	// ([Agent.raiseQuestion] keeps it, so no lane emits the raise itself).
	forget := a.presenceAskingWhole(proposed, func() { a.announceTask(hub, question) })
	wait := &taskWait{
		agent: a, id: id, question: question, proposed: proposed,
		settled:   make(chan taskSettled, 1),
		withdrawn: make(chan struct{}),
		finished:  make(chan struct{}),
	}
	var expiry <-chan time.Time
	stopTimer := func() {}
	if clock {
		expiry, stopTimer = a.taskClockTimer(countdown)
	}
	go wait.run(ctx, expiry, stopTimer, forget)
	return wait, nil
}

// taskWait is one proposal standing in front of the person, and the wait for
// its answer, which runs on a goroutine of its own from the moment the card
// goes up ([taskWait.run]). It ends one of two ways — its answer is read
// ([taskWait.answer]) or it is taken back ([taskWait.withdraw]) — and never
// both, which is [bare.Staged]'s own contract passed down.
//
// THE GOROUTINE DOES NOT OUTLIVE THE PROPOSAL. It returns on the first of the
// person's answer, the clock, the end of the turn and a withdrawal, and
// [taskWait.withdraw] waits for it to have returned.
type taskWait struct {
	agent    *Agent
	id       uint64
	question *taskQuestion
	proposed Question
	// settled carries what the wait came to, once; a withdrawn wait sends
	// nothing on it.
	settled chan taskSettled
	// withdrawn is closed by [taskWait.withdraw] and finished by
	// [taskWait.run] as it returns.
	withdrawn    chan struct{}
	withdrawOnce sync.Once
	finished     chan struct{}
}

// taskSettled is what one proposal's wait came to: the answer, or the end of
// the turn under it.
type taskSettled struct {
	answer TaskAnswer
	err    error
}

// run is the wait: the person's answer, the person's first typed rune, the
// clock, the end of the turn, or the proposal being taken back, whichever
// comes first. The clock is stopped exactly once however it ends, and the
// presence row and the question's words come down with it.
func (w *taskWait) run(ctx context.Context, expiry <-chan time.Time, stopTimer func(), forget func()) {
	defer close(w.finished)
	defer forget()
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(stopTimer) }
	defer stop()
	hold := (<-chan struct{})(w.question.hold)

	for {
		select {
		case answer := <-w.question.answer:
			w.decided(answer)
			w.settled <- taskSettled{answer: answer}
			return
		case <-hold:
			// A nil select case is disabled. Once a first rune holds the clock,
			// expiry cannot admit the work and a deleted draft cannot restore it.
			stop()
			expiry = nil
			hold = nil
		case <-expiry:
			if !w.agent.taskSilenceAdmits(w.id, w.question) {
				stop()
				expiry = nil
				continue
			}
			// Silence is a yes, and it is a yes with no redirect: the person said
			// nothing, so nothing is appended to the brief.
			silent := TaskAnswer{Approved: true}
			w.decided(silent)
			w.settled <- taskSettled{answer: silent}
			return
		case <-ctx.Done():
			w.agent.forgetTask(w.id)
			w.settled <- taskSettled{err: ctx.Err()}
			return
		case <-w.withdrawn:
			return
		}
	}
}

// decided restates the card with the answer stamped on it, and it is the SAME
// rebroadcast [taskWait.withdraw] makes for the other ending — the one place a
// proposal stops being a question is the one place that says so out loud.
//
// IT IS HERE AND NOT AT EITHER DOOR because both doors arrive here: a person's
// answer comes off the wait's own channel and the countdown's silence is
// manufactured a few lines above, and a restatement written at [Agent.ResolveTask]
// would be a card that says nothing when the clock decides.
//
// WHAT IT IS FOR IS THE REPLAY ([TaskNotice.Decided]). A window watching this
// turn has already drawn its own verdict from the key it pressed; what this
// rebroadcast changes is what the turn's backlog hands the NEXT surface to
// attach, which is the assignment with an answer under it rather than the
// question a second time. The deadline goes with it for [taskWait.withdraw]'s
// reason: nothing is counting any more.
func (w *taskWait) decided(answer TaskAnswer) {
	a := w.agent
	a.mu.Lock()
	hub := a.hub
	notice := w.question.notice
	a.mu.Unlock()
	if hub == nil {
		return
	}
	notice.Deadline = time.Time{}
	notice.Decided = &answer
	hub.send(Event{Kind: EventTaskProposal, Tool: "propose_task", Task: &notice})
}

// answer is what the wait came to, waiting for it if it has not come to
// anything yet. The wait watches the turn's own context, so a turn that ends
// under the question ends this too.
func (w *taskWait) answer() (TaskAnswer, error) {
	settled := <-w.settled
	return settled.answer, settled.err
}

// taskWithdrawnReason is why a proposal stopped being a question when the call
// that raised it was taken back: it went up while its message was still
// arriving, and the message then did not go through — cut off and asked for
// again, stopped or steered by the person, or held back by the harness
// (internal/exec/bare's stage.go). It is the engine's sentence for the
// question's withdrawal and the card's settling alike, so every window retires
// the proposal in one set of words, and it names no cause because the card
// cannot know which of those it was.
const taskWithdrawnReason = "the reply that proposed it did not go through"

// withdraw takes the proposal back before anybody's answer admitted it, and
// returns once it is gone everywhere.
//
// THE ORDER IS THE POINT. The wait is forgotten first, so an answer that lands
// a moment later finds nothing to answer and is dropped as any late answer is.
// The question is withdrawn next, in this ending's own words, before the wait
// lets go of it — letting go withdraws whatever is still standing, and would
// say the clock started the work ([questionGoneReason]). Then the wait is
// stopped and waited for, and only then is the card settled, so nothing the
// wait does can arrive after the sentence that says it is over.
//
// A proposal the person had already answered, or whose clock had already run
// out, is taken back all the same. The answer was about a call that is not
// going ahead, and the call that replaces it will be put to them afresh;
// admitting on the old answer would start work the conversation has no record
// of anybody asking for.
func (w *taskWait) withdraw() {
	a := w.agent
	a.forgetTask(w.id)
	a.WithdrawQuestion(QuestionTask, w.proposed.Token(), taskWithdrawnReason)
	w.withdrawOnce.Do(func() { close(w.withdrawn) })
	<-w.finished
	a.mu.Lock()
	hub := a.hub
	notice := w.question.notice
	a.mu.Unlock()
	notice.Deadline = time.Time{}
	notice.Withdrawn = taskWithdrawnReason
	if hub != nil {
		hub.send(Event{Kind: EventTaskProposal, Tool: "propose_task", Task: &notice})
	}
}

// taskClock is the clock's law from [Agent.openTask]'s comment, decided once
// per proposal: whether this one has a deadline at all, and how long it runs.
// Called with a.mu held, because whether anybody is watching is read off the
// turn's hub under the same lock that registers the wait.
func (a *Agent) taskClock(hub *eventHub) (clock bool, countdown time.Duration) {
	watched := a.config.AskConsent && hub != nil
	countdown = time.Duration(a.config.TaskAutoApproveSeconds) * time.Second
	if countdown < 0 {
		countdown = 0
	}
	return watched && countdown > 0 || !watched, countdown
}

// newTaskQuestion is the proposal as every surface will see it, and the two
// channels the person's answer and the person's typing come back on. The
// notice is built whole here, once, because [Agent.HoldTask] rebroadcasts it
// with only the deadline changed and must not have to rebuild it.
func newTaskQuestion(id uint64, spec taskSpec, elsewhere string, deadline time.Time, config Config) *taskQuestion {
	return &taskQuestion{
		answer: make(chan TaskAnswer, 1),
		hold:   make(chan struct{}),
		notice: TaskNotice{
			ID:         id,
			Title:      spec.title,
			Summary:    spec.summary,
			Brief:      spec.brief,
			Acceptance: spec.acceptance,
			Where:      taskCardWhere(config, id, spec),
			Ground:     spec.ground,
			Mode:       spec.mode,
			DependsOn:  spec.dependsOn,
			Deadline:   deadline,
			// What it will run on, and — when one word fit more than one model
			// — what it could run on instead. A surface draws the first as a
			// fact and offers the second as a choice; both are settled by the
			// answer the wait is waiting for.
			Model:        firstTaskModel(spec.modelOptions, spec.model),
			ModelOptions: append([]string(nil), spec.modelOptions...),
			Elsewhere:    elsewhere,
			// AND WHICH PROGRAM THE WORK IS GOING TO, so the card names it before
			// anybody is asked to say yes ([TaskNotice.Program]). The name was
			// resolved at staging, so it is always one this build carries.
			Program: spec.via,
		},
	}
}

// announceTask puts the card in front of the person, when there is a turn to
// put it in. A nil hub is a headless run, and a headless run has nobody to
// show a card to; the clock admits the work on its own.
func (a *Agent) announceTask(hub *eventHub, question *taskQuestion) {
	if hub == nil {
		return
	}
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
	notice := question.notice
	hub.send(Event{
		Kind: EventTaskProposal,
		Tool: "propose_task",
		Task: &notice,
	})
}

// taskSilenceAdmits is what an expired clock has to ask before it may approve
// anything: a timer that fired in the same instant as a hold consults the held
// state under the lock, and a held proposal keeps its wait registered. One
// that was not held is forgotten here, under the same lock, so a late answer
// finds nothing to answer.
//
// AND SILENCE IS ONLY SILENCE WHILE NOBODY HAS ANSWERED. [Agent.ResolveTask]
// takes the wait off this map under this lock and then sends the answer, so a
// proposal that is no longer registered is one somebody has ALREADY decided —
// their answer is on its way to the wait, which will read it on the next turn
// of its loop. Without this line the two are a coin toss: a select with the
// answer and the clock both ready picks either, and a person's "no" seconds
// before the deadline could be overruled by the countdown they beat. Seen three
// times in ten runs of
// [TestANoGivenWhileTheReplyArrivesBeatsTheClockThatFollows] under the race
// detector.
func (a *Agent) taskSilenceAdmits(id uint64, question *taskQuestion) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if question.held {
		return false
	}
	if _, waiting := a.taskAnswers[id]; !waiting {
		return false
	}
	delete(a.taskAnswers, id)
	return true
}

func (a *Agent) taskClockNow() time.Time {
	if a.taskNow != nil {
		return a.taskNow()
	}
	return time.Now()
}

func (a *Agent) taskClockTimer(after time.Duration) (<-chan time.Time, func()) {
	if a.taskTimer != nil {
		return a.taskTimer(after)
	}
	timer := time.NewTimer(after)
	return timer.C, func() { timer.Stop() }
}

// taskCardWhere is the card's `where`. A PROGRAM'S CARD NAMES ITS PROJECT: the
// folder it will work in itself, and that it gets a branch of its own there
// when the folder is a repository ([programPlace]). A card that showed a path
// under codeaf's state asked a person to approve work going somewhere they had
// never heard of.
func taskCardWhere(config Config, id uint64, spec taskSpec) string {
	for _, program := range config.Delegates {
		if program.Name == spec.via && strings.TrimSpace(spec.ground) != "" {
			return programPlace(program, spec.ground)
		}
	}
	return taskWhereNotice(config.Place, config.Workspace, id, spec.where, spec.mode)
}

func taskWhereNotice(place Place, workspace string, id uint64, where string, mode TaskMode) string {
	where = strings.TrimSpace(where)
	redirectedInPlace := false
	if strings.EqualFold(where, "in place") {
		_, redirectedInPlace = whereInsideRepository(where, workspace)
	}
	if where != "" && (mode == TaskModeWorktree || strings.EqualFold(where, "in place") && redirectedInPlace) {
		if trees := place.Trees(); trees != "" {
			return filepath.Join(trees, strconv.FormatUint(id, 10))
		}
		return "task folder"
	}
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

// proposalAsk is one task proposal as [Question] — the same moment
// [Agent.announceTask]'s EventTaskProposal describes, in the object every lane
// now speaks.
//
// IT IS [Agent.proposalQuestion] AT THE MOMENT OF ASKING, and the two are one
// function on purpose: that one builds this question from the wait when a
// surface asks what is open, and this one banks it when the wait is made. A
// second spelling here would be a second account of the same proposal, and the
// clock is exactly the field the two would drift on.
func (a *Agent) proposalAsk(id uint64, notice TaskNotice) Question {
	return a.proposalQuestion(id, notice)
}
