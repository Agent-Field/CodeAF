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
// Four things end a node's work, and every one of them has a name the person
// can read: its deadline, its step budget, its no-progress count, and a run of
// actions that produced nothing that was not already there
// (harness-research-notes.md §1 — Argus terminates on named thresholds rather
// than on a reviewer's judgement, arXiv:2608.05144). The fourth is the only one
// that does not end anything on its own: reaching it is what makes the harness
// ASK, with the count and the working copy in front of whoever is asked
// (effects.go). A node that has taken
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
	"errors"
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
	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
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
	//
	// SIX IS NOT RETUNED FOR A FAST WORKER. A node calling a tool every three
	// seconds spends six dead steps in eighteen of them, which reads like a hair
	// trigger and is not one: the number bounds what is SPENT — six model
	// round-trips and six tool calls that added nothing — and a worker that is
	// cheap per step is not thereby entitled to more of them. What was wrong in
	// the runs that made this comment was never the six; it was three shapes of
	// real work being counted as dead ([addedSomething]).
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
	// waitingOnItsParts is a RUNNING node that handed its work out and is parked
	// on the reports of the parts it handed out ([TaskGraph.park]). It is the
	// PARENT-STAYS state said out loud: the node is not finished, nothing has
	// gone wrong with it, and it is deliberately not being asked anything until
	// its parts are in (task_divide.go's parent-stays law). Before this it was
	// drawn as a node simply running, which is the one reading a person must not
	// be left with — a row that has said nothing for four minutes and a row that
	// is waiting on three workers look identical, and only one of them is worth
	// worrying about.
	waitingOnItsParts = "its parts"
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
	kind TaskKind
	// Ground is the repository or folder THIS WORK IS ABOUT, absolute, and Mode
	// is how the node stands on it ([TaskMode]). They are settled before the
	// node's first tool call — at the door for work somebody proposed
	// (taskstands.go), and by the working copy itself for everything else — and
	// they never move afterwards.
	//
	// THEY ARE WHAT EVERY OTHER PART OF THE MACHINERY ASKS. The guard asks which
	// directory a write may land in, the audit asks which repository a clean
	// restore is cut from, a card asks which project to name, and a checkpoint
	// carries them so a resumed node knows the same thing this one did. Before
	// they existed each of those answered for itself out of the worktree it
	// happened to hold, and issue #76 is what that cost: four parts of one
	// program disagreeing about which repository an hour of work was for.
	//
	// They are exported among unexported neighbours because they are read from
	// every one of those places by name; like the fields around them they are
	// guarded by the graph's lock and written once.
	Ground string
	Mode   TaskMode
	// Rung is which rung of the ground ladder made this node's world and Seal is
	// the one string that names that world — furrow's sealed snapshot, or the
	// machine commit's sha (groundladder.go). They are written beside Ground and
	// Mode by [TaskNode.setTree], from the tree that was actually made, and they
	// are what lets a report say what world the work was done in.
	Rung GroundRung
	Seal string
	// Frozen is THE WORLD THIS NODE STARTS FROM, when it is a part of a family
	// that froze one: the commit its parent's division put the family tree at
	// before any part of it was admitted (task_divide_wip.go). The ground ladder
	// carves this node's working copy from it and seals nothing.
	//
	// WITHOUT IT THE SIBLINGS GET DIFFERENT WORLDS. A part's working copy is
	// prepared lazily, when the frontier starts it, and a parent goes on working
	// while its parts run — so two parts cut a minute apart would each inherit
	// whatever the parent's directory happened to hold at that instant, and the
	// division that named their boundaries would have described neither.
	//
	// IT IS THE CHILD'S OWN FIELD AND NOT A LOOKUP ON THE PARENT, which is the
	// difference between a fact and a variable: a parent may divide more than
	// once, and a second division reading a field the first one wrote would put
	// the new parts in the old world — or, worse, move the old parts' world under
	// them. It is written at admission, from the spec, and never again.
	//
	// IT IS ON THE CHECKPOINT (task_store.go) because a resumed part must not
	// reseal: coming back after a restart and inheriting the parent's tree as it
	// stands NOW would be the same divergence arriving through the one road that
	// does not prepare its tree at the door.
	Frozen string
	// Base is the machine commit the parent's world was sealed into and Universe
	// is furrow's name for the fork, when a rung made either. They are here for
	// the SAME REASON Rung and Seal are — the landing needs them and the landing
	// does not always happen in the run that made the world. A person who accepts
	// a task hours later, or a session resumed after a crash, rebuilds the
	// working copy from these four fields ([TaskNode.workingCopy],
	// [TaskNode.resumeTree]); without them the inheritance would be merged back
	// over the person's own edits and a branch in a fork would be looked for in
	// the wrong repository.
	Base     string
	Universe string
	// Expects is THE CHECKABLE HALF OF THE HANDOFF this node was given: what its
	// brief assumes is already true of the world it gets (handoffcontract.go).
	// It sits beside Ground for the same reason Rung does — the ground says
	// which world, and this says what the world was promised to contain — and it
	// is written once, at admission, from the spec.
	//
	// It is deliberately NOT on the checkpoint. The contract is answered before
	// the node's first step, so a node that comes back from a checkpoint has
	// already been through it, and carrying the manifest forward would only
	// invite a second reading of a question that has been settled.
	Expects []Expectation
	brief   string
	// adjudicated says this node has already spent its one tiebreak: a division
	// the evidence gate refused on the floor has been put to the mastermind once
	// on the strength of a judge's wide reading, and the answer — whatever it was
	// — is the answer ([TaskNode.takeTiebreak], task_divide.go). It is guarded by
	// the graph's lock like every other field a worker's goroutine touches, and it
	// is deliberately NOT on the checkpoint: a restart loses the judge's reading
	// too (task_store.go re-arms from the text alone), so a node that comes back
	// cannot reach this path at all.
	adjudicated bool
	state       TaskState
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
	wrote []string
	// receipts is the tail of what the node's last worker actually RAN, kept for
	// the one reader that never watched it happen: the auditor
	// ([lastToolReceipts], task_audit.go). It is not in the checkpoint and it is
	// not drawn anywhere — it is evidence for one question, refreshed by whichever
	// worker spoke last, and a resumed node simply has none.
	receipts []toolReceipt
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
	// parkGen counts the parks this node has taken, and it is what makes ONE
	// park tellable from the next. `parked` alone cannot: a parent wakes on a
	// report, reads it, finds a part still outstanding and parks again, and to
	// anybody watching the flag from outside those are the same true. A waiter
	// that wants the park AFTER a landing therefore has to name the park it
	// already saw and wait past it ([TaskNode.parkStanding]), which is a fact
	// about identity and not about how long to wait — no clock can answer it.
	//
	// It only ever goes up, one per park, under the graph's own lock, and it is
	// never reset: a node that parks and lands and is resumed is still the node
	// that parked, and a generation that started again would make an old park
	// look like a new one.
	parkGen uint64
	// paced counts this node's calls that are parked on the provider's pacing
	// (internal/provider's patience.go). A count and not a flag because a node
	// is an agent and an agent can have more than one call out — a repair round
	// beside an audit — and the node stops being paced when the LAST of them
	// gets through, not when the first does.
	paced int
	// beat is this node's pulse on disk while it runs, and nil for a node that
	// is not running or has no store behind it (task_beat.go). Every agent that
	// stands in for the node carries a pointer to the same one — the worker, the
	// checker, each repair round — because they are one node working, and a
	// reader outside the process is asking about the node.
	beat *taskBeat
	// ctx is the context this node's whole run happens under and cancel ends it:
	// cut by jobs kill, by a person's stop, or by Close.
	//
	// BOTH ARE MADE WHEN THE NODE IS STARTED AND NOT INSIDE THE GOROUTINE THAT
	// RUNS IT ([TaskGraph.runFrontier]), which is the law issue #381 was: a node
	// that becomes reachable only from inside its own goroutine is a node a
	// quit cannot see for as long as that goroutine takes to be scheduled — and
	// what it did in that window was open a log, build a child agent and spend
	// on two model calls for a session that had already left.
	ctx    context.Context
	cancel context.CancelFunc
	// claimed says A RUNNER HAS TAKEN THIS NODE UP: [Agent.runTaskNode] sets it
	// in the one hold of the lock that also reads the context it is about to run
	// under ([TaskNode.claimRun]), and a test's stand-in for a run sets it
	// through [TaskNode.setCancel]. It is never cleared, because a node is taken
	// up once.
	//
	// IT IS THE QUESTION A STOP ASKS BEFORE IT PROMISES TO WAIT. That question
	// used to be asked of `cancel` — nil meant nobody was running this node, so
	// nothing was ever going to settle it and the stop settled it itself
	// (cancel.go's [TaskGraph.stop]). The frontier now mints the cancel at
	// admission so a quit can reach a node whose goroutine has not started yet,
	// which left `cancel` unable to answer it: every running node has a handle
	// now, including the ones nobody has picked up. So the fact is written down
	// on its own rather than inferred from a pointer that no longer means it.
	claimed bool
	// stopped marks a node a PERSON ended (cancel.go). It is set BEFORE the
	// context is cut, so that the landing this stop causes already knows whose
	// decision it was — a flag written afterwards would be a flag the update
	// announcing the end raced past.
	stopped bool
	// ending is why this node stopped where it did, once it has (task_contract.go's
	// [TaskEnding]), and "" until then and forever on a node that finished. THE
	// FIRST CAUSE WINS: [TaskNode.end] refuses to overwrite one already written,
	// because a check that refuses a run that had already given up is not the
	// news — the giving up is.
	ending TaskEnding
	// checked is WHAT THE CHECK SAID about this node's work, in the ledger's own
	// taxonomy, and "" on a node no check ever read — one whose worktree could
	// not be made, one somebody stopped before the gate, one that ran with the
	// check turned off. It is the grade a settled node teaches the ratings store
	// (taskgrade.go), and it is written down at the moment the answer is given
	// because that is the only moment anybody holds it: by the time the node
	// settles, the whole of the check is a paragraph of prose in a report.
	//
	// repairs is how many times the work was handed back before that answer —
	// the repair rounds the gate spent (task_audit.go's [Agent.auditWithRepair]).
	checked provider.Verdict
	repairs int
	// blockedBy names the task whose working copy refused this node's writes
	// (treehold.go's treeClaimGuard), in the words the refusal used, and "" when
	// nothing ever refused it. It is what turns a "going in circles" ending into
	// a "blocked" one: a worker that was told no every time it tried to write is
	// not circling, it is queued behind somebody, and the row should say whom.
	blockedBy string
	// room is the node as a PLACE: the child agent somebody can talk to and the
	// live subscribers watching it work (task_room.go). It is nil until the
	// first person enters or the runner attaches its child, and it is emptied
	// when the node lands.
	room *taskRoom
	// journal is where this node's transcript was written, recorded when its
	// child agent was built. It is the node's history, and it outlives the room
	// — and the process: it rides the checkpoint (task_store.go's
	// taskRecord.Journal), so a resumed session opens a finished task on its
	// whole transcript rather than on a blank page.
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
	// life is which of the node's three lives it is in right now — the worker,
	// the check, a repair round — in the words task_contract.go exports
	// ([TaskPhaseWorking] and the other two). "" is a node that has not started.
	//
	// IT IS HELD HERE SO THAT A READER OF THE GRAPH DOES NOT HAVE TO OPEN A FILE
	// FOR IT. The pulse on disk has the same word (task_beat.go) and is the right
	// answer for a reader in ANOTHER process; a row this session builds for its
	// own home page has the node in its hand, and reaching through the pulse's
	// mutex — which a disk write is held under — from inside the graph's lock is
	// exactly the thing armBeat is two steps to avoid. [Agent.enterPhase] writes
	// all three of the places this word lives, so they cannot disagree.
	life string
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
	// repaired is the model a REPAIR ROUND ran on, when the cascade moved that
	// round off the model the node itself is on (repair_role.go). It is "" for
	// every node that was never sent back, and for every node whose repair
	// floored onto its own model — an all-flash crew, or a model somebody named.
	//
	// IT IS THE BILL AND NOT THE ROW. [TaskNode.ran] answers "what is this node
	// running on", which is a fact a surface draws; this answers "what did the
	// last ten percent of this node cost extra", which is a question asked of the
	// project's record afterwards, so it reaches the index
	// ([TaskNode.indexEntryLocked]) and nothing a person is looking at while the
	// work runs.
	repaired string
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
	// runners counts the node goroutines this graph has out: one Add per node
	// the frontier starts, taken UNDER THE LOCK beside the transition that
	// authorized it, and one Done as that goroutine returns.
	//
	// IT IS HOW THE QUIT KNOWS THE GRAPH IS EMPTY ([TaskGraph.stopAll]), and it
	// counts goroutines rather than landings for a reason a `done` channel
	// cannot: a node whose run is interrupted by Close returns through
	// [TaskGraph.handBackLane] WITHOUT settling, on purpose, so that recovery
	// resumes it — its `done` is never closed, and a stop that waited on that
	// channel would wait for something nobody is going to send.
	runners sync.WaitGroup
	// quitting is set once, by the graph's bounded stop, and it is what makes a
	// closed session a session with no running nodes: after it the frontier
	// starts nothing, so no goroutine can join the wait that is already under
	// way. It is read and written under `mu` beside the Add above, which is
	// what keeps a node from being counted after the count is being waited on.
	quitting bool
	// report is called once per node reaching a final state, outside the lock.
	// It is how the world hears: the update event, and the note that reaches the
	// model through the steering lane.
	report func(*TaskNode)

	// store is where the graph is written down after every transition, and nil
	// for a graph with no journal behind it — a session with no file, and every
	// scripted graph in the tests (task_store.go).
	store *taskStore

	// grades is where a settled node's outcome is written down and where the
	// divider reads before it decides how much thinking a part is done with
	// (taskgrade.go). It is nil for a session with no profile directory behind
	// it — a headless run and every scripted graph in the tests — which grades
	// nothing and reads nothing, exactly as `store` is nil for a session with no
	// journal.
	grades *taskGrades

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
		// AND THE RATINGS STORE, WHICH IS THE PERSON'S AND NOT THIS SESSION'S.
		// It lives beside the settings file, it is the same file `aforge models`
		// reads, and several aforge processes write to it at once — so what is
		// held here is the path and never a handle (taskgrade.go).
		graph.grades = newTaskGrades(a.config.ProfileDir)
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
	spec.armed = g.home.armDivision(spec)
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
		// WHERE THE WORK STANDS, carried from the door that resolved it
		// (taskstands.go). A door that resolved none — a design, a subharness run,
		// a scripted graph — admits with nothing here and the working copy fills it
		// in from the repository it is cut from ([TaskNode.setTree]).
		Ground: spec.ground,
		Mode:   spec.mode,
		// AND THE WORLD IT IS TO START FROM, when its division froze one
		// (task_divide_wip.go). It arrives with the spec so that it is written
		// and checkpointed in the same breath the node is admitted in — a part
		// that existed for even an instant without knowing its world is a part
		// the frontier could start on the wrong one.
		Frozen: spec.frozen,
		// AND WHAT ITS BRIEF ASSUMES, carried from whoever wrote the brief
		// (handoffcontract.go). Every door admits with nothing here except the
		// two that ask a model for a handoff, which is the point: the harness
		// never writes an expectation on anybody's behalf.
		Expects: spec.expects,
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
	// A CLOSED SESSION HAS NO RUNNING NODES, so a pass that arrives after the
	// graph's bounded stop starts nothing ([TaskGraph.stopAll]). Every pass this
	// refuses is one a node winding up caused — a hand-back parks and turns the
	// frontier — and starting fresh work on the way out is how the orphan of
	// issue #381 was made in the first place.
	if g.quitting {
		g.mu.Unlock()
		return
	}
	var starting, failing, waiting []*TaskNode
	var releases []context.CancelFunc
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
		hold := g.holdOnStartingLocked(node, ready, busy)
		if hold == waitingMachineBusy {
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
		// THE NODE'S CONTEXT IS MADE HERE, IN THE SAME HOLD OF THE LOCK THAT
		// MARKED IT RUNNING, and not inside the goroutine below. That is what
		// makes a node reachable from the moment it starts: a stop reads
		// `cancel` under this same lock, so there is no window in which the
		// graph believes a node is running and has no handle on it. A node
		// belongs to the process and not to a turn or a surface, so the parent
		// is Background — detaching a renderer ends nothing here.
		ctx, cancel := context.WithCancel(context.Background())
		node.ctx, node.cancel = ctx, cancel
		starting = append(starting, node)
		// The handle is kept here as well as on the node, because the goroutine
		// below must release THE CONTEXT THIS PASS MADE — reading it back off
		// the node would read whatever is there by then, which in the tests is
		// a stub somebody else installed.
		releases = append(releases, cancel)
	}
	// AND THE GOROUTINES ARE COUNTED BEFORE THEY EXIST, under the lock the quit
	// reads `quitting` under, so that a node cannot join the count after the
	// quit has started waiting on it.
	g.runners.Add(len(starting))
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
		g.announce(node)
		close(node.done)
	}
	for at, node := range starting {
		g.announce(node)
		// The run is wrapped rather than started bare so that the two things
		// that are true of EVERY node's goroutine, whatever `run` is — the
		// count it was already added to comes back down, and the context made
		// above is released — happen on every road out of it, including the
		// scripted runners the tests put here.
		//
		// THE HOOK ITSELF IS READ HERE AND PASSED IN, on this goroutine, which is
		// where a bare `go g.run(node)` read it too. Reading it from inside the
		// new goroutine instead would move that read off the road the caller is
		// on, and a test that installs its runner right after starting a node
		// would be racing a read that used to have happened already.
		go func(node *TaskNode, release context.CancelFunc, run func(*TaskNode)) {
			defer g.runners.Done()
			defer release()
			run(node)
		}(node, releases[at], g.run)
	}
	// A cascade needs one more pass: the nodes just failed may block others,
	// and a failure frees no slot, so nothing else can have moved.
	if len(failing) > 0 {
		g.runFrontier()
	}
}

// holdOnStartingLocked is what is holding one READY node back, in the words the
// surface draws, and "" for a node that may start now.
//
// It is read with the graph's lock held, by the frontier, once per queued node
// per pass. The order of the arms IS the policy and it is why they are a switch
// rather than a set of ifs: the first true one is the reason a person is shown.
//
// A node that is not ready is held by its own edges and says NOTHING here:
// DependsOn is already on the notice, and a second word for the same fact would
// be the wire saying it twice.
func (g *TaskGraph) holdOnStartingLocked(node *TaskNode, ready, busy bool) string {
	switch {
	case !ready:
		return ""
	// A NODE THAT TAKES NO SLOT IS HELD BY NEITHER CEILING, and it is the one
	// case that has to come before both of them ([taskSpec.takesSlot] makes the
	// whole argument). The two ceilings model a node as an agent with a checkout
	// and a build; a design is two model calls and a person reading a card, and
	// queueing one behind a full machine would be a harness nobody can start
	// because the machine is busy running tasks.
	case !node.takesSlot():
		return ""
	case g.limit > 0 && g.running >= g.limit:
		return waitingSlot
	case busy:
		return waitingMachineBusy
	}
	return ""
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

// stopAll is THE GRAPH'S BOUNDED STOP AT QUIT, and the whole of it: nothing
// more starts, every node that is running is cut, and the quit waits — bounded
// by `grace` — for their goroutines to return.
//
// A CLOSED SESSION HAS NO RUNNING NODES. Before this, Close reached nodes only
// through the jobs round, which walks a registry a node puts itself in as its
// first act — so a node admitted in the last moments of a session was invisible
// to the quit for as long as its goroutine took to be scheduled, and what it
// did next was open a log under a session that had left, build a child agent
// and spend on model calls nobody could stop or find (issue #381).
//
// THE WAIT IS ON THE GOROUTINES AND NOT ON THE NODES' `done`, and that is not
// an accident of implementation: a run this cancels returns through
// [TaskGraph.handBackLane] WITHOUT settling, deliberately, so that recovery
// resumes it — its `done` never closes, and a wait on it would be a quit that
// hangs for the whole grace on every node it stopped.
//
// IT IS NOT [Agent.Abandon], which is ONE TURN'S bounded stop at a person's
// escape and never touches a node: that ends the work of answering, this ends
// the work itself, and a session that abandons a turn goes on running its
// tasks. Two bounded stops in one package are two mechanisms, and they are
// named beside each other here so that nobody later unifies them.
//
// It is bounded because it is what a person's quit waits behind. Past the grace
// a straggler is left to the round below — the registry closes there, so a node
// that surfaces after this can no longer register anything into a session that
// has gone.
func (g *TaskGraph) stopAll(grace time.Duration) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.quitting = true
	var cuts []context.CancelFunc
	for _, id := range g.order {
		node := g.nodes[id]
		// A node that has landed has already released its own context, and one
		// that never started has none to release; what is left is exactly the
		// work this session is doing.
		if node == nil || node.state != TaskRunning || node.cancel == nil {
			continue
		}
		cuts = append(cuts, node.cancel)
	}
	g.mu.Unlock()

	// OUTSIDE THE LOCK, because a cancel wakes a run that will immediately ask
	// the graph for its lane back.
	for _, cut := range cuts {
		cut()
	}

	// The wait is a goroutine over the count so that it can be given a deadline:
	// one that outlives the grace outlives it holding nothing, and the process
	// is on its way out behind it.
	landed := make(chan struct{})
	go func() {
		g.runners.Wait()
		close(landed)
	}()
	waitDone(landed, grace)
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
	brief := g.inheritedLocked(node)
	if orders != "" {
		// The section is rendered with a trailing newline for the block the
		// conversation wraps it in; a brief is prose and ends where it ends.
		brief += "\n\n" + strings.TrimRight(orders, "\n")
	}
	return brief
}

// inheritedLearnedLead opens the section that hands a node what ran before it.
// The count of reports is appended when any are present — the same idiom
// PERF.md records for `openFindingsLimit`: the worker is told how many there
// are, so a sink writing a six-row table can count. A heading with nothing
// under it is not printed (the emptiness law).
const inheritedLearnedLead = "What the work before you learned"

// inheritedReportFloor is the smallest share one prerequisite's report may be
// given. Below this a fragment names nothing a worker can act on — a title and
// an ellipsis — and the model reads past it the way the fanin6 sink read past
// the two sections it was never shown.
const inheritedReportFloor = 512

