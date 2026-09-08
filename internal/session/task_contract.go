package session

import (
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

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

// TaskKindHarness is a subharness being designed (harness_task.go): no
// worktree, no branch, no files, and a page that reaches the registry only if
// the person approves the card at the end of it.
const TaskKindHarness TaskKind = "harness"

// TaskKindSubharness is a subharness RUNNING (subharness_run.go): a saved
// program taking one piece of typed work, with a room, a journal of its host
// calls, and a ✕ — and, like a design, no worktree, no branch and no merge.
//
// IT IS THE ASYMMETRY THIS WAVE CAME TO FIX. Designing a harness has been a task
// since harness_task.go landed; RUNNING one blocked the conversation as a turn
// (harness.go), which made the one thing a person most wants to walk away from
// the one thing they could not. A run is a node now, and everything a node has
// — the row, the room, the id, the stop — it has for free.
const TaskKindSubharness TaskKind = "subharness"

// TaskKindJob is a piece of BACKGROUND WORK this session started that is not an
// agent at all (jobrow.go): a command running under `bash background:true`, a
// foreground command that reached its bound and was promoted (promote.go), a
// watch, a video render. It has a log file and an exit code, and no worktree, no
// branch, no room and no report anybody wrote.
//
// IT IS ON THE ROSTER BECAUSE OF WHERE WORK SHOWS, NOT BECAUSE IT IS A TASK.
// The law is that work this conversation started shows on the right, whatever
// door started it — and a background job was, until this kind existed, the one
// kind of work with no row anywhere: the registry knew about it, the `jobs` tool
// could list it, and the column beside the conversation stayed empty while a
// nine-minute sweep ran. A person watching that column had no way to tell a
// session that was working from one that had quietly stopped.
//
// WHAT THAT COSTS, said plainly, is the same price an adaptive run's rows pay
// (orchestrate.go's family seam): the id on the row names nothing in the task
// graph, so a surface that offers to open a node's room or stop it by id will
// find no node there. A job is read with the `jobs` tool and stopped with
// `jobs kill`, and its row is a row.
const TaskKindJob TaskKind = "job"

// TaskKindAdaptive is a run that PLANS ITSELF as it goes (orchestrate.go): a
// family whose root is a goal and whose children crystallize while the run is
// turning, so the denominator a row could quote is "planned so far" and not a
// number anybody fixed at the start.
//
// IT IS NOT ONE OF [taskSpec.kind]'S ANSWERS, and it cannot be: an adaptive run
// has no [TaskNode] and no spec at all — its three index rows are written
// straight from the family seam ([orchestrateFamily]). It is a TaskKind rather
// than a fourth vocabulary because the one question every surface asks of a
// landed row is "what sort of work was this", and an answer that lived in two
// enums would be two answers.
const TaskKindAdaptive TaskKind = "adaptive"

// TaskKindWord is what a person reads where the code holds a [TaskKind], and it
// is the ONE place that translation is made — [taskStateWord]'s law applied to
// the other half of a row.
//
// Ordinary work has NO WORD, and that is the emptiness law rather than an
// oversight: "plain" on a row would be a machine telling somebody that the task
// they asked for was a task. The word only ever earns its place by naming a
// kind of work that behaves differently — a run that plans itself, a saved
// shape being run or being made, a command running in the background.
//
// The vocabulary is the surface's own, taken from where a person already meets
// each thing: `adaptive` is what `/task adaptive` and the manual's
// adaptive-runs.md call it, and `saved shape` is what saved-shapes-of-work.md
// calls a subharness. No machinery word — "subharness", "harness", "node" —
// reaches a screen through here.
func TaskKindWord(kind TaskKind) string {
	switch kind {
	case TaskKindAdaptive:
		return "adaptive"
	case TaskKindSubharness:
		return "saved shape"
	case TaskKindHarness:
		return "making a saved shape"
	case TaskKindJob:
		return "background job"
	}
	return ""
}

// TaskMode is HOW a task stands on its ground — the one word that says whether
// the work is isolated from the place it is about, and how it comes back to it.
//
// IT IS DERIVED FROM THE DELIVERABLE AND NEVER ASKED. Nobody is made to answer a
// question about worktrees to get a piece of work started: the ground is
// resolved from what the conversation already holds (taskstands.go's ladder) and
// the mode falls out of what the work has to leave behind — writing in a
// repository is a branch, reading one is not, and a folder that has no history
// to branch from is copied or worked in directly.
//
// IT IS A RECORD, NOT A SCREEN WORD. A surface says "in ~/x · branch off main",
// which is these facts spelled as a sentence; none of these five strings is
// meant to be drawn as it stands.
type TaskMode string

const (
	// TaskModeWorktree is a branch cut FROM THE GROUND off its HEAD, worked in a
	// directory of the task's own and merged back when the destination is an
	// ordinary branch. A protected, moved or detached checkout leaves the task
	// branch kept instead. It is what ordinary work in a repository gets, and
	// the person's uncommitted changes are not carried into it — the branch comes
	// off HEAD and nothing else.
	TaskModeWorktree TaskMode = "worktree"
	// TaskModeReference is work that only READS its ground: the task gets a
	// folder of its own to write in and the ground stays read-only for it.
	TaskModeReference TaskMode = "reference"
	// TaskModeMirror is a plain folder — no history to branch from — copied into
	// the task's own directory, worked in there, and landed back by name.
	TaskModeMirror TaskMode = "mirror"
	// TaskModeInPlace is the person saying "here" about a referred place: the
	// work happens in the ground itself, with nothing isolating it and the turn's
	// file ledger (recovery.go) as the only undo there is. A model cannot choose
	// this mode inside a repository; its `where` is redirected to a branch.
	TaskModeInPlace TaskMode = "in place"
	// TaskModeFolder is the honest nothing — the conversation's own folder, with
	// no repository anywhere under it. The work happens there because there is
	// nowhere else it could be about.
	TaskModeFolder TaskMode = "folder"
)

// TaskEnding is WHY a node that settled `failed` stopped where it did — the one
// word under the state that tells a person whether to look for a fault, wait,
// or steer. A `failed` node carries exactly one, or none; the report's first
// line says the same thing in a sentence.
//
// THEY ARE THREE KINDS OF NEWS, and a surface draws them as three. A person
// stopped it: nothing is wrong. The connection, a threshold, a loop, another
// task's working copy or a rule the worker would not follow ended it: nothing is
// known to be wrong, and the work can go on from its branch. The check did not
// accept it, or it broke: something is wrong, and the report says what.
type TaskEnding string

const (
	// TaskEndingStopped says a person ended it ([Agent.Cancel]).
	TaskEndingStopped TaskEnding = "stopped"
	// TaskEndingWire says the run ended on the connection to the model rather
	// than on the work — a stream that reset, a socket that closed — after the
	// retries and the second worker (task_run.go) were spent too.
	TaskEndingWire TaskEnding = "wire"
	// TaskEndingCircling says the worker's own loop guard ended its turn
	// (looped.go's loopLeftUndoneNote): it kept making the same calls.
	TaskEndingCircling TaskEnding = "circling"
	// TaskEndingBlocked is [TaskEndingCircling] with a known cause: the calls it
	// kept making were writes into a working copy another task holds, and every
	// one was refused (treehold.go).
	TaskEndingBlocked TaskEnding = "blocked"
	// TaskEndingSteps says a step, progress or time threshold ended the run and
	// the work did not hold when it was checked ([Agent.landStopped]).
	TaskEndingSteps TaskEnding = "steps"
	// TaskEndingRefused says the run finished and the check did not accept what
	// it made — gaps were named, or a person refuted it.
	TaskEndingRefused TaskEnding = "refused"
	// TaskEndingStale says the work never started: what its brief assumes about
	// its world did not hold when the world was made, and the report names every
	// assumption that failed (handoffcontract.go). It is its own ending rather
	// than an error because nothing broke — a brief and a folder disagreed — and
	// because it is the one ending a person can fix by re-grounding,
	// re-dividing or re-briefing rather than by reading a stack of steps.
	TaskEndingStale TaskEnding = "stale"
	// TaskEndingNotes says the worker was stopped by the write-your-notes rule:
	// it was asked twice to write down what it was doing, its tool calls were
	// held until it did, and it sent three more replies with nothing visible in
	// them (processrule.go). It is its own ending rather than [TaskEndingCircling]
	// because the worker was not repeating itself — it was working in silence,
	// and what it worked out never reached the record its room, its check and its
	// parent all read. The work it did do is on its branch like any other halted
	// node's.
	TaskEndingNotes TaskEnding = "notes"
	// TaskEndingUpstream says the provider refused the request or would not
	// serve it — an API error, a refusal, a model that is not there. Like
	// [TaskEndingWire] it is a fact about WHO WAS ASKED and never about the work:
	// nothing was found out about the job, so a node that ended this way is not
	// evidence that anything is left to do. It is told apart from
	// [TaskEndingError] by [terminalProviderFailure], which reads the error's own
	// type, and it exists because the two used to be one word — a sibling that
	// died on an API 404 read as a gap in the ask and held an unattended run open
	// over a tree that was finished (#513).
	TaskEndingUpstream TaskEnding = "upstream"
	// TaskEndingError is everything else: a working copy that could not be
	// made, a worker that would not start, an error nobody classified.
	TaskEndingError TaskEnding = "error"
)

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

// TaskResolutions are the three answers in the order every place that offers
// them spells them.
//
// ONE SOURCE OF TRUTH, because this list is written down in three places a
// model reads and one of them used to be a hand-typed copy: the `tasks` tool's
// schema enum, that tool's description, and the landing note that tells the
// model what to type back (task_run.go's [taskNote]). A note offering a verb
// the schema rejects is the harness teaching a model a word it cannot use, and
// that is the same defect `propose_task`'s step default had when its schema
// said 40 and its executor applied 200.
var TaskResolutions = []TaskResolution{TaskAccept, TaskReaudit, TaskRefute}

// TaskResolveVerbs is that list as it reads inside a sentence —
// `accept|reaudit|refute` — which is how the landing note offers it.
func TaskResolveVerbs() string {
	words := make([]string, 0, len(TaskResolutions))
	for _, one := range TaskResolutions {
		words = append(words, string(one))
	}
	return strings.Join(words, "|")
}

// TaskResolveEnum is the same list as the JSON array a tool schema takes.
func TaskResolveEnum() string {
	words := make([]string, 0, len(TaskResolutions))
	for _, one := range TaskResolutions {
		words = append(words, strconv.Quote(string(one)))
	}
	return "[" + strings.Join(words, ",") + "]"
}

// TaskSettle is the person's standing answer to "who decides a task nobody
// could check" — the `task.settle` row (internal/config's settings.go).
//
// IT IS A POLICY AND NOT A CAPABILITY. Both values leave the model the same
// `tasks … resolve` verb and leave the person the same choices on the landed
// card; what changes is who is ASKED first, which is the whole of what a person
// is annoyed about when a third task in an afternoon lands waiting on them.
type TaskSettle string

const (
	// TaskSettleAsk puts the decision in front of the person: the landing note
	// tells the model what happened and what the choices are, and the model does
	// not spend a decision on their behalf. It is the default.
	TaskSettleAsk TaskSettle = "ask"
	// TaskSettleAuto hands the decision to the model: the landing note tells it
	// to read the report and the work and settle the node itself, and to come
	// back to the person only when it genuinely cannot tell.
	TaskSettleAuto TaskSettle = "auto"
)

// settleOrAsk reads a configured word as one of the two, defaulting to asking:
// a row this build does not recognise must not silently start deciding for
// somebody.
func settleOrAsk(word string) TaskSettle {
	if TaskSettle(strings.TrimSpace(word)) == TaskSettleAuto {
		return TaskSettleAuto
	}
	return TaskSettleAsk
}

// TaskNotice is the flat payload of EventTaskProposal and EventTaskUpdate —
// one struct for both, the way Event itself is one struct: a proposal fills
// the top half, an update the bottom, and no surface reads a field its kind
// did not set.
type TaskNotice struct {
	// ID is the proposal's token: a surface hands it back to
	// [Agent.ResolveTask]. On updates it names the node the update is about.
	ID uint64
	// Run names the adaptive run this row belongs to. Empty means ordinary work.
	Run string
	// Node names the adaptive node inside Run. THE RUN'S OWN ROW HAS NO NODE.
	Node string
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
	// Where says where the task will work. A proposal carries the resolved task
	// folder or explicit path; later notices carry the worker's actual directory.
	Where string
	// Ground is the repository or folder THE WORK IS ABOUT, absolute, and Mode is
	// how the task stands on it (taskstands.go resolves both). Where says which
	// directory the worker types in; these two say which project that directory is
	// a copy of, which is the fact a person needs to read before they approve
	// anything — a card that named only a task folder under a session was telling
	// somebody where the machinery was, never where their work was going.
	//
	// They are on the PROPOSAL and on every update, because the answer is settled
	// before the countdown starts and never moves afterwards. Empty Ground is the
	// emptiness law and not a claim: a notice written by a door that never
	// resolved one has nothing to say about it.
	Ground string
	Mode   TaskMode
	// Rung is WHICH COPY OF THE GROUND the work is being done in — the rung of
	// the ground ladder that made this node's world (groundladder.go). Where and
	// Ground say which directory and which project; this says what that directory
	// IS, which is the fact a surface needs before it can name the place in a
	// person's words ([GroundWord] holds the table).
	//
	// IT IS EMPTY UNTIL THERE IS A WORLD TO DESCRIBE. A proposal has not been
	// carved yet and a node given a folder of its own on the reference promise
	// climbed no rung at all, so both leave it unset — the emptiness law, and not
	// a claim that the work happened nowhere.
	Rung GroundRung
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
	// countdown (task.autoapprove_seconds). A zero Deadline means the clock is
	// off or has been held by typing, and only an answer resolves the proposal.
	Deadline time.Time

	// ModelOptions is the shortlist a `model` argument raised that fits more
	// than one model this install has (taskmodel.go). It is empty for every
	// ordinary proposal — one word, one model, nothing to ask — and when it is
	// set, Model is its leading member: the closest match, what the card shows,
	// and what the clock settles on if nobody picks. A surface offers these for
	// the person to confirm and hands the chosen one back on [TaskAnswer].
	ModelOptions []string

	// Elsewhere is ONE LINE about the work other windows on this project already
	// have out in the files this brief names, and "" when there is nothing to say
	// (taskpreflight.go writes it and spells the words).
	//
	// IT IS A FACT ON THE CARD, NOT A GATE. The countdown runs the same, the
	// options are the same three, and a person who ignores it gets exactly the
	// task they were shown. Empty is the emptiness law and NOT a claim of
	// clearance: a brief that named no files and a live node that has not written
	// yet both leave it empty, so a surface must draw nothing rather than "you are
	// alone in these files".
	Elsewhere string

	// ── update fields (EventTaskUpdate) ─────────────────────────────────

	// State is queued, running, done, failed or unverified.
	State TaskState
	// Elapsed is the node's age at this update.
	Elapsed time.Duration
	// StartedAt is the record's fact of when this node started. It is the zero
	// time when nothing recorded one, and every surface draws that as nothing.
	StartedAt time.Time
	// EndedAt is the record's fact of when this node landed. It is the zero time
	// when nothing recorded one, and every surface draws that as nothing.
	EndedAt time.Time
	// Report is the done/failed story in two or three lines: what it did, or
	// what stopped it. A dependent node's brief is assembled from these.
	Report string
	// Changed lists the files the node wrote, repo-relative.
	Changed []string
	// Branch is the task's branch ("task/fix-nil-map"), kept after a protected
	// landing, a conflict or a kill so the work is never silently thrown away.
	Branch string
	// Merge is how the branch came home: "merged", "kept" (finished but left
	// on its branch), "conflicted" (branch kept), "inplace" (a non-git
	// workspace ran in the person's tree), or "" while running.
	Merge string
	// Doing is the PHASE a running node of a named kind is in, in that kind's
	// own plain words — "designing", "awaiting your look" for a subharness
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
	// Context is the NAMED WORKING CONTEXT this node's room is — what a person's
	// own words in it are part of, in their own words: "designing a subharness",
	// and then "designing subharness flake-triage" once the page has a name (harness_task.go).
	// It is "" for an ordinary node, whose room is work being watched rather than
	// a thing somebody is inside.
	//
	// IT IS FOR THE PERSON'S OWN LINE AND NOT FOR THE NODE'S ROW. Doing above says
	// what the work is busy with and belongs on a roster; this says what a turn
	// somebody takes in here RUNS INSIDE, and belongs where that turn starts — a
	// transcript that draws the person's `›` line identically whether the words
	// went to a conversation or into a design thread is a transcript that cannot
	// be read back (internal/tui3's turncontext.go).
	//
	// A SURFACE DRAWS IT VERBATIM AND KEEPS NO LIST OF CONTEXTS. The name is the
	// node's own to write, so a kind of node that is also a place somebody talks
	// inside becomes visible everywhere by filling this and nowhere else. Empty
	// draws nothing at all, which is the emptiness law and is what every ordinary
	// turn on every surface gets. Like Doing it is ANNOUNCED ON CHANGE: a context
	// that has just learned its name is news that arrives without the state moving.
	Context string
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
	// Ending is WHY a node that did not finish stopped where it did, in one word
	// a surface can draw a row from ([TaskEnding]). It is set only on a node
	// that settled `failed` and is "" on every other — and on every failed node
	// checkpointed before the field existed, which a surface draws exactly as it
	// always did. It exists because every ending that keeps a branch used to wear
	// the one sentence "stopped — branch kept", and six rows of that on a rail
	// were, when the records were read, a connection that dropped, a worker that
	// gave up going in circles, a check that did not accept the work, and not one
	// person pressing stop.
	Ending TaskEnding
	// Checked is WHAT THE CHECK SAID about this node's work, and "" on a node no
	// check ever read. It is narrower than State on purpose: a node taken as it
	// stands, one landed with the check switched off and one a person accepted
	// are all done and none of them was judged (taskgrade.go's
	// [TaskNode.checkSaid]).
	Checked provider.Verdict
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

// ── the phases of a running node ────────────────────────────────────────────

// The lives a running node has, in the plain words every file here writes and a
// surface draws from.
//
// THEY ARE THE ONE SPELLING. task_beat.go writes these same strings into the
// node's pulse file for other windows to read, and task_run.go sends them on
// [EventTaskPhase] for this one, so a reader outside the process and the card in
// front of the person are never two vocabularies for the same moment.
const (
	// TaskPhaseWorking is the node's own worker, in its worktree.
	TaskPhaseWorking = "working"
	// TaskPhaseChecking is the gate looking at what the worker left.
	TaskPhaseChecking = "checking"
	// TaskPhaseRepairing is a repair round closing named gaps.
	TaskPhaseRepairing = "repairing"
	// TaskPhaseSizing is the reading that decides whether this work is handed
	// out in parts, and how (task_divide.go's [Agent.reviewDivision]).
	//
	// IT IS A LIFE OF THE NODE AND NOT A STEP OF A TOOL CALL, which is why it
	// belongs on this list beside the other three. The reading is a full call to
	// the tier that thinks — measured at thirteen seconds, and bounded at
	// [divideReviewPatience] — and it happens twice in a node's life where the
	// harness submits a drawing on the worker's behalf before its first request
	// (task_divide_sketch.go): a card that has just appeared, with a clock going
	// up and nothing else on it, for as long as the reading lasts.
	TaskPhaseSizing = "sizing"
)

// TaskPhaseNotice is the payload of [EventTaskPhase]: which node moved, which of
// the words above it moved into, and — while a repair round runs — which round
// out of how many.
//
// IT EXISTS BECAUSE A RUNNING NODE IS NOT ONE THING. The state stays `running`
// across a worker, a check and every repair round, so a surface holding only
// [EventTaskUpdate] draws the same row for a node writing code and a node that
// finished writing code eight minutes ago and has been under a check ever since.
// That gap is what makes a person conclude the work hung: the worker's last line
// scrolls past, and then nothing at all is drawn for minutes while the check
// reads the tree and a repair round rewrites it.
//
// IT IS NEWS, NOT A ROW. It carries no state, no elapsed, no cost — an update
// is where those live and this never contradicts one. A surface that ignores
// this kind is exactly the surface it was.
type TaskPhaseNotice struct {
	// ID names the node, and is the same id its [TaskNotice] carries: a surface
	// folds this into the row it already drew rather than opening a second one.
	ID uint64
	// Phase is one of the words above.
	Phase string
	// Round and Rounds are which repair round this is and how many the person's
	// settings allow ("round 1 of 1"). They are set on TaskPhaseRepairing alone
	// and are zero on the others, which is the emptiness law: a surface draws
	// no numbers at all for a check, because a check has no rounds.
	Round  int
	Rounds int
	// Text is the check's finding in ONE line, in a person's words, and it rides
	// the repairing phase because that is the moment it becomes true of the work:
	// the check did not accept it, and here is what it said. It is "" everywhere
	// else, and "" is drawn as nothing.
	//
	// It is the checker's own sentence with a plain-words opener in front of it
	// ([mendingLine], [taskFindingLine]) and never a paraphrase — the machinery's
	// names for what happened are not on this wire.
	Text string
}
