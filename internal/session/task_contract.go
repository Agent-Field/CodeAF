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
	// Deadline is when silence becomes approval — now plus the configured
	// countdown (task.autoapprove_seconds). A zero Deadline means the clock
	// is off and only an answer resolves the proposal.
	Deadline time.Time

	// ── update fields (EventTaskUpdate) ─────────────────────────────────

	// State is queued, running, done or failed.
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
}

// TaskAnswer is the surface's reply to a proposal. Approved with an empty
// Redirect starts the node as briefed; a non-empty Redirect APPENDS the
// person's words to the brief as a correction and starts it; !Approved is a
// denial, and the model reads the (optional) reason as its grooming feedback.
type TaskAnswer struct {
	Approved bool
	Redirect string
}
