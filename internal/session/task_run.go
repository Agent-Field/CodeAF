package session

// The executor: a graph of work, a frontier, and one node's life inside a
// worktree.
//
// ── THE SHAPE IS A GRAPH, TODAY IT HOLDS ONE NODE ──
//
// [TaskGraph] is nodes and edges; [TaskGraph.runFrontier] is the whole
// scheduler. A node whose dependencies are all done is RUNNABLE; runnable nodes
// start until the concurrency cap is full and the rest wait on the frontier; a
// node that finishes unlocks its dependents and its report becomes part of
// their briefs. What v1 exercises is a graph of one node with no edges, and
// nothing here is written for that case: when decomposition lands it adds EDGES
// INTO THIS EXECUTOR, not a second machine, and the only thing that changes is
// that depends_on stops being empty.
//
// THE BRIEF IS ASSEMBLED WHEN THE NODE STARTS, not when it was proposed. That
// is the point of doing it here: a dependent's prerequisites have run by then,
// so what it is handed is its own brief plus what the work before it learned —
// facts that did not exist at proposal time.
//
// ── WHY A WORKTREE ──
//
// A node edits files while the person is editing files. Sharing a checkout
// would mean the node's half-finished sweep is what the person's `go build`
// compiles, and a node killed mid-edit would leave its wreckage in their tree.
// So each node gets `git worktree add` on a branch off the person's current
// HEAD: it works in a directory of its own, and the work comes home as a MERGE
// — clean, or a conflict that keeps the branch and says so. A workspace that is
// not a repository has no such isolation to offer, and the node runs in place
// rather than pretending (Merge is "inplace", and the person is told).
//
// ── A NODE STOPS BY A NAMED THRESHOLD, NEVER BY WANDERING ──
//
// Three things end a node's work, and every one of them has a name the person
// can read: its deadline, its step budget, and its no-progress count
// (harness-research-notes.md §1 — Argus terminates on named thresholds rather
// than on a reviewer's judgement, arXiv:2608.05144). A node that has taken
// forty steps or has taken six in a row without changing a file is not working,
// it is circling, and the difference between a harness that says "stopped: 6
// steps without progress" and one that lets the thirty-minute deadline collect
// it is half an hour of the person's money and a report that explains nothing.
// Both thresholds are per-node overridable on the wire (task.go's max_steps and
// no_progress) because the right budget for a one-file rename and for a sweep
// across forty files is not the same number.
//
// ── AND IT IS NEVER THE NODE THAT SAYS IT IS DONE ──
//
// A finished run is a CLAIM. What turns it into TaskDone is an independent
// read-only auditor with hard evidence (task_audit.go): VERIFIED merges,
// REFUTED is a TaskFailed carrying the auditor's evidence, and the cascade
// below fails its dependents with it. Nothing in this file writes TaskDone off
// the child's own last words.
//
// And when the auditor gives NO verdict — twice, having been asked again — the
// node lands TaskUnverified: not done, not failed, branch kept, dependents
// waiting rather than cascading, until a person resolves it
// ([Agent.ResolveUnverified]). A false failure is still a failure, and this
// file must not manufacture one out of an audit that never happened.
//
// ── WHY A NODE NEVER ASKS ──
//
// There is nobody to ask. Its approval posture is composed from the same
// [approval.Policy] the conversation uses — allow everything, with the critical
// table (rm -rf /, mkfs, a redirect onto a raw disk, shutdown) still a floor
// under it — and where a conversation would raise a question, the node reads a
// refusal it can act on. That is exactly consent.go's law for a headless run,
// said in the node's own words.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// taskDeadline bounds one node's whole life. An hour is a long build, the
	// reading around it, and the audit after it; past that the node is not
	// working, it is stuck, and a stuck node that never ends holds a slot and
	// a worktree forever. The deadline is the LEASH, not the budget: the
	// auditor is what says a node is done.
	taskDeadline = 60 * time.Minute

	// tasksDirName is where the worktrees live in the LEGACY layout: beside the
	// job logs and under the repository, so `git worktree list` and a person's
	// file browser both find them where they were left.
	//
	// A session that has a folder of its own puts them in [Place.Trees] instead
	// (Decision 26). Git registers every worktree in .git/worktrees whatever its
	// path, so repo-local placement was never a constraint — it was only where
	// the first version happened to put them, and it is litter in somebody
	// else's repository.
	tasksDirName = ".aforge-v3/tasks"

	// taskReportLines and taskReportLineLimit bound the report. Two or three
	// lines is what a person reads off a finished card and what a dependent's
	// brief can afford to carry; the whole story is in the node's journal.
	taskReportLines     = 3
	taskReportLineLimit = 300

	// taskSlugLimit keeps a branch name readable in `git branch`.
	taskSlugLimit = 32

	// taskMaxSteps and taskNoProgress are the two named thresholds a node stops
	// by, counted in the only unit this side of the wall can see: the child's
	// finished tool calls. They are the LEASH, not the budget — the auditor is
	// what says a node is done, and a healthy node never meets either number.
	//
	// Two hundred steps is liberal on purpose: a real task reads, builds, fixes
	// and verifies, and a step cap that bites during honest work is how you get
	// "aborted" on a node that was about to finish. Six consecutive steps with
	// no NEW information and no new dirt is the shape of a spin — the detector
	// reads novelty now, so it only fires when a node is genuinely re-treading
	// the same call, and it fires in a minute rather than in thirty.
	taskMaxSteps   = 200
	taskNoProgress = 6
	// Four renewals plus the original allowance make five equal budgets: 1000
	// steps or five hours by default, and five times a wire max_steps value.
	taskMaxExtensions = 4

	// taskDepthLimit is how many tasks deep the tree may go, and taskFanLimit is
	// how many sub-tasks ONE node may hand out. They are the two bounds on
	// decomposition (task.go's fan-out law), and each answers a different way
	// for it to run away.
	//
	// TWO LEVELS, because the third has nothing left to divide. The conversation
	// grooms a piece of work; that node finds two or three genuinely independent
	// parts inside it and hands them out; a part of a part is a step, and a step
	// belongs in the hands that are already holding it. A deeper tree also costs
	// what nobody sees: every level adds a worktree, an audit and a wait, so the
	// third level is where fanning out starts being slower than working.
	//
	// FIVE CHILDREN, because a node handing out more than that has not
	// decomposed its work, it has shredded it — and it still has to read every
	// one of their reports and make one deliverable out of them. The cap is a
	// REFUSAL the model can read (task.go), not a queue: the answer to "I have
	// eight parts" is to do some of them, and the refusal says so.
	taskDepthLimit = 2
	taskFanLimit   = 5
)

// The three things a node can be waiting on, spelled once. They are the
// strings [TaskNotice.Waiting] carries, and every one of them is a word about
// the WORK rather than about the machinery that produced it: a person reading
// a card wants to know their task is not moving and roughly whose fault that
// is, and "429", "AIMD" and "governor" are three ways of telling them about
// this package instead.
const (
	// waitingSlot is the person's own task.parallel cap holding a ready node.
	waitingSlot = "slot"
	// waitingMachineBusy is the admission governor holding it: this machine is
	// carrying more than the load or the memory floor allows (task_pressure.go).
	waitingMachineBusy = "machine busy"
	// waitingRateLimited is a RUNNING node whose call is parked on the
	// provider's pacing (internal/provider's patience.go). It is the only one of
	// the three that is true of a node with a worktree and a child agent.
	waitingRateLimited = "rate limited"
)

// The three merge outcomes, and the fourth that says a branch never came home.
// They are the strings [TaskNotice.Merge] carries, spelled once.
const (
	mergeMerged     = "merged"
	mergeConflicted = "conflicted"
	mergeInPlace    = "inplace"
	mergeAborted    = "aborted"
)

// ── the graph ───────────────────────────────────────────────────────────────

// TaskNode is one piece of work: what it was admitted with, where it is in its
// life, and what it leaves behind.
//
// Every field below the mutex line is guarded by the GRAPH's lock, not one of
// its own. A node is never touched alone — starting one reads its dependencies'
// reports, finishing one unlocks its dependents — so a second lock would be a
// lock ordering to get wrong for no gain.
//
// ── THE GOAL CONTRACT: spec IS FROZEN AT ADMISSION ──
//
// NOTHING IN THIS FILE WRITES spec.request, spec.brief, spec.deliverable OR
// spec.acceptance AFTER admit — the four parts of what the node is told
// (task_brief.go). Not the frontier, not the runner, not the auditor, not a
// redirect that arrives late. The node's goal is settled the moment
// [TaskGraph.admit] takes it, and
// every reader downstream — the instruction the child is given, the acceptance
// the auditor judges against, the report a dependent inherits — reads THAT text
// and no other.
//
// This is Argus's two-tier goal contract (harness-research-notes.md §1,
// arXiv:2608.05144), and the two tiers are drawn exactly here: the semantic
// tier moves freely BEFORE admission — the model grooms the brief, the person
// redirects it and their words are appended (task.go) — and the precise
// objective moves only with authority, which in this surface means a NEW
// admission by the person. A redirect is not an edit to a running node; there
// is no path to one, and there must not be. The reason is the auditor: a
// frontier that verifies work against an acceptance which can move while the
// work runs verifies nothing, because whoever holds the pen can always make the
// work pass. What the person gets instead is honest — the running node lands
// against what it was given, and the correction is a task of its own.
//
// The only fields below that change after admission are the node's LIFE (state,
// started, cancel) and its LEAVINGS (brief, report, changed, branch, merge).
// brief is assembled once at start (runFrontier's JIT assembly) from spec.brief
// plus prerequisites' reports and is not the spec.
type TaskNode struct {
	graph     *TaskGraph
	id        uint64
	dependsOn []uint64
	// parent is the node this one was handed out BY, and 0 for the work a
	// conversation proposed. It is the family seam [TaskNotice.Parent] carries,
	// and it is not an edge: dependsOn says what must finish first, this says
	// who asked (task_contract.go states the difference).
	parent uint64
	// depth is how many tasks deep this node sits — 1 for the conversation's
	// own, 2 for a sub-task — and it is what taskDepthLimit bounds.
	depth int
	// owner is the agent that RUNS this node: the conversation for a root, and
	// the PARENT NODE'S OWN AGENT for a sub-task. That is the whole of the
	// nesting: a sub-task's worktree branches off its parent's worktree and
	// merges back into it, so a family's work comes home as one branch rather
	// than as five racing for the person's.
	//
	// It is nil for a node rehydrated from a checkpoint — the agent that owned
	// it died with the process — and for every scripted graph in the tests. Both
	// fall back to the conversation, which is the only agent still there to run
	// anything (see [TaskGraph.runner]).
	owner *Agent
	// done is closed when the node reaches a final state. It is how a waiter —
	// a test, a future join — waits without polling.
	done chan struct{}

	spec taskSpec
	// kind is what sort of node this is ([TaskKind]), settled at admission from
	// the spec and never touched again. It is a FIELD rather than a question put
	// to the spec because a node restored from a checkpoint has no spec worth
	// asking — the design it was written from is gone, and the kind is the one
	// word its history still needs (task_store.go).
	kind  TaskKind
	brief string
	state TaskState
	// report, changed, branch, worktree and merge are the node's leavings,
	// written by the goroutine that ran it and read by everybody else. worktree
	// is where it worked, and it is kept for one reader only: a recovery that has
	// to tell the person where an interrupted node's half-finished work is
	// (task_store.go).
	report  string
	changed []string
	// wrote is every path this node has written SO FAR, in the order it first
	// wrote them and capped at [taskFilesLimit]. It is the LIVE half of changed,
	// which does not exist until the node lands: a node writes for eleven minutes
	// and only then says what it wrote, and by that time the one thing another
	// window could have done about it — not open the same file — is over.
	//
	// So this is held up in the session's presence file ([Agent.presenceTasks])
	// while the work is happening, refreshed by the ordinary heartbeat.
	//
	// IT IS A FACT AND NOT AN INTENT. A path is added when a saving call has come
	// back successful and never because the node said it meant to write
	// something, which is the same bar changed is held to.
	wrote    []string
	branch   string
	worktree string
	merge    string
	started  time.Time
	// elapsed is the node's age FROZEN at the moment it landed. While it runs it
	// is zero and the age is measured from started; a node rehydrated from a
	// checkpoint has no started to measure from and this is the age it had.
	elapsed time.Duration
	// cost is what this node's agents have spent, in dollars, ACCUMULATED where
	// each of them is closed ([Agent.foldTaskUsage]). While the node runs it is
	// zero and the live figure is asked of the child directly (see
	// [TaskNode.spend]); after it lands this is the frozen answer, because the
	// node outlives its child and a surface asking a landed node what it cost
	// has nobody else to ask.
	//
	// IT IS IN THE CHECKPOINT (task_store.go), and it is a figure somebody can
	// point at. The argument against keeping it used to be that a resumed graph
	// is a graph whose child agents are gone, so a number rehydrated from a file
	// would be the only one on the task index nobody could point at a request
	// for. That dissolved when every agent started journaling a `usage` line per
	// call (sessionfile.go): the node's own transcript now holds the lines this
	// total is the sum of, and a person who doubts the figure can open them. It
	// reaches the project index (task_index.go) and the node's own notice on the
	// way past.
	cost float64
	// input, output, cacheRead and cacheWrite are that same accumulation in
	// TOKENS, folded in beside the dollars and kept on the checkpoint with them.
	//
	// They are held rather than derived because a price is a claim about a
	// moment — what the model charged when the call was made — while the tokens
	// are what happened. A node priced by a provider that later changes its
	// rates, or run on a model nobody published a price for at all, still has
	// these; a bill kept in money alone could never be worked out again.
	input      int
	output     int
	cacheRead  int
	cacheWrite int
	// noted says this node's completion note has been handed to the steering
	// lane, and it is what stops a resumed session announcing finished work
	// twice (task_store.go).
	noted bool
	// interrupted marks a node that was RUNNING when a session ended and whose
	// interrupt a recovery has consumed. It is history, and it is written down so
	// that exactly one recovery ever consumes it.
	interrupted bool
	// offer is a finished harness page waiting on the person, held for exactly
	// as long as its card is up so the checkpoint can carry it across a restart
	// ([TaskNode.carryOffer], task_store.go's harnessOfferRecord). Nil on every
	// node that is not a design mid-question.
	offer *harnessOfferRecord
	// queuedSaid marks that this node's "queued" update has been sent. A node
	// waiting behind the cap or behind an edge must show up on the surface —
	// otherwise admitted work is invisible until it starts — but a frontier pass
	// runs on every completion, and a node that announced itself on each of them
	// would be a card redrawing itself for news that has not changed. So the
	// queued update is sent once, and again only when [TaskNode.held] changes
	// under it, which is news.
	queuedSaid bool
	// held is why the frontier is not starting this queued node, in the plain
	// words [TaskNotice.Waiting] carries: waitingSlot, waitingMachineBusy, or ""
	// for a node whose own edges are the answer — a dependent waiting on its
	// prerequisites has nothing to say here that DependsOn does not already say.
	//
	// It is written by the frontier and cleared the moment the node starts or
	// fails, so a landed node never carries a reason it is waiting.
	held string
	// parked says this node has HANDED ITS LANE BACK while it waits on the work
	// it handed out (see [TaskGraph.park]). It is true only between a parent's
	// turn ending and its next report arriving, and it is what keeps a family
	// from deadlocking against the person's own task.parallel cap.
	parked bool
	// paced counts this node's calls that are parked on the provider's pacing
	// (internal/provider's patience.go). A count and not a flag because a node
	// is an agent and an agent can have more than one call out — a repair round
	// beside an audit — and the node stops being paced when the LAST of them
	// gets through, not when the first does.
	paced int
	// cancel ends this node's run: the deadline's context, cancelled early by
	// jobs kill or by Close.
	cancel context.CancelFunc
	// stopped marks a node a PERSON ended (cancel.go). It is set BEFORE the
	// context is cut, so that the landing this stop causes already knows whose
	// decision it was — a flag written afterwards would be a flag the update
	// announcing the end raced past.
	stopped bool
	// room is the node as a PLACE: the child agent somebody can talk to and the
	// live subscribers watching it work (task_room.go). It is nil until the
	// first person enters or the runner attaches its child, and it is emptied
	// when the node lands.
	room *taskRoom
	// journal is where this node's transcript was written, recorded when its
	// child agent was built. It is the node's history, and it outlives the room.
	journal string
	// revise is the lane a DESIGN's thread asks for its page to be rewritten on,
	// and nil on every other kind of node — which is what keeps the revise_design
	// verb off every other thread's belt (harness_task.go's reviseDoor states the
	// whole arrangement). It is minted once, before the thread agent is built,
	// and it is read by exactly one goroutine: the design loop parked on the card.
	revise chan string
	// doing is the PHASE a node of a named kind is in, in that kind's own plain
	// words — "designing", "awaiting your look" — and "" for an ordinary task,
	// which has no phases and whose state word is the whole truth about it
	// (harness_task.go).
	//
	// IT IS A REPLACEMENT AND NOT A DECORATION, which is the one thing that makes
	// it different from mend and held below. Those two are said BESIDE "running",
	// because a node closing a gap or waiting on a slot is running and a surface
	// that spent the state word on either would be hiding the state. A harness
	// being designed is running too, but "running" is a word about the machinery
	// and "designing" is a word about the work — so the surface draws this
	// INSTEAD, and the state underneath is unchanged.
	doing string
	// context is the NAMED WORKING CONTEXT this node's room is, in the words a
	// person would use for it — "designing a subharness", and then "designing
	// flake-triage" once the page it is writing has a name (harness_task.go) — and
	// "" for a node whose room is only work being watched.
	//
	// IT IS ABOUT WHERE A PERSON'S OWN WORDS LAND AND NOT ABOUT THE WORK. A phase
	// ([TaskNode.doing]) answers "what is this node busy with"; this answers "what
	// am I part of when I say something in here", which is the question a surface
	// drawing somebody's own line has to be able to answer. An ordinary task node
	// is briefed and left to it, so there is nothing to be part of and this stays
	// empty — the emptiness law, and what makes a surface that draws this draw
	// nothing at all for every ordinary turn.
	//
	// IT IS NAMED BY THE NODE'S OWN BODY and by nothing else, which is what keeps
	// the mechanism generic: a future kind of node that is also a place somebody
	// talks inside names itself here and every surface already draws it, with no
	// list of kinds anywhere. Like [TaskNode.doing] it is announced when it
	// changes ([TaskNode.contextNow]), because a context that has just learned its
	// name is news that arrives without the state moving.
	context string
	// mend is the gap a repair round is closing right now, in plain words, and ""
	// at every other moment (task_audit.go's repairNode). It is the ONLY thing
	// the repair loop puts on the wire while it runs: the node is still running,
	// nothing has landed, and what a surface draws is the work still going with
	// one line saying what is being finished.
	mend string
	// ran is the model this node is ACTUALLY running on, when that is not the one
	// its spec froze. It is written in exactly one place — the tool-use rescue in
	// [Agent.newTaskAgent], which swaps an incapable model for the worker tier —
	// and it exists so that swap can be told without unfreezing the spec.
	//
	// THE SPEC SAYS WHAT WAS ASKED FOR AND THIS SAYS WHAT ANSWERED. Writing the
	// fallback back into spec.model would have been the shorter fix and the wrong
	// one: the spec is the contract, frozen at admission and checkpointed, and a
	// contract that edits itself is not one. Without this field the swap was
	// invisible in the other direction — [TaskNode.notice] published the rejected
	// id while `mend` on the same card said "model X has no tools; using Y", so
	// one row disagreed with itself about what was running.
	ran string
	// settling NAMES the resolution in flight over a landed node, in the plain
	// words a second caller is told, and it is "" when nobody holds the node
	// (task_audit.go's ResolveUnverified).
	//
	// IT COVERS ALL THREE ANSWERS, not just the re-audit it started life as. An
	// accept and a refute are settles too — they finish the node and, in the
	// accept's case, merge a branch — and two of them arriving in one tool batch
	// (loop.go runs a batch concurrently) would both read TaskUnverified and both
	// come home, which is one merge over a branch the other already deleted. So
	// the state check and the settle are ONE claim, taken here (see
	// [TaskNode.claimSettle]) and held until the node has resettled.
	settling string
	// claim is the WORK'S OWN ACCOUNT of itself — the last thing the worker said,
	// before any auditor spoke — kept apart from the report because the report is
	// a composed card: the audit's lead line and this underneath it.
	//
	// A verdict that lands on a node which has ALREADY landed (task_audit.go's
	// landAudit) has to rebuild that card, and it cannot do it from the report:
	// there is no way to tell the previous auditor's lines from the work's own
	// once they are one string. Keeping the half that never changes is what lets
	// a re-audit replace the audit's half and keep the work's — in both
	// directions, which is the whole of the defect this field closes.
	claim string
}

