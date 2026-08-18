package session

import "time"

// ── The task contract ───────────────────────────────────────────────────────
//
// This file is the SEAM between the session machinery (task.go — the tool,
// the graph, the executor, the worktree) and any surface that renders a
// proposal. It is deliberately the whole contract: the Event kinds live in
// session.go, and every type a surface needs to answer a proposal or draw a
// node's life is here.
//
// THE SHAPE OF THE THING — A DAG, NOT A DEPTH. The destination is a
// dynamically evolving directed acyclic graph of work: a complex task is
// decomposed into nodes with dependency edges, the executor runs the READY
// FRONTIER (every node whose dependencies are done) in parallel, a finished
// node's report becomes part of its dependents' briefs, and a node may itself
// decompose into a child graph. What ships first is the degenerate case — a
// graph of ONE node — but every mechanic is the graph's own: the proposal is
// a node, the countdown is how a node is admitted, the worktree is how a node
// is isolated, the report is what a node hands its dependents. Nothing here
// is a "depth limit": when decomposition arrives it will be edges into this
// same executor, not a new shape.
//
// The chat model grooms a piece of work and calls propose_task with a
// self-contained brief. The person gets a countdown — redirect it, approve
// it, deny it, or let the clock approve it — and an approved node runs as a
// child agent in its own git worktree, reporting back through the same
// steering lane a background job's exit uses. The node's brief is its whole
// world: it never reads this session.

// TaskKind is WHAT SORT of work a node is, and the empty string is the ordinary
// one this whole file is written about: a piece of work handed to a child agent
// in a worktree of its own.
//
// IT IS NOT A STATE AND IT NEVER CHANGES. A node is admitted as one kind and
// settles as that kind; what moves is [TaskState] underneath it. The reason a
// surface needs it at all is that the two kinds are honestly different objects
// to draw — one has a branch, files it changed and a merge, and the other has
// none of those and could never have them — so a card that promised "the branch
// it wrote on is kept" over a design would be pointing at work that does not
// exist.
type TaskKind string

// TaskKindHarness is a sub-harness being designed (harness_task.go): no
// worktree, no branch, no files, and a page that reaches the registry only if
// the person approves the card at the end of it.
const TaskKindHarness TaskKind = "harness"

// TaskState is where one node is in its life.
type TaskState string

const (
	// TaskQueued says the node is admitted but its dependencies are not all
	// done — it waits on the frontier. A one-node graph never sits here.
	TaskQueued TaskState = "queued"
	// TaskRunning says the child agent is working in its worktree.
	TaskRunning TaskState = "running"
	// TaskDone says the run finished and the report is in.
	TaskDone TaskState = "done"
	// TaskFailed says the run ended without finishing — an error, a kill, or
	// a merge the runner would not guess at. Report says which.
	TaskFailed TaskState = "failed"
	// TaskUnverified says the run finished and NOBODY COULD SAY whether the
	// work holds: the auditor answered with neither verdict word, or the audit
	// call itself never came back (task_audit.go). It is a settled state — the
	// run is over, the slot is handed back, the branch is kept — and it is
	// deliberately NOT TaskFailed.
	//
	// "The work is wrong" and "nobody could tell me whether the work is wrong"
	// are different news with different consequences, and collapsing the second
	// into the first is how a broken auditor fails good work and then fails
	// everything downstream of it. So nothing CASCADES from here: a dependent of
	// an unverified node stays queued rather than failing, because an unverified
	// claim is not evidence and is also not a refutation. What moves it is a
	// person — [Agent.ResolveUnverified], reachable from the `tasks` tool — accepting
	// the work as done, asking for another audit, or refuting it themselves.
	TaskUnverified TaskState = "unverified"
)

// TaskResolution is what a person decides about a node no auditor could judge.
//
// The three are the only three answers there are to "nobody could verify this":
// say it holds, ask again, or say it does not. Each lands the node in one of
// the states above — done, unverified again, failed — through the same settle
// the gate itself uses, so a resolved node is indistinguishable afterwards from
// one that reached that state on its own.
type TaskResolution string

const (
	// TaskAccept takes the work as done on the person's word: the branch comes
	// home exactly as a VERIFIED one would, and the dependents unblock.
	TaskAccept TaskResolution = "accept"
	// TaskReaudit sends a fresh auditor at the same working copy. The node stays
	// unverified until that verdict lands.
	TaskReaudit TaskResolution = "reaudit"
	// TaskRefute is the person doing the auditor's job in the negative: the node
	// fails, its branch is kept, and the cascade takes its dependents.
	TaskRefute TaskResolution = "refute"
)