// inheritedLocked is the half of the brief above that A PART OF THIS WORK
// INHERITS: what the node was admitted with, and what the work before it
// learned. It is a function of its own because the division road composes every
// part's world on it (task_divide_compose.go), and the standing orders are the
// one section it must not carry — the frontier appends those to each part in its
// own right a moment later, so a part composed on the whole assembled brief
// would read the house rules twice.
//
// THE TASK'S OWN BRIEF IS NEVER CUT. Its ask, its work, what to produce and its
// done conditions are the contract. The reports take what is left of
// [taskShapeBriefLimit] after that brief and the heading that names how many
// there are. They share that room equally; a short report's unused share is
// handed to whoever is still clipped, so one long finding cannot starve five
// short ones.
//
// NO PREREQUISITE IS EVER DROPPED. A capped pot once fed a sink four of six
// sections and it wrote a confident four-row table — no node failed, the
// delivery gate passed it, the only evidence was the row count. The list gets
// thinner, never shorter. When the remaining room cannot hold every report at
// [inheritedReportFloor], the bound YIELDS and each report still gets the
// floor: arithmetic that would have produced a zero-length share is how the
// four-of-six pot happens again.
//
// A clipped report is marked with [clip], which is the one fitting mark on this
// road. [familyOf] will fit this whole string a second time as its ground; clip
// moves a trailing mark rather than stacking one, so a report is marked once
// or not at all.
func (g *TaskGraph) inheritedLocked(node *TaskNode) string {
	type learned struct {
		title  string
		id     uint64
		report string
	}
	var reports []learned
	for _, id := range node.dependsOn {
		prerequisite := g.nodes[id]
		if prerequisite == nil {
			continue
		}
		report := strings.TrimSpace(prerequisite.report)
		if report == "" {
			continue
		}
		reports = append(reports, learned{
			title:  prerequisite.spec.title,
			id:     id,
			report: report,
		})
	}
	brief := node.spec.brief
	if len(reports) == 0 {
		return brief
	}

	heading := inheritedLearnedHeading(len(reports))
	headers := make([]string, len(reports))
	bodies := make([]string, len(reports))
	overhead := len(brief) + len("\n\n") + len(heading)
	for i, item := range reports {
		headers[i] = fmt.Sprintf("\n\n%s (task %d):\n", item.title, item.id)
		bodies[i] = item.report
		overhead += len(headers[i])
	}
	fitted := shareReports(bodies, taskShapeBriefLimit-overhead)

	var out strings.Builder
	out.WriteString(brief)
	out.WriteString("\n\n")
	out.WriteString(heading)
	for i := range reports {
		out.WriteString(headers[i])
		out.WriteString(fitted[i])
	}
	return out.String()
}

// inheritedLearnedHeading is the lead plus how many reports follow it. The
// count is the cheapest defence against a silent drop: a worker told there
// are six can notice when it can only name four.
func inheritedLearnedHeading(n int) string {
	noun := "reports"
	if n == 1 {
		noun = "report"
	}
	return fmt.Sprintf("%s — %d %s:", inheritedLearnedLead, n, noun)
}

// shareReports allots a pot of bytes across reports so that one long report
// cannot starve the others, and so that NO report is dropped. Each report is
// given an equal first share; unused remainder is handed to whoever is still
// clipped. A share that would fall under [inheritedReportFloor] is lifted to
// the floor — the bound yields before a report vanishes, because a 4 KiB pot
// once fed a sink four of six sections and it wrote a confident four-row
// table. A report that fits its share is passed through unclipped, with no
// mark (the emptiness law). A report that does not is marked by [clip].
func shareReports(reports []string, pot int) []string {
	n := len(reports)
	if n == 0 {
		return nil
	}
	if need := n * inheritedReportFloor; pot < need {
		// THE BOUND YIELDS BEFORE A REPORT VANISHES. N times the floor did not
		// fit in what was left after the task's own brief; shrinking a share
		// to zero, or dropping the tail, is the four-of-six pot again.
		pot = need
	}
	share := pot / n
	allotted := make([]int, n)
	remaining := pot
	for i, report := range reports {
		allotted[i] = share
		if len(report) < share {
			allotted[i] = len(report)
		}
		remaining -= allotted[i]
	}
	for i, report := range reports {
		if remaining <= 0 {
			break
		}
		growth := len(report) - allotted[i]
		if growth <= 0 {
			continue
		}
		if growth > remaining {
			growth = remaining
		}
		allotted[i] += growth
		remaining -= growth
	}
	out := make([]string, n)
	for i, report := range reports {
		out[i] = clip(report, allotted[i])
	}
	return out
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
	g.handBackSlotLocked(node)
	g.mu.Unlock()
	// THE NOTE LANDS BEFORE THE DONE CHANNEL CLOSES. Anything waiting on
	// `done` treats closure as "this node is fully settled", and
	// reportTaskNode is part of that settlement. Closing first was a race:
	// the waiter could read an empty steering queue before the note was
	// enqueued (harness_build_test.go's TestAnApprovedDesignIsSavedAndDetectableAtOnce).
	g.announce(node)
	close(node.done)

	g.checkpoint()
	g.grade(node)
	g.runFrontier()
}