// TaskGraph is the session's work as a directed acyclic graph, plus the
// frontier executor that runs it.
type TaskGraph struct {
	mu    sync.Mutex
	nodes map[uint64]*TaskNode
	// order is admission order, and it is what makes the frontier
	// DETERMINISTIC: with a cap in play, which of two ready nodes starts first
	// must not be Go's map iteration.
	order   []uint64
	seq     uint64
	running int

	// limit is how many nodes may RUN AT ONCE, and 0 IS NO LIMIT
	// (session.Config's TaskParallel, config.KeyTaskParallel).
	//
	// It used to be a constant here, and the constant was two: a node is a whole
	// agent — its own model calls, its own build, its own checkout — and the
	// person is running one conversation beside them on the same machine. That
	// reasoning was right about what a node costs and wrong about where the
	// ceiling is. Two was a number standing in for a resource nobody had
	// measured, and on a machine with cores to spare it left them spare while a
	// queue of ready work sat still. So THE CAP STOPPED BEING THE RESOURCE
	// MODEL. What bounds a run now is the two ceilings that are really there —
	// the machine's, in task_pressure.go's governor, and the provider's, in the
	// adaptive limiter every call already goes through (internal/provider's
	// limiter.go) — and this is what is left of the old constant: a number a
	// person may set when they want one, off by default.
	//
	// What has not changed is what a cap MEANS. It is a QUEUE and never a
	// refusal: a runnable node past the cap sits on the frontier and starts when
	// a slot frees, which is what a queue in a graph is for.
	limit int

	// governor is the machine's own answer to "may one more node start"
	// (task_pressure.go), and nil is no governor: every scripted graph in the
	// tests, and a session whose person zeroed both rows.
	governor *admissionGovernor
	// polling says a poll is already armed to re-ask the governor. The frontier
	// is otherwise entirely CAUSED — an admission, a landing, a resolution — and
	// a machine getting quieter causes nothing this process can hear, so a
	// queue held on pressure is the one case that needs a clock. One timer at a
	// time, re-armed by the pass it wakes only while the hold is still there.
	polling bool
	// pollEvery is how long that timer waits, and 0 is taskPressurePoll. It is a
	// field for the tests: five real seconds is the right cadence for a machine
	// and the wrong one for a test suite.
	pollEvery time.Duration

	// claims counts the sub-task slots taken per parent by proposals that have
	// been made and not yet admitted (see [TaskGraph.claimChild]). It is empty
	// at rest — every claim is released by the admission it authorized or by the
	// proposal that came to nothing.
	claims map[uint64]int

	// run executes one node to completion and calls [TaskGraph.complete] when
	// it lands. It is a field rather than a method so the graph can be exercised
	// with a scripted runner — the scheduling law (readiness, the cap, brief
	// assembly) is the thing worth testing on its own, and it should not need a
	// provider and a git repository to be looked at.
	run func(*TaskNode)
	// report is called once per node reaching a final state, outside the lock.
	// It is how the world hears: the update event, and the note that reaches the
	// model through the steering lane.
	report func(*TaskNode)

	// store is where the graph is written down after every transition, and nil
	// for a graph with no journal behind it — a session with no file, and every
	// scripted graph in the tests (task_store.go).
	store *taskStore

	// home is the CONVERSATION whose graph this is: the agent that reports every
	// node to the surface, and the agent that runs the ones nobody else owns. It
	// is nil in the scripted graphs the tests build, which replace `run` whole.
	home *Agent

	// runs is the ADAPTIVE RUNS' rows — one entry per run, keyed by the id of the
	// run's own row and holding the last notice published for every row of that
	// family, in the order they were first drawn. runRuns is the order the runs
	// themselves arrived in, because a map has none and a column must not
	// shuffle.
	//
	// THEY ARE KEPT HERE AND THEY ARE STILL NOT NODES. Nothing in this map is in
	// `nodes`, has a spec, holds a lane, or can ever be scheduled — the family
	// seam's own law is that a run REGISTERS and does not admit (orchestrate.go),
	// and that law is the whole reason a run's row cannot be stopped by id or
	// opened as a room. What the graph adds is the two things only the graph can
	// do: the rows are REPLAYED to a lane that opens late
	// ([Agent.replayTaskRoster]), and they are WRITTEN DOWN with the graph
	// (task_store.go), so a conversation reopened tomorrow redraws the runs it
	// started rather than showing an empty column beside a transcript full of
	// them.
	//
	// KEYED BY THE ROOT ROW'S ID AND NEVER BY THE RUN'S NAME. A run's name is a
	// counter on the Agent ([Agent.RunOrchestrate]) that starts again at 1 in
	// every process, so the first run of a resumed conversation would land on top
	// of yesterday's rows; a row id comes from `seq`, which the checkpoint
	// carries across lives, so it is unique for as long as the conversation is.
	//
	// EVERY SLICE IN HERE IS IMMUTABLE. The family builds a fresh one per publish
	// and never touches it again, so this map may be read under the graph's lock
	// alone — and the graph never takes the family's lock, which is what keeps
	// the two lock orders from meeting.
	runs    map[uint64][]TaskNotice
	runRuns []uint64
}

// keepRunRows takes one adaptive run's rows and writes the graph down.
//
// It is the family's way in and the family's only way in: the rows arrive
// already built as the notices a surface was sent, so there is no second shape
// of a run's row anywhere in this package.
//
// THE GRAPH'S LOCK IS RELEASED BEFORE THE CHECKPOINT, because the checkpoint
// takes the store's lock and then the graph's, and a caller holding the graph's
// would close that cycle (task_store.go says the ordering out loud).
func (g *TaskGraph) keepRunRows(root uint64, rows []TaskNotice) {
	if g == nil || root == 0 {
		return
	}
	g.mu.Lock()
	if g.runs == nil {
		g.runs = make(map[uint64][]TaskNotice, 1)
	}
	if _, held := g.runs[root]; !held {
		g.runRuns = append(g.runRuns, root)
	}
	g.runs[root] = rows
	g.mu.Unlock()
	g.checkpoint()
}

// runRowsLocked is every run's rows in one flat walk, runs in arrival order and
// each run's own row ahead of its workers — which is the order they were first
// published in, and the order a tree wants to hang them in.
func (g *TaskGraph) runRowsLocked() []TaskNotice {
	if len(g.runRuns) == 0 {
		return nil
	}
	out := make([]TaskNotice, 0, len(g.runRuns))
	for _, root := range g.runRuns {
		out = append(out, g.runs[root]...)
	}
	return out
}

func newTaskGraph() *TaskGraph {
	return &TaskGraph{nodes: make(map[uint64]*TaskNode, 1)}
}

// graph is the session's graph, built on first use. Most conversations never
// groom a task, and one that does builds it exactly once.
func (a *Agent) graph() *TaskGraph {
	a.mu.Lock()
	defer a.mu.Unlock()
	// A NODE JOINS THE CONVERSATION'S GRAPH; IT DOES NOT KEEP ONE OF ITS OWN.
	// One id space, one roster, one cap, one checkpoint — a second graph inside
	// a worktree would be work the person cannot see, cannot stop and cannot
	// find afterwards (session.go's Config.tasker).
	if a.config.tasker != nil {
		return a.config.tasker
	}
	if a.tasks == nil {
		graph := newTaskGraph()
		graph.home = a
		graph.run = graph.runOwned
		graph.report = a.reportTaskNode
		// The two ceilings, resolved once for the life of the session. They are
		// read off the config rather than off the settings file for the reason
		// every other task row is: a scheduler that re-read a person's profile
		// mid-run would be a run whose rules changed under it.
		graph.limit = a.config.TaskParallel
		graph.governor = newAdmissionGovernor(a.config.TaskMaxLoad, a.config.TaskMinFreeMB)
		// The checkpoint is per-journal, so a session with no file gets a graph
		// with no disk behind it rather than a session that refuses to run tasks
		// (task_store.go).
		graph.store = newTaskStore(taskCheckpointPath(a.config.SessionFile))
		a.tasks = graph
	}
	return a.tasks
}

// tasker is the graph this agent's questions about tasks are answered from,
// WITHOUT BUILDING ONE: its own where it has one, and the conversation's where
// this agent is a node inside it. Nil is the honest answer for a session that
// never groomed a task, and every door in this package reads it that way.
func (a *Agent) tasker() *TaskGraph {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.config.tasker != nil {
		return a.config.tasker
	}
	return a.tasks
}

// runOwned starts one node on the agent that OWNS it (see [TaskNode.owner]): the
// conversation for the work it proposed itself, the parent node's own agent for
// a sub-task. It is the graph's `run` hook, so the scheduler stays one function
// that knows nothing about who is doing the work.
func (g *TaskGraph) runOwned(node *TaskNode) { g.runner(node).runTaskNode(node) }

// runner is that agent, with the conversation as the fallback. owner is written
// once, before the node is ever scheduled, and never again — so it is read here
// without the graph's lock, exactly as `id` is.
func (g *TaskGraph) runner(node *TaskNode) *Agent {
	if node.owner != nil {
		return node.owner
	}
	return g.home
}

// reserve takes the next id. It is separate from admission because a PROPOSAL
// has an id — the surface answers by it — while a node only exists once the
// proposal is approved.
func (g *TaskGraph) reserve() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seq++
	return g.seq
}

// admit puts an approved proposal into the graph and turns the frontier. The
// state it reports is what the node is doing by the time the tool answers:
// running, or queued behind its dependencies or the cap.
func (g *TaskGraph) admit(id uint64, spec taskSpec) TaskState {
	// WHETHER THIS WORK MAY DISCOVER THAT IT IS WIDE, decided here because this
	// is the one door every task in this package comes through whoever opened
	// it — a proposal the chat model groomed, a person's own `/task`, the route
	// judge's card. It is asked of the CONVERSATION rather than of the proposer,
	// because the conversation is where the road is wired and where the sizing
	// judge's answer was banked; a scripted graph in a test has no conversation
	// and gets the honest false ([Agent.armDivision] is nil-safe).
	spec.divide = g.home.armDivision(spec)
	node := &TaskNode{
		graph:     g,
		id:        id,
		dependsOn: spec.dependsOn,
		parent:    spec.parent,
		depth:     spec.depth,
		owner:     spec.owner,
		done:      make(chan struct{}),
		spec:      spec,
		kind:      spec.kind(),
		state:     TaskQueued,
	}
	g.mu.Lock()
	if g.nodes == nil {
		g.nodes = make(map[uint64]*TaskNode, 1)
	}
	g.nodes[id] = node
	g.order = append(g.order, id)
	// The node counts itself from here, so the slot its proposal was holding
	// goes back (see [TaskGraph.claimChild]).
	g.releaseChildLocked(spec.parent)
	g.mu.Unlock()

	// ADMISSION IS A TRANSITION, and it is checkpointed before the frontier turns
	// rather than after: a process killed between these two lines still resumes
	// with the node the person approved, queued.
	g.checkpoint()
	g.runFrontier()
	// AND THE WORK IS NAMED, on a goroutine of its own, after it has started
	// (taskname.go). This is the one door every task in this package comes
	// through, whoever opened it, so a node admitted with a raw sentence where
	// its name should be gets a name whatever started it — and the node is
	// already on the frontier before the namer is asked anything, so nothing
	// waits for it.
	g.nameNode(node)
	return node.stateNow()
}