// TaskNotice is the flat payload of EventTaskProposal and EventTaskUpdate —
// one struct for both, the way Event itself is one struct: a proposal fills
// the top half, an update the bottom, and no surface reads a field its kind
// did not set.
type TaskNotice struct {
	// ID is the proposal's token: a surface hands it back to
	// [Agent.ResolveTask]. On updates it names the node the update is about.
	ID uint64
	// Title is the one-line name of the work ("Fix the nil-map crash").
	Title string
	// Kind is what sort of node this is, and "" is the ordinary one: work in a
	// worktree. It is on the proposal AND on every update, because it is the one
	// fact about a node that is true before it starts and after it lands.
	Kind TaskKind

	// ── proposal fields (EventTaskProposal) ─────────────────────────────

	// Summary is the two-or-three-line gloss the person scans to decide.
	Summary string
	// Brief is the node's WHOLE context: the goal, every fact the chat knew
	// that the work needs, the files, the conventions, the acceptance test —
	// and, once graphs have edges, whatever its prerequisites' reports
	// taught. The node never reads this session; the brief is the contract.
	Brief string
	// Acceptance is the observable done-condition, in the model's own words.
	Acceptance string
	// DependsOn names the nodes that must finish before this one may start —
	// IDs of sibling proposals. Empty in a one-node graph.
	DependsOn []uint64
	// Parent is the work this one was SPAWNED UNDER, and 0 is a root — which is
	// every task a person or the model proposed. It is filled by an adaptive run,
	// whose nodes are registered as a family under one row for the run itself
	// (orchestrate.go's family seam), and it is what a roster draws a tree from.
	//
	// IT IS NOT A DEPENDENCY. DependsOn says what must finish first; this says
	// who asked for the work. A run's two independent nodes share a parent and
	// have no edge between them, and collapsing the two would draw a tree that
	// says the wrong thing about what is waiting for what.
	Parent uint64
	// Deadline is when silence becomes approval — now plus the configured
	// countdown (task.autoapprove_seconds). A zero Deadline means the clock
	// is off and only an answer resolves the proposal.
	Deadline time.Time

	// ModelOptions is the shortlist a `model` argument raised that fits more
	// than one model this install has (taskmodel.go). It is empty for every
	// ordinary proposal — one word, one model, nothing to ask — and when it is
	// set, Model is its leading member: the closest match, what the card shows,
	// and what the clock settles on if nobody picks. A surface offers these for
	// the person to confirm and hands the chosen one back on [TaskAnswer].
	ModelOptions []string

	// ── update fields (EventTaskUpdate) ─────────────────────────────────

	// State is queued, running, done, failed or unverified.
	State TaskState
	// Elapsed is the node's age at this update.
	Elapsed time.Duration
	// Report is the done/failed story in two or three lines: what it did, or
	// what stopped it. A dependent node's brief is assembled from these.
	Report string
	// Changed lists the files the node wrote, repo-relative.
	Changed []string
	// Branch is the worktree's branch ("task/fix-nil-map"), kept after a
	// conflict or a kill so the work is never silently thrown away.
	Branch string
	// Merge is how the branch came home: "merged", "conflicted" (branch
	// kept), "inplace" (a non-git workspace ran in the person's tree), or ""
	// while running.
	Merge string
	// Doing is the PHASE a running node of a named kind is in, in that kind's
	// own plain words — "designing", "awaiting your look" for a sub-harness
	// being written (harness_task.go) — and "" for an ordinary task, which has
	// no phases.
	//
	// A SURFACE DRAWS IT INSTEAD OF THE STATE WORD, which is what separates it
	// from Mending and Waiting below: those two are said BESIDE "running",
	// because the node is running and hiding that would hide the state. This
	// one IS the state, said in the vocabulary of the work rather than of the
	// machinery — "designing" is what a person would call it, and "running" is
	// what this package calls it. Like the two below it is ANNOUNCED ON CHANGE:
	// a phase moving is news that arrives without the state moving.
	Doing string
	// Mending is the gap being closed while a repair round runs, one plain
	// line ("adding amp-labs to the report"), and "" at every other moment.
	// A surface draws it as the task simply still working; the machinery
	// that sent it back is not the surface's to mention.
	Mending string
	// Waiting is why a QUEUED node is not running yet, or why a RUNNING one is
	// paused mid-call, one plain word or two: "" (nothing to say), "slot" (a
	// task.parallel cap holds it), "machine busy" (the admission governor
	// holds it), "rate limited" (the provider is pacing it). A surface draws
	// it as the queue telling the truth; the machinery behind it is not the
	// surface's to name. Like Mending it is ANNOUNCED ON CHANGE: a hold
	// starting and a hold ending are both news that arrives without the state
	// moving, so an update carrying only this is still one a surface folds in.
	Waiting string
	// Stopped says a PERSON ended this node ([Agent.Cancel]) rather than the
	// work ending on its own. It rides beside State rather than replacing it —
	// a stopped node still settles as `failed`, because nothing merged and its
	// dependents still cannot be briefed — and it exists because "failed" and
	// "you stopped it" are different news about the same state: one sends
	// somebody looking for a fault, and the other is the fault.
	//
	// IT IS A FACT OF THIS PROCESS AND NOT OF THE CHECKPOINT. A session resumed
	// from disk knows the node failed and does not know who ended it, so a
	// surface reading this draws the stop while it can and falls back to the
	// failure afterwards.
	Stopped bool
	// Model is the model this node runs on: the one the proposal named, the
	// configured task model, or the conversation's own (taskmodel.go). It is on
	// the proposal AND on every update, because it is a fact about the work that
	// outlives the question — a card that lands twenty minutes later still says
	// whose hands did it.
	Model string
	// CostUSD is what this node's own agent has spent, live while it runs and
	// frozen once it lands. Zero means nobody published a price — an unpriced
	// model, or a node that has not started — and it is NOT the same claim as
	// "it cost nothing", so a surface draws no figure at all for it.
	CostUSD float64
}

// TaskAnswer is the surface's reply to a proposal. Approved with an empty
// Redirect starts the node as briefed; a non-empty Redirect APPENDS the
// person's words to the brief as a correction and starts it; !Approved is a
// denial, and the model reads the (optional) reason as its grooming feedback.
// Model settles a proposal's ModelOptions, and it is read ONLY when the
// proposal carried some: it is the person choosing between models the harness
// itself could not choose between, not a surface renaming the model on work it
// was shown. An empty Model — and one naming anything outside the shortlist —
// leaves the leading option in place, which is what the card was showing.
type TaskAnswer struct {
	Approved bool
	Redirect string
	Model    string
}