// handBackSlotLocked returns one node's slot to the graph, and it is the ONE
// place that arithmetic is written.
//
// A PARKED NODE HAS ALREADY GIVEN ITS LANE BACK ([TaskGraph.park]), so landing
// it must not give the same one back twice. And a node that never took one — a
// design (see [TaskNode.takesSlot]) — must not hand one back either: both would
// be quietly raising the cap for everybody else, which is the same fault
// [TaskGraph.resettle] refuses one function down.
//
// ITS TWO CALLERS ARE THE TWO WAYS A NODE STOPS HOLDING A SLOT IT TOOK.
// [TaskGraph.complete] is the ordinary one: the runner landed the node. The
// other is cancel.go's [TaskGraph.stop], for a running node no runner had taken
// up yet — there, nobody is coming to land it, so the stop that settles it must
// also give its lane back. The lock is the caller's.
func (g *TaskGraph) handBackSlotLocked(node *TaskNode) {
	if node.parked {
		node.parked = false
		return
	}
	if g.running > 0 && node.takesSlot() {
		g.running--
	}
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
	// AND THE LESSON IS WRITTEN AGAIN, because the node has ended somewhere
	// else. A person accepting work nobody could check, or refuting it, is the
	// answer the first settle did not have — and the store's row for this node
	// is corrected by appending, which is what an append-only diary means
	// (taskgrade.go).
	g.grade(node)
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

// reportHome is [TaskGraph.report] for a graph whose home is filled in AFTER it
// is built, which is the shape a standing firing's own graph has: the session
// that is going to own it cannot be constructed until the graph is in its config
// (standing_run.go's [standingWideWork]). A conversation's graph binds the
// method directly ([Agent.graph]) because by then there is an agent to bind to;
// this reads the same field one moment later and answers nothing before there
// is one.
func (g *TaskGraph) reportHome(node *TaskNode) {
	if g != nil && g.home != nil {
		g.home.reportTaskNode(node)
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

// end writes why this node stopped, once. A second cause is dropped on the
// first-cause law stated on the field: the ending a person reads is the one
// that happened first, not the one that was written last.
func (n *TaskNode) end(ending TaskEnding) {
	if n == nil || n.graph == nil || ending == "" {
		return
	}
	n.graph.mu.Lock()
	if n.ending == "" {
		n.ending = ending
	}
	n.graph.mu.Unlock()
}

// noteBlocked records that another task's working copy refused one of this
// node's writes, naming the holder the way the refusal did.
func (n *TaskNode) noteBlocked(holder string) {
	if n == nil || n.graph == nil || strings.TrimSpace(holder) == "" {
		return
	}
	n.graph.mu.Lock()
	n.blockedBy = strings.TrimSpace(holder)
	n.graph.mu.Unlock()
}

// blockedByNow is [TaskNode.blockedBy] read from outside the lock.
func (n *TaskNode) blockedByNow() string {
	if n == nil || n.graph == nil {
		return ""
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.blockedBy
}

// endingOfClaim is why a run ended when the run itself did not choose to, and
// "" for a worker that simply finished.
//
// IT ASKS TWO THINGS, AND THE FACT COMES FIRST. A turn a process rule stopped
// says so as a value — the rule names its own ending (processrule.go's
// [processRule.ending]) — and that is read before anything else, because the
// loop knew the answer at the moment it stopped the turn and nothing here has to
// infer it. Only then are the run's LAST WORDS read: the loop guard's own
// sentence (looped.go's loopLeftUndoneNote) is the one claim this package wrote
// rather than the worker, and it is the difference between a worker that
// finished and one that was stopped for going in circles. A worker whose writes
// were refused by another task's working copy was not circling but queued, and
// is named as such.
//
// THE SENTENCE IS NOT THE EVIDENCE ANYWHERE IT DOES NOT HAVE TO BE. Matching
// prose a person reads is a reading that breaks silently the day somebody
// improves the wording, so the loop guard's line is matched here only because it
// is the one ending whose fact this package has nowhere else to get.
func endingOfClaim(claim, blockedBy string, stoppedByRule TaskEnding) TaskEnding {
	if stoppedByRule != "" {
		return stoppedByRule
	}
	if !strings.Contains(claim, loopLeftUndoneNote) {
		return ""
	}
	if blockedBy != "" {
		return TaskEndingBlocked
	}
	return TaskEndingCircling
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
// AND THE WORD IS WRITTEN BESIDE THE ID, because a pick made here is a pick.
// [TaskNode.modelPicked] reads the word to answer "did anybody name a model for
// this work" — which is what holds the repair cascade off a node whose model
// somebody chose (repair_role.go) — and a retarget that moved only the id would
// leave the person's own choice looking like an inherited default.
func (n *TaskNode) retarget(model string) {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	n.spec.model = model
	n.spec.modelWord = model
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

// runModel is [TaskNode.runModelLocked] from outside the lock: the model this
// node is ACTUALLY on, which is the frozen admitted id until something moved it.
func (n *TaskNode) runModel() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.runModelLocked()
}

// effortRung is the rung set on this node, under the same lock the model is
// read under and for the same reason: [Agent.SetTaskEffort] can move it while a
// worker is being prepared.
func (n *TaskNode) effortRung() effort.Rung {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.effort
}

// runOn moves a node onto another model WITHOUT touching the id it was admitted
// with, and writes the one line a row carries about why.
//
// It is the difference between a rescue and a choice, and that difference is the
// whole reason it is not [TaskNode.retarget]. A person picking a model in this
// node's room has decided something, and the spec learns it. A worker whose
// provider could not answer has decided nothing — the admitted id is still the
// answer to "what was this work handed to", and a rescue that overwrote it would
// erase the fact that a rescue happened at all.
func (n *TaskNode) runOn(model, note string) {
	n.graph.mu.Lock()
	n.ran = strings.TrimSpace(model)
	n.mend = note
	n.graph.mu.Unlock()
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
	return composeBrief(n.spec.request, n.brief, n.spec.deliverable, n.spec.acceptance,
		expectsSection(n.spec.expects), n.spec.origin)
}

// request is the person's own words, frozen with the rest of the spec. It is
// read by [Agent.taskRequest] so that a sub-task a node hands out inherits the
// sentence that started the family rather than the paraphrase in the middle.
func (n *TaskNode) request() string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.request
}

// origin is the pointer to the person's original words, frozen with the rest
// of the spec. It is read by [Agent.taskOriginRef] so that a sub-task a node
// hands out still points at the human's journal rather than at its own.
func (n *TaskNode) origin() taskOrigin {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.spec.origin
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

// familyPlace is the SESSION FOLDER a node's work belongs in, and it is the
// CONVERSATION'S rather than whichever agent happens to own the node.
//
// Every path a running node needs is arithmetic on one Place: the worktree it
// works in ([prepareTaskTree]), the transcript it writes ([taskJournalPath]) and
// the file its merge takes the repository's lock on (task_lock.go). There is
// exactly one right answer to which Place that is — the session that
// commissioned the family — and reading it off the OWNER was that answer only
// for as long as every node's owner was the conversation.
//
// A NODE THAT HANDS PART OF ITS WORK FURTHER OUT BREAKS THAT. A part's owner is
// its parent's WORKER, whose config carries no Place at all (see
// [Agent.newTaskAgent], which deliberately does not make a node into a second
// session), so a part's worktree landed in the person's own repository under the
// legacy flat layout while its parent's sat inside the session folder (Decision
// 26) — and the two halves of one family took the git root's lock on two
// different files, which is the one thing task_lock.go says must never happen.
//
// The zero Place is still the legacy layout and still answers "" to everything;
// what this fixes is a family disagreeing with itself about which layout it is
// in.
func (a *Agent) familyPlace(node *TaskNode) Place {
	if node != nil && node.graph != nil {
		// home is written once, before any node can run, and read without the
		// graph's lock exactly as [TaskGraph.runner] reads it.
		if home := node.graph.home; home != nil {
			return home.config.Place
		}
	}
	return a.config.Place
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
	// and progress is broader than an edit: a successful edit or write, a bash
	// that leaves the worktree dirtier, a reading taken over work that has just
	// changed, a question the node has not asked before whose answer brought
	// something back, or a re-run whose result was more new than old all reset
	// it to zero ([addedSomething]). What it catches is the spin: the same
	// query, the same file, the same nothing.
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

// setCancel hands the node the handle that ends its run. The frontier writes
// the node's own pair as it starts it ([TaskGraph.runFrontier]); this is for a
// caller that stands in for a run and wants the stop to reach it instead.
//
// STANDING IN FOR A RUN IS TAKING THE NODE UP, so this claims it as well
// ([TaskNode.claimed]): a stop must promise to wait for the caller that said it
// was running this node, rather than settle the node out from under it.
func (n *TaskNode) setCancel(cancel context.CancelFunc) {
	n.graph.mu.Lock()
	n.cancel = cancel
	n.claimed = true
	n.graph.mu.Unlock()
}

// claimRun is the one act that says "this goroutine is now running this node",
// and it answers false when there is nothing left to run.
//
// THE CLAIM AND THE TWO REASONS NOT TO RUN ARE READ UNDER ONE HOLD OF THE LOCK,
// which is the whole of why this is a function rather than three lines at the
// top of [Agent.runTaskNode]. A stop that arrives before the claim settles the
// node itself, because nothing else was ever going to (cancel.go's
// [TaskGraph.stop]); a stop that arrives after it promises to wait for this
// goroutine. Two separate reads across that seam let both happen to one node —
// the stop settling it while this goroutine went on to run it, and the run's own
// landing then closing a `done` that was already closed.
//
// A cut context with the node still running is the other road in: that is the
// quit ([TaskGraph.stopAll]), and the node deliberately stays running so a
// recovery resumes it.
func (n *TaskNode) claimRun() bool {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.state != TaskRunning || n.ctx == nil || n.ctx.Err() != nil {
		return false
	}
	n.claimed = true
	return true
}

// runContext is the context this node's run happens under, with the handle that
// ends it.
//
// It is the pair the frontier made in the same hold of the lock that marked the
// node running. A node that somehow reaches its body without one — no door in
// this package does — is given one here rather than a nil to dereference, and it
// is written to the node so that a stop can still find it.
func (n *TaskNode) runContext() (context.Context, context.CancelFunc) {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.ctx == nil {
		n.ctx, n.cancel = context.WithCancel(context.Background())
	}
	return n.ctx, n.cancel
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

// rememberedWrites is everything this node has already put its name to: the
// live list a running claim is drawn from ([TaskNode.noteWrote], carried in the
// checkpoint) and the list a landing kept. Both, because a node can be resumed
// from either side of a landing and the two are the same fact at two ages.
func (n *TaskNode) rememberedWrites() []string {
	if n == nil || n.graph == nil {
		return nil
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return mergePaths(append([]string(nil), n.wrote...), n.changed)
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
		// AND IT CARRIES THE GROUND AND THE MODE, because "nothing to merge" is
		// not "nothing to do". A MIRROR has no branch either, and its landing is
		// the ledger laid back over the person's folder by name
		// ([taskTree.landMirror]) — so a tree rebuilt without those two fields is
		// one [taskTree.comeHome] reads as an in-place task and returns from
		// having done nothing at all. An accepted or re-audited folder family laid
		// NOTHING home, and the check on one restored an empty world. That is this
		// method's own stated law: a landing is the same landing whenever it
		// happens ([TaskNode.ladderRecord]). Every other mode still lands in
		// place, exactly as it did.
		ground, mode := n.groundNow()
		return n.ladderRecord(taskTree{dir: dir, merge: mergeInPlace, ground: ground, mode: mode}), nil
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return taskTree{}, fmt.Errorf("its working copy is gone from %s, so there is nothing left to look at — its branch %s is still there", dir, branch)
	}
	// THE BRANCH CAME OFF THE GROUND, so the repository it merges back into is
	// the ground's and not whatever this process happens to be standing in — the
	// same argument [TaskNode.resumeTree] makes, and issue #76's. The workspace
	// answers only for a node admitted before a ground was ever recorded.
	ground, mode := n.groundNow()
	root, ok := repositoryRoot(ground)
	if !ok {
		if root, ok = repositoryRoot(workspace); !ok {
			return taskTree{}, fmt.Errorf("%s is no longer a repository, so its branch %s cannot come home", workspace, branch)
		}
	}
	return n.ladderRecord(taskTree{dir: dir, root: root, branch: branch, place: place, ground: ground, mode: mode}), nil
}

// ladderRecord puts back what the ground ladder wrote down about a node's world
// (groundladder.go), on a tree that was rebuilt from the record rather than
// carved in this run.
//
// IT IS WHAT MAKES A LANDING THE SAME LANDING WHENEVER IT HAPPENS. A person who
// accepts a task an hour later, and a session resumed after a crash, both reach
// [taskTree.comeHome] through a tree assembled here; a tree that had forgotten
// which rung made it would look for a branch in the wrong repository and merge
// the parent's own unfinished edits back over them.
func (n *TaskNode) ladderRecord(tree taskTree) taskTree {
	if n == nil || n.graph == nil {
		return tree
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	tree.rung, tree.seal, tree.base, tree.universe = n.Rung, n.Seal, n.Base, n.Universe
	return tree
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

// armBeat starts this node's pulse and hands it back so the runner can stop it
// (task_beat.go). It is called once, by [Agent.runTaskNode], the one door every
// kind of node goes through.
//
// The first row is written OUTSIDE the graph's lock, which is the whole reason
// this is two steps rather than one: a node starting is a state transition the
// frontier makes with the graph held, and a disk write under that lock would put
// every other node in the family behind it.
func (n *TaskNode) armBeat() *taskBeat {
	// A NODE WITH NO GRAPH HAS NO STORE AND THEREFORE NO PULSE. Every scripted
	// node in the tests is one, and so is a node whose session has no journal —
	// the same nothing [TaskGraph.checkpoint] answers with.
	if n == nil || n.graph == nil {
		return nil
	}
	n.graph.mu.Lock()
	beat := newTaskBeat(n.graph.store.beatPath(n.id), n.id, n.spec.title, n.started)
	n.beat = beat
	n.graph.mu.Unlock()
	beat.arm()
	return beat
}

// beatWriter is the pulse an agent built for this node writes into.
func (n *TaskNode) beatWriter() *taskBeat {
	if n == nil || n.graph == nil {
		return nil
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.beat
}

// beatPhase moves the node's pulse into one of task_beat.go's three words and
// hands back the way out, so a caller writes `defer node.beatPhase(x)()`.
func (n *TaskNode) beatPhase(name string) func() {
	return n.beatWriter().phase(name)
}

// enterPhase moves the node into one of its lives EVERYWHERE AT ONCE — the pulse
// other windows read off disk (task_beat.go) and the event this session's own
// surface draws from ([EventTaskPhase]) — and hands back the way out, so a
// caller writes `defer a.enterPhase(node, x, …)()`.
//
// ONE MOVE, ONE CALL, AND NO SECOND STATE MACHINE. Every site that moves the
// phase — the check, a repair round, and the reading that decides whether the
// work divides (task_divide.go) — spells the move as this one pair. Anything
// that tracked the phase a second time in order to publish it would be a second
// thing to keep in step with those lines, and the first minute they disagreed
// the card would be lying about work the pulse had right.
//
// THE WAY OUT IS ALWAYS BACK TO WORKING, and that is a fact about the callers
// rather than an assumption made here: the check, a repair round and a sizing
// reading are SIBLINGS and never nested — [Agent.auditWithRepair] runs one, then
// the other, then the first again, and a division is read while the node's own
// worker holds the turn — so each of them is entered from a node that is working
// and left to a node that is working. The pulse restores whatever it actually
// saved; this says the word that is true either way.
func (a *Agent) enterPhase(node *TaskNode, phase string, round, rounds int, text string) func() {
	if node == nil {
		return func() {}
	}
	teller := node.phaseTeller(a)
	restore := node.beatPhase(phase)
	node.living(phase)
	teller.emitTaskPhase(TaskPhaseNotice{ID: node.id, Phase: phase, Round: round, Rounds: rounds, Text: text})
	return func() {
		restore()
		node.living(TaskPhaseWorking)
		teller.emitTaskPhase(TaskPhaseNotice{ID: node.id, Phase: TaskPhaseWorking})
	}
}

// phaseTeller is the agent a phase move is ANNOUNCED FROM: the conversation
// whose graph this node is in, and never merely whoever noticed the move.
//
// IT MATTERS BECAUSE NOT EVERY MOVE IS NOTICED BY THE SAME AGENT. A check and a
// repair round are run by the agent that OWNS the node, which for a node the
// conversation started is the conversation itself — so for two sites this
// changes nothing. A DIVISION IS READ BY THE NODE'S OWN WORKER
// (task_divide.go), and a worker's only lanes are its room's: a move announced
// from there reaches somebody standing inside the task and nobody at all on the
// rail, the card or the home row, which is every surface the phase was built
// for. The same is true of a PART's check, whose owner is its parent's worker.
//
// So it goes the way a node's ordinary updates already go — the conversation's
// own hub and its standing subscription ([TaskGraph.reportHome],
// [Agent.TaskUpdates]) — and the receiver stands in only for a graph that has no
// conversation behind it, which is every scripted graph in the tests.
func (n *TaskNode) phaseTeller(noticed *Agent) *Agent {
	if n == nil || n.graph == nil {
		return noticed
	}
	// home is written once, before any node can run, and read without the
	// graph's lock exactly as [TaskGraph.runner] and [Agent.familyPlace] read it.
	if home := n.graph.home; home != nil {
		return home
	}
	return noticed
}

// living records the node's phase on the node itself, under the graph's lock, so
// that anything already holding the graph can read it without a second lock and
// without a file. It announces nothing: the phase's own event has already gone
// out, and a second announce would put the same news on the wire twice.
func (n *TaskNode) living(phase string) {
	if n == nil || n.graph == nil {
		return
	}
	n.graph.mu.Lock()
	n.life = phase
	n.graph.mu.Unlock()
}

// lifeNow answers the word living recorded, under the same lock, and "" for a
// node that has never moved. It is the steer door's read (task_room.go's
// [Agent.SteerTask]): the beat writes the same word to disk for OTHER
// processes, and reading the beat here would be a second in-process authority
// racing the first — and a file mutex under a keypress.
func (n *TaskNode) lifeNow() string {
	if n == nil || n.graph == nil {
		return ""
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.life
}

// taskFindingLine is the check's finding as a PERSON reads it: the plain-words
// gap the checker named, with the plain-words verdict in front of it.
//
// The opener is the whole reason this exists. [mendingLine] hands back the gap
// alone — "the restore contains nothing" — and a gap on its own line under a
// running row reads like a note somebody left rather than the reason the work is
// being done again. "not done — " is what happened, in the words this codebase
// says it in; the machinery's own names for it are banned from anything a person
// reads and are not on this wire.
func taskFindingLine(evidence []string) string {
	gap := mendingLine(evidence)
	if gap == "" {
		return ""
	}
	return "not done — " + gap
}

// emitTaskPhase puts one phase move in front of whoever is watching.
//
// It is [Agent.emitTaskUpdate]'s two lanes, for [Agent.emitTaskUpdate]'s reason
// said one layer down: a node under check outlives the turn that proposed it by
// minutes, so the standing subscription is where most of these land.
func (a *Agent) emitTaskPhase(notice TaskPhaseNotice) {
	event := Event{Kind: EventTaskPhase, Tool: "propose_task", TaskPhase: &notice}
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
	// THE RECORD IS WHAT HAPPENED AND NOT WHAT WAS PLANNED. The ground and the
	// mode are written from the tree that was actually made, so a proposal that
	// meant to cut a branch and found a repository with no commit under it says
	// what it really got. A tree that knows neither leaves what the door resolved
	// standing.
	if tree.ground != "" {
		n.Ground, n.Mode = tree.ground, tree.mode
	}
	// AND WHICH RUNG OF THE GROUND LADDER MADE THE WORLD (groundladder.go). It
	// travels with the ground because it is the other half of the same fact: the
	// ground says which folder the work is about, and this says which copy of it
	// the work actually happened in.
	n.Rung, n.Seal = tree.rung, tree.seal
	n.Base, n.Universe = tree.base, tree.universe
	n.graph.mu.Unlock()
	n.graph.checkpoint()
}

// expectations is the handoff's checkable manifest, read under the graph's lock
// like every other field beside it. A node nobody wrote one for answers nothing,
// and the preflight then has nothing to do — which is the ordinary task.
func (n *TaskNode) expectations() []Expectation {
	if n == nil || n.graph == nil {
		return nil
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if len(n.Expects) == 0 {
		return nil
	}
	return append([]Expectation{}, n.Expects...)
}

// groundNow is the node's ground and mode, read under the graph's lock like
// every other field beside them.
func (n *TaskNode) groundNow() (string, TaskMode) {
	if n == nil || n.graph == nil {
		return "", ""
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.Ground, n.Mode
}

// stand is the node's ground in the shape the working copy is prepared from. It
// is deliberately not the spec's: a node that has already run once — a resume, a
// repair round, a re-model — stands where it stood, and the ladder is climbed
// exactly once, at the door.
func (n *TaskNode) stand() taskStand {
	ground, mode := n.groundNow()
	return taskStand{dir: ground, mode: mode, frozen: n.frozenWorld()}
}

// frozenWorld is the commit this node was admitted to start from
// ([TaskNode.Frozen]), read under the lock every field beside it is read under.
// A node that is nobody's part, and a part of a family with no tree to freeze,
// both answer nothing.
func (n *TaskNode) frozenWorld() string {
	if n == nil || n.graph == nil {
		return ""
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.Frozen
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
	if n.noteHandedOver() {
		n.graph.checkpoint()
	}
}

// noteHandedOver turns the mark on and answers whether THIS call is the one that
// turned it, which is the caller's cue that the checkpoint is still owed.
//
// It is split out of [TaskNode.markNoted] because the delivery road makes this
// mark inside the news handover ([Agent.handOverTaskNews]), where a parked parent
// may be waiting to read it — and the checkpoint is a file written to disk, which
// is not something to hold that seam across.
func (n *TaskNode) noteHandedOver() bool {
	n.graph.mu.Lock()
	already := n.noted
	n.noted = true
	n.graph.mu.Unlock()
	return !already
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
	// The three halves of Waiting, and no two of them can be true of one node:
	// held is written only while a node is QUEUED, and the other two are running
	// nodes — one parked on its parts, which makes no calls at all, and one whose
	// calls are being paced. A running node outranks a stale hold because it is
	// the one that is happening now, and the parts outrank the pacing because a
	// parked node has no call for a provider to pace.
	waiting := n.held
	switch {
	case n.state != TaskRunning:
	case n.parked:
		waiting = waitingOnItsParts
	case n.paced > 0:
		waiting = waitingRateLimited
	}
	where := strings.TrimSpace(n.worktree)
	if where == "" {
		where = n.spec.where
	}
	return TaskNotice{
		ID:    n.id,
		Title: n.spec.title,
		Kind:  n.kind,
		Where: where,
		// AND WHICH PROJECT THAT DIRECTORY IS A COPY OF, on every update and not
		// only on the proposal: a row drawn from a checkpoint, a roster replayed
		// after a resize and a card watching work land all ask the same question,
		// and only the node knows the answer.
		Ground: n.Ground,
		Mode:   n.Mode,
		// AND WHAT THAT DIRECTORY IS, which the surface cannot work out for
		// itself: the same ladder makes a worktree for one node and a fork for
		// the next, and only the node knows which it got (groundladder.go).
		Rung:      n.Rung,
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
		Ending:    n.endingLocked(),
		// WHAT IT IS RUNNING ON, WHICH IS THE SPEC'S UNLESS SOMETHING SWAPPED IT.
		// See [TaskNode.ran] for why the swap is a second field rather than an
		// edit to the frozen spec.
		Model:   n.runModelLocked(),
		CostUSD: cost,
	}
}

// endingLocked is the ending a notice and a record carry, with the graph held.
// A node a person stopped carries [TaskEndingStopped] whether or not anything
// wrote it — the flag was the whole of that fact before the ending existed, and
// it stays the source of it.
func (n *TaskNode) endingLocked() TaskEnding {
	if n.state != TaskFailed {
		return ""
	}
	if n.stopped {
		return TaskEndingStopped
	}
	return n.ending
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
	// The speaker is withdrawn the moment the worker's reading is over
	// (runTaskChild), which is minutes before its usage is folded into the
	// frozen figure at retire — and a card whose price dropped by the whole
	// worker for the length of the check would be money lying mid-run. The bill
	// remembers who is still owed for until the fold happens.
	if child := room.billed(); child != nil {
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
	note := taskNote(notice, taskURI(node.journalPath()), a.settlePolicy(), a.addressLanding(notice))
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
	// SAID ONCE, ACROSS LIVES. The checkpoint records that this node's completion
	// has been announced, so a session resumed from it restores the node as
	// history instead of telling the model that finished work has just landed
	// (task_store.go). On the delivery road the mark is made INSIDE the handover,
	// between the queue and the wake, for the reason stated there.
	if notice.Kind == TaskKindHarness {
		a.enqueueAmbientNote(note)
		node.markNoted()
	} else {
		a.deliverTaskNote(node, note)
	}
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
	message := wakeNote(note)
	message.replyTags = []TaskReplyTag{{
		ID: node.id, Title: node.title(), Request: node.request(),
	}}
	reader.enqueueNote(message)
	// ── THE QUEUE, THEN THE FACT, THEN THE WAKE, AND NEVER IN ANY OTHER ORDER ──
	//
	// A parent parked on its pieces asks two questions of this moment and the
	// answers must not be able to disagree: is anything still outstanding
	// ([Agent.childrenOutstanding], which is `noted` read from outside), and is
	// anything owed to the model ([Agent.taskNewsOwed]). Sliding the mark between
	// the two calls below is what makes both readings safe whichever instant the
	// waiter took them in.
	//
	// MARKED AFTER THE QUEUE, so a waiter that sees this child is no longer
	// outstanding also sees a note owed and re-enters the model with it. Marked
	// before the wake, so a waiter that read "still outstanding" a moment ago is
	// released by the close below rather than parking on a generation that nothing
	// is ever going to close again. The old order — deliver, wake, mark — left
	// exactly that gap, and it is a parent asleep for the rest of the run.
	//
	// AND THE FACT AND THE WAKE ARE ONE STEP ([Agent.handOverTaskNews]). An order
	// the writer keeps is only half of the promise: the waiter reads two facts
	// held under two different locks, so a delivery that could land BETWEEN those
	// reads is a delivery seen half-done however carefully it was written. Both
	// sides meet on one lock, and [Agent.taskNewsStanding] is the read.
	//
	// THE CHECKPOINT IS WRITTEN AFTER THE SEAM and only by the delivery that
	// actually moved the mark. It is the same record it always was; what has
	// changed is that a parked parent is no longer kept waiting on a file while
	// the two facts it reads disagree.
	handedOver := false
	reader.handOverTaskNews(func() { handedOver = node.noteHandedOver() })
	if handedOver {
		node.graph.checkpoint()
	}
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
//
// EXCEPT THAT A LADDER MAY NOT END IN A PERSON WHO IS NOT THERE.
//
// [Config.AskConsent] is this build's one notion of "somebody is watching this
// session and will answer" — it is what an interactive surface sets and what
// `--once` and every other headless door deliberately leaves false. Where it is
// false there is no card to press `[d] decide these for me` on, no settings
// panel to turn `task.settle` to auto in, and nobody to read a landing that says
// it is waiting on them. A node that landed needing a look in such a session is
// a run that has stopped, and it was measured stopping: on a ten-hour benchmark
// the main task landed "finished, but needs your look" and the harness sat there
// until the wall clock ran out.
//
// So an unattended session reads as AUTO, which is not a bypass and not a new
// road: it is the same [TaskSettleAuto] the person's own "decide these for me"
// button sets (internal/tui3's settleAlways), reached by the same landing note,
// answered by the same `tasks … resolve` verb, with the same standing escape —
// the auto note tells the model to come back to the person when it genuinely
// cannot tell. What changes is only who is asked FIRST, which is all this policy
// has ever changed.
//
// IT NEVER GOES THE OTHER WAY. A session somebody IS watching keeps the row they
// set, and a blank row still reads as asking: a build that started deciding on
// behalf of a person who is sitting right there would be the opposite defect.
func (a *Agent) settlePolicy() TaskSettle {
	// NOBODY WATCHING AND NOBODY TO WATCH FOR ARE THE SAME FACT HERE. The first
	// arm is a headless run with no events going anywhere; the second is a
	// session that is being watched by a terminal nobody is sitting at, which is
	// what a [Steward] means (principal.go). Both leave the ask clause naming a
	// card and a person who will never answer it, and a landing that waits for an
	// answer nobody is coming to give is a landing that waits forever.
	if !a.config.AskConsent || a.steward() != nil {
		return TaskSettleAuto
	}
	return settleOrAsk(a.config.TaskSettle)
}

// unattendedRun reports that the CONVERSATION this node's family belongs to was
// left running with a ceiling and nobody is coming back to it — the `--yolo`
// posture with a budget, which is the one thing in this build that means a run
// carries its own work on ([Steward], principal.go).
//
// ── IT IS ASKED OF THE CONVERSATION AND NEVER OF THE RUNNER ─────────────────
//
// A node's own agent is not the one to ask. A sub-task is run by its PARENT
// NODE'S agent ([TaskNode.owner]), and a worker holds a [Person] by law — it has
// no budget, no acceptance and no ceiling of its own (principal_wire.go refuses
// a worker a steward outright). So the question "is anybody coming back to this"
// belongs to the conversation the whole family hangs off, which is the graph's
// home and is written once before any node can run.
//
// AND IT IS NARROWER THAN [Agent.settlePolicy] ON PURPOSE. Auto also covers a
// headless `--once` — which ends with its one turn either way — and a nested task
// under a session somebody IS watching, whose decision belongs to the worker that
// commissioned it and which can read the diff and answer. Neither of those is the
// run that was measured stalling, and taking their decisions away would be the
// widening this seam exists to avoid.
func (n *TaskNode) unattendedRun() bool {
	// home is written once, before any node can run, and read without the
	// graph's lock — the same reading [TaskNode.enterPhase] takes of it.
	if n == nil || n.graph == nil || n.graph.home == nil {
		return false
	}
	return n.graph.home.steward() != nil
}

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

// landingAddress is WHAT THIS SESSION'S GOAL OWNER SAID about one landing, in
// the only two shapes the note has any use for (principal.go).
//
// brief is what the next attempt opens on, and an empty one means nothing more
// is being started on the strength of this landing. person says there is
// somebody to offer a follow-up TO — which is the whole of what the old note
// assumed and never checked.
//
// THE TWO ARE NOT OPPOSITES. A [Steward] whose loop guard has fired answers
// neither: there is no brief, because the same thing has stopped this three
// times, and there is no person, because nobody is there. That ending had no
// sentence at all before this type existed; it had the person's one, addressed
// to an empty room.
type landingAddress struct {
	brief  string
	person bool
}

// haltedVerb is the verb a landing note uses for an ending that is nobody's
// finding — and "" for the endings that are (refused, error) and for a node
// that gave no reason, both of which stay "failed".
func haltedVerb(ending TaskEnding) string {
	switch ending {
	case TaskEndingWire:
		return "lost the connection"
	case TaskEndingCircling:
		return "went in circles"
	case TaskEndingBlocked:
		return "was blocked by another task"
	case TaskEndingSteps:
		return "ran out of steps"
	case TaskEndingNotes:
		return "would not write its notes down"
	}
	return ""
}

func taskNote(notice TaskNotice, transcript string, settle TaskSettle, address landingAddress) string {
	var note strings.Builder
	verb := "finished"
	switch {
	case notice.Stopped:
		// THE PERSON ENDED IT, and the model must not tell them their work
		// failed. Nothing was found wrong with it: somebody pressed stop, and the
		// only honest verb for that is the one they would use themselves.
		verb = "stopped"
	case notice.State == TaskFailed && haltedVerb(notice.Ending) != "":
		// HALTED, NOT FAILED. The wire dropped, the loop guard fired, another
		// task held the files, the steps ran out: nothing was found wrong with
		// the work, and "failed" would send the model — and then the person —
		// looking for a fault in work nobody has judged. The verb names what
		// happened so the model can offer the one useful thing: running it again
		// from its branch.
		verb = haltedVerb(notice.Ending)
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
		note.WriteString(incompleteClause(address))
	}
	if len(notice.Changed) > 0 {
		note.WriteString("\nchanged: " + strings.Join(notice.Changed, ", "))
	}
	switch notice.Merge {
	case mergeMerged:
		note.WriteString("\nits branch " + notice.Branch + " merged into yours")
	case mergeConflicted:
		// A FOLDER FAMILY HAS NO BRANCH TO OFFER. Its landing refuses the same way
		// a merge does and for the same reason — the same file changed on both
		// sides (task_mirror_manners.go) — but what it can offer is the directory
		// its work is still in, which its own sentence has already named at the top
		// of this report. The emptiness law is why nothing is written here rather
		// than a line with a hole where a branch name would go.
		if notice.Branch != "" {
			note.WriteString("\nits branch " + notice.Branch + " did not merge cleanly and was kept — merge it yourself when you are ready")
		}
	case mergeAborted:
		// WHAT IT MADE IS ON THAT BRANCH, and saying so is the difference
		// between a person going to look and a person assuming an ending they
		// were told nothing about threw the work away ([keptWork]). The shorter
		// sentence is for a node that left nothing: offering to merge an empty
		// branch would send them after work that does not exist.
		//
		// AND A LANDING THAT SAVED NOTHING SAYS NEITHER. It wears this same mark
		// and there is nothing on its branch to merge; where its work is sitting
		// is the first sentence of its own report, and a line under that offering
		// a branch would send the person past it (task_land_unsaved.go).
		switch {
		case unsavedLanding(notice.Report):
		case len(notice.Changed) > 0:
			note.WriteString("\nit was stopped; what it made is committed on its branch " +
				notice.Branch + ", which was kept — merge that branch to take the work")
		default:
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
	// LIVE JOBS ARE SNAPSHOTTED BEFORE THE GRAPH LOCK, so the registry lock
	// and the graph lock never meet. announceJobRow releases the registry
	// before it writes the graph (jobrow.go); taking the other order here
	// would be the deadlock that comment exists to prevent. A snapshot a
	// moment old is the same snapshot a live watcher already drew; a job
	// that moves after this returns will announce onto the lane itself.
	liveJobs := a.liveJobNotices()
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
		// A BACKGROUND JOB GOES OUT ON ITS OWN LANE, restored or not. The rows are
		// kept together because they share a row-space and a checkpoint
		// (jobrow.go), and they are SENT apart because a job is not a task and the
		// surfaces that draw the two have nothing in common (jobnotice.go). A job
		// replayed onto the task lane would reach a surface that correctly ignores
		// it, and a conversation reopened tomorrow would draw none of the work it
		// ran yesterday.
		if job, isJob := jobNoticeFromRow(row); isJob {
			// A LIVE JOB IS THE REGISTRY'S, NOT THE CHECKPOINT'S. The row is
			// what tomorrow will read; it never held Started, Command, Kind
			// or the name that arrived after the first announce. A lane
			// attaching while the process is still going would otherwise
			// draw a blank clock and a page with no command until the job
			// ended — announceJobRow will not fill those in, because a
			// running job that already has a row is a start announced once.
			if fresh, ok := liveJobs[job.ID]; ok {
				job = fresh
			}
			stream.send(Event{Kind: EventJobUpdate, Job: &job})
			continue
		}
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
	// no context here. The context and its cancel were made by the frontier when
	// it marked this node running, one hold of the graph's lock before this
	// goroutine existed ([TaskGraph.runFrontier]), and the frontier releases it
	// when this function returns; a stop reaches this node through that handle
	// whether or not this line has been reached yet. Time is checked only
	// between completed turns below.
	ctx, cancel := node.runContext()

	// AND THE NODE IS TAKEN UP HERE, OR NOT RUN AT ALL. Close and a person's stop
	// both land in the window between the admission and this line, and the whole
	// of issue #381 was what a run did in it: a job log opened under a session
	// that had left, a child agent built, two model calls of real spend for work
	// that was already stopped. [TaskNode.claimRun] refuses both — a cut context
	// and a node the stop has already settled — under the one lock the stop reads
	// the claim under. The lane goes back for the reason it goes back below: the
	// goroutine holding it is returning on this line. A node the quit cut keeps
	// its state, so recovery reads a node that was running and resumes it; a node
	// the stop settled has moved already, and the hand-back leaves it alone
	// ([TaskGraph.park] answers nothing for a node that is not running).
	if !node.claimRun() {
		node.graph.handBackLane(node)
		return
	}

	// AND THE STORE LEARNS IT IS ALIVE AT THE CADENCE OF ITS WORK (task_beat.go).
	// The checkpoint is written at admission and at landing, so between them the
	// only thing an outside reader could do was guess; from here the node writes a
	// pulse of its own at every provider call boundary, and the pulse goes away
	// when the node lands and the checkpoint's row becomes the authority again.
	defer node.armBeat().stop()

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
		//
		// THE LANE GOES BACK ANYWAY, because the goroutine that was holding it is
		// returning on this line. The state is left alone — that is what recovery
		// reads — but a slot booked against a node nothing is doing is the
		// concurrency cap quietly falling by one for every node that comes after,
		// and a frontier that is never turned again is the queue behind it never
		// moving ([TaskGraph.handBackLane]).
		node.graph.handBackLane(node)
		return
	}
	// NOTHING OUTLIVES THE WORK IT WAS HANDED OUT FOR. A sub-task's worktree is
	// branched off its parent's and merges back into it, so a child still
	// running after its parent has landed is work with nowhere to come home to.
	// In the ordinary case there is nothing here to stop — the runner above does
	// not let the node land while a child of it is still going (see
	// [runTaskChild]), and [Agent.workTaskNode] has already cut them on every
	// road where it did not — and this is what answers the two bodies that are
	// not a worker in a worktree.
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
// AND IT RE-ADDRESSES THE ROW RATHER THAN RE-DELIVERING IT (pending.go). It used
// to paste the child's whole landing note into the conversation a second time,
// an hour after the first, because that note was THE ONLY PLACE THE DEMAND EVER
// APPEARED: no surface drew an answers row for a nested node at all, so a
// sentence to the model was the whole of the gate. It is not any more — every
// node that needs a look is on [Agent.PendingDecisions] from the moment it
// finishes, at any depth, and every surface draws it. What is owed here is one
// sentence saying the question has changed hands, said once for all of them
// rather than once per child.
func (a *Agent) bubbleUnverifiedChildren(node *TaskNode) {
	var waiting []*TaskNode
	for _, kid := range node.graph.children(node.id) {
		if kid.stateNow() == TaskUnverified {
			waiting = append(waiting, kid)
		}
	}
	if len(waiting) == 0 {
		return
	}
	a.deliverTaskNote(node, readdressedLead(node, waiting))
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
	// THE PARK IS NUMBERED AS IT IS TAKEN, inside the same hold that sets the
	// flag, so that no reader can ever see the flag of one park beside the
	// number of another (see [TaskNode.parkGen]).
	node.parkGen++
	if g.running > 0 {
		g.running--
	}
	g.mu.Unlock()
	// AND THE ROW SAYS SO. A hold is not a state — nothing about this node moved —
	// so it travels as [TaskNotice.Waiting] on an update of its own, exactly as a
	// queued node's slot and a paced node's provider do (see [waitingOnItsParts]).
	g.announce(node)
	// The lane is free NOW, and the piece this parent is waiting for is very
	// often the node that was queued behind it.
	g.runFrontier()
}

// handBackLane gives one node's slot back WITHOUT settling it, and turns the
// frontier on what that freed.
//
// It is for the one ending that is not an ending: a run whose process is going
// away, whose node stays TaskRunning in the checkpoint so the next session
// resumes it ([Agent.runTaskNode]). The state is deliberately untouched — that
// is the whole mechanism recovery reads — but the goroutine is gone, and a lane
// held by nobody is the person's task.parallel cap silently shrinking for the
// rest of the process.
//
// It borrows `parked`, which is exactly the fact being recorded: this node has
// handed its lane back and is not using one. [TaskGraph.complete] already knows
// not to hand the same lane back twice for such a node, so a recovery that later
// settles it cannot double-count.
func (g *TaskGraph) handBackLane(node *TaskNode) {
	if node == nil || g == nil {
		return
	}
	g.park(node)
}

func (g *TaskGraph) unpark(node *TaskNode) {
	g.mu.Lock()
	if !node.parked {
		g.mu.Unlock()
		return
	}
	node.parked = false
	g.running++
	g.mu.Unlock()
	// A hold ENDING is news the same way a hold starting is, and the row would
	// otherwise wear "waiting · its parts" until whatever this node does next
	// happens to send an update.
	g.announce(node)
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

// waitingOnItsPieces reports whether this node has handed its lane back and is
// waiting on the work it handed out. It is what lets a room say which kind of
// wait a steered line is landing in ([Agent.SteerTask]): a node in the middle of
// a step reads the line at its next one, and a node parked here has no step
// coming until somebody wakes it.
func (n *TaskNode) waitingOnItsPieces() bool {
	parked, _ := n.parkStanding()
	return parked
}

// parkStanding answers BOTH questions somebody watching a park has to ask, under
// ONE hold of the graph's lock: whether this node is on its park now, and WHICH
// park it is on ([TaskNode.parkGen]).
//
// THE PAIR IS ONE READING BECAUSE THE TWO FACTS MOVE TOGETHER. A parent woken by
// one part's report unparks, reads it, finds another part still outstanding and
// parks again, all in a few microseconds; two separate reads across that window
// answer with the flag of one park and the number of another, which is a park
// that never existed. It is the same law [Agent.taskNewsStanding] states for the
// other pair a parked parent lives by, and it is stated here for the same
// reason: no order of two reads can hold it.
func (n *TaskNode) parkStanding() (parked bool, generation uint64) {
	if n == nil || n.graph == nil {
		return false, 0
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.parked, n.parkGen
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

// openTaskWorld makes the working copy this node will live in and writes down
// what it is, and it answers false having settled the node when there is no
// copy to be had.
//
// It is the first thing that happens to a node and it happens before anything
// costs money: a directory, a branch, and four lines in the log saying what the
// directory is a copy of. Each of those lines is written under the emptiness
// law — a task standing in the folder it was already about has no second world
// to name, and a line saying so would be the log explaining a distinction that
// does not exist here.
func (a *Agent) openTaskWorld(ctx context.Context, node *TaskNode, log io.Writer) (taskTree, bool) {
	// The FAMILY'S place and the OWNER'S workspace, which are two different
	// questions: where this session keeps things ([Agent.familyPlace]) and which
	// checkout this node's branch comes off, which for a part is its parent's
	// worktree.
	place := a.familyPlace(node)
	tree, resumed := node.resumeTree(place, a.config.Workspace)
	var err error
	if !resumed {
		tree, err = prepareTaskTreeOn(ctx, place, a.config.Workspace, a.journalID(), node.id, node.title(), node.stand())
	}
	if err != nil {
		node.end(TaskEndingError)
		node.finish("could not prepare a working copy: "+err.Error(), nil, "", "")
		return taskTree{}, false
	}
	// Written down before a single tool call runs in it: from here on, a process
	// that dies leaves a checkpoint that knows where this node's work is.
	node.setTree(tree)
	fmt.Fprintf(log, "task %d · %s\nworking in %s\n", node.id, node.title(), tree.dir)
	// AND WHAT THAT DIRECTORY IS A COPY OF, whenever the two are different
	// (taskstands.go). It is the first thing anybody reading this log after a
	// surprise wants to know, and the emptiness law is why it is not printed for a
	// task standing in the directory it is already working in.
	if tree.ground != "" && tree.ground != tree.dir {
		fmt.Fprintf(log, "standing on %s · %s\n", tree.ground, tree.mode)
	}
	// AND WHICH WORLD THAT COPY IS, which is the ground law's own record
	// (groundladder.go): a report that cannot name the world the work was done in
	// is a report nobody can check. The emptiness law keeps it off the log of a
	// task standing in the folder it was already about.
	if world := tree.world(); world != "" {
		fmt.Fprintf(log, "its world is %s\n", world)
	}
	// AND WHAT THE WORLD COULD NOT BE, LOUDLY. A tree that fell short of what was
	// promised about it says so here and again in the brief below, because a
	// degradation nobody is told about is the shape this whole seam was written
	// to end (task_tree_mirror.go).
	if tree.note != "" {
		fmt.Fprintf(log, "%s\n", tree.note)
	}
	return tree, true
}

// briefMatchesItsWorld is ── THE HANDOFF CONTRACT, ANSWERED BEFORE ANY OF THE
// MONEY ──
//
// It hangs off nobody: the brief, the world and the log are the whole of what
// this reads, which is the same reason [landHome] and [keepHome] are functions
// rather than methods.
//
// The brief says what this work assumes about its world; the world has just
// been made. This is the one moment the two can be held up against each
// other for nothing, and it is BEFORE the first worker is built rather than
// inside its first turn, so a contract that does not hold costs a directory
// walk instead of a model call (handoffcontract.go).
//
// It is journaled either way. A contract that held is one line and the run
// goes on; a contract that did not is the node's whole report, and it names
// every expectation that failed rather than the first, because a brief
// written against a world one commit behind fails several at once.
func briefMatchesItsWorld(node *TaskNode, tree taskTree, log io.Writer) bool {
	expects := node.expectations()
	if len(expects) == 0 {
		return true
	}
	if unmet := preflightExpectations(tree.dir, expects); len(unmet) > 0 {
		fmt.Fprintf(log, "its brief does not match its world:\n%s\n", strings.Join(unmet, "\n"))
		node.end(TaskEndingStale)
		node.finish(staleGroundReport(tree.world(), unmet), nil, tree.branch, abortedMerge(tree))
		return false
	}
	fmt.Fprintf(log, "its brief matches its world · %d checked\n", len(expects))
	return true
}

// settleUnfinished is the three roads out of a run where the work never reached
// the gate at all, and it answers false when none of them was taken — which is
// the ordinary case of a node that finished and is about to be checked.
//
// THEY ARE READ IN THIS ORDER AND THE ORDER IS THE ANSWER. The threshold comes
// FIRST because it is the most specific: it cancelled the child itself, so every
// check below it would also be true, and each of them would say something less
// useful than the name of the threshold that fired.
//
// NONE OF THE THREE MERGES. Two of them keep the branch with the work committed
// on it ([keepHome]), because a run that was cut is not a finding about the
// deliverable and the person is owed what was made; the third leaves the
// checkpoint resumable and settles nothing at all.
func (a *Agent) settleUnfinished(ctx context.Context, node *TaskNode, tree taskTree, changed []string, report, stopped string, runErr error, log io.Writer) (TaskState, bool) {
	switch {
	case stopped != "":
		fmt.Fprintf(log, "%s\n", stopped)
		// A THRESHOLD IS NOT A REASON TO LOSE THE WORK, and it is not a finding
		// about it either. The landing turn has already run and whatever the node
		// made is on disk, so the work is judged before anything is written down
		// about it ([Agent.landStopped]).
		return a.landStopped(ctx, node, tree, changed, report, stopped, log), true
	case ctx.Err() != nil:
		if node.wasStopped() {
			merge, changed := keepHome(node, tree, changed)
			node.end(TaskEndingStopped)
			node.finish(withReport("stopped", report), changed, tree.branch, merge)
			return TaskFailed, true
		}
		// Lifecycle cancellation is an interruption, never a finding about the
		// work. The running checkpoint is deliberately left resumable.
		node.finish(withReport("paused — it resumes", report), changed, tree.branch, abortedMerge(tree))
		return "", true
	case runErr != nil:
		merge, changed := keepHome(node, tree, changed)
		// THE WIRE AND AN ERROR ARE DIFFERENT NEWS. Both end the node, but a
		// person reading "lost the connection" restarts it and a person reading
		// "ended with an error" goes looking for the fault; the report keeps the
		// error's own words either way.
		if diedOnTheWire(runErr) {
			node.end(TaskEndingWire)
			node.finish(withReport("lost the connection to the model: "+runErr.Error(), report), changed, tree.branch, merge)
			return TaskFailed, true
		}
		node.end(TaskEndingError)
		node.finish(withReport("it ended with an error: "+runErr.Error(), report), changed, tree.branch, merge)
		return TaskFailed, true
	}
	return "", false
}

// workTaskNode does the work and reports the state the node ended in. Every
// failure is a state and a report rather than an error: a node that could not
// get a working copy has to be able to say so to the person who asked for it.
func (a *Agent) workTaskNode(ctx context.Context, node *TaskNode, listed *job) TaskState {
	log := taskLog(listed)
	// THE WORLD IS MADE AND THEN ASKED WHETHER IT IS THE ONE THE BRIEF ASSUMED,
	// and both happen before a single model call is bought. Either can settle the
	// node outright, and both say so the same way.
	tree, ok := a.openTaskWorld(ctx, node, log)
	if !ok {
		return TaskFailed
	}
	if !briefMatchesItsWorld(node, tree, log) {
		return TaskFailed
	}

	// THE NODE'S SPEND IS THE PERSON'S, so it is folded into the session's
	// auxiliary usage — the pocket the title and the compaction summary come out
	// of — rather than charged to whichever turn happened to propose it. It is
	// kept ON THE NODE as well, in the same call, because the node outlives its
	// child: a surface asking a landed node what it cost has nobody else to ask
	// (see [TaskNode.spend]). A node that was re-modelled has TWO children, and
	// both of them cost money, so the fold is per child rather than per node.
	var child *Agent
	retire := func() {
		if child == nil {
			return
		}
		_ = child.Close()
		a.foldTaskUsage(node, child)
		// The fold moved this child's whole tally onto the node, so the bill
		// stops naming it — a price read now comes off the frozen figure, and a
		// bill left standing would count the same money twice
		// ([TaskNode.spend]).
		node.openRoom().bill(nil)
		child = nil
	}
	defer retire()
	// ── THE NURSERY LAW: NO PART OUTLIVES THE COORDINATION IT WAS CUT OUT OF ──
	//
	// REGISTERED AFTER retire SO IT RUNS BEFORE IT, and the order is the whole
	// point. A part's job row lives in its OWNER'S registry, and a part's owner is
	// this node's WORKER (task_divide.go) — so `child.Close()` reaches every part
	// still running through [jobRegistry.shutdown], which cancels a task job's
	// context WITHOUT marking it stopped ([job.signal] takes the `stop` handle and
	// never the explicit one, which only [jobRegistry.kill] calls). A part cut that
	// way reads its own cancel as A PROCESS QUITTING: it lands on the "paused — it
	// resumes" road below, whose whole meaning is that a NEXT process will pick the
	// node up — and it returns "" so the state stays TaskRunning for recovery to
	// find. There is no next process. The part sat in the graph as running work
	// nothing was doing, its lane never handed back, its `done` never closed, for
	// fifty minutes until the run hit its wall.
	//
	// So the parts are stopped HERE, on every road out of this function, while the
	// worker they belong to is still open: [TaskGraph.stop] marks each one before
	// it cuts it, so a part reads its ending as what it is — stopped, its branch
	// kept, its report saying so — settles, hands its lane back and cascades. The
	// ordinary road, where the tail loop already waited for every report, finds
	// nothing to do: stopChildren skips a settled child.
	//
	// IT IS NOT PART OF retire, which also runs mid-loop when a provider fault
	// sends this node round again on another model — and that node is the SAME node
	// with the SAME parts still working for it (see `handedOut` below).
	defer node.graph.stopChildren(node.id)

	var (
		// A RESUMED NODE STARTS WITH WHAT IT ALREADY WROTE. The landing stages by
		// name ([stageTaskWork]), and the process that died took this run's own
		// tally with it while leaving the files on disk — so a second attempt that
		// started from nothing would abandon everything the first one made. A node
		// that has never run answers with nothing, which is every other node.
		changed = node.rememberedWrites()
		stopped string
		runErr  error
		report  string
		// movedFrom is the model this node was admitted on, once it has stopped
		// being the model it is running on. Empty is the ordinary case.
		movedFrom string
		// handedOut is the receipt for the parts the harness gave away on this
		// node's behalf before it started, and an empty string is every node that
		// was not handed a division (task_divide_sketch.go). It is kept OUTSIDE the
		// loop because a second worker built after a provider fault is the same node
		// with the same parts already running: it must read the same sentence, and
		// the division must not be put a second time.
		handedOut string
		// wireRetried says the second worker a wire death buys has been built,
		// and the next one ends the node.
		wireRetried bool
	)
	// ONE WORKER, OR TWO. The second exists for exactly one reason, stated at
	// [terminalProviderFailure]: a node whose worker died because the PROVIDER
	// could not answer has learned nothing about the work, and throwing away a
	// prepared worktree over that is throwing away the part that was expensive.
	for {
		worker, err := a.newTaskAgent(ctx, taskGroundDir(node, tree), node, "")
		if err != nil {
			node.end(TaskEndingError)
			node.finish("could not start the task: "+err.Error(), nil, tree.branch, tree.merge)
			return TaskFailed
		}
		child = worker
		// THE ROOM OPENS HERE, because this is the first moment there is anybody
		// in it: from now until the node lands, its events reach whoever is
		// watching and the person's words reach this child's steering lane
		// (task_room.go). The bill is separate from the speaker on purpose: the
		// speaker leaves when the worker's reading is over, the bill stands
		// until retire folds the money ([taskRoom.bill], [TaskNode.spend]).
		room := node.openRoom()
		room.speaking(child)
		room.bill(child)

		// AND THE DIVISION SOMEBODY ALREADY DREW IS PUT HERE, BEFORE THE FIRST
		// REQUEST. A turn handed over on a mark's sketch arrives with its parts
		// already named by a mastermind, and waiting for a cheap worker to re-derive
		// them was measured never happening at all — so the harness submits the
		// drawing on this worker's behalf, through the same verb and the same gates
		// the worker's own division goes through (task_divide_sketch.go). It lands
		// the node in the coordinating state a mid-run division lands it in, by the
		// same road: the parts are children, so the tail of [runTaskChild] holds this
		// node open and folds their reports.
		//
		// A NODE WITH NOTHING DRAWN, A ROAD THAT IS OFF, AND A DIVISION THE GATES OR
		// THE REVIEWER REFUSED ALL ANSWER THE SAME EMPTY STRING, and the node then
		// runs as one worker — which is what every task did before this existed.
		//
		// EXCEPT FOR THE ONE ANSWER THAT IS NOT ABOUT THE DIVISION. The reviewer
		// that reads a drawn division may come back saying the work left over is
		// not work for any worker at all, and it says so having read the parts, the
		// brief and the evidence together, before this node has spent anything. It
		// was measured being thrown away: a task whose whole remainder was an
		// approving review GitHub only takes from a human ran anyway for nine
		// minutes and $1.24, fixed a file in an empty repository looking for
		// something it could do, and was failed by the check. So it lands here
		// instead, needing a person, with the reader's own sentence as its report
		// ([Agent.landNeedsPerson]) — and this line is where the saving is, because
		// everything past it is the run, the check and the repair round.
		if handedOut == "" {
			var person string
			if handedOut, person = child.divideFromSketch(ctx); person != "" {
				return a.landNeedsPerson(node, tree, person, log)
			}
		}

		var wrote []string
		wrote, stopped, runErr = runTaskChild(ctx, child, node, withReport(node.instruction(), withReport(tree.note, handedOut)), tree.dir, a.taskLimits(node), room, log)
		// The files SURVIVE the worker that wrote them. A second run starts in
		// the same working copy, so what the first one saved is still on disk and
		// still the node's leavings.
		changed = mergePaths(changed, wrote)
		report = taskReport(child)
		// AND WHAT IT ACTUALLY RAN, kept for the judge that never watched it happen.
		// The check a worker runs last is usually the most expensive thing in the
		// task, and an auditor made to rediscover and repeat it from nothing is an
		// auditor that spends its whole deadline finding out what the worker already
		// knew (task_audit.go's [lastToolReceipts]). It is read here, while the
		// worker's transcript still exists — `retire` closes it.
		node.keepReceipts(lastToolReceipts(child, auditReceiptCount))
		// AND THE FILES THE NODE MADE WITH A COMMAND AND THEN NAMED. They are
		// folded into the same list on the way past, so everything downstream —
		// the card, the auditor's packet, the index it stages — reads ONE account
		// of what this node produced ([declaredFiles]).
		changed = mergePaths(changed, declaredFiles(lastSaid(child), tree.dir))

		if movedFrom != "" || stopped != "" || ctx.Err() != nil {
			break
		}
		// THE CONNECTION DIED, NOT THE WORK. A run whose last call ended on the
		// wire — after the call's own retries were spent (loop.go's
		// retryablePattern) — has learned nothing about the work and thrown
		// away nothing on disk: the working copy, the files it wrote and its
		// checkpoint are all still here. So it is run ONCE MORE on the same model
		// in the same copy, the way a model move already builds a second worker
		// below, before the wire is allowed to end the node. Once, because a
		// provider that is down stays down for the second worker too, and a node
		// that loops on that is a node spending money to be told the same thing.
		if !wireRetried && diedOnTheWire(runErr) {
			wireRetried = true
			fmt.Fprintf(log, "the run ended on the connection rather than on the work: running again on %s\n", node.runModel())
			retire()
			continue
		}
		if !a.movesForFailure(node, runErr, log) {
			break
		}
		next, moved := a.escalateNodeModel(node)
		if !moved {
			break
		}
		movedFrom = node.runModel()
		fmt.Fprintf(log, "%s could not answer: running again on %s\n", movedFrom, next)
		// The retarget machinery is the room's own (task_room.go's RetargetTask):
		// the row, the roster, the card and the checkpoint all learn the new model
		// from these three lines, and the worker below reads it off the node.
		node.runOn(next, taskModelMovedNote(movedFrom, next))
		node.graph.checkpoint()
		a.emitTaskUpdate(node.notice())
		retire()
	}
	// AND THE REPORT SAYS SO, on every road out of here — done, failed, stopped,
	// unchecked. A node that quietly finished on a model nobody chose is a card
	// whose cost, voice and quality all belong to a model the person never sees.
	if movedFrom != "" {
		report = withReport(taskModelMovedSentence(movedFrom, node.runModel()), report)
	}

	// THE THREE WAYS A RUN ENDS WITHOUT THE WORK EVER REACHING THE GATE — a
	// threshold, a cancel, an error — settle here and nothing below them runs
	// ([Agent.settleUnfinished]).
	if settled, ended := a.settleUnfinished(ctx, node, tree, changed, report, stopped, runErr, log); ended {
		return settled
	}
	// WHY THE RUN ENDED WHEN IT DID NOT CHOOSE TO is written before the gate
	// reads it, so that a check refusing a run that had already given up does not
	// become the reason on the row ([TaskNode.end]'s first-cause law). It is the
	// loop's own answer where the loop has one — a rule the worker would not
	// follow — and the worker's last words otherwise ([endingOfClaim]).
	node.end(endingOfClaim(lastSaid(child), node.blockedByNow(), child.stoppedOnProcessRule()))

	// THE GATE. Everything above is the node's own account of itself; what
	// follows is somebody else's (task_audit.go). Only a VERIFIED verdict
	// reaches comeHome, so only verified work is ever merged onto the person's
	// branch — and a refuted node keeps its branch exactly as a killed one does,
	// because "not proven" is not "throw it away". With the audit row off the
	// gate stands open and the node's own account merges — marked unaudited,
	// because 'done' should never wear 'verified's clothes.
	if !a.config.TaskAudit {
		// THE SENTENCE SAYING NOTHING CHECKED THIS LEADS, and the node's own
		// account stands under it — which is the one place this road differs from
		// the ordinary finishing line it otherwise shares ([Agent.landFinished]).
		//
		// THE SETTING KEY IS THE ONE PIECE OF MACHINERY VOCABULARY A PERSON IS
		// ALLOWED TO SEE, and only because it is an ADDRESS: they turned this row
		// off, this is the row's name, and a sentence that translated it would
		// leave them holding a word their settings sheet does not answer to
		// (task_audit.go's vocabulary law). Everything either side of it is plain.
		return a.landFinished(node, tree, changed,
			"nothing checked this work: the task.audit setting is off", report, " (unaudited)", log)
	}
	// THE GATE MAY SEND THE WORK BACK BEFORE IT ANSWERS. What returns from here
	// is the end of the whole loop — the last verdict, the gaps of every round,
	// and the files and the claim as the LAST worker left them (task_audit.go).
	outcome := a.auditWithRepair(ctx, node, tree, changed, report, log)
	changed, report = outcome.changed, outcome.claim
	verdict := outcome.verdict
	switch {
	// ── A CANCEL IS AN INTERRUPTION, NEVER A FINDING ──
	//
	// The check was running and something outside the work cut it: a settle-kill
	// from the runner, a quit, a deadline on the session rather than on the node.
	// None of those looked at the deliverable, and until this arm was written the
	// KILL WROTE THE VERDICT — the node landed FAILED with "stopped while its work
	// was being checked" as its whole account, on a benchmark cell where the
	// runner's own settle produced the ctx.Err() it was reading.
	//
	// SO IT LANDS AS UNFINISHED WORK AND KEEPS BOTH HALVES. The node's own claim
	// is what it says it did, [auditVerdict.checkedSoFar] is whatever the check had
	// already said before it was cut, and the lead is the vocabulary a landing
	// already has for work nobody could stand behind (task_audit.go's
	// [incompleteLead], withdrawn.go's [unverifiedEdits]). Unverified rather than
	// failed is the state that matches the sentence: nothing merges on a check that
	// never finished, and nobody made a finding to fail it on. It is the arm two
	// hundred lines above ("paused — it resumes") reasoning about the same fact one
	// phase later, where there is a claim and possibly a verdict to carry.
	case ctx.Err() != nil:
		merge, changed := keepHome(node, tree, changed)
		node.finish(withReport(taskCutMidCheck, withReport(report, verdict.checkedSoFar())),
			changed, tree.branch, merge)
		return TaskUnverified
	case !verdict.answered:
		// NOBODY COULD SAY, and who is asked about that is the posture's to
		// answer ([Agent.landUnchecked]).
		return a.landUnchecked(node, tree, changed, report, verdict, log)
	case !verdict.verified:
		// INCOMPLETE, WITH EVERY ROUND'S GAPS. The node's own claim is dropped
		// exactly as it was before: somebody looked at the work and said what is
		// missing, and that answers the claim.
		merge, changed := keepHome(node, tree, changed)
		node.end(TaskEndingRefused)
		node.finish(gapsOutcome(outcome.gaps), changed, tree.branch, merge)
		return TaskFailed
	}

	// THE WORK HOLDS. Its own account leads and what it was checked on stands
	// under it, which is the ordinary shape of a finished node's report — and
	// every remaining question, the ground and the merge, is the one every road
	// home asks ([Agent.landFinished]).
	return a.landFinished(node, tree, changed, report, verdict.doneOutcome(), "", log)
}

// landUnchecked settles a node NOBODY COULD SAY ANYTHING ABOUT, and which of its
// two endings it takes is decided by whether anybody is coming back to the run.
//
// It is a function of its own rather than an arm of the gate because the gate is
// already at its ledgered length (complexity_test.go) and because the two endings
// are one question asked once: a check that could not run is a person's to decide
// where there is a person, and the harness's where there is not.
//
// ── A CHECK THAT COULD NOT RUN IS NEVER A PERSON QUESTION WHEN NOBODY IS COMING
// BACK ([TaskNode.unattendedRun]) ──
//
// On a run somebody left going with a ceiling, the road this used to take was:
// land needing a look, wake whoever holds the decision with the auto note, have
// them read the work and call `tasks … resolve accept`, and merge. Every step of
// that is spend, and it ends where this ends. The measured run never got through
// it: the accept was refused by the tree, the node was offered again, and the
// loop ate the parent's remaining minutes (#513).
//
// SO THE WORK IS TAKEN AS IT STANDS AND THE LANDING SAYS SO. The checking has
// already been run twice ([Agent.auditNode] asks a fresh checker after a
// non-answer), the tail names what became of it and that there was nobody to ask
// (task_audit.go's [takenAsItStands]), and the state is the same TaskDone the
// accept would have reached one round-trip later.
//
// AND EVERY OTHER SESSION IS UNTOUCHED. A person sitting in front of the card
// keeps their four answers; a nested task under one of those still goes to the
// worker that commissioned it, which can read the diff and decide. A harness
// that took either decision away would be the opposite defect.
func (a *Agent) landUnchecked(node *TaskNode, tree taskTree, changed []string, report string, verdict auditVerdict, log io.Writer) TaskState {
	if node.unattendedRun() {
		return a.landFinished(node, tree, changed, report, takenAsItStands(verdict), " (unchecked)", log)
	}
	// Not done — nothing merges on an answer nobody gave — and not failed
	// either, because no finding was made about this work. The node's own claim
	// is kept UNDER the non-answer: whoever is asked to resolve this needs both
	// halves, what the work says it did and what the checker said instead of an
	// answer (task_contract.go's TaskUnverified).
	merge, changed := keepHome(node, tree, changed)
	node.finish(withReport(verdict.lookOutcome(), report), changed, tree.branch, merge)
	return TaskUnverified
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
			// THE SAME ROAD HOME A NODE THAT FINISHED ON ITS OWN TAKES, and it is
			// the same function ([Agent.landFinished]): the same last question about
			// the ground, the same merge, the same report with the node's own account
			// leading and what it was checked on under it. Nothing anywhere in it
			// mentions the counter — the run was interrupted, the deliverable was not
			// — and the threshold's own sentence appears only in the log line, where
			// whoever is reading the machinery is the only one who wants it.
			return a.landFinished(node, tree, changed, report, verdict.doneOutcome(),
				" ("+stopped+", and the work holds)", log)
		}
		fmt.Fprintf(log, "landed work was not accepted: %s\n", verdict.report())
	}
	merge, changed := keepHome(node, tree, changed)
	node.end(TaskEndingSteps)
	node.finish(withReport(stopped, report), changed, tree.branch, merge)
	return TaskFailed
}

// landShifted settles a node whose work holds and whose GROUND MOVED while it
// held — somebody else landed in, or is still writing, a file this node wrote
// (groundladder.go).
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
	merge, kept := keepHome(node, tree, changed)
	fmt.Fprintf(log, "not merged: %s\n", shift)
	node.finish(withReport(needsLookLead+shift, report), kept, tree.branch, merge)
	return TaskUnverified
}

// landConflicted settles a node whose work holds and whose branch WOULD NOT
// MERGE — the same file changed here and on the person's branch while the node
// worked.
//
// A CONFLICTED MERGE IS NOT A LANDING, and that is the defect this repairs. The
// merge was already attempted and abandoned by [taskTree.comeHome]; what came
// back was a branch nobody had taken, and every caller marked the node done
// anyway. A measured task settled with a patch that was an unresolved merge —
// no reviewable diff, its own report admitting the branch had not merged —
// while the card read as finished work. Nothing about "done" was true.
//
// SO IT ENDS WHERE [Agent.landShifted] ENDS: needs your look, the branch kept
// with the work committed on it, the conflicting files named in the report. The
// person's tree is untouched — no markers, no half-merge ([abandonMerge]) — and
// merging is a thing they do when they are ready, which is what the completion
// note has always said a kept branch means.
//
// IT DOES NOT COMMIT AGAIN. comeHome committed before it tried the merge, so
// the branch already holds the work.
//
// AND IT CARRIES THE MARK IT IS HANDED rather than making one. A branch that
// would not go and work that could not be committed at all are different news —
// one says it would not merge, the other says nobody saved it — and the mark is
// what the completion note and the row read to tell them apart
// (task_land_unsaved.go). Hardcoding the conflict here is what made a landing
// that saved nothing indistinguishable from one that saved everything.
func (a *Agent) landConflicted(node *TaskNode, tree taskTree, changed []string, report, merge, detail string, log io.Writer) TaskState {
	fmt.Fprintf(log, "not merged: %s\n", detail)
	node.finish(withReport(needsLookLead+detail, report), changed, tree.branch, merge)
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
	ground, mode := n.groundNow()
	if merge == mergeInPlace {
		// AND A RESUMED FAMILY REVALIDATES ITS TREE THROUGH THE ONE CALL THAT
		// MAKES IT (task_tree_mirror.go). A mirror opened by the run that died is
		// found already open and is left exactly as it is; one that was never
		// opened gets the second chance a restart is — the disk may be writable
		// now — and, failing that, recomputes the sentence its parts' sharing of
		// this directory has to be said out loud in. Recomputing beats persisting
		// it: the checkpoint would carry an answer about a machine that has since
		// been rebooted, and there would be two accounts of one fact.
		return openFamilyTree(taskTree{dir: dir, merge: mergeInPlace, ground: ground, mode: mode}), true
	}
	// THE BRANCH CAME OFF THE GROUND, so the repository it merges back into is the
	// ground and not whatever this process happens to be standing in. A node
	// resumed against the wrong repository is a branch that cannot be found, and
	// before the ground was recorded that was every node whose conversation was
	// opened outside the project it was working on.
	root, ok := repositoryRoot(ground)
	if !ok {
		if root, ok = repositoryRoot(workspace); !ok {
			return taskTree{}, false
		}
	}
	return n.ladderRecord(taskTree{dir: dir, root: root, branch: branch, place: place, ground: ground, mode: mode}), true
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
	saved, problem, _ := commitTaskWork(tree.dir, title, changed)
	changed = alsoChanged(changed, saved)
	// THE INHERITANCE COMES BACK OUT OF A KEPT BRANCH TOO, for the reason it does
	// at a merge (groundladder.go): what the sentence offers the person is the
	// node's work, and a branch whose first commit is somebody else's unfinished
	// edits is a branch nobody can read.
	tree.replayOwnWork()
	// AND THE KEPT BRANCH IS PUT WHERE THE PERSON CAN REACH IT. The sentence
	// offers them a branch in their own repository, so a node that worked in a
	// repository of its own owes the same fetch a merge would have owed
	// (groundladder.go's [taskTree.carryBranchHome]).
	tree.carryBranchHome()
	if problem == "" {
		// THE WORKING COPY IS ONLY GIVEN BACK ONCE THE BRANCH HOLDS THE WORK. A
		// commit that could not be made leaves this directory holding the only copy
		// there is, and unregistering it would point the person at a branch with
		// nothing on it while a later sweep took the files (task_land_unsaved.go).
		tree.releaseKept()
	}
	return mergeAborted, changed
}

// releaseKept unregisters a settled task's worktree while preserving its
// branch. THE BRANCH IS THE RECOVERY ARTIFACT; a registered task directory is
// only live machinery, and failed or aborted machinery must not remain in the
// person's repository after the node has stopped.
func (t taskTree) releaseKept() {
	if t.root == "" || t.dir == "" || t.merge == mergeInPlace {
		return
	}
	defer lockGitRoot(t.place, t.root)()
	t.releaseKeptLocked()
}

// releaseKeptLocked is the same cleanup for a caller already holding the root
// lock, which is the conflict arm inside [taskTree.comeHome].
func (t taskTree) releaseKeptLocked() {
	left := leftBehind(t.dir)
	rememberLeftBehind(t.dir, left)
	if t.ownRepository() {
		// A universe was never registered as a worktree of anybody, so there is
		// no registration to unpick and `git worktree remove` would be asking
		// the person's repository about a directory it has never heard of. Its
		// leavings are already written down, and its record is furrow's to drop.
		t.dropUniverse()
		return
	}
	// Keep the task folder's uncommitted leavings without keeping a git
	// registration. Moving it aside lets git remove its administrative record;
	// removing the pointer file then turns the restored directory into ordinary
	// files rather than a broken worktree.
	aside := t.dir + ".unregistering"
	if err := os.Rename(t.dir, aside); err == nil {
		_, _ = git(t.root, "worktree", "remove", "--force", t.dir)
		_ = os.Remove(filepath.Join(aside, ".git"))
		_ = os.Rename(aside, t.dir)
	} else {
		_, _ = git(t.root, "worktree", "remove", "--force", t.dir)
	}
	_, _ = git(t.root, "worktree", "prune")
	_ = os.Remove(filepath.Dir(t.dir))
}

func (a *Agent) taskProgress(ctx context.Context, node *TaskNode, dir string, evidence []string, log io.Writer) (bool, string) {
	if check := a.config.TaskProgressCheck; check != nil {
		return check(node.instruction(), append([]string(nil), evidence...))
	}
	// THE SAME DOOR THE NODE'S OWN AUDIT WOULD GET (task_checks.go). A progress
	// check reads a running tree and decides whether the work is moving; it has
	// no business with a wider hand than the judge that will grade the result,
	// and no reason for a narrower one.
	auditor, err := a.newAuditAgent(dir, node, auditDoorFor(node, auditPlace{ground: dir, ran: dir}))
	if err != nil {
		return false, "the progress check could not start: " + err.Error()
	}
	defer func() { _ = auditor.Close(); a.foldTaskUsage(node, auditor) }()
	// ── THE READER STANDS IN THE WORKER'S OWN GROUND, AND HAS TO LOOK AT IT ──
	//
	// `dir` is the tree the work is running in, and it is the tree this reader is
	// put in ([Agent.newAuditAgent]'s Workspace and the door's ground). What was
	// missing was the OBLIGATION: the question said "read the working copy if
	// useful", and a reader that does not open it is answering "is this work
	// moving toward the brief" from a list of attempted calls alone — which is a
	// question about the world answered from a story about intentions. The
	// benefit of the doubt then goes to WORKING every time, because a story of
	// clean calls has nothing in it that looks like failure.
	ground := strings.TrimSpace(dir)
	auditor.mu.Lock()
	auditor.system = `You are checking the progress of running work, read-only. Decide only whether the recent evidence and the working copy show movement toward the brief or repeated motion without new information. Read the working copy before you answer: the evidence lists what was attempted, and only the working copy says what is there. Answer in at most four lines. The first word must be WORKING or CIRCLING, followed by concrete evidence. WORKING means the leash should be renewed; CIRCLING means it should land now.`
	auditor.mu.Unlock()
	question := "Decide whether this running task is still WORKING TOWARD THE BRIEF or CIRCLING."
	if ground != "" {
		question += " You are standing in the working copy the work is running in, at " + ground + ". Read it before you answer."
	}
	question += " Recent evidence:\n" + strings.Join(evidence, "\n") + "\n\nBrief:\n" + node.instruction() + "\n\nAnswer WORKING or CIRCLING first, then concise evidence."
	// ask puts one question and answers with what was said and how many times the
	// reader reached for the tree while saying it.
	ask := func(text string) (string, int, error) {
		events, err := auditor.Submit(ctx, text)
		if err != nil {
			return "", 0, err
		}
		looked := 0
		for event := range events {
			if event.Kind == EventToolBegin {
				looked++
				fmt.Fprintf(log, "checkpoint · %s\n", event.Hint)
			}
		}
		return strings.TrimSpace(lastSaid(auditor)), looked, nil
	}
	said, looked, err := ask(question)
	if err != nil {
		return false, "the progress check could not be asked: " + err.Error()
	}
	// REQUIRED IS A THING THAT HAPPENS, NOT A WORD IN A PROMPT. A reader that
	// answered without opening anything is asked once more, told what it did, and
	// its second answer stands. Once and not until it complies: a reader that
	// will not look twice is a reader whose answer is the best available, and a
	// loop here would spend the node's money arguing with it.
	if looked == 0 && ground != "" {
		again, _, err := ask("You answered without reading the working copy. Open it now — list what is there and look at the files the evidence names — and answer again. WORKING or CIRCLING first, then concise evidence.")
		if err == nil && again != "" {
			said = again
		}
	}
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
//
// A FORKED HAND COUNTS HERE TOO, and it is the same question with a smaller
// piece of work in it: this agent handed part of what it is doing to something
// else, and has not been told what came of it. A hand is a stream now rather
// than a barrier (fork.go), so a node CAN reach the end of its turn with hands
// still out — and the two readers of this answer are exactly the two that must
// not get it wrong. The tail loop in [runTaskChild] would land the node on top
// of a hand's unread report and throw away the writes the fork was for; the
// no-progress counter would read a node whose work is in somebody else's hands
// as a node spinning.
func (a *Agent) childrenOutstanding() bool {
	if a.jobs.handsOutstanding() {
		return true
	}
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

// reportedChildren counts the task reports this worker has already been handed.
// It is separate from [Agent.childrenOutstanding] because the transition from
// one count to the next is news even when the last child made "outstanding"
// false before the runner reached the event that was already in flight.
func (a *Agent) reportedChildren() int {
	a.mu.Lock()
	graph, parent := a.config.tasker, a.config.taskID
	a.mu.Unlock()
	if graph == nil {
		return 0
	}
	reported := 0
	for _, kid := range graph.children(parent) {
		if kid.reported() {
			reported++
		}
	}
	return reported
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
// the file they saved, one branch up — except on a call that saved nothing,
// which is a reading and is admitted as one by [couldHaveTaught]), and note,
// forget, track, commit and change_setting (a node writing its own memory or its
// own settings again is not learning anything). bash is absent because it is
// BOTH, and is handled on its own below.
var knowledgeTools = map[string]bool{
	"read":                true,
	"read_document":       true,
	"ls":                  true,
	"grep":                true,
	"find":                true,
	"web_search":          true,
	"web_fetch":           true,
	"jobs":                true,
	"recall":              true,
	"view_image":          true,
	"manual":              true,
	"tasks":               true,
	"settings":            true,
	"list_harnesses":      true,
	"services":            true,
	"gmail_read":          true,
	"gmail_search":        true,
	"calendar_list":       true,
	"slack_search":        true,
	"slack_read_thread":   true,
	"slack_list_channels": true,
}

// taughtSomething reports whether one call advanced the node's KNOWLEDGE: a
// knowledge tool, or a bash, whose RESULT was MORE NEW THAN OLD to this node —
// measured line by line, over the lines it has already been shown (novelty.go).
//
// ── PROGRESS IS INFORMATION, NEVER ACTIVITY ──
//
// This used to ask whether the CALL was new — a target the node had not aimed at
// before — and the difference between the two questions was measured at nine
// wasted polls in a row. A model waiting on a background job wrote
// `sleep 30 && tail jobs/1.log`, then `sleep 45 && tail jobs/1.log`, then
// `sleep 60 && …`: nine distinct command strings, nine identical `(no output)`
// answers, and every one of them reset the no-progress counter, because typing a
// different number is activity and the counter was measuring activity. The node
// learned nothing nine times and the one mechanism built to notice that told it
// it was doing well.
//
// So the key is the RESULT and not the call. A new command whose answer the node
// has already been given is not a discovery; the same command run twice with a
// different answer IS one, which is the honest reading of `go test` after an
// edit. SUCCESS is still not required — a new failure is new information — and
// the seen map still keys per node, so one node's discoveries say nothing about
// another's.
//
// BASH IS BOTH HANDS and is admitted here on the knowledge half alone. `go
// build` writes, `go test ./...`, `git log` and `rg` do not, and the tool name
// says nothing about which one this was — so a command whose output the node has
// never read counts as the world answering a question it has never asked, and
// the writing half of the same call is answered by the worktree, one caller up
// ([worktreeMoved]), which asks it of every hand rather than of this one.
//
// It RECORDS AS IT ANSWERS, so the caller must ask it on every step and never
// behind a short-circuit: a result that was already progress for some other
// reason must not also be spendable as a fresh one the next time it comes back.
//
// THE TOOL NAME STAYS IN THE KEY. Two different hands that happen to answer the
// same bytes — an `ls` and a `bash ls` — are two ways of learning the same
// thing, and only the second of them is a spin.
//
// AND A RESULT THE HARNESS WROTE IS NOT THE WORLD ANSWERING. A withdrawn hand
// and a refused door (withdrawn.go's [Event.HarnessMade]) are neither progress
// nor a spin, which the caller's own reading already has right — this returns
// early so that those bytes are not remembered EITHER, because a sentence this
// side of the wall wrote must not be able to make a later, real result look
// like something the node had already been told.
func taughtSomething(event Event, ledger *progressLedger) bool {
	if event.HarnessMade || !couldHaveTaught(event) {
		return false
	}
	return freshAnswer(event, ledger)
}

// couldHaveTaught reports whether one call is even the kind that can bring the
// world back: a knowledge hand, a bash, or THE READING HALF OF A HAND THAT DOES
// BOTH.
//
// That third clause is edit_video, and it is not a special case — it is the same
// law as bash's one paragraph up, applied to the other hand that is both. Which
// half a call was is [producedAFile]'s to say: a `measure` wrote nothing, so
// what it did was tell the node how long a clip runs, whether it carries sound
// and how big its frame is, which are the facts a cut is planned from. Without
// this clause a node that measured its three clips before joining them was three
// steps nearer being stopped for making no progress — the precise punishment for
// exploring that [knowledgeTools] was widened to the whole read-only belt to end.
func couldHaveTaught(event Event) bool {
	if event.Tool == "bash" || knowledgeTools[event.Tool] {
		return true
	}
	return savingTools[event.Tool] && !producedAFile(event.Tool, event.Args)
}

// addedSomething is the WHOLE of what resets the no-progress counter, in one
// place so the runner's switch and the tests that hold it to the law cannot
// answer differently.
//
// Three ways a step adds to the run, and the ledger's books are kept as it
// answers:
//
//   - it SAVED a file ([producedAFile], on a call that ended rather than failed);
//   - it CHANGED THE WORKTREE ([worktreeMoved]), which is the backstop under
//     every hand nobody classified;
//   - it TAUGHT the node something ([taughtSomething] → [progressLedger.read]),
//     which is now three questions and not one — see the ledger for why.
//
// A HARNESS-MADE RESULT IS NEITHER, and it returns before anything is recorded:
// a hand that was withdrawn and a door that refused the call never reached the
// world, so those bytes must not be able to make a later real result look like
// something the node had already been told (withdrawn.go).
//
// AND THE TWO THAT MOVED THE WORK ARM THE NEXT READING. A step that changed the
// deliverable is information itself, and it also makes the reading after it
// information by construction — the node is about to measure a state that did
// not exist a step ago.
func addedSomething(event Event, saved, moved bool, ledger *progressLedger) bool {
	if event.HarnessMade {
		return false
	}
	added := taughtSomething(event, ledger) || saved || moved
	if added {
		ledger.informed()
	}
	if saved || moved {
		ledger.wrote()
	}
	return added
}

// freshAnswer reports whether this call came back with bytes the node has not
// been told before, and records them either way.
//
// [Event.Output] is a display copy, capped ([capOutput]) — which is the right
// thing to compare and not a compromise: two results capped at the same mark are
// the same result as far as anybody, model included, can see, and the cap is the
// same on every call so it can never make two different answers look alike more
// than one truncated page deep.
//
// THE JOB FOOTER COMES OFF FIRST, and it is the reason this function cannot just
// hash the string it is handed. Every result carries the state of every
// outstanding job at its foot (jobfooter.go), and that line holds an elapsed
// time — so it differs on every single call, and a hash taken over it would
// report novelty for a result that had not changed a byte. That is exactly the
// defect above, with the counter's one honest signal inverted into noise.
//
// AND THE NOVELTY IS MEASURED AT THE LINE (novelty.go). Stripping the footer
// answers the one line the harness itself appends; it cannot answer the clock
// the WORLD prints, and every long-running command has one. So the question is
// not "have I seen this result" but "how much of this result have I seen" —
// more new than old ([taughtLineThreshold]) is a step that taught the node
// something, and one changed line in sixteen is not.
func freshAnswer(event Event, ledger *progressLedger) bool {
	added, _, _ := ledger.read(event.Tool, event.Args, stripJobFooter(event.Output))
	return added
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
// progress: the worktree noticed it ([worktreeMoved]). What it is NOT is
// something that comes home on its own. Inside a node the unnamed picture lands
// in the harness's own corner (landing.go's ImagesDir over a node's empty
// Place), which is the one directory a landing never stages; a node whose
// deliverable that picture IS says so in its report and it lands by name
// ([declaredFiles]).
//
// IT IS ALSO THE LANDING BELT. The turn that lands a stopped node is allowed
// exactly these hands and no others ([runTaskChild]'s LAND NOW pass), because
// "save what you already have" and "this call saves something" are the same
// question asked twice — and reading it off one map is what stops the two
// answers drifting apart, which is exactly what happened when the landing pass
// spelled out `write` and `edit` by hand (design-law §ONE SOURCE OF TRUTH).
//
// edit_video (tools_editvideo.go) is here for the LANDING half above all, and it
// is the one name on this list that does not always save: three of its four
// actions write a file — a joined cut, a saved frame, a scored cut — and
// `measure` writes nothing. So THIS MAP ANSWERS ONE OF THE TWO QUESTIONS IT USED
// TO ANSWER. "Which verbs can save something" is the landing belt's question and
// it is still asked here — a node ordered to land the film it spent its life
// cutting must be handed the joining verb, which is the precise defect the
// landing belt was rebuilt to end. "Did THIS call save something" belongs to
// [producedAFile] and to every reader with a finished call in front of it,
// because a measure that reported as a landing kept a standing run folder
// forever and a measure that reported as work moving reset the counter that
// catches spin.
var savingTools = map[string]bool{
	"edit":           true,
	"write":          true,
	"edit_video":     true,
	"generate_image": true,
	"generate_music": true,
	"generate_video": true,
	"speak":          true,
}

// producedAFile reports whether ONE call actually put something on disk, and it
// is what every reader of a finished call asks — what a standing firing came to
// (standing_run.go), whether the work moved (looped.go's [loopWatch.count]),
// whether a landing turn saved anything, and which file a step changed
// ([changedPath]).
//
// THE HAND'S NAME WAS NOT ENOUGH AND THIS IS WHERE THAT STOPPED BEING TRUE.
// [savingTools] answers whether a VERB can save, which is the right question for
// the landing belt and the wrong one here: a wordless standing firing whose only
// act was `edit_video {"action":"measure"}` reported as landed and its run
// folder was never reaped, and a node measuring one clip six times had every
// measurement counted as the work moving.
//
// THE CONDITION IS NOT SPELLED HERE. It is [mutatingTools]', which is the one
// place this build says which CALLS write rather than which verbs can, so the
// guards, the revert ledger and this counter cannot come to different answers
// about the same call (recovery.go). A saving hand nobody wrote a condition for
// saves whenever it succeeds, which is every other name on the list.
//
// It reads the call's ARGUMENTS rather than its answer for [changedPath]'s
// reason: what was asked for is in the arguments, and what came back is a
// sentence about it.
func producedAFile(tool, arguments string) bool {
	if !savingTools[tool] {
		return false
	}
	if writes, conditional := mutatingTools[tool]; conditional {
		return writes(arguments)
	}
	return true
}

// landsLater are the saving hands whose file arrives AFTER the call returns —
// the verbs that answer with a job id and land as a note minutes on
// (tools_video.go, tools_music.go). Every name here is in [savingTools] too:
// a finished render did save something, and it counts as progress the moment
// its note says so. What they cannot do is land on a LANDING TURN. The node is
// closed the instant that turn ends, Close kills every job it still owns
// after a two-second grace ([jobShutdownGrace]), and a render is minutes — so
// a landing turn handed these verbs would submit, hear "job 1 started", and
// have its work cancelled before the bytes existed, while the person was
// told the file was produced. Absent is honest; present and doomed is not.
var landsLater = map[string]bool{
	"generate_music": true,
	"generate_video": true,
}

// landingBelt is the belt a node keeps for its LAND NOW turn: [savingTools]
// minus [landsLater], in the order the node already had them so the model sees
// the same list minus the hands it is being told not to reach for.
func landingBelt(tools []bare.Tool) []bare.Tool {
	kept := make([]bare.Tool, 0, len(savingTools))
	for _, tool := range tools {
		if savingTools[tool.Name] && !landsLater[tool.Name] {
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
// arguments are the record of what was asked (see [Event.Args]). And it asks
// [producedAFile] rather than the hand's name, because a call that saved
// nothing changed no path — an edit_video `measure` that happened to carry a
// `path` argument the action ignores would otherwise have been reported to the
// person as a file this step wrote.
func changedPath(event Event, dir string) (string, bool) {
	if !producedAFile(event.Tool, event.Args) {
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

// declaredFiles are the deliverables a node NAMED in its last words, and they
// are the one door left open by the law that only what a node's own hands wrote
// comes home ([stageTaskWork]).
//
// THE HONEST CASE IT EXISTS FOR: a node whose job is to run a scaffold. The
// files are real, they are the deliverable, and no `write` or `edit` call ever
// named one of them — so the node says so, on one line, and they land. A node
// that ran `pip install` says nothing and the virtualenv stays where it fell.
// The declaration is the node taking responsibility for a file it did not type,
// which is exactly the difference between a deliverable and a dropping.
//
// IT IS READ OFF THE NODE'S WHOLE LAST MESSAGE and not off the carried report,
// which is cut to its first few lines ([taskReport]): the line belongs at the
// bottom, under the account of the work, where it is out of the person's way.
//
// EVERY NAME IS CHECKED AGAINST THE DISK BEFORE IT IS BELIEVED. A model listing
// a file it meant to write is the ordinary failure here, and a name with nothing
// behind it must not become a path on a card or an error in a staging call.
func declaredFiles(said, dir string) []string {
	var declared []string
	for _, line := range strings.Split(said, "\n") {
		rest, found := cutDeclaration(line)
		if !found {
			continue
		}
		for _, name := range strings.Split(rest, ",") {
			name = strings.Trim(strings.TrimSpace(name), "`'\"*")
			if name == "" || len(declared) >= declaredFilesLimit {
				continue
			}
			relative, ok := insideWorktree(dir, name)
			if !ok {
				continue
			}
			if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(relative))); err != nil || info.IsDir() {
				continue
			}
			declared = append(declared, relative)
		}
	}
	return declared
}

// declaredFilesLimit bounds one report's declaration. A node that names two
// hundred files has stopped declaring a deliverable and started pasting a
// directory listing, and the cap is what keeps a report from becoming a staging
// script.
const declaredFilesLimit = 50

// cutDeclaration finds the `files:` line and hands back what it named. The
// leading bullet and the bold markers a model reaches for are trimmed first,
// because "- **files:** a.go" is the same sentence and refusing it would teach
// nobody anything.
func cutDeclaration(line string) (string, bool) {
	trimmed := strings.TrimLeft(strings.TrimSpace(line), "-*• \t")
	if len(trimmed) < len(declarationWord) {
		return "", false
	}
	if !strings.EqualFold(trimmed[:len(declarationWord)], declarationWord) {
		return "", false
	}
	return strings.TrimLeft(trimmed[len(declarationWord):], "* \t"), true
}

// declarationWord is the one spelling, said once here and once in the node's
// own prompt (prompts/task.md's "What comes home"), because a prompt that asked
// for a word this did not read would be a promise the harness does not keep.
const declarationWord = "files:"

// insideWorktree answers whether a name the model wrote points at something in
// the node's own working copy, and gives it back worktree-relative.
func insideWorktree(dir, name string) (string, bool) {
	name = filepath.FromSlash(strings.TrimSpace(name))
	if name == "" {
		return "", false
	}
	if filepath.IsAbs(name) {
		relative, err := filepath.Rel(dir, name)
		if err != nil {
			return "", false
		}
		name = relative
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
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
	// THE FOLD DOOR, not the ordinary auxiliary one: the node journaled these
	// same tokens into the machine's usage ledger as it spent them, and folding
	// the total in again would count them twice ([Agent.addFoldedUsage]).
	a.spendLedger(node).addFoldedUsage(&ai.Response{Usage: &ai.Usage{
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
	return a.newTaskAgentOn(ctx, dir, node, suffix, "")
}

// newTaskAgentOn is [Agent.newTaskAgent] with the model said outright, and it
// exists for exactly one caller: the repair round, whose model is the cascade's
// answer rather than the node's (repair_role.go, task_audit.go's repairNode).
//
// AN EMPTY `on` IS THE ORDINARY CASE and means "the node's own", so every other
// caller is byte-for-byte where it was. What a named model does NOT do is move
// the node: the spec is not touched, the row is not touched, and the escalation
// is recorded as a bill rather than as a retarget — a repair round is one worker
// among several a node takes, and a node whose row started naming the repair's
// model would be telling a person their work moved when it did not.
func (a *Agent) newTaskAgentOn(ctx context.Context, dir string, node *TaskNode, suffix, on string) (*Agent, error) {
	// The model it is ACTUALLY on rather than the id it was admitted with, so a
	// second worker built for a node that was moved is built for where the node
	// now is ([TaskNode.runOn]). They are the same string for every node nothing
	// has moved, which is almost all of them.
	model := node.runModel()
	if on = strings.TrimSpace(on); on != "" {
		model = on
	}
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
	// The rung this session's own next turn would ask for, resolved once here
	// under the lock the ladder's fields are read under, and carried into the
	// child as its floor (effort.go). It is read for the PARENT'S model rather
	// than the node's: what is being inherited is the person's depth, and their
	// dial is a fact about the conversation they turned it in.
	inherited := a.effortLocked(a.model)
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
			// THE ROW IS ONLY MOVED FOR THE NODE'S OWN WORKER. A named model
			// belongs to one round and not to the node (see [Agent.newTaskAgentOn]),
			// so a rescue inside a repair round swaps the model it is about to call
			// and says nothing on the card: the sentence would be about a worker the
			// person was never told existed, and it would overwrite the one line the
			// repair loop legitimately owns there — the gap being closed.
			if on == "" {
				node.graph.mu.Lock()
				node.mend = taskModelRescueNote(model, fallback)
				// AND THE ROW SAYS WHAT IT IS RUNNING ON, not what it was asked to run
				// on: the sentence above and [TaskNode.notice]'s model are two halves of
				// one card, and until this line they named different models.
				node.ran = fallback
				node.graph.mu.Unlock()
			}
			model = fallback
		}
	}
	// A WINDOW MEASURED FOR ANOTHER MODEL IS NOT A FACT ABOUT THIS ONE, so a node
	// running elsewhere is not handed the conversation's figure. It is handed the
	// CARD'S figure for the model it is actually going to run — the same catalog
	// the conversation asks, through the same seam ([Agent.childWindow]) — and
	// zero only when nothing can say, which is this package's own conservative
	// default. Compacting early costs a fold and a cold prompt cache;
	// overflowing costs the turn.
	window := a.childWindow(model)
	// AND THE PROVIDER REPAIR TRAVELS WITH THE CLIENT, WHICH IS WHY IT IS NOT IN
	// THE LITERAL BELOW. Routing, ModelFallbacks and NearestModels are read in
	// exactly one place — [New], where they are handed to the provider client
	// (agent.go) — and this hands the node THAT CLIENT. So a worker asks through
	// the person's own routing strategy, falls back down the person's own list,
	// and gets the catalog's nearest-model rescue when there is no list, without
	// carrying a copy of any of the three: they are facts about the connection,
	// and there is one connection. Copying them onto the node's Config would be
	// three fields nothing reads. What a node must NOT share is the request
	// wrapper around that client — see [unwrapCompleter] for the cache lineage.
	client := unwrapCompleter(a.client)
	// ONE PLACE ANSWERS BOTH QUESTIONS ABOUT THIS WORKER'S FILES, and they are the
	// same question: the transcript it writes and the litter it leaves both belong
	// to the family, never to the directory it happens to be working in
	// ([Agent.familyPlace], landing.go). It is read here, under the lock, because
	// the literal below is built after it is released.
	family := a.familyPlace(node)
	journal := taskJournalPath(family, a.sessionID(), node.id, suffix)
	// AND THE CONVERSATION EVERY DOLLAR THIS WORKER SPENDS BELONGS TO, resolved
	// here because here is the only place that can: this agent is the node's
	// parent, so either it already carries a root — it is itself a node, and the
	// root travelled down when it was built — or it IS the root, and its own
	// journal names it. Reading it under the lock keeps it beside the journal
	// name, which is the other fact about lineage this literal needs.
	//
	// WHAT IT IS FOR is the money segment on the conversation's own status line.
	// A node's ledger lines name the node's journal, which is a file nobody
	// outside the family has heard of, so before this the only way to add a
	// running tree's spend back onto the conversation that started it was to
	// wait for the fold at close ([Agent.foldTaskUsage]) — hours, on the run
	// that produced issue #145. With the root on the row a reader adds up the
	// subtree from the ledger it is already reading, each call once.
	root := strings.TrimSpace(a.config.rootSession)
	if root == "" {
		root = a.sessionID()
	}
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
		fixesDir: a.config.fixesBucket(),
		// THE FAMILY SPENDS INTO ONE LEDGER, and it is the ledger the
		// conversation was pointed at. Empty is the machine's own file, which is
		// every door in the product; what this line fixes is the case where it
		// is not — a test, or a second brain on one laptop — where a node used to
		// fall back to the machine's ledger while its parent wrote somewhere
		// else, and a rollup over the family then found half of it
		// (usage_ledger.go).
		usageLedger: parent.usageLedger,
		// AND WHOSE MONEY IT IS. Every line this worker records names the
		// conversation the work is rooted in, however deep the family goes.
		rootSession: root,
		// AND WHERE ITS LITTER GOES, which is NOT its workspace. A worker is not a
		// session and carries no Place — that is deliberate (session.go) — so with
		// nothing here its job logs and its stubbed tool results landed in
		// <workspace>/.aforge-v3, and a worker's workspace is the person's
		// repository or a worktree of it. landing.go states the law and the
		// measured failure; this line is the whole of the fix for a task node.
		droppings: family,
		// AND WHETHER THIS DIRECTORY IS A PROJECT, which is the family's question
		// and not the worker's: a worker carries no Place (session.go), so the
		// answer is settled here, once, while the family's is in hand.
		ownSpace:      standingInOwnSpace(family, dir),
		Workspace:     dir,
		Model:         model,
		APIKey:        parent.APIKey,
		BaseURL:       parent.BaseURL,
		ContextWindow: window,
		// AND THE CATALOG ITSELF, so a node that switches its own model later
		// learns that model's window rather than keeping this one (agent.go's
		// SetModel). A child without it is a child whose compaction stops
		// following the window the moment it moves.
		ContextWindowFor: parent.ContextWindowFor,
		CompactEnabled:   parent.CompactEnabled,
		SessionFile:      journal,
		// ── how hard this worker thinks ─────────────────────────────────────
		//
		// A NODE IS THE PERSON'S OWN WORK AT ONE REMOVE, so it inherits their
		// depth rather than running at whatever a child agent's zero value is —
		// which is what it did, silently, and made a conversation dialled to
		// max hand its hardest part to a worker asking for nothing.
		//
		// THE NODE'S OWN RUNG IS THE WORK'S, AND THE PARENT'S ANSWER IS THE
		// FLOOR UNDER IT. What arrives as this child's default is the rung the
		// parent's own next turn would ask for — already resolved, so a dial the
		// person turned, the conversation's sticky rung and the install's row
		// have all been folded once and cannot disagree here. A rung set on THIS
		// task then sits above it, which is the whole reason [Agent.SetTaskEffort]
		// is worth having: a person who dials one piece of work deeper means that
		// piece of work, not the conversation it came from.
		//
		// The role is worker, which has no floor of its own — a task is not
		// machinery running while nobody watches, it is the job.
		Effort:        node.effortRung(),
		EffortRole:    effort.RoleWorker,
		DefaultEffort: inherited,
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
		// AND THE NODE'S PULSE TRAVELS THE SAME WAY, for the same reason: the
		// checker and each repair round are the same NODE working, so a reader
		// outside the process must see one heartbeat across all of them rather
		// than three files appearing and disappearing (task_beat.go).
		beat: node.beatWriter(),
		// AND SO DOES THE NODE'S TALLY OF ITS OWN FAILURES, for the pulse's
		// reason one layer in: the worker, each repair round and the gate are the
		// same NODE, and the one question the gate cannot answer alone is whether
		// the round it is judging lost its calls on the wire
		// (taxonomy_boundary.go).
		failures: a.tallyFor(node),
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
		// AND THE ANSWER TO "CAN THIS MODEL HOLD A TOOL", without which the
		// rescue above works exactly one level deep. It is read at the top of
		// THIS constructor, off the parent's config, so a worker that did not
		// carry it built its own pieces with no check at all: a part, or a
		// propose_task child of a node, would start on a model that cannot call
		// a tool and spend a whole run discovering it. It is the same catalog
		// row the picker filters on, and a node is the same worker doing the
		// same job somewhere quieter.
		SupportsParameter: parent.SupportsParameter,
		ReasoningProfile:  parent.ReasoningProfile,
		// The leash's checkpoint seam travels for the reason a seam exists at
		// all: it stands in for the read-only checker on the agent that OWNS the
		// node ([Agent.taskProgress]), and a part's owner is its parent's worker.
		// Production leaves it nil and asks the real checker either way; a seam
		// that stopped one level short meant a part's leash was the one threshold
		// nothing could put a deterministic answer behind.
		TaskProgressCheck: parent.TaskProgressCheck,
		RolesSource:       parent.RolesSource,
		// AND THE ONE-MODEL PROMISE TRAVELS WITH THE LADDER IT OVERRIDES. The
		// flag is a promise about the SESSION — every text call it makes rides
		// the model the person named — and a session is not only the turns typed
		// into it: it is the nodes those turns hand out and the hands those nodes
		// lift. A child that forgot the flag would be a rung the flag never
		// reached, and it would be exactly the crew-only rungs that forgot,
		// because they are the only ones that need telling (auxiliary.go's
		// [Agent.callRole], #443). Every other place a child copies RolesSource
		// copies this beside it, for this reason: fork.go, orchestrate.go and
		// task_audit.go.
		OneModel:       parent.OneModel,
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
		// The foreground-command clock travels with the worker for the same
		// reason the task settings above do: a build handed to a node is still
		// the person's work, and a setting that stopped at the conversation
		// would mean something different as soon as work was handed out.
		BashBackgroundAfterSeconds: parent.BashBackgroundAfterSeconds,
		tasker:                     tasker,
		taskID:                     nodeID,
		taskDepth:                  depth,
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
// person asking "what did the auditor actually run" wants a file to open.
//
// THE STAMP CARRIES MICROSECONDS BECAUSE A NAME THAT REPEATS IS A JOURNAL TWO
// AGENTS BOTH OWN. It used to be good only to the second, and the burden of
// making the name unique was pushed onto the caller's suffix — which the audit
// does with a nonce and a node's own journal cannot, because the node has
// nothing to add. Two nodes carrying the same id under one session name, minted
// inside the same second, were therefore handed ONE path: the second agent
// either resumed the first one's transcript or, while the first still held it,
// was refused outright with a [SessionLockedError] and the work it was starting
// never began. That is not a rare shape — a session with no journal file of its
// own is named by the constant "unfiled" for every window on the machine, and
// node ids start again at one in every graph — and it is the whole of the flake
// this stamp was widened to close. Microseconds are fixed width, so the
// lexicographic "newest name" [findTaskJournal] reads is still the newest file.
func taskJournalPath(place Place, session string, id uint64, suffix string) string {
	name := fmt.Sprintf("%s_%d%s.jsonl", journalMoment().Format("20060102-150405.000000"), id, suffix)
	return filepath.Join(taskJournalDir(place, session), name)
}

// journalMoment is the clock [taskJournalPath] names files by, and it NEVER
// ANSWERS THE SAME MICROSECOND TWICE in one process.
//
// A stamp is only unique if the clock behind it moved, and two nodes minted
// close enough together read one instant on any machine whose clock is coarser
// than the code asking it. Rather than pick a resolution and hope, the second
// caller inside one tick is handed the tick after it: the names stay in minting
// order, they stay fixed width, and "unique" stops being a probability.
//
// It is deliberately not a counter appended to the name. [findTaskJournal] finds
// a node's transcript by the `_<id>` its stem ENDS with and picks the newest by
// name, so everything that distinguishes two files has to live in the stamp at
// the front of it.
var journalClock struct {
	mu   sync.Mutex
	last time.Time
}

func journalMoment() time.Time {
	journalClock.mu.Lock()
	defer journalClock.mu.Unlock()
	// Truncated to the resolution the NAME carries, because that is the only
	// resolution the answer means anything at: two instants a hundred nanoseconds
	// apart are one file name, so comparing anything finer would call a collision
	// a move forward. Truncate also drops the monotonic reading, which is what
	// makes the comparison a wall-clock one to match.
	now := time.Now().Truncate(time.Microsecond)
	if !now.After(journalClock.last) {
		now = journalClock.last.Add(time.Microsecond)
	}
	journalClock.last = now
	return now
}

// taskJournalDir is the directory every one of a session's node transcripts is
// written into: the session folder's tasks/, or the legacy parallel tree for a
// session with no folder. It is the one answer to "where would this session's
// task journals be" — [taskJournalPath] mints new names inside it and
// [findTaskJournal] looks for old ones in it — so the two cannot look in
// different places.
func taskJournalDir(place Place, session string) string {
	if journals := place.NodeJournals(); journals != "" {
		return journals
	}
	// The legacy tree, through the one seam: os.UserHomeDir was read directly
	// here, which is why AFORGE_HOME moved every other v3 file and left a node's
	// transcript behind in the real home (Decision 26, "one home, one seam").
	return filepath.Join(LooseTasksRoot(), session)
}

// findTaskJournal is the node's transcript found by its id rather than by its
// name: the newest `<stamp>_<id>.jsonl` in the session's journal directory, or
// "" when there is none.
//
// It exists for checkpoints written before the journal path was carried on the
// record (task_store.go's taskRecord.Journal): the file is on disk and named
// with the node's id, so a session that never learned the name can still find
// the file. THE MAIN TRANSCRIPT ONLY — the stem must end in exactly `_<id>`, so
// the audits (`_<id>-audit-<nonce>`) and repair rounds (`_<id>-repair1`) that
// sit beside it under the same id are never mistaken for the run that IS the
// node. Several mains under one id are a node that ran more than once — an
// interrupt resumed — and the stamp leads the name, so the greatest name is the
// latest run.
func findTaskJournal(dir string, id uint64) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	want := fmt.Sprintf("_%d", id)
	var newest string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		stem := strings.TrimSuffix(name, ".jsonl")
		if !strings.HasSuffix(stem, want) || stem == want {
			continue
		}
		if name > newest {
			newest = name
		}
	}
	if newest == "" {
		return ""
	}
	return filepath.Join(dir, newest)
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
	// ground is the repository or folder the work is ABOUT and mode is how this
	// tree stands on it (taskstands.go). For a worktree they say the same thing
	// root and branch already do; for every other mode they are the only record
	// of it — a mirror's landing has nowhere else to learn which folder to copy
	// itself back into, and an audit restore has nowhere else to learn which
	// repository to cut a clean copy from.
	ground string
	mode   TaskMode
	// rung is WHICH RUNG OF THE GROUND LADDER made this world and seal is the
	// one string that names it — furrow's sealed snapshot, or the machine
	// commit's sha (groundladder.go). They are carried so that a report can say
	// what world the work was done in, which is the question nobody could answer
	// about the run the ground law was written from.
	rung GroundRung
	seal string
	// base is the machine commit the parent's world was sealed into, when a rung
	// made one. It is the replay point the landing takes the inheritance back out
	// at ([taskTree.replayOwnWork]) and it is empty for a parent that had nothing
	// uncommitted, which is the ordinary case.
	base string
	// universe is the furrow fork's name, when a fork made this world, and it is
	// the only handle furrow takes for dropping the record afterwards.
	universe string
	// note is the one sentence this world owes the node standing in it, and it
	// is empty for every world that came out as promised — which is nearly all
	// of them. It exists because a promise that quietly did not hold is worse
	// than one that was never made: a family whose tree could not be opened
	// gives its parts the parent's own directory to share, and the parent has to
	// be told so while it can still act on it (task_tree_mirror.go). The job log
	// prints it and the worker's own brief carries it.
	note string
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
	return prepareTaskTreeAt(context.Background(), place, workspace, session, id, title, "", "")
}

// prepareTaskTreeAt applies the placement contract before it touches git. An
// explicit place is worked in exactly as named; only an empty where takes the
// default road of cutting a worktree from the conversation's repository.
// frozen is the family's own world, when this node is a part of one: the commit
// its parent froze the family tree at, which the ladder cuts from instead of
// sealing a tree that has moved since (task_divide_wip.go). Empty for everything
// that is not a part, which is almost every task.
func prepareTaskTreeAt(ctx context.Context, place Place, workspace, session string, id uint64, title, where, frozen string) (taskTree, error) {
	where = strings.TrimSpace(where)
	if strings.EqualFold(where, "in place") {
		return taskTree{dir: workspace, merge: mergeInPlace, ground: canonicalPath(workspace), mode: TaskModeInPlace}, nil
	}
	if where != "" {
		dir, err := resolveTaskWhere(where, workspace)
		if err != nil {
			return taskTree{}, fmt.Errorf("task workspace: %w", err)
		}
		// NAMING A PLACE IS ASKING FOR IT. [resolveTaskWhere] deliberately does
		// not touch the disk, because the proposal card resolves the same words
		// only to SHOW them ([taskWhereNotice]) and a preview may not leave a
		// directory behind. This is the other side of that split: the moment the
		// work is actually being placed, a person who said `use ~/scratch` about
		// a folder that is not there yet meant "work there", and answering "no
		// such file" would be the surface refusing an errand it can simply run.
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return taskTree{}, fmt.Errorf("task workspace: %w", err)
		}
		return taskTree{dir: dir, merge: mergeInPlace, ground: dir, mode: TaskModeInPlace}, nil
	}
	// AN OWNED CONVERSATION TAKES THE ORDINARY ROAD, and there is no arm here
	// for it. A conversation opened outside any project has a workspace of its
	// own — work/, which the door makes into a repository with a first commit
	// precisely so that tasks get worktrees (cmd/aforge's prepareOwnedWorkspace)
	// — so it HAS somewhere to stand and needs nothing said about projects. This
	// once refused every such task with "this task needs a project", which was a
	// person being turned away from work that needed no repository at all: file
	// an issue, read something, write a document. The two roads below already
	// answer the only case that has nowhere to branch from — an owned workspace
	// from an older build, or one whose git init failed, which is not a
	// repository and runs in place and says so.
	root, ok := repositoryRoot(workspace)
	if !ok || !hasCommit(root) {
		// A directory that is no repository, and a repository with no commit to
		// branch from, are one answer: there is nothing to cut a worktree off, and
		// pretending to isolate is worse than not isolating.
		return taskTree{dir: workspace, merge: mergeInPlace, ground: canonicalPath(workspace), mode: TaskModeFolder}, nil
	}
	return cutTaskWorktree(ctx, place, root, session, id, title, frozen)
}

// hasCommit reports whether a repository has a HEAD to branch from. A fresh
// `git init` has none, and every road that would cut a branch has to ask.
func hasCommit(root string) bool {
	_, err := git(root, "rev-parse", "--verify", "HEAD")
	return err == nil
}

// taskOwnFolder is the directory ONE NODE WORKS IN, wherever the work is about:
// trees/<id> under the session that proposed it, or — for the layout that has no
// session folder — the same name under the repository being borrowed. It is the
// path prepareTaskTree has always used, lifted out so that the modes that are
// not a worktree can stand in the same place a worktree would have.
func taskOwnFolder(place Place, workspace, session string, id uint64) (string, os.FileMode) {
	dir := filepath.Join(workspace, filepath.FromSlash(tasksDirName), taskTreeSession(session), strconv.FormatUint(id, 10))
	mode := os.FileMode(0o755)
	if trees := place.Trees(); trees != "" {
		dir, mode = filepath.Join(trees, strconv.FormatUint(id, 10)), 0o700
	}
	// Git resolves symlinks before it registers a worktree. Record that same
	// spelling from the start so the checkpoint, cleanup and git all name one
	// directory even while the final path does not exist yet.
	return canonicalPath(dir), mode
}

// cutTaskWorktree is the branch itself: `git worktree add -b` off the GROUND's
// HEAD, in a directory of the node's own.
//
// THE REPOSITORY IT CUTS FROM IS AN ARGUMENT NOW, and that one change is most of
// what issue #76 came to. It used to be whatever repository the conversation
// happened to be standing in, which for a chat opened in a home directory was an
// empty repository the door had just minted — so the work was isolated from
// nothing, guarded as though it were, and checked in a tree that held none of it.
// The ground is resolved from evidence instead (taskstands.go) and handed here.
//
// WHAT THE BRANCH CARRIES IS HEAD AND NOTHING ELSE: the person's uncommitted
// changes stay in their checkout, unread and untouched, and the record says so
// so that nobody has to find out by looking.
func cutTaskWorktree(ctx context.Context, place Place, root, session string, id uint64, title, frozen string) (taskTree, error) {
	// THE LEGACY LAYOUT HANGS OFF THE REPOSITORY, not off the workspace: a
	// conversation standing in a subdirectory of a project still puts its
	// worktrees in one place, which is what keeps a sweep able to find them.
	dir, mode := taskOwnFolder(place, root, session, id)
	// AND THE LADDER CHOOSES HOW THE WORLD IS MADE (groundladder.go). Every road
	// that grounds a node in a repository arrives here, so this one call is what
	// makes the ground law true for all of them rather than for the one road
	// somebody remembered to change.
	return carveGround(ctx, groundOrder{
		place:   place,
		ground:  root,
		root:    root,
		dir:     dir,
		mode:    mode,
		branch:  "task/" + slugify(title) + "-" + shortID(),
		title:   title,
		promise: TaskModeWorktree,
		frozen:  frozen,
	})
}

// cutWorktreeAt is the git of it, with the two names handed in: a directory to
// stand the working copy in and a branch to cut.
//
// IT IS SEPARATE FROM [cutTaskWorktree] SO THAT THERE IS ONE WORKTREE ROAD AND
// NOT TWO. A node's tree is named from its id under the session's trees/; the
// conversation's own standing tree on a referred folder is named from that
// folder (standingtree.go) and must NEVER land in the id space a node counts
// through, or the reclaim below — which is safe precisely because the only
// thing that can be sitting at a node's path is that node's own wreckage —
// would be clearing out a live working copy. Everything else about a worktree
// is identical for both, so everything else is here.
func cutWorktreeAt(place Place, root, dir, branch string, mode os.FileMode) (taskTree, error) {
	return cutWorktreeFrom(place, root, dir, branch, mode, "HEAD")
}

// cutWorktreeFrom is [cutWorktreeAt] with the START POINT handed in, which is
// the whole of what the ground law changed about cutting a worktree: a task's
// branch is carved from a commit that HOLDS THE PARENT'S WORLD (groundladder.go's
// [sealGroundWork]) rather than from HEAD, which holds only what somebody last
// committed. The conversation's own working copy on a referred folder still
// asks for HEAD and gets exactly what it always got.
func cutWorktreeFrom(place Place, root, dir, branch string, mode os.FileMode, from string) (taskTree, error) {
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
	if out, err := git(root, "worktree", "add", "-b", branch, dir, from); err != nil {
		return taskTree{}, fmt.Errorf("git worktree add: %s", firstLine(out))
	}
	return taskTree{dir: dir, root: root, branch: branch, place: place, ground: root, mode: TaskModeWorktree}, nil
}

// prepareTaskTreeOn gives one node a place to work ON ITS GROUND, which is the
// road every ordinary task takes now.
//
// The five modes are five different promises about one directory, and each is
// kept here or not made at all ([TaskMode] says what each one is for):
//
//   - WORKTREE cuts the branch from the ground itself.
//   - REFERENCE gives the node a folder of its own and leaves the ground alone.
//     The read-only half is the guard's to enforce; what this owes is a working
//     copy that is not inside the thing being read.
//   - MIRROR copies the folder in, so that a folder with no history behind it
//     still gets the isolation a repository gets for free.
//   - IN PLACE and FOLDER are the two honest un-isolations: the work happens in
//     the ground, because somebody said "here" or because there is nowhere else.
//
// A GROUND NOBODY RESOLVED FALLS BACK TO THE OLD ROAD, unchanged. Every door
// that admits a node does not resolve one — a subharness run, a design, a graph
// scripted by a test — and the tree they get is the tree they always got, with
// its ground filled in from the repository it was actually cut from.
func prepareTaskTreeOn(ctx context.Context, place Place, workspace, session string, id uint64, title string, stand taskStand) (taskTree, error) {
	ground := canonicalPath(strings.TrimSpace(stand.dir))
	if ground == "" {
		// A PART REACHES THIS ROAD AND ITS FREEZE HAS TO TRAVEL WITH IT. A part
		// admitted by `divide_work` carries no ground of its own (task_divide.go
		// names no stand), so it finds its worktree by asking the directory it is
		// standing in — which is the family tree — and without the freeze this
		// fall-through would seal that tree as its parent has left it a minute
		// later.
		return prepareTaskTreeAt(ctx, place, workspace, session, id, title, "", stand.frozen)
	}
	switch stand.mode {
	case TaskModeInPlace, TaskModeFolder:
		if err := os.MkdirAll(ground, 0o755); err != nil {
			return taskTree{}, fmt.Errorf("task workspace: %w", err)
		}
		// A task standing IN its ground inherits the world by standing in it,
		// which is the ground law's easiest case and the one rung nobody has to
		// climb (groundladder.go's [GroundRungHere]).
		return taskTree{dir: ground, merge: mergeInPlace, ground: ground, mode: stand.mode, rung: GroundRungHere}, nil
	case TaskModeReference:
		dir, mode := taskOwnFolder(place, workspace, session, id)
		if err := os.MkdirAll(dir, mode); err != nil {
			return taskTree{}, fmt.Errorf("task workspace: %w", err)
		}
		// A REFERENCE IS THE ONE MODE THE LADDER DOES NOT CLIMB, and that is the
		// promise rather than an omission: the node was given a folder of its
		// own precisely so that it does NOT hold the ground, which it may only
		// read. Copying the world in would be the opposite of what was asked.
		return taskTree{dir: dir, merge: mergeInPlace, ground: ground, mode: stand.mode}, nil
	case TaskModeMirror:
		dir, mode := taskOwnFolder(place, workspace, session, id)
		tree, err := carveGround(ctx, groundOrder{
			place:   place,
			ground:  ground,
			dir:     dir,
			mode:    mode,
			title:   title,
			promise: TaskModeMirror,
		})
		if err != nil {
			return taskTree{}, err
		}
		// AND THE COPY IS WRITTEN DOWN AS THE FOLDER AS IT STOOD
		// (task_mirror_manners.go). The bytes in it are the ones the family was
		// given, and without a record of them the landing cannot tell the person's
		// own edit from the family's work and writes over it in silence.
		rememberGroundBaseline(tree.dir)
		// AND THE COPY IS OPENED AS THE FAMILY'S OWN TREE (task_tree_mirror.go).
		// A mirror that is a repository is a mirror whose parts cut real
		// worktrees off it and merge back into it through the one road every
		// repository part already takes — which is how the promise both prompts
		// make to a part becomes true for a folder as well.
		return openFamilyTree(tree), nil
	}
	root, ok := repositoryRoot(ground)
	if !ok || !hasCommit(root) {
		// The ground turned out to have no history to branch from between the
		// proposal and this moment, or never had one. The honest answer is the one
		// the old road gives: work in it and say so, rather than claim a branch
		// that was never cut.
		return taskTree{dir: ground, merge: mergeInPlace, ground: ground, mode: TaskModeInPlace, rung: GroundRungHere}, nil
	}
	return cutTaskWorktree(ctx, place, root, session, id, title, stand.frozen)
}

// mirrorGround copies a plain folder into the node's own directory so that work
// on it is isolated the way work on a repository is.
//
// IT SKIPS WHAT THE AUDIT'S OWN COPY SKIPS and stops where it stops
// ([copyOriginal], [auditRestoreEntries]): a repository's metadata and the
// harness's own corner are nobody's deliverable, and a folder holding a build
// output big enough to cost minutes is a folder this is not worth doing to.
// Refusing loudly beats a task that appears to hang before its first step.
func mirrorGround(ground, dir string) string {
	visited := 0
	var walk func(relative string) string
	walk = func(relative string) string {
		entries, err := os.ReadDir(filepath.Join(ground, filepath.FromSlash(relative)))
		if err != nil {
			return ""
		}
		for _, entry := range entries {
			child := entry.Name()
			if relative != "" {
				child = relative + "/" + entry.Name()
			}
			if entry.Name() == ".git" || child == aforgeDroppings {
				continue
			}
			if visited++; visited > auditRestoreEntries {
				return fmt.Sprintf("%s holds more than %d files, which is more than a task can be given a copy of", ground, auditRestoreEntries)
			}
			source := filepath.Join(ground, filepath.FromSlash(child))
			target := filepath.Join(dir, filepath.FromSlash(child))
			if entry.IsDir() {
				if err := os.MkdirAll(target, 0o755); err != nil {
					return "this folder could not be copied for the task: " + err.Error()
				}
				if problem := walk(child); problem != "" {
					return problem
				}
				continue
			}
			if err := copyPath(source, target); err != nil {
				return "this folder could not be copied for the task: " + err.Error()
			}
		}
		return ""
	}
	return walk("")
}

// inOwnSpace is [standingInOwnSpace] asked of a whole Config, and the two
// answers are the two kinds of agent there are. A CONVERSATION carries its Place
// and can be asked directly. A WORKER carries none on purpose (session.go's
// droppings states why), so the constructor that built it wrote the answer down.
func (c Config) inOwnSpace() bool {
	return c.ownSpace || standingInOwnSpace(c.Place, c.Workspace)
}

// standingInOwnSpace answers whether a worker's directory is THE CONVERSATION'S
// OWN SPACE rather than a project: the owned session's work/ repository, or a
// worktree cut from it under trees/.
//
// IT IS ASKED BECAUSE THE TWO LOOK IDENTICAL FROM INSIDE. A worker that opens a
// directory holding nothing cannot tell a conversation that never had a project
// from a checkout that failed, and the second reading is the one that sends it
// hunting for a repository nobody ever named. One line in its prompt settles it
// (prompt.go), and the line is only true of these directories: a place the
// person named explicitly is somewhere they chose, and never this.
func standingInOwnSpace(place Place, dir string) bool {
	if !place.Owned {
		return false
	}
	dir = canonicalPath(strings.TrimSpace(dir))
	if dir == "" {
		return false
	}
	for _, own := range []string{place.Workspace, place.Work(), place.Trees()} {
		own = canonicalPath(strings.TrimSpace(own))
		if own == "" {
			continue
		}
		if dir == own || strings.HasPrefix(dir, own+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// resolveTaskWhere turns the words a person or a proposal spelled for `where`
// into one absolute directory: `~` is their home, a relative name hangs off the
// conversation's own workspace, and an absolute path is taken as it stands.
//
// A PATH THAT IS NOT THERE YET IS NOT AN ERROR. Somebody who names a fresh
// folder is saying where the work should go, not making a claim about what is
// already on disk, and the caller that is really placing work creates it
// ([prepareTaskTreeAt]). The one refusal left is a path that EXISTS and is not a
// directory — a file cannot be worked in, and silently creating something beside
// it would be the surface guessing.
//
// IT READS THE DISK AND NEVER WRITES IT, because the proposal card resolves the
// same words for display before the person has said yes ([taskWhereNotice]).
func resolveTaskWhere(where, workspace string) (string, error) {
	if where == "~" || strings.HasPrefix(where, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		where = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(where, "~"), string(filepath.Separator)))
	}
	if !filepath.IsAbs(where) {
		where = filepath.Join(workspace, where)
	}
	dir, err := filepath.Abs(where)
	if err != nil {
		return "", err
	}
	switch info, err := os.Stat(dir); {
	case errors.Is(err, os.ErrNotExist):
		// Nothing there yet, which is the ordinary shape of naming a new place.
	case err != nil:
		// Anything else — a permission wall, a broken mount — is a real fact
		// about the disk and is handed back rather than papered over.
		return "", err
	case !info.IsDir():
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return filepath.Clean(dir), nil
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

// comeHome commits what the node WROTE and merges its branch into the person's.
// It reports the outcome and, ONLY when the outcome needs explaining, a line or
// two for the node's report: an ordinary merge is already said by the Merge and
// Branch fields and by the completion note, and saying it a third time inside
// the report is the same sentence three times on one card. A conflict and a
// working copy full of things nobody wrote are both worth explaining, and both
// come back here.
//
// WHAT COMES HOME IS WHAT THE NODE'S OWN HANDS WROTE, which is the whole of
// [stageTaskWork]'s law and the reason wrote is an argument rather than a walk
// of the directory. A node that ran `pip install` inside its worktree has a
// virtualenv in it and did not write one; landing it committed 26 hunks of
// vendored noise over a change nobody could find.
//
// THE MERGE IS ATTEMPTED WHATEVER THE PERSON'S TREE LOOKS LIKE. A dirty
// checkout is the normal state of somebody who has been working, and refusing
// to try would make "I had a file open" the reason a finished task did not
// land. If git cannot do it — a real conflict, or local changes it would have
// to overwrite — the branch is KEPT and named, and nothing of the node's work
// is lost.
func (t taskTree) comeHome(title string, wrote []string) (string, string, landingRefusal) {
	if t.mode == TaskModeMirror {
		return t.landMirror(wrote)
	}
	if t.merge == mergeInPlace || t.root == "" {
		return mergeInPlace, "", refusedNothing
	}
	// A LANDING THAT COULD NOT SAVE THE WORK STOPS HERE. Nothing merges, nothing
	// is released, and the branch and the working copy both stay exactly where
	// they are — what is on that disk is the only copy of the work there is
	// (task_land_unsaved.go). Going on used to merge a branch holding nothing and
	// then remove the directory the work was in.
	if _, problem, why := commitTaskWork(t.dir, title, wrote); problem != "" {
		return mergeAborted, unsavedSentence(t.dir, problem), why
	}
	// THE INHERITANCE GOES BACK OUT BEFORE THE WORK COMES IN. A branch carved
	// off the ground ladder's machine commit holds the parent's uncommitted
	// world underneath the node's own commits, and merging that would hand
	// somebody a merge of their own unfinished edits — which git refuses
	// outright (groundladder.go's [taskTree.replayOwnWork]). It is nothing at all
	// for a tree whose parent had nothing uncommitted, which is most of them.
	//
	// AND A LIFT THAT WOULD NOT GO IS SAID OUT LOUD, on every road out of here.
	// The branch then holds the person's own unfinished edits as well as the
	// node's work, the report is the only place they can learn it, and the
	// silence used to be the whole of the account (groundcarry.go).
	stranded := ""
	if t.replayOwnWork() {
		stranded = strandedGroundSentence(t.branch)
	}
	// Read AFTER the commit and BEFORE the worktree is removed: what is still
	// sitting there once the node's own work is committed is by definition what
	// the node did not write, and this is the only moment it can be named.
	left := leftBehind(t.dir)

	defer lockGitRoot(t.place, t.root)()
	// AND THE NODE'S COMMITS ARE PUT WHERE THE GROUND CAN REACH THEM. A branch
	// cut in a worktree has been in this repository all along and this does
	// nothing; a branch cut inside a universe is in a repository of its own, and
	// one fetch of one branch is the whole difference between the two landing
	// roads (groundladder.go's [taskTree.carryBranchHomeLocked]).
	if out, err := t.carryBranchHomeLocked(); err != nil {
		t.releaseKeptLocked()
		return mergeConflicted, withReport(withReport(unreachedSentence(t.branch, t.dir, out), stranded),
			leftBehindSentence(left, true)), refusedByTheWork
	}
	// AND THE MERGE IS THE CARRY-OR-REFUSE ONE (groundcarry.go). The ground a
	// task was carved from is the ground it merges into: work of the person's own
	// standing in the way is set aside for the merge and put back afterwards, and
	// where it cannot be put back the tree goes back exactly as it was and the
	// branch is kept with the files NAMED. It never fails with a sentence that
	// names nothing, which is what this used to do.
	landed, said := t.mergeIntoGround()
	if !landed {
		// The committed branch is the durable recovery point. Keeping the failed
		// worktree registered would leave the person's repository pointing into a
		// task folder that a later sweep may remove underneath it.
		t.releaseKeptLocked()
		return mergeConflicted, withReport(withReport(said, stranded),
			leftBehindSentence(left, true)), refusedByTheWork
	}
	// The working copy is given back only once its work is in, and which road
	// that takes is the rung's own (groundladder.go's [taskTree.releaseLanded]).
	t.releaseLanded()
	// The working copy has just gone, and the sentence says where its leavings
	// went with it rather than sending anybody to look in a directory that is no
	// longer there.
	return mergeMerged, withReport(withReport(said, stranded),
		leftBehindSentence(left, false)), refusedNothing
}

// landMirror brings a mirrored folder home: the files the node wrote, laid over
// the ground BY NAME, and the ones it wrote and then deleted taken away again.
//
// IT IS THE AUDIT'S OWN LAYING ([layWork]) and not a second copier, for the
// reason the restore states about itself: what ships is `wrote` and there may
// only ever be one reading of it. A mirror that landed by walking its own
// directory would carry back everything a build left in it.
//
// The outcome is the in-place one, because from where the person sits that is
// what happened: their folder has the work in it, there is no branch, and there
// is nothing to merge. How it got there is [TaskNode.Mode]'s to say.
//
// EXCEPT WHERE THE FOLDER MOVED UNDER IT, which is the one outcome that is not
// in-place: a file the person edited themselves while the work ran is a file
// this refuses to write over (task_mirror_manners.go).
func (t taskTree) landMirror(wrote []string) (string, string, landingRefusal) {
	if strings.TrimSpace(t.ground) == "" || strings.TrimSpace(t.dir) == "" {
		return mergeInPlace, "", refusedNothing
	}
	// AND IT DOES NOT WRITE OVER A FILE THAT CHANGED UNDER IT
	// (task_mirror_manners.go). The mark is [mergeConflicted] because that is what
	// this is — the same file changed on both sides — and because every one of the
	// five roads that reach here already reads that one word and settles the node
	// needing the person's look with the names in front of them. Nothing is laid,
	// the copy is left whole, and what a person does about two versions of their
	// own file is theirs to decide, exactly as it is on a repository ground.
	if changed := groundChanged(t.dir, t.ground, wrote); len(changed) > 0 {
		return mergeConflicted, groundChangedSentence(t.dir, t.ground, changed), refusedByTheWork
	}
	// A LAY THAT COULD NOT HAPPEN IS NOT A LANDING EITHER, and it says so with
	// the mark every road refuses ([cameHome]): the ledger goes into the folder
	// whole or not at all (task_lay.go), and the copy it came from is untouched,
	// so everything the family made is still in the directory this names.
	if problem := layWork(t.dir, t.ground, wrote); problem != "" {
		// A FOLDER THAT WOULD NOT TAKE THE LAY IS THE FOLDER REFUSING, and it will
		// refuse the same way next time — a file where a directory has to go, a
		// mount that will not be written. It is typed here, where the lay was
		// refused, rather than read back out of the sentence.
		return mergeAborted, unlaidSentence(t.dir, t.ground, problem), refusedByTheTree
	}
	return mergeInPlace, "", refusedNothing
}

// conflictSentence is what a person reads when a branch would not merge: which
// files were changed on both sides, in their own words and by name.
//
// It NAMES THE FILES rather than quoting git, because git's first line on a
// failed merge is usually "Auto-merging x" — the last thing that worked, not the
// thing that did not. The quote is kept for the other shape of failure, the
// merge git refused before it started (local changes it would have to
// overwrite), where git's own sentence is the only account there is.
func conflictSentence(branch string, clashing []string, out string) string {
	line := "its branch " + branch + " did not merge cleanly and was kept: "
	if len(clashing) > 0 {
		return line + namedFew(clashing, conflictNamesShown) + " changed on both sides"
	}
	// THE ONE SHAPE WHERE AN EMPTY INDEX IS THE TRUTH. A merge git refused before
	// it started never touched the index, so there is nothing to read there and
	// the file list is in the BODY of git's message rather than on its first line
	// (groundcarry.go's [overwrittenPaths]). Quoting the first line alone is what
	// ended a person's whole report in a bare colon naming no files at all.
	if blocked, _ := overwrittenPaths(out); len(blocked) > 0 {
		return line + namedFew(blocked, conflictNamesShown) + " already hold work of your own"
	}
	if said := firstLine(out); said != "" && !strings.HasSuffix(strings.TrimSpace(said), ":") {
		return line + said
	}
	// AND NEVER A BARE COLON. git said nothing this can hand a person, and saying
	// so is a whole sentence; trailing off is not.
	return line + "git would not say which files it was about"
}

// unreachedSentence is what a person reads when a node worked in a copy of their
// repository and that copy would not give the branch up — a directory removed
// under a running task, a disk that filled. It NAMES THE DIRECTORY, because
// unlike a conflict there is nothing in their own repository to look at yet and
// the work is all in that one place.
func unreachedSentence(branch, dir, out string) string {
	return "its work is on " + branch + " in " + dir + " and was kept: " + firstLine(out)
}

// conflictNamesShown and leftBehindNamesShown are how many paths a sentence
// carries before it stops naming them. A conflict is usually one or two files
// and a person wants every one; a working copy full of somebody's virtualenv is
// three thousand, and a report that listed them would be the noise this whole
// file exists to keep off their branch.
const (
	conflictNamesShown   = 8
	leftBehindNamesShown = 5
)

// conflictedPaths are the files a merge could not settle, read off the index
// while the merge is still in progress. An empty answer is a merge that failed
// before it touched the index.
func conflictedPaths(root string) []string {
	out, err := git(root, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil
	}
	return nonEmptyLines(out)
}

// abandonMerge takes the person's checkout back out of a merge, and it exists
// because of one law: A CONFLICT MARKER IS NEVER WRITTEN ONTO THE PERSON'S
// BRANCH. A task that leaves `<<<<<<<` in a file they did not open has handed
// them a broken tree and called it a landing.
//
// `merge --abort` is the whole answer whenever there is a merge to abort, and it
// fails harmlessly when git refused before touching the index — so the second
// move is asked for only when the repository says it is still mid-merge, which
// is the one case where doing nothing would leave the markers behind.
func abandonMerge(root string) {
	if _, err := git(root, "merge", "--abort"); err == nil {
		return
	}
	if _, err := git(root, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err != nil {
		return
	}
	_, _ = git(root, "reset", "--merge")
}

// leftBehind is what is sitting in the node's working copy that the node did
// not write: a virtualenv a command built, a compiler's output, a cache. It is
// read after [commitTaskWork] has staged and committed the node's own work, so
// everything git still reports is by definition something nothing wrote down.
//
// IGNORED FILES ARE NOT IN IT, exactly as they are not in the commit: a
// repository that has said it does not care about a path has already answered
// this question, and repeating it in the report would be the harness arguing
// with the person's .gitignore.
func leftBehind(dir string) []string {
	// Once a failed task has been unregistered, this ordinary directory may sit
	// beneath the repository and `git status` would report paths from that
	// parent. The snapshot was taken while the worktree still knew its own root.
	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		if paths := rememberedLeftBehind(dir); paths != nil {
			return paths
		}
	}
	out, err := git(dir, "status", "--porcelain", "--untracked-files=all",
		"--", ".", ":(exclude)"+aforgeDroppings)
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range nonEmptyLines(out) {
		if len(line) < 4 {
			continue
		}
		// The porcelain line is two status letters, a space, then the path; a
		// rename carries both names and the one that exists now is the second.
		path := strings.TrimSpace(line[3:])
		if _, renamed, found := strings.Cut(path, " -> "); found {
			path = renamed
		}
		if path = strings.Trim(path, `"`); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

const leftBehindRecord = "left-behind.json"

// rememberLeftBehind keeps the answer in aforge's private task metadata before
// Git forgets the worktree. It writes an empty array too: that distinguishes a
// task known to have no leavings from an older folder with no snapshot.
func rememberLeftBehind(dir string, paths []string) {
	metadata := filepath.Join(dir, aforgeDroppings)
	if err := os.MkdirAll(metadata, 0o755); err != nil {
		return
	}
	if paths == nil {
		paths = []string{}
	}
	contents, err := json.Marshal(paths)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(metadata, leftBehindRecord), contents, 0o600)
}

// rememberedLeftBehind distinguishes no record from a recorded empty answer:
// nil means the folder predates this cleanup law and should use Git's answer.
func rememberedLeftBehind(dir string) []string {
	contents, err := os.ReadFile(filepath.Join(dir, aforgeDroppings, leftBehindRecord))
	if err != nil {
		return nil
	}
	var paths []string
	if err := json.Unmarshal(contents, &paths); err != nil {
		return nil
	}
	return paths
}

// leftBehindSentence is the one line a person gets about those files, and WHERE
// THEY ARE NOW is the half of it that has to be true.
//
// A merged node's working copy is removed the moment its work is in, taking an
// installed environment and a directory of build output with it — which is what
// a throwaway checkout is for, and which a sentence saying the files are "still
// there" would send somebody looking for. A kept one is still on disk, and there
// the same files really are waiting.
func leftBehindSentence(paths []string, kept bool) string {
	if len(paths) == 0 {
		return ""
	}
	line := "it left files it did not write, and they went with its working copy rather than onto your branch: "
	if kept {
		line = "it left files it did not write, and they are still in its task folder rather than on its branch: "
	}
	return line + namedFew(paths, leftBehindNamesShown)
}

// namedFew lists paths the way a sentence does — the first few by name and the
// rest as a count, because a person reading a card wants to recognise the thing
// rather than audit it.
func namedFew(paths []string, most int) string {
	if len(paths) <= most {
		return strings.Join(paths, ", ")
	}
	return strings.Join(paths[:most], ", ") + fmt.Sprintf(" and %d more", len(paths)-most)
}

// nonEmptyLines is the shape every plumbing answer in this file comes back in:
// one path per line, blanks dropped.
func nonEmptyLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// commitTaskWork puts what the node wrote into one commit on its own branch.
// Without it there would be nothing to merge: a node's work is files on disk,
// and git only moves what has been committed.
//
// The identity is passed per-command rather than configured, so a machine with
// no git identity still commits and the person's own config is not touched.
// "nothing to commit" is not a failure — a node that only read is a node with
// an empty branch, and an empty branch merges cleanly.
//
// It ANSWERS WITH THE FILES IT COMMITTED, read off the index it just built
// rather than off the list it was handed, because the two can differ honestly:
// a path the node wrote and then deleted, a path .gitignore refuses, a path the
// node saved outside its own worktree. A node whose branch never comes home is
// told about its work out of this list.
//
// AND IT ANSWERS WHAT WENT WRONG, in git's own words, because the caller cannot
// see it any other way and the caller is about to merge. Its silent endings all
// looked like the empty branch a node that only read leaves: the tree could not
// be staged into, the index could not be read, or git refused the commit. A
// landing read them as nothing to do, merged a branch holding nothing and
// removed the working copy the work was sitting in (task_land_unsaved.go, #255).
func commitTaskWork(dir, title string, wrote []string) ([]string, string, landingRefusal) {
	saved, _, why, err := commitTaskWorkAs(dir, "task: "+clip(firstLine(title), 72), wrote)
	if err != nil {
		return nil, firstLine(err.Error()), why
	}
	return saved, "", refusedNothing
}

// commitTaskWorkAs is [commitTaskWork] with the sentence the commit carries
// handed in, and it exists so that ONE COMMIT ROAD STAYS ONE ROAD.
//
// A landing is not the only moment the harness commits a node's ledger: a parent
// that is about to hand its work out checkpoints the ledger onto the family's
// own branch first, so that the parts wake up standing in it
// (task_divide_wip.go). What differs between the two is one string. A second
// body spelling `git commit` for the sake of that string is two roads that must
// stay in step — the same identity, the same `--no-verify`, the same reading of
// what was actually staged — and the day either moved, only one of them would.
//
// IT ANSWERS THREE THINGS AND THE COMMIT IS THE ONE THAT MATTERS. The paths are
// what was staged, read off the index. THE COMMIT IS THE SHA IT WROTE, empty
// when there was nothing to write, and the error is a `git commit` that would
// not run — a hook, a read-only object store, a repository somebody broke.
//
// THOSE TWO USED TO BE THROWN AWAY, and that was a fault with teeth: the paths
// came back looking exactly like a commit that had happened, so a caller could
// merge a branch that had nothing on it, remove the only working copy holding
// the edits, or — at a division — pin a world believing it held work that was
// still on the floor. A caller that cannot act on the answer may still discard
// it; a caller that can is now able to.
func commitTaskWorkAs(dir, message string, wrote []string) ([]string, string, landingRefusal, error) {
	if problem, why := stageTaskWork(dir, wrote); problem != "" {
		return nil, "", why, errors.New(problem)
	}
	saved, problem := stagedPaths(dir)
	if problem != "" {
		return nil, "", refusalFromGit(problem), errors.New(problem)
	}
	if len(saved) == 0 {
		// Nothing the node wrote is different from HEAD, which is the ordinary
		// answer for a node that only read and for a ledger already committed by a
		// round before this one. IT IS NOT A FAILURE, there is no commit, and there
		// is nothing to refuse: the landing goes on and merges a branch that holds
		// what it always held.
		return nil, "", refusedNothing, nil
	}
	if out, err := git(dir,
		"-c", "user.name=aforge", "-c", "user.email=aforge@localhost",
		"commit", "--no-verify", "-m", message); err != nil {
		// A COMMIT THAT WOULD NOT GO IS USUALLY ABOUT THE COMMIT — a signature it
		// could not make, a rule the repository holds — and those are refusals a
		// second answer can get past, so the reading defaults to the work and only
		// the words that name the PLACE say otherwise ([refusalFromGit]).
		return saved, "", refusalFromGit(firstLine(out)), fmt.Errorf("git commit: %s", firstLine(out))
	}
	head, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return saved, "", refusalFromGit(firstLine(head)), fmt.Errorf("git rev-parse: %s", firstLine(head))
	}
	return saved, strings.TrimSpace(head), refusedNothing, nil
}

// unheldLedgerPaths is every path the node's ledger names that this tree does
// NOT hold in its history: still untracked, or tracked and changed since the
// last commit.
//
// IT IS THE QUESTION A CHECKPOINT HAS TO ASK ABOUT ITSELF. [stageTaskWork]
// answers what git said when the index would not take the work, and a path the
// repository IGNORES is deliberately not that (task_land_unsaved.go's
// [unstagedWork]) — which is right for a landing, where a name no commit was ever
// going to hold must not cost the node everything else it wrote, and not enough
// for a family's checkpoint, where a path left behind is a part starting without
// a file its brief tells it to open.
//
// WHAT IT DOES NOT COUNT IS WHAT `.gitignore` COVERS. `git status` says nothing
// about an ignored file, which is exactly the reading wanted here: a node that
// wrote something its project is configured not to keep has not lost anything by
// this commit not holding it, because no commit anywhere was ever going to.
func unheldLedgerPaths(dir string, wrote []string) []string {
	paths := stageableWork(dir, wrote)
	if len(paths) == 0 {
		return nil
	}
	out, err := git(dir, append([]string{"status", "--porcelain", "--untracked-files=all", "--"}, paths...)...)
	if err != nil {
		return nil
	}
	var unheld []string
	for _, line := range nonEmptyLines(out) {
		if len(line) > 3 {
			unheld = append(unheld, strings.TrimSpace(line[3:]))
		}
	}
	return unheld
}

// stagedPaths is what the index holds that HEAD does not: the node's whole
// change, by name, repo-relative and already slash-separated by git.
//
// IT SAYS WHEN IT COULD NOT READ THE INDEX AT ALL, because the empty answer is
// otherwise the same one a node that only read gives — and its caller merges and
// then removes the only other copy of the work on the strength of it.
func stagedPaths(dir string) ([]string, string) {
	out, err := git(dir, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, firstLine(out)
	}
	return nonEmptyLines(out), ""
}

// stagedDiffStat is the node's change AS A SHAPE: one line per file with how
// much of it moved, read off the same index [stagedPaths] reads.
//
// It is the `--stat` and never the diff itself. The whole diff is already in the
// working copy the reader is standing in — a repair round can open any of it
// with `git diff --cached` and nothing here should pay to copy it into a prompt
// — and what a reader cannot get in one glance is the SHAPE: which files, how
// big, and therefore where the ninety percent that already works lives
// (task_audit.go's repairInstruction is the one caller). An empty answer is what
// a workspace that is not a repository gives, and it renders as nothing.
func stagedDiffStat(dir string) string {
	out, err := git(dir, "diff", "--cached", "--stat")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// stageTaskWork puts THE PATHS THE NODE'S OWN HANDS WROTE into the worktree's
// index, and it is the step BOTH the audit and the commit need.
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
// IT IS NOT `git add -A`, AND THAT IS THE WHOLE POINT OF IT. A node works in a
// directory it is free to make a mess in: it runs the test suite, it installs
// what the suite needs, it builds. `add -A` called every one of those droppings
// the deliverable — one measured task landed twenty-six hunks of vendored
// virtualenv and not one line of the change it was asked for, and another
// committed three thousand files of a `.venv_test`. A file a COMMAND made is
// not what the node wrote; a file the node's own `write` or `edit` made is
// (see [savingTools], and [declaredFiles] for the deliverable a command
// generated and the node then named).
//
// wrote is worktree-relative and comes from the run's own record of its saving
// calls ([runTaskChild]'s changed, the same list the auditor is shown and the
// same list the card names) — ONE source of truth, never a second walk of the
// directory that could disagree with it.
//
// The batch is one call because the ordinary node writes a handful of files. It
// falls back to one call per path ([unstagedWork]) because a single path git
// refuses — one that .gitignore covers, one the node deleted from outside its
// own worktree — fails the whole batch, and one unstageable name must not cost
// the node everything else it wrote. IGNORED PATHS STAY IGNORED either way,
// exactly as they did under `add -A`: git refuses them and the retry moves on.
//
// The harness's own droppings are not the node's work either: a background job
// the node started wrote its log under the workspace (jobs.go), and a build log
// in the diff — or merged into the person's branch — is noise they did not ask
// for.
//
// IT ANSWERS WHAT GIT SAID when the index would not take the work, and empty
// when there is nothing to report. A directory that is not a worktree, an add
// nothing survived and a refused reset were all silent, and a landing that
// cannot see them merges an empty branch over the work (task_land_unsaved.go).
func stageTaskWork(dir string, wrote []string) (string, landingRefusal) {
	if out, err := git(dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		// AND THIS IS THE SEAM THAT KNOWS THE PLACE IS NOT A REPOSITORY. It is a
		// question asked and answered here, so the refusal it produces is typed
		// where it happens rather than read back out of anybody's prose
		// (task_land_unsaved.go's [landingRefusal]).
		return firstLine(out), refusedByTheTree
	}
	paths := stageableWork(dir, wrote)
	if len(paths) == 0 {
		return "", refusedNothing
	}
	problem := ""
	if out, err := git(dir, append([]string{"add", "--all", "--"}, paths...)...); err != nil {
		if unstagedWork(dir, paths) {
			problem = firstLine(out)
		}
	}
	if out, err := git(dir, "reset", "--quiet", "--", aforgeDroppings); err != nil && problem == "" {
		problem = firstLine(out)
	}
	if problem == "" {
		return "", refusedNothing
	}
	return problem, refusalFromGit(problem)
}

// stageableWork turns the run's record of what it wrote into pathspecs git can
// be handed: inside the worktree, deduped, and never the harness's own corner.
//
// EVERY PATH IS LITERAL. A file a node wrote called `report[1].md` is a glob to
// git's pathspec parser and a filename to everybody else, and the `:(literal)`
// prefix is how a name gets to mean itself.
//
// A path OUTSIDE the worktree is dropped rather than reached for. A node that
// saved something into the person's home has not made it part of this branch,
// and `git add ../..` is either an error or a much worse kind of success.
func stageableWork(dir string, wrote []string) []string {
	seen := make(map[string]bool, len(wrote))
	paths := make([]string, 0, len(wrote))
	for _, raw := range wrote {
		clean := strings.TrimSpace(raw)
		if clean == "" {
			continue
		}
		if filepath.IsAbs(filepath.FromSlash(clean)) {
			relative, err := filepath.Rel(dir, filepath.FromSlash(clean))
			if err != nil {
				continue
			}
			clean = relative
		}
		clean = filepath.ToSlash(filepath.Clean(filepath.FromSlash(clean)))
		switch {
		case clean == "" || clean == "." || clean == "..", strings.HasPrefix(clean, "../"),
			clean == aforgeDroppings, strings.HasPrefix(clean, aforgeDroppings+"/"),
			seen[clean]:
			continue
		}
		seen[clean] = true
		paths = append(paths, literalPathspec+clean)
	}
	return paths
}

// repositoryRoot is the canonical top of the repository a directory sits in.
// Git may report a resolved path even when its caller arrived through a
// symlink; canonicalizing here makes every later path and repository lock use
// that same spelling.
func repositoryRoot(dir string) (string, bool) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(out)
	if root == "" {
		return "", false
	}
	return canonicalPath(root), true
}

// git runs one command in a directory and returns its combined output. There is
// no context: every call here is a local plumbing command that either answers
// immediately or is a broken repository, and a node's own deadline already
// bounds the run it belongs to.
func git(dir string, args ...string) (string, error) {
	return gitWith(dir, nil, args...)
}

// gitWith is [git] with something extra in its environment, and it exists for
// exactly one caller: the ground ladder stages a parent's tree into AN INDEX OF
// ITS OWN so that handing out a child never moves the parent's index
// ([sealGroundWork]). GIT_INDEX_FILE is the only way to say that to git, and a
// second copy of the pager and editor settings beside it would be the drift the
// one-source-of-truth law forbids.
func gitWith(dir string, environment []string, args ...string) (string, error) {
	// A COMMAND WITH NO DIRECTORY RUNS WHEREVER THE PROCESS HAPPENS TO BE.
	//
	// exec.Cmd reads an empty Dir as "the calling process's working directory",
	// so a caller whose ground was never made — a tree with no working copy, a
	// node landing before it stood anywhere — does not fail here: it commits
	// into whatever repository the person's shell is sitting in. That is not a
	// theory. One `go test ./internal/session/` run wrote "task: Rewrite",
	// "task: Measure" and "task: Paint" onto the branch of the checkout it was
	// launched from, over work somebody else was doing, and they reached the
	// remote before a rebase surfaced them.
	//
	// Every call site here names a directory, so one that ever answers "" is a
	// defect, and a defect is an error rather than a commit in somebody else's
	// tree. The witness is hermetic_checkout_test.go, which fails the run if
	// this package's own checkout moved while the suite was running.
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("git: no directory to run in")
	}
	command := exec.Command("git", args...)
	command.Dir = dir
	// A pager or an editor in the middle of a merge would hang a node forever on
	// a terminal it does not have.
	command.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	command.Env = append(command.Env, environment...)
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

// ── ONE MOVE, WHEN THE PROVIDER RATHER THAN THE WORK FAILED ─────────────────

// terminalProviderFailure reports whether the error a worker ended on is the
// PROVIDER having failed this node, rather than anything about the work.
//
// The distinction is what keeps this from becoming a second audit. A tool that
// failed never reaches here at all — a failed call is a result the worker reads
// and goes on from, and only a turn that could not be completed ends a run. Work
// that is merely incomplete does not reach here either: that is a finished
// worker with a thin claim, and whether it holds is task_audit.go's question and
// nobody else's. What is left is the shape this answers — a refusal, an account
// limit, an exhausted set of retries against a provider that would not serve
// this model — and none of those says one word about the brief.
//
// TWO ARE DELIBERATELY EXCLUDED:
//
//   - A CUT THE TURN ALREADY ANSWERED. A stream cut over and over has already
//     walked the fallback chain inside the turn that died (loop.go), so the
//     first model this would move to is the model that just failed there. A
//     whole second worker to re-learn that is the most expensive way to find out
//     nothing.
//   - A CONTEXT OVERFLOW, which is a fact about the transcript. The answer to it
//     is a shorter conversation, which the turn loop already tries; another
//     model with another window is a guess dressed as a rescue.
func terminalProviderFailure(err error) bool {
	if err == nil {
		return false
	}
	if _, isCut := provider.CutFrom(err); isCut {
		return false
	}
	if isContextOverflow(err.Error()) {
		return false
	}
	var refusal *provider.RefusalError
	if errors.As(err, &refusal) {
		return true
	}
	var api *provider.APIError
	return errors.As(err, &api)
}

// nextNodeModel is where a node goes when the model it is on cannot answer: the
// ADAPTER'S OWN CHAIN, the same one a conversation's turn hops along and the
// same one an endpoint refusal walks (internal/provider's endpoints.go).
//
// One spelling of "the next model" for the whole binary. A second list here —
// the worker tier, a catalog guess of this file's own — would be a second answer
// to a question already answered, and the first thing to drift.
//
// Empty is A MOVE THAT IS ABSENT rather than one that fails: a build with no
// chain, or `--one-model`, and the node fails on the error it always failed on.
func (a *Agent) nextNodeModel(node *TaskNode) (string, bool) {
	chain, ok := a.client.(modelChain)
	if !ok {
		return "", false
	}
	options := chain.FallbackModels(node.runModel())
	if len(options) == 0 {
		return "", false
	}
	return options[0], true
}

// mergePaths adds what a second worker wrote to what the first one did, in
// order and without repeats. The working copy is the same one, so a file the
// first run saved is still the node's leavings whether or not the second run
// touched it again.
func mergePaths(kept, added []string) []string {
	if len(kept) == 0 {
		return added
	}
	seen := make(map[string]bool, len(kept))
	for _, path := range kept {
		seen[path] = true
	}
	for _, path := range added {
		if seen[path] {
			continue
		}
		seen[path] = true
		kept = append(kept, path)
	}
	return kept
}