// claimChild takes one of a parent node's fan-out slots, or says in the model's
// own terms why there is none left. An empty answer is a slot held.
//
// THE CLAIM IS TAKEN BEFORE THE PROPOSAL IS ASKED ABOUT and released by the
// admission it authorized ([TaskGraph.admit]) or by the proposal that came to
// nothing ([TaskGraph.releaseChild]). Counting admitted nodes alone would not
// hold, because a tool batch runs its calls CONCURRENTLY (loop.go) — and a
// model fanning out emits its propose_task calls in one batch, which is exactly
// the moment this cap is for. Every one of them would read the same count and
// every one of them would pass.
func (g *TaskGraph) claimChild(parent uint64) string {
	if parent == 0 {
		// The conversation's own work is not fanned out and is not capped: the
		// person is watching every proposal go by and can stop any of them.
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	held := g.claims[parent]
	for _, node := range g.nodes {
		if node != nil && node.parent == parent {
			held++
		}
	}
	if held >= taskFanLimit {
		return fmt.Sprintf("no: you have already handed out %d pieces of this work, which is as many as one task may. Do the rest in your own hands, or finish these and report what is left undone.", taskFanLimit)
	}
	if g.claims == nil {
		g.claims = make(map[uint64]int, 1)
	}
	g.claims[parent]++
	return ""
}

// releaseChild hands a slot back for a proposal that never became a node — the
// person declined it, the turn ended under the question, the model named a
// model this install does not have.
func (g *TaskGraph) releaseChild(parent uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.releaseChildLocked(parent)
}

func (g *TaskGraph) releaseChildLocked(parent uint64) {
	if parent == 0 || g.claims == nil {
		return
	}
	if g.claims[parent] <= 1 {
		delete(g.claims, parent)
		return
	}
	g.claims[parent]--
}

// children are the nodes one parent handed out, in admission order. It is what
// the parent's own agent reads with the `tasks` tool (tools_tasks.go) and what
// its runner waits on before it lets the node land (see [runTaskChild]).
func (g *TaskGraph) children(parent uint64) []*TaskNode {
	if g == nil || parent == 0 {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var kids []*TaskNode
	for _, id := range g.order {
		if node := g.nodes[id]; node != nil && node.parent == parent {
			kids = append(kids, node)
		}
	}
	return kids
}

// runFrontier is the scheduler, and it is the whole of it.
//
// One pass: fail every queued node whose dependencies cannot be met, start
// every queued node whose dependencies are all done and that nothing is holding
// back. A node that fails here unlocks nothing, so its own dependents are failed
// by the pass this one tail-calls — a cascade walks the graph one layer per pass
// rather than needing a recursive walk holding the lock.
//
// THREE THINGS CAN HOLD A READY NODE, and a held node says which on the wire
// ([TaskNotice.Waiting]): the person's own cap ([TaskGraph.limit]), this
// machine's load or memory ([TaskGraph.governor]), and — for a node that is
// already running — the provider pacing its calls, which is set from the far
// end (see [TaskNode.pacing]). Every one of them is a HOLD ON STARTING and
// never a refusal: the node stays queued, and the next pass asks again.
//
// It is called after admission and after every completion, and it is safe to
// call when nothing can move: the cost of a pass with nothing to do is one lock
// and a walk of the order slice.
func (g *TaskGraph) runFrontier() {
	// The machine is asked BEFORE the lock and at most once a pass. It is two
	// small file reads behind a one-second cache, and the graph's lock is held
	// by everything that announces a node — no reading of /proc belongs under
	// it, however cheap.
	busy := g.governor.holds()
	// AND THE PERSON'S STANDING ORDERS ARE RESOLVED BEFORE THE LOCK TOO, and at
	// most once a pass, for the governor's reason: every node in one graph sits
	// in one place, so the answer is the same for all of them, and reading a
	// folder per starting node would be the same question asked ten times
	// (standing_world.go).
	orders := g.standingWorld()

	g.mu.Lock()
	var starting, failing, waiting []*TaskNode
	machineHeld := false
	for _, id := range g.order {
		node := g.nodes[id]
		if node == nil || node.state != TaskQueued {
			continue
		}
		ready, blocked := g.readinessLocked(node)
		if blocked != "" {
			node.state = TaskFailed
			node.report = blocked
			node.held = ""
			failing = append(failing, node)
			continue
		}
		// What is holding this node, in the words the surface draws. A node that
		// is not ready is held by its own edges and says NOTHING here: DependsOn
		// is already on the notice, and a second word for the same fact would be
		// the wire saying it twice.
		hold := ""
		switch {
		case !ready:
		// A NODE THAT TAKES NO SLOT IS HELD BY NEITHER CEILING, and it is the one
		// case that has to come before both of them ([taskSpec.takesSlot] makes
		// the whole argument). The two ceilings model a node as an agent with a
		// checkout and a build; a design is two model calls and a person reading
		// a card, and queueing one behind a full machine would be a harness
		// nobody can start because the machine is busy running tasks.
		case !node.takesSlot():
		case g.limit > 0 && g.running >= g.limit:
			hold = waitingSlot
		case busy:
			hold = waitingMachineBusy
			machineHeld = true
		}
		if !ready || hold != "" {
			// ANNOUNCE ON CHANGE, which is Mending's discipline: the first time
			// a node is seen waiting, and again whenever the reason under it
			// moves — a slot hold that becomes a machine hold is news, and a
			// frontier pass that found nothing new is not.
			if !node.queuedSaid || node.held != hold {
				node.queuedSaid = true
				node.held = hold
				waiting = append(waiting, node)
			}
			continue
		}
		// JIT: the brief is assembled here, with the prerequisites' reports in
		// hand, and never at proposal time when they did not exist yet.
		node.brief = g.briefLocked(node, orders)
		node.state = TaskRunning
		node.started = time.Now()
		// The hold is lifted by the start itself, so the running update this
		// node is about to send carries no reason to be waiting.
		node.held = ""
		if node.takesSlot() {
			g.running++
		}
		starting = append(starting, node)
	}
	g.mu.Unlock()

	// A machine hold is the one hold nothing will come along and lift.
	if machineHeld {
		g.armPoll()
	}

	// The pass's transitions reach the disk BEFORE the runs they authorize start:
	// a node that is running in this process must never be a node the checkpoint
	// still calls queued, or a kill in that window would resume work that is
	// already in a worktree.
	if len(starting) > 0 || len(failing) > 0 {
		g.checkpoint()
	}

	for _, node := range waiting {
		g.announce(node)
	}
	for _, node := range failing {
		close(node.done)
		g.announce(node)
	}
	for _, node := range starting {
		g.announce(node)
		go g.run(node)
	}
	// A cascade needs one more pass: the nodes just failed may block others,
	// and a failure frees no slot, so nothing else can have moved.
	if len(failing) > 0 {
		g.runFrontier()
	}
}

// armPoll sets the one clock this scheduler has.
//
// Everything else that turns the frontier is CAUSED — a node was admitted, a
// node landed, a person resolved one — and every cause is something this
// process did. A machine getting quieter is not: somebody else's build
// finishing is not an event, and a queue held on pressure with nothing running
// beside it would wait forever for a completion that is never coming. So a
// held pass arms a timer, and the pass that timer wakes arms the next one only
// if the hold is still there. When the hold lifts the nodes start, nothing
// re-arms, and the clock stops existing again.
//
// ONE TIMER AT A TIME. Ten held nodes are one hold, and a pass that armed a
// timer per node would poll ten times a period for one reading.
func (g *TaskGraph) armPoll() {
	g.mu.Lock()
	if g.polling {
		g.mu.Unlock()
		return
	}
	g.polling = true
	every := g.pollEvery
	g.mu.Unlock()
	if every <= 0 {
		every = taskPressurePoll
	}
	time.AfterFunc(every, func() {
		g.mu.Lock()
		g.polling = false
		g.mu.Unlock()
		g.runFrontier()
	})
}

// doomedDependencies is the proposal-time half of [readinessLocked]: which of
// these ids could never gate anything. An id no node carries is a dependency
// that can never resolve — the number usually belongs to something that is not
// a task at all, a background job or an adaptive run, whose ids look just like
// task ids in the conversation — and an id whose node already failed is a wait
// that only ends in the cascade. Both are cheaper refused at the door than
// admitted and killed on the next frontier turn, which is what used to happen:
// the person watched a task appear and die in the same breath, over a number
// the model mistook.
func (g *TaskGraph) doomedDependencies(ids []uint64) (missing, failed []uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, id := range ids {
		node := g.nodes[id]
		switch {
		case node == nil:
			missing = append(missing, id)
		case node.state == TaskFailed:
			failed = append(failed, id)
		}
	}
	return missing, failed
}

// readinessLocked answers two questions at once: may this node start, and is it
// waiting on something that will never come. The second is why a dependency on
// a FAILED node is not simply "not ready yet" — a node whose prerequisite did
// not finish is a node whose brief can never be assembled, and leaving it
// queued forever would be a task the person watches wait on nothing.
func (g *TaskGraph) readinessLocked(node *TaskNode) (bool, string) {
	ready := true
	for _, id := range node.dependsOn {
		prerequisite := g.nodes[id]
		if prerequisite == nil {
			return false, fmt.Sprintf("it waits on task %d, which is not in this session's work", id)
		}
		switch prerequisite.state {
		case TaskDone:
		case TaskFailed:
			return false, fmt.Sprintf("it waits on task %d, which did not finish", id)
		case TaskUnverified:
			// AN UNVERIFIED PREREQUISITE IS NOT A FAILED ONE. Nobody said its
			// work is wrong — only that nobody could say it is right — so this
			// node WAITS rather than dying in the cascade. What will move it is
			// a person resolving that node ([Agent.ResolveUnverified]), and until
			// then a brief assembled from an unaudited report would be work
			// built on a claim nothing stands behind.
			ready = false
		default:
			ready = false
		}
	}
	return ready, ""
}

// briefLocked assembles what one node is handed: its own brief, then what the
// work before it learned, then the standing orders the person holds over this
// place. The headings are plain words rather than markers because the node reads
// them as prose — it is a colleague being told what the last shift found and
// what the house rules are, not a data structure.
//
// THE ORDERS COME LAST AND NOT FIRST. What the node is doing and what it was
// told by the work ahead of it are the job; the orders are the conditions the
// job is done under, and a brief that opened with them would read as the job
// being about the conditions. `orders` is [TaskGraph.standingWorld]'s answer,
// resolved ONCE per frontier pass outside this lock.
func (g *TaskGraph) briefLocked(node *TaskNode, orders string) string {
	var learned strings.Builder
	for _, id := range node.dependsOn {
		prerequisite := g.nodes[id]
		if prerequisite == nil || strings.TrimSpace(prerequisite.report) == "" {
			continue
		}
		fmt.Fprintf(&learned, "\n\n%s (task %d):\n%s",
			prerequisite.spec.title, id, prerequisite.report)
	}
	brief := node.spec.brief
	if learned.Len() > 0 {
		brief += "\n\nWhat the work before you learned:" + learned.String()
	}
	if orders != "" {
		// The section is rendered with a trailing newline for the block the
		// conversation wraps it in; a brief is prose and ends where it ends.
		brief += "\n\n" + strings.TrimRight(orders, "\n")
	}
	return brief
}

// complete settles one node and turns the frontier again. The runner has
// already written the node's leavings through [TaskNode.finish]; this is the
// state transition and the slot being handed back.
func (g *TaskGraph) complete(node *TaskNode, state TaskState) {
	g.mu.Lock()
	node.state = state
	if !node.started.IsZero() {
		// The age stops here. A finished node's elapsed is a fact about how long
		// the work took, and a checkpoint that recomputed it from `started` would
		// have finished work ageing on disk.
		node.elapsed = time.Since(node.started)
	}
	// A PARKED NODE HAS ALREADY GIVEN ITS LANE BACK ([TaskGraph.park]), so
	// landing it must not give the same one back twice. And a node that never
	// took one — a design (see [TaskNode.takesSlot]) — must not hand one back
	// either: both would be quietly raising the cap for everybody else, which
	// is the same fault [TaskGraph.resettle] refuses one function down.
	if node.parked {
		node.parked = false
	} else if g.running > 0 && node.takesSlot() {
		g.running--
	}
	g.mu.Unlock()
	close(node.done)

	g.checkpoint()
	g.announce(node)
	g.runFrontier()
}

// resettle moves a node that has ALREADY landed to a new final state: an
// unverified node a person accepted, refuted, or had audited again
// ([Agent.ResolveUnverified]).
//
// IT IS NOT [TaskGraph.complete], and the two things it deliberately does not do
// are the reason it exists. It does not close `done` — that channel was closed
// when the node first landed, and closing it twice would panic. And it does not
// hand back a slot: the slot went back with the run that ended, so a second
// decrement here would be this node quietly raising the concurrency cap for
// everybody else. What it shares with complete is everything that matters —
// the checkpoint, the announcement, and the frontier pass that lets the
// dependents move on the new state.
func (g *TaskGraph) resettle(node *TaskNode, state TaskState) {
	g.mu.Lock()
	node.state = state
	g.mu.Unlock()

	g.checkpoint()
	g.announce(node)
	g.runFrontier()
}

// announce tells the world about one node's current state, outside the lock: a
// report hook that emits an event and writes to a journal must never run with
// the graph held.
func (g *TaskGraph) announce(node *TaskNode) {
	if g.report != nil {
		g.report(node)
	}
}

// node looks one up by id.
func (g *TaskGraph) node(id uint64) *TaskNode {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.nodes[id]
}

// ── one node, from the outside ──────────────────────────────────────────────

func (n *TaskNode) stateNow() TaskState {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.state
}

func (n *TaskNode) wasStopped() bool {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.stopped
}

func (n *TaskNode) markStopped() {
	n.graph.mu.Lock()
	n.stopped = true
	n.graph.mu.Unlock()
}

// model is the id this node runs on, and "" for a node admitted before anybody
// chose one — a checkpoint written by an older build, a scripted graph in a
// test. Its caller reads that emptiness as "the conversation's own".
func (n *TaskNode) model() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.model
}

// retarget writes the person's EXPLICIT pick onto this node. It is the only
// write to a spec after admission anywhere in this package, and task_room.go's
// header states the law it is the exception to: the freeze is against a
// conversation's `/model` drifting work nobody chose it for, never against the
// person choosing for one node in that node's own room.
//
// A RESCUE'S SWAP IS SUPERSEDED BY A PERSON'S PICK. [TaskNode.ran] exists so the
// tool-use fallback can be told without unfreezing the spec, and once the spec IS
// the person's own answer there is nothing left for it to say — a row that kept
// it would name the rescued model while the person was looking at the one they
// just chose. The sentence beside it goes with it, and only when it is the
// rescue's own: a repair round's `mend` is about the work and has nothing to do
// with this.
func (n *TaskNode) retarget(model string) {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	n.spec.model = model
	if n.ran != "" {
		n.ran = ""
		if isTaskModelRescueNote(n.mend) {
			n.mend = ""
		}
	}
}

// runModelLocked is the model a row about this node should NAME: the one it is
// actually running on where a rescue swapped it, and the spec's frozen id
// everywhere else. The caller holds the graph lock, which is why it is spelled
// in the name — this is read from inside [TaskNode.notice], which takes that
// lock for the whole of its work.
func (n *TaskNode) runModelLocked() string {
	if n.ran != "" {
		return n.ran
	}
	return n.spec.model
}

// assembledBrief is the brief the node is actually working from: its own, plus
// its prerequisites' reports.
func (n *TaskNode) assembledBrief() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.brief
}

func (n *TaskNode) title() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.title
}

// instruction is what the child agent is asked, and it is a DOCUMENT rather
// than a sentence: the person's own request, then the work, then what to
// produce, then what done means (task_brief.go composes it, and is the only
// place that decides the order).
//
// The node never sees the conversation, so this is everything it will ever know
// about why it exists. That is why the person's words are in it: a brief is one
// account of the job written by a model that heard another one, and a worker
// holding both can tell when they have come apart.
func (n *TaskNode) instruction() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return composeBrief(n.spec.request, n.brief, n.spec.deliverable, n.spec.acceptance)
}

// request is the person's own words, frozen with the rest of the spec. It is
// read by [Agent.taskRequest] so that a sub-task a node hands out inherits the
// sentence that started the family rather than the paraphrase in the middle.
func (n *TaskNode) request() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.request
}

// acceptance is the frozen contract, read by the auditor. It is deliberately
// the SAME field [TaskNode.instruction] hands the child: two readers of one
// text, so there is no version of this where the work was finished against one
// acceptance and judged against another.
func (n *TaskNode) acceptance() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.acceptance
}

// limits are the node's named thresholds, its own if it named them and the
// defaults if it did not.
func (n *TaskNode) limits() taskLimits {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return taskLimits{
		maxSteps:   thresholdOr(n.spec.maxSteps, taskMaxSteps),
		noProgress: thresholdOr(n.spec.noProgress, taskNoProgress),
		deadline:   taskDeadline,
	}
}

func (a *Agent) taskLimits(node *TaskNode) taskLimits {
	limits := node.limits()
	if a.config.TaskDeadline > 0 {
		limits.deadline = a.config.TaskDeadline
	}
	return limits
}

// taskLimits is what stops a node short of its deadline. Both are counted in
// the child's finished tool calls.
type taskLimits struct {
	// maxSteps is the whole budget: the node stops when it has taken this many.
	maxSteps int
	// noProgress is how many CONSECUTIVE steps may pass with no progress —
	// and progress is broader than an edit: a successful edit or write, a
	// bash that leaves the worktree dirtier, or a read/search/fetch of a
	// target the node has not looked at before all reset it to zero. What it
	// catches is the spin: the same query, the same file, the same nothing.
	noProgress int
	// deadline is one checkpoint interval, renewed in the same-sized unit.
	deadline time.Duration
}

// thresholdOr reads a wire value that may be absent. Zero and negative both
// mean "the model did not choose", because a schema field the model omitted and
// a field it filled with nonsense should not be two different behaviours.
func thresholdOr(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

// setCancel hands the node the handle that ends its run.
func (n *TaskNode) setCancel(cancel context.CancelFunc) {
	n.graph.mu.Lock()
	n.cancel = cancel
	n.graph.mu.Unlock()
}

// finish writes the node's leavings before its state changes, so the update
// that announces "done" carries them.
func (n *TaskNode) finish(report string, changed []string, branch, merge string) {
	n.graph.mu.Lock()
	n.report = strings.TrimSpace(report)
	n.changed = changed
	n.branch = branch
	n.merge = merge
	n.graph.mu.Unlock()
	// THE VERDICT IS A TRANSITION TOO. The state change follows immediately, but
	// what a person needs after a kill is the merge outcome and the branch name,
	// and those are written here.
	n.graph.checkpoint()
}

// noteWrote records that this node has just written a path, so that this
// session's presence can say so while the work is still going.
//
// IT DEDUPES AGAINST THE NODE'S OWN LIST and not against the run's, because a
// repair round is a second run over ONE node and both runs' paths are that one
// node's ([alsoChanged] makes the same argument about the leavings).
//
// It takes the graph's lock, which is the lock [Agent.presenceTasks] reads the
// list under, so a heartbeat never sees half an append.
func (n *TaskNode) noteWrote(path string) {
	if n == nil || n.graph == nil {
		return
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if len(n.wrote) >= taskFilesLimit {
		// PAST THE CAP THE TAIL IS DROPPED, exactly as a landed row's list is
		// ([taskFileCitations]). Nothing here carries a count beside the list, so
		// stopping cannot make anything say a false number.
		return
	}
	for _, seen := range n.wrote {
		if seen == path {
			return
		}
	}
	n.wrote = append(n.wrote, path)
}

// leavings is what a landed node left behind: what it said, what it wrote, and
// where its branch stands. A resolution that changes one of them has to carry
// the other three forward, because [TaskNode.finish] writes all four and a
// caller that passed nil for the ones it was not changing would erase them.
func (n *TaskNode) leavings() (report string, changed []string, branch, merge string) {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	changed = append([]string(nil), n.changed...)
	return n.report, changed, n.branch, n.merge
}

// workingCopy rebuilds the node's [taskTree] from what it wrote down when it
// started (setTree), so a node that has already landed can be merged or audited
// again.
//
// THE ROOT IS THE PERSON'S REPOSITORY, not the worktree's own: `git rev-parse
// --show-toplevel` inside a linked worktree answers the worktree, and a merge
// aimed there would merge the branch into itself. It is derived from the
// workspace exactly as prepareTaskTree derived it.
//
// A working copy that is GONE is an error rather than a silent in-place tree:
// the node's changes live in that directory, and pretending otherwise would
// merge an empty branch and call it done.
func (n *TaskNode) workingCopy(place Place, workspace string) (taskTree, error) {
	n.graph.mu.Lock()
	dir, branch, merge := n.worktree, n.branch, n.merge
	n.graph.mu.Unlock()

	if merge == mergeInPlace || strings.TrimSpace(branch) == "" {
		// It ran in the person's own tree: there is nothing to merge and the
		// place to look is where they are standing.
		if strings.TrimSpace(dir) == "" {
			dir = workspace
		}
		return taskTree{dir: dir, merge: mergeInPlace}, nil
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return taskTree{}, fmt.Errorf("its working copy is gone from %s, so there is nothing left to look at — its branch %s is still there", dir, branch)
	}
	root, ok := repositoryRoot(workspace)
	if !ok {
		return taskTree{}, fmt.Errorf("%s is no longer a repository, so its branch %s cannot come home", workspace, branch)
	}
	return taskTree{dir: dir, root: root, branch: branch, place: place}, nil
}

// mending sets — or clears — the gap this node is closing, and TELLS THE WORLD
// on the way past.
//
// The announcement is the whole reason it is a method rather than a field
// assignment. A repair round takes minutes; a surface that learned about it only
// when the node landed would show a task sitting still through the one stretch
// where the most interesting thing about it is what it is finishing. The state
// does not move — a repairing node is a RUNNING node, and reportTaskNode returns
// after the event for a running one — so this is an update and never a landing.
func (n *TaskNode) mending(line string) {
	n.graph.mu.Lock()
	changed := n.mend != line
	n.mend = line
	n.graph.mu.Unlock()
	if changed {
		n.graph.announce(n)
	}
}

// pacing records that one of this node's calls has parked on the provider's
// pacing, or has stopped being parked, and TELLS THE WORLD when that changes
// what the node's card should say.
//
// It is [TaskNode.mending]'s shape for [TaskNode.mending]'s reason. A node
// whose provider is rate limiting it is a node that will sit there for minutes
// looking exactly like a node that is working, and the person watching it
// deserves the one word that distinguishes those. The state does not move — a
// paced node is a RUNNING node — so this is an update and never a landing.
//
// IT IS CALLED FROM THE PROVIDER'S OWN GOROUTINE, inside the send that is
// waiting (internal/provider's patience.go), so it does the least a signal can
// do: a counter under the graph's lock and, only on a change, one announce.
// The running check is what keeps a call that parked and was then cancelled
// from announcing anything about a node that has already landed.
func (n *TaskNode) pacing(parked bool) {
	n.graph.mu.Lock()
	before := n.paced > 0
	switch {
	case parked:
		n.paced++
	case n.paced > 0:
		n.paced--
	}
	changed := before != (n.paced > 0)
	running := n.state == TaskRunning
	n.graph.mu.Unlock()
	if changed && running {
		n.graph.announce(n)
	}
}

// claimSettle claims a node that NEEDS A LOOK for exactly one resolution, and
// the state check is part of the claim rather than a question asked before it.
//
// That is the whole point of the function. Every settle here — accept, refute,
// a landing re-audit — reads the node's state, spends a while outside the lock
// (an os.Stat, a `git rev-parse`, five minutes of auditor), and then writes a
// final state. Two of them that each checked before either wrote would both
// pass, and the second would merge a branch the first already brought home or
// overwrite a real refutation with a stale accept. One claim, taken under the
// graph's lock with the state, and nobody else can start.
//
// what is the claim in PLAIN WORDS — "a re-audit", "your accept" — because it
// is read back to whoever lost the race. [TaskNode.releaseSettle] hands it back.
func (n *TaskNode) claimSettle(what string) error {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.state != TaskUnverified {
		// THE SENTINEL RIDES THIS ONE TOO (task_audit.go's [ErrTaskDecided]).
		// Losing the race here means the same thing it means at the door: the
		// question is gone, and a surface holding a card about it needs to know
		// that rather than to keep asking.
		return settledAlready(n.id, n.state)
	}
	if n.settling != "" {
		return fmt.Errorf("task %d is already being resolved — %s is in flight — so wait for that to land rather than putting a second answer on top of it", n.id, n.settling)
	}
	n.settling = what
	return nil
}

func (n *TaskNode) releaseSettle() {
	n.graph.mu.Lock()
	n.settling = ""
	n.graph.mu.Unlock()
}

// keepClaim records the work's own account of itself, and clears nothing: a
// round that came back with nothing to say leaves the last thing that was said
// standing. See [TaskNode.claim].
func (n *TaskNode) keepClaim(claim string) {
	claim = strings.TrimSpace(claim)
	if claim == "" {
		return
	}
	n.graph.mu.Lock()
	n.claim = claim
	n.graph.mu.Unlock()
}

// workClaim is the work's own account, and the whole carried report when there
// is none — a node landed by an older build, or replayed from a checkpoint
// written before the claim was kept. Carrying the report whole is the honest
// fallback: it may lead with a stale non-answer, but nothing here can tell that
// half from the work's, and dropping it would delete the only account there is.
func (n *TaskNode) workClaim(report string) string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.claim == "" {
		return report
	}
	return n.claim
}

// addSpend charges one agent's whole bill to the node: the dollars and the four
// token counts behind them.
//
// It ADDS rather than sets, because a node is more than one agent: the worker,
// and the auditor that judges it (task_audit.go). The person asked for a task,
// not for a task and separately for a judge, so the node's figure is what the
// whole node cost — which is the same pocket [Agent.foldTaskUsage] charges the
// session from.
//
// The money is guarded and the tokens are not, and the asymmetry is the point.
// Zero dollars is left alone rather than written: a provider that reported no
// price is not a node that was free, and a row saying "$0.00" would be this
// build stating a figure nobody gave it. Tokens have no such problem — a call
// that nobody priced still read and wrote a countable number of them, and those
// are exactly what makes an unpriced node re-pricable later.
func (n *TaskNode) addSpend(used Usage) {
	if n == nil {
		return
	}
	n.graph.mu.Lock()
	if used.CostUSD > 0 {
		n.cost += used.CostUSD
	}
	n.input += used.Input
	n.output += used.Output
	n.cacheRead += used.CacheRead
	n.cacheWrite += used.CacheWrite
	n.graph.mu.Unlock()
}

// setTree records where the node is working, the moment it has somewhere to
// work. It is the one write that makes an interrupt recoverable: a node killed
// mid-run has a branch and a directory on disk, and this is where the checkpoint
// learns their names (task_store.go).
func (n *TaskNode) setTree(tree taskTree) {
	n.graph.mu.Lock()
	n.worktree = tree.dir
	n.branch = tree.branch
	n.merge = tree.merge
	n.graph.mu.Unlock()
	n.graph.checkpoint()
}

// openRoom returns the node's room, opening it on first use — the runner
// attaching its child, or a person entering a node that has not started yet.
// A node whose life is over has no room: nothing more will happen in it, and
// its history is its journal (task_room.go).
//
// THE ROOM CLOSES WITH THE NODE, and it is hung on the node's own done channel
// rather than on a call at the end of the run, because there is more than one
// way to a final state: the runner's return, the frontier's cascade over a
// dependent whose prerequisite failed, and a recovery consuming an interrupt.
// All three close done, so all three empty the room and end every watcher's
// channel — including the one Close takes, which kills the node's job and lands
// it exactly as `jobs kill` would.
func (n *TaskNode) openRoom() *taskRoom {
	n.graph.mu.Lock()
	if n.state.settled() {
		n.graph.mu.Unlock()
		return nil
	}
	room, opening := n.room, false
	if room == nil {
		room = newTaskRoom()
		n.room = room
		opening = true
	}
	done := n.done
	n.graph.mu.Unlock()

	if opening {
		go func() {
			<-done
			room.close()
		}()
	}
	return room
}

// setJournal records where this node's transcript is being written. The path is
// minted with a timestamp in it (taskJournalPath), so it is written down the
// moment it exists rather than recomputed later into the name of a file nobody
// wrote.
func (n *TaskNode) setJournal(path string) {
	n.graph.mu.Lock()
	n.journal = path
	n.graph.mu.Unlock()
}

func (n *TaskNode) journalPath() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.journal
}

// markNoted records that this node's completion note has been handed to the
// steering lane, so no later life of this session says it again.
func (n *TaskNode) markNoted() {
	n.graph.mu.Lock()
	already := n.noted
	n.noted = true
	n.graph.mu.Unlock()
	if !already {
		n.graph.checkpoint()
	}
}

// notice copies the node out from under the lock, shaped for an
// EventTaskUpdate. Rendering never holds the graph.
func (n *TaskNode) notice() TaskNotice {
	// The spend is read BEFORE the graph lock is taken, and it has to be: it
	// asks the room for the child agent and the child agent for its own usage,
	// each of which is a lock of its own. Taking them under the graph's would be
	// a second lock order in a package that has one.
	cost := n.spend()
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.noticeLocked(cost)
}

// noticeLocked is [TaskNode.notice] for a caller already holding the graph
// lock, with the spend read before that lock was taken (notice says why that
// order is the only one). It exists for [Agent.replayTaskRoster], which builds
// every row of the roster inside ONE hold of the lock so the batch is a single
// instant of the graph rather than a smear across a node landing mid-walk.
func (n *TaskNode) noticeLocked(cost float64) TaskNotice {
	// A landed node's age is frozen, and a rehydrated one has only the age its
	// checkpoint recorded; only a node that is still running is measured.
	elapsed := n.elapsed
	if elapsed == 0 && !n.started.IsZero() {
		elapsed = time.Since(n.started)
	}
	changed := make([]string, len(n.changed))
	copy(changed, n.changed)
	// The two halves of Waiting, and they cannot both be true of one node: held
	// is written only while a node is queued, paced only while its child agent
	// is making calls. A running node that is parked on the provider is the one
	// that outranks, because it is the one that is happening now.
	waiting := n.held
	if n.state == TaskRunning && n.paced > 0 {
		waiting = waitingRateLimited
	}
	return TaskNotice{
		ID:        n.id,
		Title:     n.spec.title,
		Kind:      n.kind,
		DependsOn: n.dependsOn,
		Parent:    n.parent,
		State:     n.state,
		Elapsed:   elapsed,
		Report:    n.report,
		Changed:   changed,
		Branch:    n.branch,
		Merge:     n.merge,
		Doing:     n.doing,
		Context:   n.context,
		Mending:   n.mend,
		Waiting:   waiting,
		Stopped:   n.stopped,
		// WHAT IT IS RUNNING ON, WHICH IS THE SPEC'S UNLESS SOMETHING SWAPPED IT.
		// See [TaskNode.ran] for why the swap is a second field rather than an
		// edit to the frozen spec.
		Model:   n.runModelLocked(),
		CostUSD: cost,
	}
}

// spend is what this node has cost so far, in dollars.
//
// IT IS THE FROZEN FIGURE PLUS THE LIVE CHILD, and both halves are real at the
// same time: a node accumulates n.cost as each of its agents is folded in and
// closed ([Agent.foldTaskUsage]) — an auditor, a repair, a design thread — while
// the worker that is still running has not been folded into anything yet. Adding
// them is what stops the first fold from freezing the node's price for the rest
// of its life, which is what reading n.cost alone once it was non-zero did.
//
// The fold-then-close instant can show the same money twice, for as long as it
// takes the line after the fold to close the child. It converges on the next
// notice and it is not worth a lock: this is a figure a surface draws, and a
// price that is briefly high and then right is a better trade than every reader
// of it queueing behind the graph.
//
// Zero means "nobody has published a price", which is what an unpriced model and
// a node that has not started both look like from here — and a surface that
// draws this draws nothing rather than a $0.00 it made up.
func (n *TaskNode) spend() float64 {
	n.graph.mu.Lock()
	room, frozen := n.room, n.cost
	n.graph.mu.Unlock()
	if child := room.speaker(); child != nil {
		return frozen + child.Usage().CostUSD
	}
	return frozen
}

// ── the world hearing about a node ──────────────────────────────────────────

// reportTaskNode is the graph's report hook: one event for a surface, a row in
// the project's index, and — on a final state — one note for the model.
//
// The note rides the STEERING LANE, exactly as a background job's exit does
// (jobs.go). A node's completion is news that arrives while the model is busy,
// it must land at a step boundary rather than inside a tool batch, and from the
// model's side it is a line somebody said. Giving it a second lane would be a
// second ordering rule for the same kind of message.
//
// THE INDEX ROW IS WRITTEN HERE FOR THE SAME REASON THE NOTE IS: this is the
// one place in the build where "this node has finished" is a fact rather than a
// guess, and a row written anywhere else would be a second definition of landed
// (task_index.go).
func (a *Agent) reportTaskNode(node *TaskNode) {
	notice := node.notice()
	a.emitTaskUpdate(notice)
	if notice.State == TaskRunning || notice.State == TaskQueued {
		return
	}
	a.recordTaskIndex(node)
	note := taskNote(notice, taskURI(node.journalPath()), a.settlePolicy())
	// WHETHER IT IS WORTH A TURN OF ITS OWN depends on whether anybody is waiting
	// for a sentence about it. An ordinary task was handed off and forgotten: it
	// lands minutes later on a silent session, and the answer the person asked
	// for is the model's paragraph about it, so the note WAKES one — routed to
	// its parent's reader when it has one ([Agent.deliverTaskNote]).
	//
	// A DESIGN IS THE OTHER CASE. It ends the moment the person answers its card
	// — they are at the keyboard, they just decided, and the settle card is
	// already on screen saying what became of it — so a turn started here would be
	// the model reading their own answer back to them. The note is ambient: real,
	// carried, and read by whatever they say next (harness_task.go).
	if notice.Kind == TaskKindHarness {
		a.enqueueAmbientNote(note)
	} else {
		a.deliverTaskNote(node, note)
	}
	// SAID ONCE, ACROSS LIVES. The checkpoint records that this node's completion
	// has been announced, so a session resumed from it restores the node as
	// history instead of telling the model that finished work has just landed
	// (task_store.go).
	node.markNoted()
}

// deliverTaskNote hands one landed node's news to WHOEVER ASKED FOR THE WORK:
// the conversation for a task it proposed itself, and the PARENT NODE'S OWN
// AGENT for a sub-task, whose model is the one that has to fold the piece back
// into the whole and is the only reader that can.
//
// The conversation is the fallback and not a second delivery. A parent that has
// already landed — stopped, or out of time, with a child still finishing — has
// no agent left to read anything, and news with nowhere to go belongs in front
// of the person rather than nowhere. Sending it to both would tell the person's
// model that work it never commissioned has just finished.
func (a *Agent) deliverTaskNote(node *TaskNode, note string) {
	reader := a
	if node.parent != 0 {
		if parent := node.graph.node(node.parent); parent != nil {
			if child := parent.openRoom().speaker(); child != nil && child.takesNotes() {
				reader = child
			}
		}
	}
	reader.enqueueSteering(note)
	reader.postTaskNews()
}

// taskNote is what the model reads when a node lands: the outcome, the report,
// and the three facts it cannot infer — what changed, whether the work came
// home, and where the whole story is.
//
// THE VERB IS THE STATE'S OWN WORD, and each one is chosen against the thing it
// must not be mistaken for. The model is about to tell a person what happened,
// in its own sentence, and every wrong word here is a wrong word there:
//
//	finished          the gate let it through, and the report is the evidence
//	                  it went through on
//	failed            somebody looked and made a finding — and when the finding
//	                  was that work is missing, the report says "incomplete — "
//	                  and what is missing
//	needs your look   nobody could look, or nobody would say — which is not the
//	                  same news and does not cascade
//
// NOT ONE OF THEM IS THE HARNESS'S OWN VOCABULARY. The model reads this line and
// says it back to a person in its own words, so "auditor", "verdict", VERIFIED
// and REFUTED must not be in it — the person asked for work, not for a trial,
// and a model handed a courtroom will hold one (task_audit.go's vocabulary law).
// The one word here that comes from the machinery is `reaudit`, and it is there
// because the model has to TYPE it back: a handle is an address, not a finding.
//
// THE TRANSCRIPT URI RIDES THE FIRST LINE, when the node has a journal to point
// at. It is the same handle the `tasks` tool hands out for a node somebody wants
// to read for themselves (task_index.go's TranscriptURI), and it is here so the
// model can hand it over — or read it — without first going looking for the
// row. Empty for a node whose journal this session no longer knows, and then
// the line simply ends after the title.
//
// THE SETTLE POLICY CHANGES ONE CLAUSE AND NOTHING ELSE ([settleClause]). Under
// `ask` the note is informational — the person has the decision on the card in
// front of them, and the model's job is to say what it thinks. Under `auto` the
// same note becomes an instruction to read the work and settle it. Every other
// line of every other landing is identical either way, because the policy is
// about who decides and not about what happened.
// settlePolicy is this agent's standing answer to "who decides a landing nobody
// could check" (task_contract.go's [TaskSettle]). A blank row reads as asking,
// which is the default and the only safe reading of a caller that said nothing.
func (a *Agent) settlePolicy() TaskSettle { return settleOrAsk(a.config.TaskSettle) }

// The two sentences a landing nobody could check ends with, and which one is
// written is the whole of what `task.settle` changes.
//
// BOTH SAY THE SAME FACTS FIRST — neither done nor failed, branch kept,
// dependents waiting — because those are true whoever decides. What differs is
// the LAST clause: under ask it hands the model an address it may pass on, and
// under auto it hands the model a job.
//
// The verbs are interpolated from [TaskResolutions] and never spelled here, so
// the note can never offer a word the tool's own schema would reject.
const (
	settleAskLead  = "\nit is neither done nor failed, its branch is kept, and anything waiting on it waits until somebody decides: tasks id "
	settleAutoLead = "\nit is neither done nor failed, its branch is kept, and anything waiting on it waits until you decide. Read the report above and the work itself — the transcript, the diff on its branch — and then settle it yourself with tasks id "
	// settleAutoTail is the escape the auto note must always leave open. A policy
	// that says "decide" with no way to say "I cannot" is a policy that produces
	// a confident guess about work nobody read.
	settleAutoTail = ". Only ask the person when you genuinely cannot tell from the evidence, and then say what you would need to see."
	settleAskTail  = "\nthe person can also answer this on the card in front of them; say what you think and leave the choice with them unless they ask you to make it."
)

// settleClause is the tail of an unverified landing note, under one policy.
func settleClause(id uint64, settle TaskSettle) string {
	address := strconv.FormatUint(id, 10) + " resolve " + TaskResolveVerbs()
	if settle == TaskSettleAuto {
		return settleAutoLead + address + settleAutoTail
	}
	return settleAskLead + address + settleAskTail
}

func taskNote(notice TaskNotice, transcript string, settle TaskSettle) string {
	var note strings.Builder
	verb := "finished"
	switch {
	case notice.Stopped:
		// THE PERSON ENDED IT, and the model must not tell them their work
		// failed. Nothing was found wrong with it: somebody pressed stop, and the
		// only honest verb for that is the one they would use themselves.
		verb = "stopped"
	case notice.State == TaskFailed:
		verb = "failed"
	case notice.State == TaskUnverified:
		// NOT "failed", and the wording is the whole point of the state: the
		// model is about to tell the person what happened, and "failed" would
		// be it reporting a finding nobody made (task_contract.go). It is also
		// not "could not be verified", which was the same sentence in the
		// harness's vocabulary — this says whose problem it now is.
		verb = "needs your look"
	}
	fmt.Fprintf(&note, "task %d %s: %s", notice.ID, verb, notice.Title)
	if transcript != "" {
		note.WriteString(" · transcript " + transcript)
	}
	if notice.Report != "" {
		note.WriteString("\n" + notice.Report)
	}
	if notice.State == TaskUnverified {
		note.WriteString(settleClause(notice.ID, settle))
	}
	// AN INCOMPLETE LANDING IS AN INVITATION, NOT A DEAD END. The work was sent
	// back as many times as it was allowed and what is still missing is written
	// above, in the person's terms, with the branch it is sitting on. The one
	// thing the model must not do with that is quietly spend another task on it:
	// the harness has already tried that, at the person's expense, and the next
	// move is theirs to choose. The lead is what says which failure this is —
	// a killed node and a node that ran out of time reach TaskFailed too, and
	// neither of them has a gap anybody could offer to close.
	if notice.State == TaskFailed && strings.HasPrefix(notice.Report, incompleteLead) {
		note.WriteString("\nwhat is missing is above and the branch is kept: offer them a follow-up in their own words before anything else is spent on it")
	}
	if len(notice.Changed) > 0 {
		note.WriteString("\nchanged: " + strings.Join(notice.Changed, ", "))
	}
	switch notice.Merge {
	case mergeMerged:
		note.WriteString("\nits branch " + notice.Branch + " merged into yours")
	case mergeConflicted:
		note.WriteString("\nits branch " + notice.Branch + " did not merge cleanly and was kept — merge it yourself when you are ready")
	case mergeAborted:
		// WHAT IT MADE IS ON THAT BRANCH, and saying so is the difference
		// between a person going to look and a person assuming an ending they
		// were told nothing about threw the work away ([keptWork]). The shorter
		// sentence is for a node that left nothing: offering to merge an empty
		// branch would send them after work that does not exist.
		if len(notice.Changed) > 0 {
			note.WriteString("\nit was stopped; what it made is committed on its branch " +
				notice.Branch + ", which was kept — merge that branch to take the work")
		} else {
			note.WriteString("\nit was stopped; its branch " + notice.Branch + " was kept")
		}
	case mergeInPlace:
		note.WriteString("\nit worked directly in the workspace: there was no repository to branch")
	}
	return note.String()
}

// emitTaskUpdate puts one update in front of whoever is watching.
//
// TWO LANES, and they carry the same event because they answer to two different
// lifetimes. The turn's hub is what a Submit caller is reading, and it is the
// right place for an update that happens while the model works. But a node
// outlives the turn that proposed it: its "done" lands minutes later, when
// there is no turn and no hub, so [Agent.TaskUpdates] is the standing
// subscription a surface holds for the whole session.
//
// A surface reading both sees an in-turn update on both, exactly as a surface
// holding two Submit channels sees each event on each (see [Agent.Submit]).
func (a *Agent) emitTaskUpdate(notice TaskNotice) {
	event := Event{Kind: EventTaskUpdate, Tool: "propose_task", Task: &notice}
	a.mu.Lock()
	hub := a.hub
	watchers := make([]*eventStream, len(a.taskWatchers))
	copy(watchers, a.taskWatchers)
	a.mu.Unlock()

	if hub != nil {
		hub.send(event)
	}
	for _, watcher := range watchers {
		watcher.send(event)
	}
}

// TaskUpdates is a standing subscription to every task update this session
// emits, for the whole life of the session rather than one turn.
//
// It exists because a node's most important event — it finished, here is the
// report, here is what merged — happens when no turn is running and no Submit
// channel is open. A surface that draws task cards subscribes once at startup;
// a surface that does not draw them never calls this and pays nothing.
//
// The channel is never closed by a turn ending — a turn's end is not the end of
// the work it handed off. A surface holds it for the life of the session and
// stops reading when it stops drawing.
//
// AND THE FIRST SUBSCRIBER IS HANDED WHAT ARRIVED WHILE THE WINDOW WAS SHUT.
// The fold was built inside New ([Agent.drainStandingInbox]), where there was
// nobody to send it to, so it waited here for the surface to open the lane; the
// stream is unbounded, so handing it over is an append and never a wait. It is
// handed over ONCE — a second lane on the same session is a second view of the
// same conversation, not a second person arriving.
//
// EVERY subscriber is handed the task ROSTER, by contrast, not only the first:
// the rows are facts about the graph rather than news, and a lane opened by a
// surface with nothing drawn yet — a conversation resumed from its checkpoint,
// one switched back to behind home — needs all of them to rebuild its column
// ([Agent.replayTaskRoster]).
func (a *Agent) TaskUpdates() <-chan Event {
	lane, _ := a.WatchTaskUpdates()
	return lane
}

// WatchTaskUpdates is [Agent.TaskUpdates] with a way to stop.
//
// It is the same standing subscription; stop takes the watcher off the
// session's list and ends its pump. A surface that keeps several conversations
// alive and shows one at a time needs it: without a way off the list, detaching
// leaves a queue the session keeps filling and a goroutine parked on a channel
// nobody will read again (agent.go's [eventStream.leave]).
//
// It is a SECOND DOOR rather than a changed one because [Agent.TaskUpdates]'
// shape is the one internal/tui3 declares in its own interface.
//
// stop is never nil and calling it twice is calling it once.
func (a *Agent) WatchTaskUpdates() (<-chan Event, func()) {
	stream := newEventStream()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		stream.close()
		return stream.out, func() {}
	}
	a.taskWatchers = append(a.taskWatchers, stream)
	news := a.standingNews
	a.standingNews = nil
	a.mu.Unlock()
	// THE ROSTER GOES OUT FIRST OF ALL, to EVERY new lane. A lane is opened by
	// a surface that has no rows yet — a conversation resumed from its
	// checkpoint, or one switched back to behind home — and every row it is
	// missing already exists in the graph, announced once on lanes that closed
	// with the surface that held them. Replaying the graph's own notices here
	// is what makes the column rebuildable from the engine's record; a surface
	// that watched all along re-hears what it already drew, and drawing a row
	// twice is drawing it once (tui3's taskUpdate keys rows by id).
	a.replayTaskRoster(stream)
	// THE BACKLOG GOES OUT BEFORE THE STREAM DOES: news the standing side raised
	// while nobody was watching is replayed onto this stream, so a surface that
	// attached a moment late still sees the card rather than a lane that looks
	// like it never fired.
	for _, event := range news {
		stream.send(event)
	}
	var once sync.Once
	return stream.out, func() {
		once.Do(func() {
			a.mu.Lock()
			a.taskWatchers = dropWatcher(a.taskWatchers, stream)
			a.mu.Unlock()
			stream.leave()
		})
	}
}

// replayTaskRoster sends one [EventTaskUpdate] per node this conversation's
// graph holds, in admission order, onto the lane that has just opened.
//
// It exists because the graph outlives every lane that reported it: a node's
// events go out when they happen, to whoever is subscribed at that moment, and
// a surface that attaches later — a conversation resumed from its checkpoint,
// or one switched back to behind home — holds an empty column with no way to
// ask for the rows again. This is the asking: the same notices a live emit
// would have carried, rebuilt from the nodes themselves, so a replayed row and
// a live row cannot disagree about what a node looks like.
//
// THE SPENDS ARE READ FIRST AND THE ROWS ARE BUILT UNDER ONE HOLD OF THE GRAPH
// LOCK. The spends first because notice's lock order demands it — each is a
// room and a child agent with locks of their own. The single hold because a
// node that lands mid-replay would otherwise race its own fresher event onto
// the stream ahead of a staler snapshot row, and a terminal node never speaks
// again, so the stale row would stand for the rest of the session. Under the
// lock no node can move, and [eventStream.send] is an append that never waits,
// so holding it across the walk costs nobody anything. A node admitted between
// the two holds is not in the walk and needs no row here: its lane is already
// registered, so its own events reach it live.
//
// A node's own agent replays nothing: its graph is the conversation's
// (Config.tasker), and the conversation's lanes are where the roster belongs.
func (a *Agent) replayTaskRoster(stream *eventStream) {
	if a.config.InTask {
		return
	}
	graph := a.graph()
	graph.mu.Lock()
	nodes := make([]*TaskNode, 0, len(graph.order))
	for _, id := range graph.order {
		if node := graph.nodes[id]; node != nil {
			nodes = append(nodes, node)
		}
	}
	runs := len(graph.runRuns)
	graph.mu.Unlock()
	if len(nodes) == 0 && runs == 0 {
		return
	}
	costs := make([]float64, len(nodes))
	for i, node := range nodes {
		costs[i] = node.spend()
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	for i, node := range nodes {
		notice := node.noticeLocked(costs[i])
		stream.send(Event{Kind: EventTaskUpdate, Tool: "propose_task", Task: &notice})
	}
	// AND THE ADAPTIVE RUNS GO OUT UNDER THE SAME HOLD, for the same reason and
	// with one difference: a run's row is not rebuilt here, it is REPLAYED. The
	// notices in [TaskGraph.runs] are the notices a live watcher was sent, kept
	// by the family that sent them (orchestrate.go's [orchestrateFamily.publish]),
	// so a replayed row and a live row cannot disagree about anything — including
	// the forming line a run wears before it has any workers, which no rebuilt row
	// would have known to carry.
	//
	// A run whose rows were restored from a checkpoint is in here too, settled
	// (task_store.go's [runRecord]), and it replays through this same line: one
	// roster door, one row-space, whether the run is happening now or happened
	// yesterday.
	for _, notice := range graph.runRowsLocked() {
		row := notice
		stream.send(Event{Kind: EventTaskUpdate, Tool: "propose_task", Task: &row})
	}
}

// dropWatcher takes one stream off a standing lane's list and answers with what
// is left. It is written once and shared by all three of them — task updates,
// adaptive runs, harness designs — because a second spelling of "find it and cut
// it out" is a second place the loop can be wrong about a lane nobody is on.
//
// A stream the list does not hold comes back unchanged, which is the ordinary
// case for a caller that stopped twice.
func dropWatcher(watchers []*eventStream, stream *eventStream) []*eventStream {
	for at, held := range watchers {
		if held == stream {
			return append(watchers[:at], watchers[at+1:]...)
		}
	}
	return watchers
}

// ── running one node ────────────────────────────────────────────────────────

// runTaskNode is one node's whole life: a working copy, a child agent, the run,
// and the branch coming home.
//
// It is the graph's run hook, so everything it does is bracketed by the graph:
// the node was marked running before this started, and [TaskGraph.complete] at
// the end frees the slot and turns the frontier.
func (a *Agent) runTaskNode(node *TaskNode) {
	// A NODE BELONGS TO THE PROCESS, NOT THE SURFACE. Detaching a renderer ends
	// no context here. Close and an explicit stop reach this cancel through the
	// job registry; time is checked only between completed turns below.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node.setCancel(cancel)

	// The node is a JOB, from the registry every other piece of background work
	// comes from: one id space, one row in `jobs list`, one `jobs kill`, one
	// death at Close. What differs is the middle, which is this function.
	listed, err := a.jobs.startTask(node.id, node.title(), cancel, node.markStopped)
	if err == nil {
		defer listed.settle(0)
	}

	// WHICH BODY THIS NODE HAS. Everything above and below is the same for all
	// three kinds — the deadline, the job row, the settle — and the middle is
	// what a node of this spec IS: a worker in a worktree, a subharness being
	// written in a room (harness_task.go), or a subharness being RUN in one
	// (subharness_run.go).
	work := a.workTaskNode
	switch {
	case node.spec.design != nil:
		work = a.designHarnessNode
	case node.spec.run != nil:
		work = a.runSubharnessNode
	}
	state := work(ctx, node, listed)
	if state == "" {
		// Close interrupted this process-owned run. Keep TaskRunning in the
		// checkpoint; recovery turns it back into queued work and resumes it.
		return
	}
	// NOTHING OUTLIVES THE WORK IT WAS HANDED OUT FOR. A sub-task's worktree is
	// branched off its parent's and merges back into it, so a child still
	// running after its parent has landed is work with nowhere to come home to.
	// In the ordinary case there is nothing here to stop — the runner above does
	// not let the node land while a child of it is still going (see
	// [runTaskChild]) — and this is what answers the parent that was killed or
	// ran out of time.
	node.graph.stopChildren(node.id)
	node.graph.complete(node, state)
	// AND WHATEVER IS LEFT WAITING ON A DECIDER WHO HAS GONE HOME.
	// [TaskGraph.stopChildren] deliberately leaves settled children alone, and a
	// child that landed needing a look IS settled — so before this it simply sat
	// there, its one landing note delivered to a parent agent that has now
	// finished reading anything.
	a.bubbleUnverifiedChildren(node)
}

// bubbleUnverifiedChildren hands a settled parent's still-undecided sub-tasks
// UP one level, so that a decision nobody took does not die with the node that
// was going to take it.
//
// THE HIERARCHY LAW, STATED FROM THE ENGINE'S SIDE: inside a family the PARENT
// is the decider. A child's landing note goes to the parent node's own agent
// ([Agent.deliverTaskNote]), which runs with the `tasks` tool and can read the
// diff, so while the parent is alive there is nothing for a person to do and
// nothing that should be put in front of them. The moment the parent settles
// that stops being true: the child is now work waiting on a decider who does
// not exist, and the only honest place for it is one level up — the
// grandparent's agent if there is one, and the person's conversation if there
// is not, which is exactly the routing [Agent.deliverTaskNote] already does for
// the PARENT's own news.
//
// It re-uses the child's own landing note rather than inventing a second
// sentence, and leads it with why it is being said again: a person reading two
// identical lines an hour apart has no way to tell which one is the one that
// still needs them.
func (a *Agent) bubbleUnverifiedChildren(node *TaskNode) {
	for _, kid := range node.graph.children(node.id) {
		if kid.stateNow() != TaskUnverified {
			continue
		}
		notice := kid.notice()
		note := orphanLead(node) + "\n" +
			taskNote(notice, taskURI(kid.journalPath()), a.settlePolicy())
		a.deliverTaskNote(node, note)
	}
}

// orphanLead says why a landing that was already reported is being reported
// again, in the plain words the rest of these notes are written in.
func orphanLead(parent *TaskNode) string {
	return "task " + strconv.FormatUint(parent.id, 10) + " has finished, and a piece of work it handed out is still waiting on somebody to decide:"
}

// park hands a RUNNING node's lane back while it waits on the work it handed
// out; unpark takes one again when it goes back to work.
//
// A PARENT WAITING ON ITS PIECES IS NOT USING A LANE. It has stopped talking,
// its worker is idle, and the only thing that can move it is one of its own
// children finishing. Holding the lane anyway is a deadlock on any machine where
// the person set task.parallel: the parent holds the slot, the child it is
// waiting for can never have one, and both sit there until the parent's deadline
// collects them an hour later.
//
// Unpark takes the lane back unconditionally, cap or no cap. The cap governs
// STARTS ([TaskGraph.runFrontier]) and this node started long ago; making a
// parent queue for permission to read a report it has already been handed would
// be the same deadlock with more steps in it.
func (g *TaskGraph) park(node *TaskNode) {
	g.mu.Lock()
	if node.parked || node.state != TaskRunning {
		g.mu.Unlock()
		return
	}
	node.parked = true
	if g.running > 0 {
		g.running--
	}
	g.mu.Unlock()
	// The lane is free NOW, and the piece this parent is waiting for is very
	// often the node that was queued behind it.
	g.runFrontier()
}

func (g *TaskGraph) unpark(node *TaskNode) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !node.parked {
		return
	}
	node.parked = false
	g.running++
}

// park and unpark reach [TaskGraph.park] from the node, and they are NIL-SAFE on
// purpose: [runTaskChild] also drives workers that have no row in this graph at
// all — an adaptive run's node, whose scheduler is its own — and "hand my lane
// back" is simply not something those have to do.
func (n *TaskNode) park() {
	if n == nil {
		return
	}
	n.graph.park(n)
}

func (n *TaskNode) unpark() {
	if n == nil {
		return
	}
	n.graph.unpark(n)
}

// stopChildren ends every unsettled node one parent handed out, exactly as
// `jobs kill` ends one ([TaskGraph.stop]): the child's branch is kept, its
// report says a person's stop did it, and its dependents cascade. A child that
// has already settled is left alone.
func (g *TaskGraph) stopChildren(parent uint64) {
	for _, kid := range g.children(parent) {
		if kid.stateNow().settled() {
			continue
		}
		_, _ = g.stop(kid.id)
	}
}

// workTaskNode does the work and reports the state the node ended in. Every
// failure is a state and a report rather than an error: a node that could not
// get a working copy has to be able to say so to the person who asked for it.
func (a *Agent) workTaskNode(ctx context.Context, node *TaskNode, listed *job) TaskState {
	log := taskLog(listed)
	tree, resumed := node.resumeTree(a.config.Place, a.config.Workspace)
	var err error
	if !resumed {
		tree, err = prepareTaskTree(a.config.Place, a.config.Workspace, a.journalID(), node.id, node.title())
	}
	if err != nil {
		node.finish("could not prepare a working copy: "+err.Error(), nil, "", "")
		return TaskFailed
	}
	// Written down before a single tool call runs in it: from here on, a process
	// that dies leaves a checkpoint that knows where this node's work is.
	node.setTree(tree)
	fmt.Fprintf(log, "task %d · %s\nworking in %s\n", node.id, node.title(), tree.dir)

	child, err := a.newTaskAgent(ctx, tree.dir, node, "")
	if err != nil {
		node.finish("could not start the task: "+err.Error(), nil, tree.branch, tree.merge)
		return TaskFailed
	}
	defer func() {
		_ = child.Close()
		// The node's spend is the person's, so it is folded into the session's
		// auxiliary usage — the pocket the title and the compaction summary come
		// out of — rather than charged to whichever turn happened to propose it.
		// It is kept ON THE NODE as well, in the same call, because the node
		// outlives its child: a surface asking a landed node what it cost has
		// nobody else to ask (see [TaskNode.spend]).
		a.foldTaskUsage(node, child)
	}()

	// THE ROOM OPENS HERE, because this is the first moment there is anybody in
	// it: from now until the node lands, its events reach whoever is watching
	// and the person's words reach this child's steering lane (task_room.go).
	room := node.openRoom()
	room.speaking(child)

	changed, stopped, runErr := runTaskChild(ctx, child, node, node.instruction(), tree.dir, a.taskLimits(node), room, log)
	report := taskReport(child)

	switch {
	// The threshold comes FIRST because it is the most specific answer: it
	// cancelled the child itself, so every check below would also be true, and
	// each of them would say something less useful than the name of the
	// threshold that fired.
	case stopped != "":
		fmt.Fprintf(log, "%s\n", stopped)
		// A THRESHOLD IS NOT A REASON TO LOSE THE WORK, and it is not a finding
		// about it either. The landing turn has already run and whatever the node
		// made is on disk, so the work is judged before anything is written down
		// about it ([Agent.landStopped]).
		return a.landStopped(ctx, node, tree, changed, report, stopped, log)
	case ctx.Err() != nil:
		if node.wasStopped() {
			merge, changed := keptWork(tree, node.title(), changed)
			node.finish(withReport("stopped", report), changed, tree.branch, merge)
			return TaskFailed
		}
		// Lifecycle cancellation is an interruption, never a finding about the
		// work. The running checkpoint is deliberately left resumable.
		node.finish(withReport("paused — it resumes", report), changed, tree.branch, abortedMerge(tree))
		return ""
	case runErr != nil:
		merge, changed := keptWork(tree, node.title(), changed)
		node.finish(withReport("it ended with an error: "+runErr.Error(), report), changed, tree.branch, merge)
		return TaskFailed
	}

	// THE GATE. Everything above is the node's own account of itself; what
	// follows is somebody else's (task_audit.go). Only a VERIFIED verdict
	// reaches comeHome, so only verified work is ever merged onto the person's
	// branch — and a refuted node keeps its branch exactly as a killed one does,
	// because "not proven" is not "throw it away". With the audit row off the
	// gate stands open and the node's own account merges — marked unaudited,
	// because 'done' should never wear 'verified's clothes.
	if !a.config.TaskAudit {
		// THE GROUND IS CHECKED WHEREVER WORK WOULD MERGE, and with the check off
		// this is one of the places it would. A person who turned verification off
		// has not asked to be merged over the top of another window (taskground.go).
		if shift := a.groundShift(node, changed); shift != "" {
			return a.landShifted(node, tree, changed,
				withReport("nothing checked this work: the task.audit setting is off", report), shift, log)
		}
		merge, detail := tree.comeHome(node.title())
		fmt.Fprintf(log, "merge: %s %s (unaudited)\n", merge, detail)
		// THE SETTING KEY IS THE ONE PIECE OF MACHINERY VOCABULARY A PERSON IS
		// ALLOWED TO SEE, and only because it is an ADDRESS: they turned this row
		// off, this is the row's name, and a sentence that translated it would
		// leave them holding a word their settings sheet does not answer to
		// (task_audit.go's vocabulary law). Everything either side of it is plain.
		node.finish(withReport("nothing checked this work: the task.audit setting is off",
			withReport(report, detail)), changed, tree.branch, merge)
		return TaskDone
	}
	// THE GATE MAY SEND THE WORK BACK BEFORE IT ANSWERS. What returns from here
	// is the end of the whole loop — the last verdict, the gaps of every round,
	// and the files and the claim as the LAST worker left them (task_audit.go).
	outcome := a.auditWithRepair(ctx, node, tree, changed, report, log)
	changed, report = outcome.changed, outcome.claim
	verdict := outcome.verdict
	switch {
	case ctx.Err() != nil:
		merge, changed := keptWork(tree, node.title(), changed)
		node.finish(withReport("stopped while its work was being checked", report), changed, tree.branch, merge)
		return TaskFailed
	case !verdict.answered:
		// NOBODY COULD SAY. Not done — nothing merges on an answer nobody gave —
		// and not failed either, because no finding was made about this work.
		// The node's own claim is kept UNDER the non-answer: whoever is asked to
		// resolve this needs both halves, what the work says it did and what the
		// checker said instead of an answer (task_contract.go's TaskUnverified).
		merge, changed := keptWork(tree, node.title(), changed)
		node.finish(withReport(verdict.lookOutcome(), report), changed, tree.branch, merge)
		return TaskUnverified
	case !verdict.verified:
		// INCOMPLETE, WITH EVERY ROUND'S GAPS. The node's own claim is dropped
		// exactly as it was before: somebody looked at the work and said what is
		// missing, and that answers the claim.
		merge, changed := keptWork(tree, node.title(), changed)
		node.finish(gapsOutcome(outcome.gaps), changed, tree.branch, merge)
		return TaskFailed
	}

	// THE WORK HOLDS, AND THE QUESTION IS WHETHER IT HOLDS AGAINST TODAY. The
	// check the gate ran was run inside this node's own working copy, which is a
	// copy of the world as it was when the node started — so a verdict of
	// verified says nothing at all about a file another window has landed in
	// since. That is the one question left before a merge, and taskground.go is
	// where it is asked.
	if shift := a.groundShift(node, changed); shift != "" {
		return a.landShifted(node, tree, changed, withReport(report, verdict.doneOutcome()), shift, log)
	}

	merge, detail := tree.comeHome(node.title())
	fmt.Fprintf(log, "merge: %s %s\n", merge, detail)
	// THE WORK'S OWN ACCOUNT LEADS, AND WHAT IT WAS CHECKED ON STANDS UNDER IT.
	// Everything downstream reads this report from the top: the settle card quotes
	// its first sentence as what came of the work, the project's index keeps that
	// same line as the row's outcome (task_index.go's taskOutcome), and the chat
	// model reads it before writing the paragraph the person actually asked for.
	// With the check's evidence in front, all three carried a verification command
	// — "`git diff --cached --stat` shows staged new file …" — where what the work
	// FOUND belonged, and a model handed proof-of-check as the headline grades the
	// deliverable instead of delivering it (prompts/system.md's rule for the moment
	// work lands). The evidence is still here, because a finished card is owed what
	// was checked; it is simply not the news. The state says "done" — nothing here
	// says it a second time in the harness's own vocabulary.
	node.finish(withReport(report, withReport(verdict.doneOutcome(), detail)), changed, tree.branch, merge)
	return TaskDone
}

// landStopped settles a node whose threshold fired — and it is where a landing
// stopped being a verdict about the deliverable.
//
// A THRESHOLD IS A STATEMENT ABOUT THE TRAJECTORY, NEVER ABOUT THE WORK. The
// counter in [runTaskChild] kills a node that aimed at the same target twice; it
// has no idea whether the files being re-read are the finished job. Both were
// true at once in the wild: a node wrote all six of the stories it was asked
// for, spent six steps re-reading them to satisfy itself, and was stopped for
// spinning — correctly, the same target twice IS the spin. Its landing turn then
// said "The six files are already written… Done — Chapter 1… The files are the
// deliverable", the six files were on disk, and the person was shown ✗ failed
// and a kept branch next to a report saying the work was done. Before this,
// every landing skipped the gate below, so a node stopped at a threshold could
// never be verified, never merged, and could only read as a failure.
//
// SO THE WORK IS STILL JUDGED. The node gets the same check an ordinary
// finishing node gets — one pass of [Agent.auditNode] against the same frozen
// acceptance, in the same worktree, staged the same way — and when it holds the
// node lands exactly as a finishing node lands: done, merged, and the threshold's
// sentence gone from the report. That last part is the point of the whole repair.
// The first line of a landed report is what the settle card quotes and what the
// project's index keeps, and "stopped: 6 steps without progress" standing over
// work that was checked and merged would be the counter taking the headline off
// the deliverable.
//
// ONE PASS, AND NO REPAIR ROUND: that is the bound. Ordinary verification may
// send work back for another go ([Agent.auditWithRepair], task.repair_rounds);
// this may not — a repair round is another full worker in the worktree, and
// handing one to a node that was just stopped for burning steps is paying twice
// for the run the threshold ended. What remains is what the ladder already bounds
// on its own: a nudge, at most one fresh checker, and five minutes apiece
// (task_audit.go's auditNode).
//
// AND IT IS ASKED ONLY WHEN THERE IS SOMETHING TO ASK. No acceptance is nothing
// to judge against, the audit row switched off is nobody to ask, and a node whose
// context is already cut has been killed rather than landed — each of those goes
// straight to the ending below without spending a checker.
//
// EVERY OTHER ANSWER KEEPS TODAY'S HONEST ENDING: the report leads with the
// threshold that fired and the node's own last words stand under it, the branch
// is committed and kept ([keptWork]), and nothing merges. The threshold stays the
// lead there because it is still the most specific thing anyone knows — the work
// did not hold AND the run was cut short — and a person who is being offered a
// branch rather than a merge needs to know why in the first line.
func (a *Agent) landStopped(ctx context.Context, node *TaskNode, tree taskTree, changed []string, report, stopped string, log io.Writer) TaskState {
	if a.config.TaskAudit && ctx.Err() == nil && strings.TrimSpace(node.acceptance()) != "" {
		verdict := a.auditNode(ctx, node, tree, changed, report, log)
		if verdict.verified && ctx.Err() == nil {
			// The same last question the ordinary finishing line asks, for the same
			// reason: this branch is about to merge (taskground.go). The threshold's
			// own sentence is left out of what follows exactly as it is left out of
			// the merge below — the run was interrupted, the deliverable was not, and
			// the news here is the file somebody else is in.
			if shift := a.groundShift(node, changed); shift != "" {
				return a.landShifted(node, tree, changed, withReport(report, verdict.doneOutcome()), shift, log)
			}
			merge, detail := tree.comeHome(node.title())
			fmt.Fprintf(log, "merge: %s %s (%s, and the work holds)\n", merge, detail, stopped)
			// THE SAME REPORT A NODE THAT FINISHED ON ITS OWN GETS. Its own account
			// leads, what it was checked on stands under it, and nothing anywhere in
			// it mentions the counter — the run was interrupted, the deliverable was
			// not (see [Agent.workTaskNode]'s finishing line, which this mirrors).
			node.finish(withReport(report, withReport(verdict.doneOutcome(), detail)), changed, tree.branch, merge)
			return TaskDone
		}
		fmt.Fprintf(log, "landed work was not accepted: %s\n", verdict.report())
	}
	merge, changed := keptWork(tree, node.title(), changed)
	node.finish(withReport(stopped, report), changed, tree.branch, merge)
	return TaskFailed
}

// landShifted settles a node whose work holds and whose GROUND MOVED while it
// held — somebody else landed in, or is still writing, a file this node wrote
// (taskground.go).
//
// IT IS NOT A NEW ENDING. It is [TaskUnverified]'s ending, reached by a third
// road: the branch is committed and kept ([keptWork]) exactly as it is for the
// landing nobody could judge, the report leads with [needsLookLead] in the same
// person's words, and everything downstream — the settle card, the rail, the
// note's "needs your look" verb, the bubbling of a still-undecided child up to
// whoever is left to decide ([Agent.bubbleUnverifiedChildren]) — is the machinery
// that was already there. Nothing about this landing has to know why it was
// asked for.
//
// THE REASON RIDES IN THE REPORT AND NOWHERE ELSE, which is what puts it in front
// of BOTH readers without a second channel: the person reads it on the card,
// whose first line is this one, and the model reads it inside the landing note
// ([taskNote] prints the report whole). It composes with the settle policy rather
// than replacing it — [settleClause] still writes the ask or the auto tail
// underneath, so a session that decides these itself is handed the fact and the
// job in the order it already expects them.
//
// AND THE WORK IS NOT MERGED. That is the point of routing here rather than
// merging and marking: the person's branch is the thing being protected, and a
// merge that has already happened is not a warning, it is a cleanup. Accepting on
// the card merges it the ordinary way ([Agent.acceptTask]).
func (a *Agent) landShifted(node *TaskNode, tree taskTree, changed []string, report, shift string, log io.Writer) TaskState {
	merge, kept := keptWork(tree, node.title(), changed)
	fmt.Fprintf(log, "not merged: %s\n", shift)
	node.finish(withReport(needsLookLead+shift, report), kept, tree.branch, merge)
	return TaskUnverified
}

// resumeTree reuses the durable working copy after a process interruption.
func (n *TaskNode) resumeTree(place Place, workspace string) (taskTree, bool) {
	n.graph.mu.Lock()
	dir, branch, merge, interrupted := n.worktree, n.branch, n.merge, n.interrupted
	n.graph.mu.Unlock()
	if !interrupted || strings.TrimSpace(dir) == "" {
		return taskTree{}, false
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return taskTree{}, false
	}
	if merge == mergeInPlace {
		return taskTree{dir: dir, merge: mergeInPlace}, true
	}
	root, ok := repositoryRoot(workspace)
	if !ok {
		return taskTree{}, false
	}
	return taskTree{dir: dir, root: root, branch: branch, place: place}, true
}

// withReport joins the runner's own sentence and the child's words, dropping
// whichever is empty. An empty report is a real outcome — a node killed before
// it said anything — and "\n\n" around nothing would be a blank line the person
// reads as missing text.
func withReport(lead, tail string) string {
	lead, tail = strings.TrimSpace(lead), strings.TrimSpace(tail)
	switch {
	case lead == "":
		return tail
	case tail == "":
		return lead
	default:
		return lead + "\n" + tail
	}
}

// abortedMerge is what a branch that never came home is marked with. A node
// that ran in place has nothing to abort: its edits are already in the person's
// tree, and calling that "aborted" would say work was thrown away that is
// sitting in front of them.
//
// It is for the two endings whose work is ALREADY where this mark says it is: a
// paused node, which resumes in the same worktree and commits when it finally
// comes home, and a verdict landing on a node that settled once already
// (task_audit.go's landAudit), whose branch was committed then. Every ending
// that settles work for the FIRST time goes through [keptWork] instead, which
// makes the same mark after making it true.
func abortedMerge(tree taskTree) string {
	if tree.merge == mergeInPlace {
		return mergeInPlace
	}
	return mergeAborted
}

// keptWork is abortedMerge for a node that has SETTLED without merging —
// stopped at a threshold, killed, errored, or turned back at the gate — and it
// is the difference between a promise and a fact.
//
// THE REPORT SAYS "ITS BRANCH WAS KEPT", SO THE BRANCH HAS TO HOLD THE WORK.
// Nothing but [taskTree.comeHome] used to commit, and comeHome is exactly what
// these endings skip — so a node that produced two real files was landed with a
// sentence naming a branch that had nothing on it, its work sitting untracked
// in a worktree directory nobody named. The commit here is the whole repair:
// the branch now holds what the node made, `git merge` on it works, and the
// files come back BY NAME to be told to the person, including the ones no
// argument ever named ([commitTaskWork]).
//
// IT STILL DOES NOT MERGE, and that is deliberate and unchanged. Only work that
// was checked reaches the person's branch (the gate in [Agent.workTaskNode]);
// "not proven" is not "throw it away", and it is not "land it either" — the
// person is told where it is and brings it home themselves.
func keptWork(tree taskTree, title string, changed []string) (string, []string) {
	if tree.merge == mergeInPlace || tree.root == "" || strings.TrimSpace(tree.dir) == "" {
		return abortedMerge(tree), changed
	}
	return mergeAborted, alsoChanged(changed, commitTaskWork(tree.dir, title))
}

// runTaskChild submits the brief, consumes the node's own events internally,
// and enforces the two thresholds from the same stream.
//
// The events are NOT forwarded INTO THE CONVERSATION. A node's tool rows belong
// to its journal, not to the chat: a person watching a chat must not have forty
// of somebody else's greps scroll past. What is kept for the chat is the one
// thing the person needs to see afterwards — which files the node wrote — read
// off the edit and write calls as they end.
//
// They ARE forwarded into the node's own ROOM, which is the other half of that
// same rule: the events a chat must not carry are exactly the events somebody
// who walked into this node's page came to see (task_room.go). The publish is
// the first thing this loop does with an event, so a watcher sees the child's
// narrative in the order it happened rather than in the order this side of the
// wall got round to counting it — and it runs for a nil room too, which is the
// ordinary case of a node nobody is watching.
//
// THE STEP IS ONE FINISHED TOOL CALL, and it is the only unit available from
// out here: the child's model round-trips are inside its own loop, and this
// side of the wall sees the calls they produce. PROGRESS is any of the three
// halves of the job — a SUCCESSFUL call to a hand that saves a file
// ([savingTools], on EventToolEnd and never EventToolFailed, because an edit
// whose oldText did not match changed nothing and a node repeating it is the
// exact spin the counter exists to catch), a step that left the worktree
// different from how it found it ([worktreeMoved]), or a step that TAUGHT the
// node something it did not know ([taughtSomething]). What the counter kills is
// the fourth thing: the same call again, changing nothing, learning nothing.
//
// The cancel is this function's own, hung off the node's context, so tripping a
// threshold ends the child the way `jobs kill` does — a cancelled turn, its
// partial work kept, its branch intact — and the caller is told WHICH threshold
// fired rather than being left to infer it from a context error that has three
// possible causes.
func runTaskChild(ctx context.Context, child *Agent, node *TaskNode, instruction, dir string, limits taskLimits, room *taskRoom, log io.Writer) ([]string, string, error) {
	if limits.deadline <= 0 {
		limits.deadline = taskDeadline
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()

	events, err := child.Submit(runCtx, instruction)
	if err != nil {
		return nil, "", err
	}
	var (
		changed    []string
		seen       = map[string]bool{}
		seenInfo   = map[string]bool{}
		lastDirt   string
		failure    error
		stopped    string
		steps      int
		idle       int
		extensions int
		deadline   = time.Now().Add(limits.deadline)
		evidence   []string
	)
	checkpoint := func(threshold string) bool {
		if node == nil {
			stopped = "stopped at " + threshold
			stop()
			return false
		}
		owner := node.owner
		if owner == nil {
			owner = node.graph.home
		}
		if owner == nil {
			stopped = "stopped at " + threshold
			stop()
			return false
		}
		working, reason := owner.taskProgress(ctx, node, dir, evidence, log)
		if working && extensions < taskMaxExtensions {
			extensions++
			deadline = deadline.Add(limits.deadline)
			fmt.Fprintf(log, "checkpoint: working — renewed %d of %d\n", extensions, taskMaxExtensions)
			return true
		}
		if working {
			reason = "the work used all 4 extensions"
		}
		stopped = fmt.Sprintf("stopped at %s: %s", threshold, strings.TrimSpace(reason))
		stop()
		return false
	}
	drain := func(events <-chan Event) {
		for event := range events {
			room.publish(event)
			switch event.Kind {
			case EventToolBegin:
				fmt.Fprintf(log, "· %s\n", event.Hint)
			case EventToolEnd, EventToolFailed:
				steps++
				evidence = append(evidence, event.Tool+" "+strings.TrimSpace(event.Args))
				if len(evidence) > 24 {
					evidence = evidence[len(evidence)-24:]
				}
				// ALL THREE QUESTIONS ARE ASKED OF EVERY STEP, and each is
				// asked before any of them is read, because two of them RECORD
				// as they answer. The worktree fingerprint has to be refreshed
				// on the step that moved it whichever question noticed, or the
				// next step inherits a stale one and reads somebody else's dirt
				// as its own; the target has to be recorded even on a step that
				// was already progress for another reason, or the same call can
				// be spent twice. A short-circuiting `switch` did both wrong.
				moved := worktreeMoved(dir, &lastDirt)
				learned := taughtSomething(event, seenInfo)
				path, wrote := changedPath(event, dir)
				saved := wrote && event.Kind == EventToolEnd
				if saved && !seen[path] {
					seen[path] = true
					changed = append(changed, path)
					// SAID OUT LOUD THE MOMENT IT IS TRUE. The node's leavings are
					// written once at the end; this is the same fact told while
					// another window could still act on it.
					node.noteWrote(path)
				}
				switch {
				// EXPLORATION IS PROGRESS, and so is PRODUCTION. A research
				// node may never write until its final words; a build node may
				// spend its first dozen steps reading; a node making pictures
				// paints, looks at what it painted, and paints again without
				// ever calling edit or write. What stops a node is SPINNING —
				// the same target again, no new file, no new dirt — not the
				// absence of an edit (PMCoder's "reads saturated", not "reads
				// happened").
				case saved, moved, learned:
					idle = 0
				default:
					idle++
				}
				// Named once. The loop keeps draining after the cancel — the child
				// is still finishing its batch and closing its stream — and a second
				// threshold tripping on the way out must not rewrite the reason the
				// node was stopped.
				if stopped != "" {
					continue
				}
				switch {
				case steps >= limits.maxSteps*(extensions+1):
					checkpoint(fmt.Sprintf("%d-step checkpoint", limits.maxSteps*(extensions+1)))
				case !time.Now().Before(deadline):
					checkpoint("deadline checkpoint")
				case idle >= limits.noProgress:
					stopped = fmt.Sprintf("stopped: %d steps without progress", limits.noProgress)
					stop()
				}
			case EventError:
				failure = event.Err
			}
		}
	}
	drain(events)
	if stopped != "" && ctx.Err() == nil {
		// THE LANDING TURN happens after the active request has drained. It is a
		// fresh turn so no request is killed mid-flight; the instruction forbids
		// exploration and permits only writing the deliverable already in hand.
		//
		// THE LANDING BELT IS [savingTools] AND NOT A PAIR OF NAMES. A node
		// whose deliverable is a picture, a piece of music or a video saves it
		// with the verb that makes it, and a landing pass that admitted only
		// write and edit took that verb away at the one moment the node was
		// being ordered to produce — see [landingInstruction] for what the node
		// then said. The instruction is generated FROM the belt, so a media verb
		// the machine does not have is neither offered nor named.
		child.armMu.Lock()
		oldTools, oldDefinitions := child.tools, child.definitions
		child.tools = landingBelt(oldTools)
		child.definitions, _ = toolDefinitions(child.tools)
		landing := landingInstruction(child.tools)
		child.armMu.Unlock()
		if events, err := child.Submit(ctx, landing); err == nil {
			for event := range events {
				room.publish(event)
				if event.Kind == EventToolEnd {
					if path, wrote := changedPath(event, dir); wrote && !seen[path] {
						seen[path] = true
						changed = append(changed, path)
						node.noteWrote(path)
					}
				}
			}
		}
		child.armMu.Lock()
		child.tools, child.definitions = oldTools, oldDefinitions
		child.armMu.Unlock()
	}

	// ── THE NODE THAT HANDED PART OF ITS WORK OUT ──
	//
	// A parent's turn ends the moment it has nothing left to say, and its
	// sub-tasks are still working: the belt hands the id back immediately and
	// tells it not to wait (task.go). So the turn ending is NOT the node ending.
	// The runner holds it open, and every report that lands re-enters the model
	// with it — the same turn a landing starts in a conversation, started here
	// by the one who is reading it ([Agent.resumeTurn]).
	//
	// THE WAIT IS ON THE REPORT AND NEVER ON THE STATE. A node is settled a
	// moment before its news is handed over ([Agent.deliverTaskNote]), and a
	// waiter watching the state would stop waiting inside that moment and land
	// its parent on a report nobody read.
	//
	// A tripped threshold or a cut context ends this exactly as it ends the
	// turn above: the children are stopped with the parent (see
	// [TaskGraph.stopChildren]) and their branches are kept.
	for stopped == "" && runCtx.Err() == nil {
		// The generation is taken BEFORE the question, so a report landing
		// between the two closes the channel this select is about to wait on.
		news := child.taskNewsWait()
		owed, working := child.taskNewsOwed(), child.childrenOutstanding()
		if owed == 0 && !working {
			break
		}
		if owed == 0 {
			// The lane goes back for exactly as long as the wait lasts
			// ([TaskGraph.park]).
			node.park()
			select {
			case <-news:
			case <-runCtx.Done():
			}
			node.unpark()
			continue
		}
		next := child.resumeTurn(runCtx)
		if next == nil {
			break
		}
		drain(next)
	}
	return changed, stopped, failure
}

func (a *Agent) taskProgress(ctx context.Context, node *TaskNode, dir string, evidence []string, log io.Writer) (bool, string) {
	if check := a.config.TaskProgressCheck; check != nil {
		return check(node.instruction(), append([]string(nil), evidence...))
	}
	auditor, err := a.newAuditAgent(dir, node)
	if err != nil {
		return false, "the progress check could not start: " + err.Error()
	}
	defer func() { _ = auditor.Close(); a.foldTaskUsage(node, auditor) }()
	auditor.mu.Lock()
	auditor.system = `You are checking the progress of running work, read-only. Decide only whether the recent evidence and working copy show movement toward the brief or repeated motion without new information. Answer in at most four lines. The first word must be WORKING or CIRCLING, followed by concrete evidence. WORKING means the leash should be renewed; CIRCLING means it should land now.`
	auditor.mu.Unlock()
	question := "Decide whether this running task is still WORKING TOWARD THE BRIEF or CIRCLING. Read the working copy if useful. Recent evidence:\n" + strings.Join(evidence, "\n") + "\n\nBrief:\n" + node.instruction() + "\n\nAnswer WORKING or CIRCLING first, then concise evidence."
	events, err := auditor.Submit(ctx, question)
	if err != nil {
		return false, "the progress check could not be asked: " + err.Error()
	}
	for event := range events {
		if event.Kind == EventToolBegin {
			fmt.Fprintf(log, "checkpoint · %s\n", event.Hint)
		}
	}
	said := strings.TrimSpace(lastSaid(auditor))
	upper := strings.ToUpper(firstLine(said))
	if strings.HasPrefix(upper, "WORKING") {
		return true, said
	}
	if said == "" {
		said = "the progress check returned no evidence"
	}
	return false, said
}

// childrenOutstanding reports whether any sub-task THIS agent handed out has
// yet to deliver its report. It is false in a conversation and in a node that
// never fanned out: neither has a family to be outstanding.
func (a *Agent) childrenOutstanding() bool {
	a.mu.Lock()
	graph, parent := a.config.tasker, a.config.taskID
	a.mu.Unlock()
	for _, kid := range graph.children(parent) {
		if !kid.reported() {
			return true
		}
	}
	return false
}

// familyDepth is how many tasks deep this node sits, and 1 is the floor: a node
// admitted before depth was recorded — a checkpoint from an older build, a
// scripted graph in a test — is the conversation's own work, which is what
// depth 1 means.
func (n *TaskNode) familyDepth() int {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.depth < 1 {
		return 1
	}
	return n.depth
}

// reported says this node's news has been handed to whoever asked for the work.
// It is `noted` read from outside, and it is deliberately a fact about the
// DELIVERY rather than about the state (see the wait in [runTaskChild]).
func (n *TaskNode) reported() bool {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.noted
}

// knowledgeTools are the hands on a node's belt (tools.go's [Agent.belt]) that
// return what the world is rather than change it. A call to one of them aimed
// at a target the node has not aimed at before is PROGRESS, because the node
// came back knowing something it did not know a step ago.
//
// It is the whole read-only half of the belt and not a shortlist, which is the
// point: the counter used to name six of them, and every other look at the
// world counted as a stall — so a node that read a scanned page, asked after
// the build it started, LOOKED AT A PICTURE IT HAD JUST MADE, or recalled its
// own working state was punished for exploring. When a hand is added to belt(),
// it belongs here or it belongs to [savingTools], and one of the two is almost
// always true; a hand that is neither is caught by the worktree anyway
// ([worktreeMoved]) on any step where it actually left something behind.
//
// What is deliberately ABSENT: every hand in [savingTools] (they are counted as
// the file they saved, one branch up), and note, forget, track, commit and
// change_setting (a node writing its own memory or its own settings again is
// not learning anything). bash is absent because it is BOTH, and is handled on
// its own below.
var knowledgeTools = map[string]bool{
	"read":           true,
	"read_document":  true,
	"ls":             true,
	"grep":           true,
	"find":           true,
	"web_search":     true,
	"web_fetch":      true,
	"jobs":           true,
	"recall":         true,
	"view_image":     true,
	"manual":         true,
	"tasks":          true,
	"settings":       true,
	"list_harnesses": true,
	"services":       true,
	"gmail_read":     true,
	"gmail_search":   true,
	"calendar_list":  true,
}

// taughtSomething reports whether one call advanced the node's KNOWLEDGE: a
// knowledge tool, or a bash, aimed at a target it has not aimed at before. The
// same search retried is not new information, and SUCCESS is not required — a
// new target that failed still taught the node that it failed. The seen map
// keys tool+target so re-reading one file while reading another new one still
// counts exactly once.
//
// BASH IS BOTH HANDS and is admitted here on the knowledge half alone. `go
// build` writes, `go test ./...`, `git log` and `rg` do not, and the tool name
// says nothing about which one this was — so a command the node has never run
// counts as the world answering a question it has never asked, and the writing
// half of the same call is answered by the worktree, one caller up
// ([worktreeMoved]), which asks it of every hand rather than of this one.
//
// It RECORDS AS IT ANSWERS, so the caller must ask it on every step and never
// behind a short-circuit: a call that was already progress for some other
// reason must not also be spendable as a fresh target the next time it is made.
//
// The judgement is made on the CALL and never on the result: [Event.Output] is
// a display copy, capped, and a counter that read it would be deciding a node's
// life from bytes that were truncated for a person's screen.
func taughtSomething(event Event, seen map[string]bool) bool {
	if event.Tool != "bash" && !knowledgeTools[event.Tool] {
		return false
	}
	return freshTarget(event, seen)
}

// freshTarget reports whether this call aimed somewhere the node has not aimed
// before, and records it either way.
//
// The WHOLE CALL is the target, not one field of it: paging one long file by
// offset is exploration, fetching one page twice is a spin, and only the args in
// full tell them apart. Display-capped args compare fine — two calls capped at
// the same mark are the same call as far as anyone can see.
func freshTarget(event Event, seen map[string]bool) bool {
	key := event.Tool + " " + strings.TrimSpace(event.Args)
	if seen[key] {
		return false
	}
	seen[key] = true
	return true
}

// argField reads one string field out of a tool call's display args. The args
// arrive compacted for show; a field that did not survive the cap simply
// counts as no-new-target, which is the safe direction.
func argField(args, field string) string {
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return ""
	}
	var value string
	if raw, ok := parsed[field]; ok {
		_ = json.Unmarshal(raw, &value)
	}
	return value
}

// worktreeMoved reports whether this step left the worktree different from how
// the step before it left it, and records the new fingerprint either way.
//
// IT IS ASKED OF EVERY HAND AND NOT ONLY OF BASH, which is the backstop under
// the two lists above: a hand nobody classified — a service call that saves a
// report, a harness that writes itself, a tool added next month — is still
// judged by the one thing that cannot be argued with, which is whether there is
// something in the tree now that was not there a step ago. Without it a node
// whose whole job was producing files could be killed for having produced them
// with the wrong verb.
//
// A non-git directory answers "" forever — stable, so it never moves the
// counter either way, and such a node is judged on novelty alone.
func worktreeMoved(dir string, last *string) bool {
	dirt := worktreeDirt(dir)
	if dirt == *last {
		return false
	}
	*last = dirt
	return true
}

// worktreeDirt is the worktree's dirty fingerprint: the porcelain listing
// hashed, so a step that creates, modifies or deletes a file reads as progress
// while one that only inspects does not.
//
// TWO FLAGS CARRY THE WHOLE OF ITS ACCURACY. `--untracked-files=all` names each
// new file rather than the directory holding it — without it a node that wrote
// marketing/first.png and then marketing/second.png saw the identical line
// "?? marketing/" both times and its second file read as a stall, which is
// exactly the shape of work that makes many files in one new folder. The
// exclude drops the harness's own droppings: a background job writes its log
// under .aforge-v3 while the node works (jobs.go), and a tree that dirties
// itself on a timer would make every step look like progress forever — the same
// exclusion [stageTaskWork] makes for the same reason.
func worktreeDirt(dir string) string {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain",
		"--untracked-files=all", "--", ".", ":(exclude)"+aforgeDroppings).Output()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(out)
	return string(sum[:8])
}

// aforgeDroppings is the one directory under a node's worktree that is the
// harness's and never the node's work: job logs, saved pictures a session keeps
// for itself, anything this program leaves behind while the node works. Named
// once because two places have to agree about it — the fingerprint above and
// the index [stageTaskWork] builds — and a disagreement would mean a node
// judged as working on files that never reach its branch.
const aforgeDroppings = ".aforge-v3"

// savingTools are the hands that PUT A FILE ON DISK at a path the call itself
// names. They are the producing half of the belt, and the counterpart to
// [knowledgeTools]: a successful call to one of them moved the node's diff, and
// the file it names is one of the files the person is told about afterwards.
//
// It is not just edit and write, and that is the correction. A node making
// pictures paints with generate_image, a node making a voiceover writes a wav
// with speak, and both of them used to be read as a node touching nothing —
// so a session that spent a minute generating two real files and looking at
// them was stopped as spinning and told it had made "6 steps without progress".
// Producing IS the work for that node; it simply spells it with a different
// verb.
//
// A call that saved something under a name it did NOT give — generate_image
// with no path, which lands under a timestamped name of its own — is not
// nameable from the arguments and is not listed here as a file. It is still
// progress: the worktree noticed it ([worktreeMoved]), and what it left behind
// is picked up by name when the node's work is committed ([commitTaskWork]).
//
// IT IS ALSO THE LANDING BELT. The turn that lands a stopped node is allowed
// exactly these hands and no others ([runTaskChild]'s LAND NOW pass), because
// "save what you already have" and "this call saves something" are the same
// question asked twice — and reading it off one map is what stops the two
// answers drifting apart, which is exactly what happened when the landing pass
// spelled out `write` and `edit` by hand (design-law §ONE SOURCE OF TRUTH).
var savingTools = map[string]bool{
	"edit":           true,
	"write":          true,
	"generate_image": true,
	"generate_music": true,
	"generate_video": true,
	"speak":          true,
}

// landingBelt is the belt a node keeps for its LAND NOW turn: [savingTools] and
// nothing else, in the order the node already had them so the model sees the
// same list minus the hands it is being told not to reach for.
func landingBelt(tools []bare.Tool) []bare.Tool {
	kept := make([]bare.Tool, 0, len(savingTools))
	for _, tool := range tools {
		if savingTools[tool.Name] {
			kept = append(kept, tool)
		}
	}
	return kept
}

// landingInstruction names the hands the landing belt actually carries, so the
// sentence and the belt cannot disagree.
//
// The old wording said "except write or edit" while the node's deliverable was
// sometimes a picture, and a node that had spent its whole life painting was
// told in one breath to save its deliverable and that it could not use the verb
// that saves one. It answered, truthfully and uselessly, "I have no image
// tooling available now."
func landingInstruction(tools []bare.Tool) string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return "LAND NOW. Write the deliverable or final summary from what you already have. " +
		"Do no new exploration. Do not call tools except " + englishList(names) +
		" when needed to save the deliverable."
}

// englishList joins names the way a sentence does: "a", "a or b", "a, b or c".
func englishList(names []string) string {
	switch len(names) {
	case 0:
		return "none"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// changedPath reads the file one saving call touched, workspace-relative.
//
// It reads the CALL's arguments rather than the result because that is where
// the path is: the tools answer with a sentence about what they did, and the
// arguments are the record of what was asked (see [Event.Args]).
func changedPath(event Event, dir string) (string, bool) {
	if !savingTools[event.Tool] {
		return "", false
	}
	var fields struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(event.Args), &fields); err != nil {
		return "", false
	}
	path := strings.TrimSpace(fields.Path)
	if path == "" {
		return "", false
	}
	if relative, err := filepath.Rel(dir, path); err == nil && !strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(relative), true
	}
	return filepath.ToSlash(path), true
}

// taskReport is the node's last word: the final assistant message, cut to three
// lines. It is read off the child's transcript rather than accumulated from its
// deltas because both paths — a streaming provider and a non-streaming one —
// end with the same recorded message, and only one of them emits deltas.
func taskReport(child *Agent) string {
	return firstLines(lastSaid(child), taskReportLines)
}

// lastSaid is an agent's final assistant message, whole. It is what taskReport
// clips and what the auditor's verdict is parsed out of (task_audit.go) — a
// verdict is four lines and a report is three, and cutting before the parse
// would be the harness deciding a verdict was too long to read.
func lastSaid(child *Agent) string {
	entries := child.Transcript()
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.Role != "assistant" || strings.TrimSpace(entry.Text) == "" {
			continue
		}
		return entry.Text
	}
	return ""
}

// firstLines is the first n non-empty lines, each clipped.
func firstLines(text string, n int) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kept = append(kept, clip(line, taskReportLineLimit))
		if len(kept) == n {
			break
		}
	}
	return strings.Join(kept, "\n")
}

// foldTaskUsage charges the node's spend to the session, and records it on the
// node on the way past — this is the only moment anybody can ask a child agent
// what it cost, because the line after this one closes it.
func (a *Agent) foldTaskUsage(node *TaskNode, child *Agent) {
	used := child.Usage()
	// The node keeps the WHOLE tally, not just the money. One widening here
	// covers every fold there is — the worker, each repair round, the auditor,
	// a harness design thread — because all of them arrive through this door.
	node.addSpend(used)
	if used.Input == 0 && used.Output == 0 && used.CostUSD == 0 {
		return
	}
	cost := used.CostUSD
	// The CHILD's model and the child's OWN call count, not one call and not the
	// conversation's model: a node that ran forty steps on a small model is forty
	// requests to that model, and folding it in as a single call on the model the
	// person is chatting to would put a number in the session's books that never
	// happened.
	a.spendLedger(node).addAuxiliaryUsage(&ai.Response{Usage: &ai.Usage{
		PromptTokens:             used.Input,
		CompletionTokens:         used.Output,
		CacheReadInputTokens:     used.CacheRead,
		CacheCreationInputTokens: used.CacheWrite,
		Cost:                     &cost,
	}}, child.Model(), used.Calls)
}

// spendLedger is WHICH SET OF BOOKS this node's spend goes into: the agent that
// owns the node, or — when that agent has already closed its own — the
// conversation at the root of the graph.
//
// THE ONE-LEDGER LAW HAS AN ORDERING PROBLEM AND THIS IS THE ANSWER TO IT. On
// the ordinary path a part folds into its parent worker and the parent later
// folds whole into the session, and the order is safe because the parent's tail
// loop waits on every part's report before it exits (see [runTaskChild]). A
// STOPPED PARENT DOES NOT WAIT. Its own agent is closed and folded in
// [Agent.workTaskNode]'s defer, and only after that does [Agent.runTaskNode]
// reach [TaskGraph.stopChildren] — so every part cut down with it would fold
// into an agent whose total nobody is ever going to read again, and the money
// would sit on the node's row, visible and uncounted, while the session ledger
// was short by a whole part. That is the failure on exactly the path somebody
// takes when they are worried about what this is costing, and it was never
// division-specific: a propose_task child stopped with its parent lost the same
// way. Threshold and deadline endings take the same road.
//
// SKIPPING A CLOSED HOP IS THE SAME TOTAL BY A SHORTER ROUTE. The money is the
// person's either way; the only thing the parent's books add on the way past is
// a line in the parent's own journal, and a parent that has finished reading is
// not going to read it.
func (a *Agent) spendLedger(node *TaskNode) *Agent {
	if a == nil {
		return a
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if !closed || node == nil || node.graph == nil {
		return a
	}
	if home := node.graph.home; home != nil && home != a {
		return home
	}
	return a
}

// ── the child agent ─────────────────────────────────────────────────────────

// newTaskAgent builds the agent that IS the node: the same package, the same
// loop, the same hands — a different workspace, a different journal, and a belt
// with what a node has nobody to use on left off (tools.go).
//
// THE FAMILY RIDES ONLY ON THE RUN THAT IS THE NODE. The conversation's graph,
// this node's id and its depth are what let the node hand part of its own work
// further out (task.go), and they are handed to the worker with no suffix and
// to nothing else: a repair round is closing a gap somebody named in work that
// is already done, and a round that fanned out would be spawning children with
// no runner left to wait for them.
//
// It inherits the conversation's client, window and capabilities because a node
// is the same worker doing the same job somewhere quieter. It inherits neither
// the transcript nor the memory file: the brief is the node's whole world by
// construction, and two agents appending to one memory file would be two writers
// on a document neither can see the other editing.
//
// THE MODEL IS THE NODE'S OWN, and the conversation's only when the node has
// none (taskmodel.go). It is read BEFORE this takes a.mu, because the spec lives
// under the graph's lock and this package takes one lock at a time.
//
// AND THE ONE THING IT DOES INHERIT OF WHAT THE PERSON IS REMEMBERED TO WANT:
// the same pre-turn router the conversation runs, asked against this node's
// brief instead of a typed message (memory.go). It travels as WORDS in the
// node's system prompt and never as the store itself — a family of eight nodes
// must not be eight writers on one brain — and it is routed here, before the
// child exists, because a node has no turn of its own to route against. No
// store, no reflex, or a router that answered nothing: the node opens with
// exactly the prompt it always did.
func (a *Agent) newTaskAgent(ctx context.Context, dir string, node *TaskNode, suffix string) (*Agent, error) {
	model := node.model()
	var (
		tasker *TaskGraph
		nodeID uint64
		depth  int
	)
	if suffix == "" {
		tasker, nodeID, depth = node.graph, node.id, node.familyDepth()
	}
	a.mu.Lock()
	parent := a.config
	if strings.TrimSpace(model) == "" {
		model = a.model
	}
	// A TASK WITHOUT TOOLS CANNOT START. The catalog's supported-parameter row
	// is the same capability fact the picker filters on. Swap once to the
	// worker tier; if that is the same incapable model, refuse here rather than
	// spending a request to discover it mid-run.
	if parent.SupportsParameter != nil {
		if supported, known := parent.SupportsParameter(model, "tools"); known && !supported {
			fallback, resolveErr := roles.Resolve(roles.Source(parent.RolesSource), roles.RoleWorker, a.model)
			if resolveErr != nil || strings.EqualFold(strings.TrimSpace(fallback), strings.TrimSpace(model)) {
				a.mu.Unlock()
				return nil, fmt.Errorf("model %s does not support tool use, and the worker tier resolves to the same model", model)
			}
			if ok, fallbackKnown := parent.SupportsParameter(fallback, "tools"); fallbackKnown && !ok {
				a.mu.Unlock()
				return nil, fmt.Errorf("model %s and worker-tier fallback %s do not support tool use", model, fallback)
			}
			node.graph.mu.Lock()
			node.mend = taskModelRescueNote(model, fallback)
			// AND THE ROW SAYS WHAT IT IS RUNNING ON, not what it was asked to run
			// on: the sentence above and [TaskNode.notice]'s model are two halves of
			// one card, and until this line they named different models.
			node.ran = fallback
			node.graph.mu.Unlock()
			model = fallback
		}
	}
	window := parent.ContextWindow
	if !strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(a.model)) {
		// A WINDOW MEASURED FOR ANOTHER MODEL IS NOT A FACT ABOUT THIS ONE. The
		// figure the surface handed down is the conversation model's, and a node
		// running elsewhere gets zero — this package's own conservative default —
		// rather than a number that could be four times the window it actually
		// has. Compacting early costs a summary; overflowing costs the turn.
		window = 0
	}
	client := unwrapCompleter(a.client)
	journal := taskJournalPath(parent.Place, a.sessionID(), node.id, suffix)
	a.mu.Unlock()

	// Written on the node the moment it is minted: the name carries a timestamp,
	// so this is the only moment anybody can learn it, and [Agent.TaskJournal] is
	// how a person opens the node's whole transcript afterwards (task_room.go).
	//
	// A REPAIR ROUND DOES NOT TAKE THE NODE'S JOURNAL OVER. It is a worker with a
	// suffix, its transcript sits beside the node's in the same directory under
	// its own name, and the node keeps pointing at the run that IS the node —
	// the one the person's "read the transcript" means. Repointing it each round
	// would leave the index and the completion note aimed at the last ten percent
	// of a job, with the ninety unreachable.
	if suffix == "" {
		node.setJournal(journal)
	}

	return newAgent(Config{
		memoryBrief: a.memoryBlock(ctx, node.assembledBrief()),
		// The node learns from, and into, the PROJECT'S error→fix file rather
		// than one of its own (fixstore.go states why a node cannot find it
		// alone). A worker hammering a build in a worktree is the richest source
		// of error→fix pairs this product has, and every one of them would be
		// lost in a private file nobody reads.
		fixesDir:       a.config.fixesBucket(),
		Workspace:      dir,
		Model:          model,
		APIKey:         parent.APIKey,
		BaseURL:        parent.BaseURL,
		ContextWindow:  window,
		CompactEnabled: parent.CompactEnabled,
		SessionFile:    journal,
		// ALLOW EVERYTHING EXCEPT THE FLOOR. approval's critical table still
		// turns an allow into a "prompt" for the handful of shapes that destroy
		// a disk or drop the machine, and a prompt in a node is a refusal it can
		// read (consent.go, InTask) — never a question and never a hang.
		ApprovalPolicy: &approval.Policy{Default: approval.ActionAllow},
		AskConsent:     false,
		InTask:         true,
		// InTask is also what makes this agent's calls PATIENT with a provider
		// that is pacing them (agent.go's sessionCompleter), and this is how the
		// node hears about it while it happens: a card that would otherwise show
		// a task working says it is waiting instead.
		pacing: node.pacing,
		// AND A DESIGN THREAD IS A ROOM RATHER THAN A WORKER, which is the one
		// place the two node kinds want different agents. A worker's turns belong
		// to the runner driving it, so a line steered at it lands in the turn it
		// is already in; a design thread spends most of its life with no turn
		// running at all — the page is written, the card is up, and the person is
		// reading it — so a line steered at it has to START one or it is a
		// question nothing ever answers (agent.go's wakeLocked, harness_task.go).
		roomThread: node.kind == TaskKindHarness,
		// AND THE DESIGN THREAD'S ONE EXTRA HAND, wired here for roomThread's
		// reason: a belt is assembled once, when the agent is constructed
		// (agent.go), so a door handed over after this call would be a verb the
		// model is never told it has. It is nil for every other node — the node
		// has no revision lane to close over — which is what keeps revise_design
		// off every other belt (harness_task.go's reviseDoor).
		reviseDesign:   node.reviseDoor(),
		SupportsImages: parent.SupportsImages,
		RolesSource:    parent.RolesSource,
		SearchProvider: parent.SearchProvider,
		SearchFetcher:  parent.SearchFetcher,
		// The person's connected accounts travel too, for the reason the search
		// pair does: a node is the same worker doing the same job somewhere
		// quieter, and work briefed around a mailbox needs the mailbox. What a
		// node CANNOT do is connect a new one — there is nobody in a worktree to
		// ask — and use_service says exactly that when the account is not
		// connected already (tools_connect.go).
		Connect:    parent.Connect,
		connectHub: parent.connectHub,
		// The media belt travels for the search pair's reason: a node briefed to
		// draw a diagram needs the hand that draws it, and the resolver is what
		// says which model does (media_contract.go).
		Media:      parent.Media,
		MediaModel: parent.MediaModel,
		MediaPick:  parent.MediaPick,
		// A node reads documents on the rung the person chose, like the
		// conversation does (tools_doc.go): the same worker, working somewhere
		// quieter, must not silently drop to a different engine — or to a paid
		// one — because it is running in a worktree.
		DocumentEngine: parent.DocumentEngine,
		// The person's check on task work travels with the work: a sub-task is
		// judged by whatever they said should judge a task, and a family that
		// audited by a different rule than the conversation would be the setting
		// meaning two things (task_audit.go).
		TaskAudit: parent.TaskAudit,
		// And so does who decides a landing nobody could check. A parent node's
		// own agent is the reader of its children's landing notes, so a family
		// running under a different `task.settle` than the conversation would tell
		// a parent to hand a decision to a person it cannot reach (task_contract.go's
		// [TaskSettle]).
		TaskSettle: parent.TaskSettle,
		tasker:     tasker,
		taskID:     nodeID,
		taskDepth:  depth,
		// AND THE ROAD ITSELF, WITHOUT WHICH IT IS OPEN ON PAPER ONLY. Divide is
		// the person's own setting for whether wide work may hand its parts out
		// (cmd/aforge's chatv3.go, config's Swarm), and it is set on the
		// CONVERSATION — which can never divide, because [Config.mayDivide] also
		// wants mayFanOut and a conversation is not in a task. Every agent that
		// CAN divide is built right here, so a constructor that did not carry the
		// setting down was a decision the worker's hands never heard about:
		// divideTools returned nil and renderSystemAt left prompts/divide.md out,
		// for every worker in the running program, while the roster line, the
		// schema and three manual pages all promised the road.
		//
		// It is copied bare rather than gated here on purpose: whether THIS node
		// may divide is one question with one reader ([Config.mayDivide]), which
		// asks the node it was armed on. This line is only the person's yes
		// travelling with the work.
		Divide: parent.Divide,
	}, client)
}

// sessionID names the conversation a node's journal belongs under. A session
// with no file on disk still has a lineage worth grouping by, so the fallback is
// a stable word rather than an empty path segment. Called with a.mu held.
func (a *Agent) sessionID() string {
	if a.file != nil {
		if id := a.file.ID(); id != "" {
			return id
		}
	}
	return "unfiled"
}

// journalID is the same name for callers that do NOT hold a.mu and that must
// tell "there is no journal" apart from "there is one called unfiled". It is
// empty in the first case, and [taskTreeSession] is what decides what an empty
// one becomes — a decision about paths that belongs with the paths, not here.
func (a *Agent) journalID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.file == nil {
		return ""
	}
	return a.file.ID()
}

// taskJournalPath is where a node's own transcript lives: tasks/<when>_<id>.jsonl
// inside the session's own folder (Decision 26), and everything else the node
// spent an agent on beside it under a suffix: <when>_<id>-audit-<nonce>.jsonl
// for one check, <when>_<id>-repair1.jsonl for one repair round.
//
// THE JOURNAL FOLLOWS THE CONVERSATION THAT COMMISSIONED IT. The legacy answer
// is the parallel tree ~/.aforge/v3/tasks/<session>/, which is the same names in
// a directory nobody deleting a session would think to look in; a session that
// has a folder keeps its nodes inside it, and the parallel tree dies with the
// flat layout.
//
// It is a REAL SESSION FILE — the node is an agent, and everything it did is
// resumable and readable with the same tools — kept under the conversation that
// asked for it, so "what did that task actually do" is one directory listing
// away. A machine with no home directory gets an in-memory node instead of a
// failed one: the journal is evidence, not a prerequisite.
//
// The audit gets a file of its own rather than a section of the node's, because
// it is a different agent with a different context: two transcripts written into
// one journal would be the exact context mixing the audit exists to avoid, and a
// person asking "what did the auditor actually run" wants a file to open. The
// SUFFIX IS THE CALLER'S TO MAKE UNIQUE — the stamp here is only good to the
// second, and [newAgent] resumes a file that is already there, so two agents
// handed one path would be one agent with two names (task_audit.go).
func taskJournalPath(place Place, session string, id uint64, suffix string) string {
	name := fmt.Sprintf("%s_%d%s.jsonl", time.Now().Format("20060102-150405"), id, suffix)
	if journals := place.NodeJournals(); journals != "" {
		return filepath.Join(journals, name)
	}
	// The legacy tree, through the one seam: os.UserHomeDir was read directly
	// here, which is why AFORGE_HOME moved every other v3 file and left a node's
	// transcript behind in the real home (Decision 26, "one home, one seam").
	return filepath.Join(home.Dir(), "v3", "tasks", session, name)
}

// unwrapCompleter reaches past the session's own request wrapper.
//
// [sessionCompleter] stamps THIS conversation's prompt-cache key on every
// request it carries. Handing it to a node would put the node's transcript on
// the conversation's cache lineage — two different prefixes asking the same
// replica for a warm head, each cold-starting the other — so the node is built
// on the client underneath and newAgent gives it a lineage of its own.
func unwrapCompleter(client Completer) Completer {
	if wrapper, ok := client.(sessionCompleter); ok {
		return wrapper.inner
	}
	return client
}

// taskLog is the node's job log as a writer, and io.Discard when the registry
// could not open one. A node whose log file failed still runs.
func taskLog(listed *job) io.Writer {
	if listed == nil || listed.sink == nil {
		return io.Discard
	}
	return listed.sink
}

// ── the working copy ────────────────────────────────────────────────────────

// taskTree is where one node works and how its work comes home.
type taskTree struct {
	// dir is the node's workspace: a worktree, or the person's own directory
	// when there is no repository.
	dir string
	// root is the repository the branch merges back into, empty in place.
	root string
	// branch is the node's branch, empty in place.
	branch string
	// merge is the outcome so far: "inplace" for a non-repository, and empty
	// while a branch is still out.
	merge string
	// place is the session folder this tree belongs to, carried for one reason:
	// it is what says where the root repository's lock lives (task_lock.go). It
	// is the zero Place for the legacy layout, and for an in-place tree, which
	// takes no lock at all.
	place Place
}

// gitRoot is the in-process half of the root repository's lock, and the file
// lock beside it (task_lock.go) is the half that reaches the other terminal.
//
// Two nodes finishing at once would otherwise race on the index lock and one
// would fail with git's "another git process seems to be running", which is a
// merge lost to a coincidence rather than to a conflict. Node-local work (the
// commit inside its own worktree) is not serialized: a worktree has an index of
// its own, and that is the whole point of it.
var gitRoot sync.Mutex

// prepareTaskTree gives one node a place to work.
//
// A repository gets `git worktree add` on a fresh branch off the person's
// CURRENT HEAD — that is what makes the node's work a merge later, and what
// keeps a killed node's half-finished edits out of the person's checkout.
// Anything else — not a repository, or a repository with no commit to branch
// from — runs IN PLACE and says so, because pretending to isolate is worse than
// not isolating.
//
// THE PATH CARRIES THE SESSION, not just the node's id. Ids come from a counter
// that starts at one in every fresh conversation, so a directory named by the id
// alone is a name two windows on one repository both pick within a minute of
// each other — and the loser's live worktree, with everything it had not
// committed yet, is what the winner cleared out of the way. The branch name has
// always been discriminated this way (the shortID below); this is the same law
// applied to the place the work actually sits.
//
// A SESSION WITH A FOLDER PUTS THEM IN ITS OWN (Decision 26): trees/<id>/ under
// the session, so the person's repository is borrowed and never littered, and so
// deleting a session is removing one directory. Nothing else about the branch
// law changes — the same `git worktree add -b` off the same HEAD, the same merge
// home — because git registers a worktree wherever it lives. The session in the
// path stops being a discriminator and becomes a containment: the path IS inside
// one session's folder, so the forced remove below can only ever be reclaiming
// after ourselves.
func prepareTaskTree(place Place, workspace, session string, id uint64, title string) (taskTree, error) {
	root, ok := repositoryRoot(workspace)
	if !ok {
		return taskTree{dir: workspace, merge: mergeInPlace}, nil
	}
	if _, err := git(root, "rev-parse", "--verify", "HEAD"); err != nil {
		// A repository with no commits has no HEAD to branch from. The honest
		// answer is the non-repository one.
		return taskTree{dir: workspace, merge: mergeInPlace}, nil
	}

	dir := filepath.Join(root, filepath.FromSlash(tasksDirName), taskTreeSession(session), strconv.FormatUint(id, 10))
	mode := os.FileMode(0o755)
	if trees := place.Trees(); trees != "" {
		dir, mode = filepath.Join(trees, strconv.FormatUint(id, 10)), 0o700
	}
	branch := "task/" + slugify(title) + "-" + shortID()

	defer lockGitRoot(place, root)()
	if err := os.MkdirAll(filepath.Dir(dir), mode); err != nil {
		return taskTree{}, err
	}
	// A directory already at this name belongs to a run of THIS session that is
	// no longer running, so it is cleared out of the way — the worktree
	// registration first, so the add that follows does not fail on a stale one.
	//
	// That it can only be our own is the whole point of the session in the path.
	// One live process holds one session id, because the transcript that names it
	// is flocked while it is open (sessionfile.go), and the id space under it is a
	// counter this process owns. So the only way to find this directory occupied
	// is to have been here before and died — a killed aforge, a crash, a resume
	// that is re-running a node its checkpoint still calls queued — and reclaiming
	// after ourselves is the one case where a forced remove destroys nothing
	// anybody is still using. Before the session was in the path this same code
	// was as likely to be deleting another window's live work.
	if _, err := os.Stat(dir); err == nil {
		_, _ = git(root, "worktree", "remove", "--force", dir)
		_, _ = git(root, "worktree", "prune")
		_ = os.RemoveAll(dir)
	}
	if out, err := git(root, "worktree", "add", "-b", branch, dir, "HEAD"); err != nil {
		return taskTree{}, fmt.Errorf("git worktree add: %s", firstLine(out))
	}
	return taskTree{dir: dir, root: root, branch: branch, place: place}, nil
}

// taskTreeSession is the path segment that keeps one window's worktrees away
// from another's: the conversation's own id, which is 16 random hex characters
// minted per session file (sessionfile.go's newSessionID).
//
// A session with no file on disk still needs a name nobody else will pick, and
// it cannot borrow the journal's — there isn't one. It gets this process's
// nonce instead, minted once and used by every unfiled node in it, so the
// grouping still holds and two unfiled aforges still cannot collide. The one
// thing it may NOT be is a constant like "unfiled", which is the bug this
// function exists to prevent wearing a friendlier name.
func taskTreeSession(session string) string {
	slug := slugify(session)
	if strings.TrimSpace(session) == "" || slug == "task" {
		// slugify answers "task" for anything with no character in it a path may
		// carry, and a shared fallback is the collision this guards against.
		return unfiledSession()
	}
	return slug
}

// unfiledSession is this process's stand-in for a session id, minted on first
// use and stable for the life of the process.
var unfiledSession = sync.OnceValue(func() string { return "unfiled-" + shortID() })

// comeHome commits whatever the node wrote and merges its branch into the
// person's. It reports the outcome and, ONLY when the outcome needs explaining,
// one line for the node's report: an ordinary merge is already said by the
// Merge and Branch fields and by the completion note, and saying it a third
// time inside the report is the same sentence three times on one card.
//
// THE MERGE IS ATTEMPTED WHATEVER THE PERSON'S TREE LOOKS LIKE. A dirty
// checkout is the normal state of somebody who has been working, and refusing
// to try would make "I had a file open" the reason a finished task did not
// land. If git cannot do it — a real conflict, or local changes it would have
// to overwrite — the branch is KEPT and named, and nothing of the node's work
// is lost.
func (t taskTree) comeHome(title string) (string, string) {
	if t.merge == mergeInPlace || t.root == "" {
		return mergeInPlace, ""
	}
	_ = commitTaskWork(t.dir, title)

	defer lockGitRoot(t.place, t.root)()
	// THE MERGE COMMIT CARRIES THE SAME NAME THE NODE'S OWN COMMIT DID
	// ([commitTaskWork]). A merge that is not a fast-forward writes a commit,
	// and git refuses to write one for a checkout with no user.name — which is
	// every hermetic HOME and some fresh machines — so without these two flags
	// a clean merge came back as "conflicted: Committer identity unknown" and
	// the branch was kept for a conflict that never existed.
	if out, err := git(t.root,
		"-c", "user.name=aforge", "-c", "user.email=aforge@localhost",
		"merge", "--no-edit", t.branch); err != nil {
		// --abort is best-effort: a merge that never started (git refused
		// before touching the index) has nothing to abort, and it says so.
		_, _ = git(t.root, "merge", "--abort")
		return mergeConflicted, fmt.Sprintf("its branch %s did not merge cleanly and was kept: %s",
			t.branch, firstLine(out))
	}
	// The branch is gone only once its work is in: removing the worktree first
	// keeps `git branch -d` from refusing on a checked-out branch.
	if _, err := git(t.root, "worktree", "remove", t.dir); err != nil {
		_, _ = git(t.root, "worktree", "remove", "--force", t.dir)
	}
	_, _ = git(t.root, "branch", "-d", t.branch)
	// And the session's own directory once its last worktree has gone home. The
	// remove is deliberately not recursive: it succeeds on an empty directory and
	// fails on one that still holds a node, which is precisely the question being
	// asked. Without it every conversation that ever ran a task would leave an
	// empty directory in the repository forever. Under a session folder the same
	// remove empties trees/ when the last node comes home, which costs nothing
	// and leaves the folder listing honest.
	_ = os.Remove(filepath.Dir(t.dir))
	return mergeMerged, ""
}

// commitTaskWork puts everything the node wrote into one commit on its own
// branch. Without it there would be nothing to merge: a node's work is files on
// disk, and git only moves what has been committed.
//
// The identity is passed per-command rather than configured, so a machine with
// no git identity still commits and the person's own config is not touched.
// "nothing to commit" is not a failure — a node that only read is a node with
// an empty branch, and an empty branch merges cleanly.
//
// It ANSWERS WITH THE FILES IT COMMITTED, read off the index it just built,
// because the index is the only complete account of what a node left behind: a
// hand that saved a file under a name it chose for itself is in there, and no
// argument the model wrote ever said that name ([savingTools]). A node whose
// branch never comes home is told about its work out of this list.
func commitTaskWork(dir, title string) []string {
	if !stageTaskWork(dir) {
		return nil
	}
	saved := stagedPaths(dir)
	_, _ = git(dir,
		"-c", "user.name=aforge", "-c", "user.email=aforge@localhost",
		"commit", "--no-verify", "-m", "task: "+clip(firstLine(title), 72))
	return saved
}

// stagedPaths is what the index holds that HEAD does not: the node's whole
// change, by name, repo-relative and already slash-separated by git.
func stagedPaths(dir string) []string {
	out, err := git(dir, "diff", "--cached", "--name-only")
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}

// stageTaskWork puts everything the node wrote into the worktree's index, and
// it is the step BOTH the audit and the commit need.
//
// The audit needs it because `git diff` shows tracked files only: a node whose
// whole change was three new files has an empty diff and a full index, and an
// auditor reading the empty one refutes good work for a reason that has nothing
// to do with the work. `git diff --cached` in a staged tree shows all of it.
// The commit needs it because git only moves what has been staged. Running it
// twice is free — the second add finds nothing new — and running it once from
// two places would be the audit and the merge disagreeing about what the node
// wrote.
//
// The harness's own droppings are not the node's work: a background job the
// node started wrote its log under the workspace (jobs.go), and a build log in
// the diff — or merged into the person's branch — is noise they did not ask for.
func stageTaskWork(dir string) bool {
	if _, err := git(dir, "add", "-A"); err != nil {
		return false
	}
	_, _ = git(dir, "reset", "--quiet", "--", aforgeDroppings)
	return true
}

// repositoryRoot is the top of the repository a directory sits in.
func repositoryRoot(dir string) (string, bool) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(out)
	if root == "" {
		return "", false
	}
	return root, true
}

// git runs one command in a directory and returns its combined output. There is
// no context: every call here is a local plumbing command that either answers
// immediately or is a broken repository, and a node's own deadline already
// bounds the run it belongs to.
func git(dir string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = dir
	// A pager or an editor in the middle of a merge would hang a node forever on
	// a terminal it does not have.
	command.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	out, err := command.CombinedOutput()
	return string(out), err
}

// slugify turns a title into the branch-name half a person can read:
// lowercase, one dash between words, nothing git has to be escaped from.
func slugify(title string) string {
	var slug strings.Builder
	dash := false
	for _, char := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			slug.WriteRune(char)
			dash = false
		default:
			if !dash && slug.Len() > 0 {
				slug.WriteByte('-')
				dash = true
			}
		}
		if slug.Len() >= taskSlugLimit {
			break
		}
	}
	trimmed := strings.Trim(slug.String(), "-")
	if trimmed == "" {
		return "task"
	}
	return trimmed
}

// shortID keeps two tasks with the same title on two different branches — the
// same work proposed twice in one session is the ordinary case, not a mistake.
func shortID() string {
	var raw [3]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano()%0xffffff, 16)
	}
	return hex.EncodeToString(raw[:])
}
