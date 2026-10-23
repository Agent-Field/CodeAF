// Package factory is the vocabulary of the factory floor: what the surface
// draws and what a person may do to it, with NO engine behind it. The engine
// arrives through [Seam]: [LocalSeam] over the person's own store, or
// [FixtureSeam]'s still floor for drawing and tests.
//
// THE TWO LAYERS. A [Recipe] is a fixed, ordered list of stages written once
// per repo or team. A run is that recipe compiled into tasks; the inside of a
// stage is dynamic (split, parallel, fix tasks, a parked question) and nothing
// inside a stage can add a stage, except plan, within the bounds [Adapt] holds
// it to and the one word the recipe gives each kind ([AdaptMode]). Loops are a
// condition on a stage ([Stage.Until] with [Stage.Max]), never an edge. There
// is no graph for a person to draw.
//
// NO MODEL ON A STAGE. A stage runs as an ordinary task with the ordinary crew:
// worker, planner and checker picked for the class of work (internal/crewroute).
// The one word a person may add is effort ([Stage.Effort]); pins and `redo
// stronger` remain the room's own doors, during or after a run.
//
// ONE POOL. Money is the item's cap and the day's rail, never a knob per stage.
package factory

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// Kind is what a floor item is. It is a string and open: an issue, a pull
// request, a red CI run, a chore, a standing order's firing, work a chat split
// off. A new kind needs no edit here.
type Kind string

const (
	KindIssue Kind = "issue"
	KindPR    Kind = "pr"
	KindCI    Kind = "ci"
	KindChore Kind = "chore"
)

// Tier is how far the item's author is trusted. A stranger's words may be
// drafted on, never shipped on, without a person.
type Tier string

const (
	TierOwner    Tier = "owner"
	TierCollab   Tier = "collaborator"
	TierStranger Tier = "stranger"
)

// Origin is where the item came from.
type Origin string

const (
	OriginForge    Origin = "github"
	OriginTerminal Origin = "terminal"
	OriginChat     Origin = "chat"
)

// State is the item's place on the floor. The floor groups by it.
type State string

const (
	StateNew       State = "new"
	StateQueued    State = "queued"
	StateRunning   State = "running"
	StateNeedsYou  State = "needs you"
	StateLanded    State = "landed"
	StateShipped   State = "shipped"
	StateDismissed State = "dismissed"
)

// Gate is the old `ask me at` word (plan, ship, none), READ AND NEVER
// WRITTEN: an approve step ([StageGate], [Approve]) is where a person sits in a
// run now. An item or a recipe written with a gate is read into approve steps
// ([MigrateGates], [ApproveForGate]): plan is an approve after plan, ship an
// approve before the result (before the first post step, else last), none is
// no approve at all.
type Gate string

const (
	GatePlan Gate = "plan"
	GateShip Gate = "ship"
	GateNone Gate = "none"
)

// StageKind is what runs a stage. There are four and there will not be a fifth:
// a conversation, a command, a person, a write to a source.
type StageKind string

const (
	StageChat  StageKind = "chat"  // a conversation with its crew and its nested tasks
	StageCheck StageKind = "check" // a deterministic command; no model
	StageGate  StageKind = "gate"  // a person: yes, no, or words as the stage's result
	StagePost  StageKind = "post"  // a source write, consent-gated
)

// Stage is one sentence the factory runs at its place in the recipe. The
// fields after Ask are the whole of what is structured about it. When is how
// a recipe adapts without anyone touching it: a stage with a condition is
// skipped, dim, with its reason, on an item the condition does not fit.
type Stage struct {
	Name   string    // plan · write · test · review · neaten · security · proof
	Kind   StageKind // chat unless said otherwise
	Ask    string    // the sentence as typed; the brief is compiled from it
	When   string    // "" or always · thin · large · touches auth · has ui
	Effort string    // "" (the knee) · cheap · strong — the crew's one word
	Fanout string    // one · per-file · per-finding · per-claim
	Until  string    // done · clean · green · proven
	Max    int       // rounds before it stops and asks; 0 means one
	// OldGate and OldGateWhen are a stage's gate as a file or a document
	// written before approve steps said it (`gate plan when large`). They are
	// read, turned into approve steps ([ExpandGates]) and never written.
	OldGate     Gate     `json:"Gate,omitempty"`
	OldGateWhen string   `json:"GateWhen,omitempty"`
	Proof       []string // what this stage must show
	On          bool     // an item may switch a banked stage off
	// Why is one line saying why this stage is here, or changed, for this
	// item; "" for the recipe's own stage.
	Why string
	// By is who put the stage here or last changed it: `recipe`, `manager`,
	// `plan` or `you` ([ByRecipe] and its siblings). "" is the recipe.
	By string
	// Fixed says the recipe file binds this stage: its line ends `· fixed`
	// or its section's heading does. NO ONE CHANGES A FIXED STAGE'S ASK,
	// THINKING OR LOOP, OR SWITCHES IT OFF: not the manager, not plan, not
	// the person ([Edit], [FixedRefusal]). It may be switched on. The law
	// changes only by changing the file. Missing reads false.
	Fixed bool `json:",omitempty"`
}

// Recipe is what a product banked: stages in order, per kind of item, and the
// policy every proof must show. Stages is the list for an issue and the
// fallback for a kind ByKind does not name. Habits are the banked sentences
// the recipe file keeps under `## habits` (recipefile.go), which a [Repo]
// carries as its own Habits once loaded.
type Recipe struct {
	DefaultBudget float64
	// Source and Problems keep the pipeline read visible on every surface.
	Source   string
	Problems []string
	Stages   []Stage
	ByKind   map[Kind][]Stage
	Policy   []string
	Habits   []string
	// Adapt is how much the plan stage may change an item's stages, per kind.
	// A kind it does not name is [AdaptFree]; read it through [Recipe.AdaptFor].
	Adapt map[Kind]AdaptMode
	// Merge is the pipeline's `merge:` settings; the zero value is auto-merge
	// off and the forge's own choice of method.
	Merge MergePolicy
}

// For is the stage list a kind runs.
func (r Recipe) For(k Kind) []Stage {
	if s, ok := r.ByKind[k]; ok && len(s) > 0 {
		return s
	}
	return r.Stages
}

// Triage is the cheap read made on arrival (internal/factory/triage): dim
// facts a person reads. Intake uses size, cost and the explicitly judged
// ask-first sentences only when the repository opts into automatic starts.
type Triage struct {
	Type string // bug · feat · chore · question · review
	Size string // S · M · L
	// SizeFrom says where Size came from: [SizeFromRead], [SizeFromDiff],
	// [SizeFromChat], or "" when nobody recorded it. A document written before
	// this field existed decodes to "" and its size is UNKNOWN, not a guess to
	// be trusted: [ShownSize] draws nothing for it.
	SizeFrom  string `json:",omitempty"`
	Area      string
	Readiness int // 0..100; under 55 the item is thin and wants questions
	Est       float64
	DupOf     int
	// Risk is what the work touches that a person would want to know before it
	// runs, in short phrases: `touches money`, `touches auth`, `has ui`,
	// `migration`. Empty is "nothing said", never "safe".
	Risk      []string
	Read      string // one sentence
	Questions []string
	// Dup is the issue or pull request this item may repeat, as the read wrote
	// it (`#950`, `acme/api#12`), and "" when it named none.
	Dup string
	// Priority is the read's guess at what to take first: 1 first to 5 later,
	// and 0 when nobody said.
	Priority int
	// Reason is the priority's why in at most five words, drawn dim at the
	// right of a row while the floor is ordered `first`.
	Reason string
	// TriagedAt is when the read was made, and stays set on a read that could
	// not be understood, so ONE ITEM IS NEVER TRIAGED TWICE. Zero is never.
	TriagedAt time.Time
	// StartJudged distinguishes a complete policy read from an omitted answer.
	StartJudged  bool     `json:",omitempty"`
	StartMatched []string `json:",omitempty"`
	StartPaths   []string `json:",omitempty"`
	StartRules   []string `json:",omitempty"`
}

// Where a [Triage.Size] came from. Only the first two are ever shown; a chat
// model's guess is kept so the read can replace it, and is drawn as nothing.
const (
	SizeFromRead = "read" // the model's completed read of the item
	SizeFromDiff = "diff" // a forge's measured changed-line count
	SizeFromChat = "chat" // the chat model's guess when it added the item
)

// UnmarshalJSON reads a triage document, including one written before Risk
// was a list.
//
// AN OLD DOCUMENT MUST NOT EMPTY THE FLOOR. Risk used to be one level word
// (`low`, `mid`, `high`) and every item on disk carries it, if only as "".
// Decoding that string into a list would fail the whole document, and the
// store skips a document it cannot read, so every row written before this
// field changed shape would vanish. A string is read as no risk said: the old
// level is not a phrase about what the work touches.
func (t *Triage) UnmarshalJSON(data []byte) error {
	type plain Triage
	var raw struct {
		plain
		Risk json.RawMessage
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*t = Triage(raw.plain)
	t.Risk = nil
	if r := bytes.TrimSpace(raw.Risk); len(r) > 0 && r[0] == '[' {
		var list []string
		if err := json.Unmarshal(r, &list); err != nil {
			return err
		}
		t.Risk = list
	}
	return nil
}

// Repo is a connected repository and the product team that owns it. A
// product owns many repos, one recipe and one policy; a repo may carry
// exceptions. An item may span several repos, which are its places.
type Repo struct {
	Name   string
	Team   string // the product team
	Areas  []string
	Habits []string // banked sentences
	Recipe Recipe
	CIRed  bool
	Hue    int
	// Watched says the repository is on the watched list the floor keeps
	// ([RepoSwitch]): the poll reads it and the factory works on its jobs.
	// The factory's queue stamps it; a floor read straight off the items
	// does not know it and leaves it false.
	Watched bool
}

// PhaseState is a stage's state inside a running stream.
type PhaseState string

const (
	PhasePending PhaseState = "pending"
	PhaseRunning PhaseState = "running"
	PhaseDone    PhaseState = "done"
	PhaseFailed  PhaseState = "failed"
	PhaseWaiting PhaseState = "waiting"
)

// Phase is one stage as it runs: its state, what round it is on, how many
// tasks it fanned out into.
type Phase struct {
	Name  string
	Kind  StageKind
	State PhaseState
	Note  string
	Round int
	Tasks int
	Left  time.Duration
	// Chat and Handle name the stage's conversation when the stage is one:
	// the room a person walks into from the phase strip.
	Chat   string
	Handle string
	// Carry is what happened while the step was paused that its chat is told
	// when it carries on in the same conversation: the answer to the question
	// a pause cut short (internal/factory/run's inbox.go). Taken by the round
	// that resumes the step.
	Carry []string `json:",omitempty"`
	// Result is what the step's last round reported (its plan edit left
	// out, its output cut at [ResultOutputMost]), kept so a run revived
	// after a restart still hands the steps after it what came before.
	Result *StageResult `json:",omitempty"`
	// At is when the phase ended, done or failed. Zero while it has not
	// ended, and again once it is sent back to run. The wire's step.at is
	// this instant.
	At time.Time `json:",omitempty"`
	// Started is when the phase's latest round began: set as a round starts
	// in a fresh chat, kept when a round carries on in the chat a pause cut
	// short, and zero again once the phase is pending. The wire's
	// step.started_at is this instant; a phase recorded before it existed has
	// none.
	Started time.Time `json:",omitempty"`
	// Rounds is one line per finished try, oldest first, bounded to the
	// step's max. The last Result stays; these lines are what each try did.
	Rounds []RoundLine `json:",omitempty"`
	// Failure is set while this step is the one that errored. A later round
	// of the same step clears it, and clears the job's failure for this
	// step. The record keeps the incident.
	Failure *Failure `json:",omitempty"`
}

// ResultOutputMost is how much of a step's output [Phase.Result] keeps, in
// bytes.
const ResultOutputMost = 16 << 10

// LogLine is one line of a stream's grain: a thought, a shell call, a test,
// a write, something said, a question, a failure, a success.
type LogLine struct {
	At    time.Time
	Glyph string
	Tone  string // thought · shell · test · write · said · ask · fail · ok
	Text  string
}

// Stream is an item on a bench.
type Stream struct {
	Phases   []Phase
	Cur      int
	Spent    float64
	Started  time.Time
	Ended    time.Time
	Activity []int // 0..7, oldest first
	Log      []LogLine
	Paused   bool
	Findings int
	Bench    int
	Room     string // the item team's id, when known; its Traffic is this log
}

// Claim is one row of a proof sheet: what was claimed, whether it was shown,
// and the evidence in the claim's own medium.
type Claim struct {
	Text     string
	OK       bool
	Evidence string
	Medium   string // test · screenshot · transcript · benchmark · policy
}

// Pull-request states the factory records after it opens one. THE JOB'S OWN
// STATE STAYS shipped, which the wire calls done. These words ride on the
// pull request, so PR open and Merged need no new job state. Contract stays 2.
const (
	PROpen   = "open"
	PRMerged = "merged"
	PRClosed = "closed"
)

// PRReview is one review on a pull request the factory opened.
type PRReview struct {
	Body  string
	URL   string
	By    string
	State string
	At    time.Time
}

// PRChecks is how that pull request's check runs add up. A zero count is
// left off the wire: it is not a claim that nothing passed.
type PRChecks struct {
	Passed  int
	Failed  int
	Pending int
}

// PullRequest keeps the published work separate from the source issue, so
// reporting its receipt never changes the identity of the job being run.
// State, the merge, the reviews and the check summary are filled when a
// later poll reads the pull request again.
// PRReadProblem describes one incomplete section of an otherwise current PR read.
// Other sections stay useful. Detail is diagnostic data, never instructions.
type PRReadProblem struct {
	Area      string `json:"area"`
	Code      string `json:"code"`
	Summary   string `json:"summary"`
	Detail    string `json:"detail"`
	Retryable bool   `json:"retryable"`
}

// PRFeedback stores the semantic review cursor separately from display text.
type PRFeedback struct {
	ID       string
	Revision string
	Resolved bool
	External bool
}

type PullRequest struct {
	FeedbackItems    []PRFeedback    `json:",omitempty"`
	FeedbackTracked  bool            `json:",omitempty"`
	Problems         []PRReadProblem `json:",omitempty"`
	ReadinessKey     string          `json:",omitempty"`
	ReadinessContext string          `json:",omitempty"`
	BaseSHA          string          `json:",omitempty"`
	Feedback         string          `json:",omitempty"`
	CheckRuns        []CheckRun      `json:",omitempty"`
	Conflict         bool
	Draft            bool
	Number           int
	URL              string
	CI               string
	State            string
	MergedBy         string
	MergedAt         time.Time
	ReviewDecision   string
	Reviews          []PRReview
	Required         int
	Checks           PRChecks
	// HeadSHA is the commit the pull request's branch points at, Mergeable
	// whether the forge says it can merge ([MergeableYes], [MergeableNo], or
	// "" while it has not worked that out), and ChecksRead and ReviewsRead
	// whether the last follow read those at all. [MergeGate] refuses on any
	// of them unknown: a fact nobody read is never a yes.
	HeadSHA     string `json:",omitempty"`
	Mergeable   string `json:",omitempty"`
	ChecksRead  bool   `json:",omitempty"`
	ReviewsRead bool   `json:",omitempty"`
}

// Item is one row on the floor.
// QKindAskFirst is an intake question, before a run or any step exists.
const QKindAskFirst = "ask_first"

type Item struct {
	// Autonomy is explicit on new jobs. Missing on older jobs means manual.
	Autonomy string      `json:",omitempty"`
	Manual   bool        `json:",omitempty"`
	Effort   string      `json:",omitempty"`
	FollowUp *PRFollowUp `json:",omitempty"`
	// SetupHeld keeps first-run imports in Backlog until a person starts them.
	SetupHeld bool `json:",omitempty"`
	// Job decisions belong to the binary; presentation derives from these facts.
	Priority    *JobPriority `json:",omitempty"`
	DismissedBy []string     `json:",omitempty"`
	DoneHow     string       `json:",omitempty"`
	DoneAt      time.Time    `json:",omitempty"`
	Approval    *Approval    `json:",omitempty"`
	StopUndo    *StopUndo    `json:",omitempty"`
	// MergePolicy is the pipeline's merge settings as they stood when the
	// proof was approved: approving publishes, and the approval says whether
	// the pull request it opens may merge on its own. nil is auto-merge off.
	MergePolicy *MergePolicy `json:",omitempty"`
	// Merging is a merge on its way, from Merge (or the pipeline's
	// auto-merge) until the runner reports what the forge did.
	Merging *MergeRun `json:",omitempty"`

	// Owners is derived from default-branch CODEOWNERS for the wire and fixture.
	Owners  []string `json:",omitempty"`
	ID      int
	Repo    string   // the repo it arrived on
	Product string   // the product team that owns it
	Places  []string // every repo it touches; Repo alone until the plan says more
	Num     int
	Kind    Kind
	Title   string
	Body    string
	Author  string
	Tier    Tier
	Origin  Origin
	Synced  bool
	Created time.Time
	Changed time.Time
	State   State

	// ChangeBase pins the comparison before the first round so commits made by
	// later rounds do not disappear from the reported change.
	ChangeBase string  `json:",omitempty"`
	Change     *Change `json:",omitempty"`
	Draft      bool    `json:",omitempty"`
	// WorktreeMachine keeps resumed runs and publishing beside the files the
	// first worker made, even after its lease expires or the factory restarts.
	WorktreeMachine string `json:",omitempty"`
	// Publishing lasts from recorded approval until the publisher reports its
	// outcome, so the surface cannot say shipped before the pull request exists.
	Publishing    bool        `json:",omitempty"`
	HeadCommit    string      `json:",omitempty"`
	PublishStatus *Publishing `json:",omitempty"`
	Triage        Triage
	Stages        []Stage // the item's copy of the recipe, toggled
	Cap           float64

	// Assignee is the forge's person; Taker and Starter keep the person who
	// took or started otherwise unassigned work. Author is never ownership:
	// opening an issue must not volunteer the reporter's machines or money.
	Assignee string `json:",omitempty"`
	Starter  string `json:",omitempty"`
	Taker    string `json:",omitempty"`
	// SourceAssignees is the set of logins the forge listed as assignees at
	// the last read: a FACT about the forge, never the execution owner. nil
	// means never read (or the forge did not say); non-nil and empty means the
	// forge said nobody is assigned, and that survives a round trip through
	// the store. Sorted, lowercase, without duplicates ([NormalizeSourceAssignees]).
	// Nothing derives Person() from it: two assignees are not one owner.
	SourceAssignees []string `json:",omitzero"`
	// OldGate is the item's `ask me at` from before approve steps, read
	// into its stages when the item is ([Item.UnmarshalJSON]) and never
	// written: "" on every item in memory.
	OldGate  Gate `json:"Gate,omitempty"`
	Stream   *Stream
	Question string
	// QKind is why the item is waiting: approve, cap, scope, step, ask_first
	// or out_of_attempts. The wire's needs_you.kind is derived from this.
	QKind string
	// AskedFirstReason keeps the matched rule visible after the question is answered.
	AskedFirstReason string `json:",omitempty"`
	// Ask is the person's own words when the job did not start from a GitHub
	// issue. A GitHub issue keeps that on the wire and leaves Ask empty.
	Ask *JobAsk `json:",omitempty"`
	// Failure is a step that errored. Nil means the job has not failed.
	// It is not a question.
	Failure *Failure `json:",omitempty"`
	// Recovery is the service-owned, bounded retry of an interrupted step.
	Recovery *StepRecovery `json:",omitempty"`
	// PublishError is the first line of a publish that failed. The job stays
	// landed so the approval already recorded can retry. Empty means none.
	PublishError string `json:",omitempty"`
	Proof        []Claim
	Policy       []Claim
	Marked       bool
	Labels       []string
	Checks       string
	Diff         string
	// Asking is the question a step of the run put to the manager and that
	// is not answered yet (the inbox, internal/factory/run's inbox.go): with
	// the manager while it reads it, with the person once the manager sent it
	// on. It is kept on the item so a question sent to the person outlives a
	// pause and a restart; nil is none.
	Asking *Asked
	// ManagerAnswer confirms the last durable decision independently of lease delivery.
	ManagerAnswer *ManagerAnswer `json:",omitempty"`
	// Adapted is what was changed about the item's stages, one line each in
	// the order it changed them, each starting with who changed them
	// (`manager set review: …`, `plan added arch`), `why: …` last: the record
	// [Edit] writes and the surface draws under the stages line
	// ([AdaptedLine]).
	Adapted []string
	// Notes are sentences a person settled on in the item's own conversation
	// for the stages to read (session's `factory_item`), oldest first. They
	// are kept on the item now; THE STAGES' BRIEFS WILL CARRY THEM WHEN THE
	// RUNNER LANDS, and until then nothing reads them but the item's own
	// conversation and its page.
	Notes []string
	// Model is the model this job's steps run on, as a person chose it for
	// this job; "" is the factory's default. Every step reads it when it
	// starts, and a change reaches a step already running at its next turn
	// (run's SetModel), as a conversation's model does.
	Model           string `json:",omitempty"`
	AutomaticModels *bool  `json:",omitempty"`
	// Talk is the item's own conversation, by its session file: the one `T`
	// opens on the floor ([Seam.Talk]). "" IS NONE, which is every item until
	// a person asks for one — the conversation is never made by default.
	Talk string
	// ChatSpent is what Talk spent while the job had no run: the job's chat
	// before its first Start, in dollars. A JOB'S CHAT SPENDS FROM THE JOB'S
	// BUDGET (the owner's ruling of 2026-10-20), and with no run there is no
	// Stream to hold it; the next start carries it into the run's Spent and
	// zeroes it (run's freshStream). A job with a stream books its chat's
	// spend there instead ([AddChatSpend]), so [JobSpent] reads one figure.
	ChatSpent float64 `json:",omitempty"`
	// Thread is replicated from the worker so every machine reads the same
	// conversation. AnsweredQuestions keeps asks after the open gate clears.
	Thread            []Message  `json:",omitempty"`
	AnsweredQuestions []Question `json:",omitempty"`
	// Heard is the instant of the last thing the person typed into Talk that
	// the runner has taken: as the brief before a run (into Notes), as the
	// steer during one, or as the answer to a question it asked. Zero is
	// nothing taken yet, so a first run reads the whole conversation.
	Heard time.Time

	// URL is the item's page on its source (an issue's or pull request's
	// html_url), and "" for work that was never on one, which is how the
	// floor's `g` knows there is nowhere to open.
	URL string
	// Comments are the last few things said on the item, oldest first, as its
	// source keeps them (internal/factory/github reads the last three). NIL IS
	// NOT READ YET and an empty list is read and nothing said: a busy
	// repository's first read folds its items before their comments.
	Comments []Comment
	// Files are a pull request's changed files, the most changed first, at
	// most twenty; nil for anything that is not a pull request, or for one
	// whose files are not read yet (its Diff is then not read yet either).
	Files []FileChange
	// PlannedFiles is a CACHE of the paths the plan step's explicit Files
	// declaration named, kept for older builds reading the persisted item. It is not
	// the truth readers use: [PlanFilesOf] derives the list from the saved plan
	// result, so an old cache cannot outlive the plan it came from. Nil is
	// unknown, never "the previous plan's files".
	PlannedFiles []string `json:",omitempty"`
	// CheckRuns are the checks on a pull request's head, one per run. Checks
	// stays the one-word summary the row draws; these are what the page lists.
	CheckRuns []CheckRun
	// Base is the branch a pull request asks to merge into and Head the
	// branch it comes from, `owner:branch` when it comes from a fork; HeadSHA
	// is the head's commit when the source last read it. "" for anything that
	// is not a pull request, or one read before codeaf kept them. A step's
	// worktree is cut at the pull request's head (internal/factory/run's
	// workdir.go), and its brief names Base so `git diff` shows the change.
	Base    string `json:",omitempty"`
	Head    string `json:",omitempty"`
	HeadSHA string `json:",omitempty"`
	// PullRequest is the published receipt, kept separately from the source.
	PullRequest *PullRequest `json:",omitempty"`
	// Publish is the proof gate's next action, pr or comment, as measured by
	// the worker's worktree. Empty means the worker has not measured it yet.
	Publish string `json:",omitempty"`
	// CommentURL is the receipt for approved proof that needed no new commit.
	CommentURL string `json:",omitempty"`

	// Activity is the item's own short history, oldest first: it arrived, it
	// changed upstream, it was read, a conversation was made, its stages
	// changed. At most [ActivityMost] are kept; the oldest go first.
	Activity []Event
}

// Person is who may run the item. The forge assignee takes precedence over
// local intent, and an explicit Take supersedes an earlier start.
func (it Item) Person() string {
	for _, handle := range []string{it.Assignee, it.Taker, it.Starter} {
		if handle = NormalizeHandle(handle); handle != "" {
			return handle
		}
	}
	return ""
}

// NormalizeHandle lets workers and stored ownership compare forge handles
// without treating display prefixes or case as a different person.
func NormalizeHandle(handle string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
}

// NormalizeSourceAssignees makes a forge's assignee list comparable: handles
// normalized, duplicates dropped, sorted; a blank entry makes the whole list
// unknown. nil stays nil (unknown) and a non-nil list stays non-nil even when
// empty (known none); the input is never modified.
func NormalizeSourceAssignees(logins []string) []string {
	if logins == nil {
		return nil
	}
	out := make([]string, 0, len(logins))
	for _, l := range logins {
		// A LOGIN-LESS ENTRY MAKES THE WHOLE LIST UNKNOWN: dropping it would
		// turn a response this reader does not understand into a proven
		// none, or a partial list into a sole owner.
		if l = NormalizeHandle(l); l == "" {
			return nil
		}
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	slices.Sort(out)
	return out
}

// SoleSourceAssignee is the one login a forge's assignee list names, and ""
// for none, for several and for unknown. Several assignees are not one owner,
// so no member is ever picked on their behalf.
func SoleSourceAssignee(logins []string) string {
	if n := NormalizeSourceAssignees(logins); len(n) == 1 {
		return n[0]
	}
	return ""
}

// SameSourceAssignees compares two lists as presence plus set: unknown equals
// only unknown, and known-empty differs from unknown.
func SameSourceAssignees(a, b []string) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return slices.Equal(NormalizeSourceAssignees(a), NormalizeSourceAssignees(b))
}

// Comment is one thing said on an item, by its author's login.
type Comment struct {
	Author string
	Body   string
	At     time.Time
}

// FileChange is one file a pull request changes, with its line counts.
type FileChange struct {
	Path    string
	Added   int
	Removed int
}

// CheckRun is one check on a pull request's head: its name, its state (the
// run's conclusion when it finished, otherwise its status: `success`,
// `failure`, `in_progress`, `queued`), and its page.
type CheckRun struct {
	Source string `json:",omitempty"`
	AppID  int    `json:",omitempty"`
	Name   string
	State  string
	URL    string
}

// Event is one line of an item's activity: when, and what, in a few plain
// words (`arrived from github`, `read`, `talked`).
type Event struct {
	At   time.Time
	What string
}

// Ref is the item's short name: #123, or ci.
//
// AN ITEM WITH NO FORGE NUMBER IS NAMED BY THE FLOOR'S OWN ID. Work a chat or a
// terminal put on the floor never had an issue number, and `#0` on every one of
// those rows named nothing a person could say back; the store's id is the one
// the chat's own sentence uses (`#<id> ... is on the factory floor`), so a row,
// the peek and the item page say the number the conversation already said. A
// forge item keeps its forge number, which is the one its repository uses.
func (it Item) Ref() string {
	if it.Kind == KindCI {
		return "ci"
	}
	if it.Num == 0 && it.ID > 0 {
		return "#" + itoa(it.ID)
	}
	return "#" + itoa(it.Num)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Shift is the handover: what happened since the person last looked.
// SpentToday is the runner's day figure ([DaySpend]), separate from Spent.
type Shift struct {
	Since       time.Time
	Shipped     int
	Arrived     int
	Asked       int
	Handled     int
	Spent       float64
	SpentToday  float64
	MergedToday int
	MergedWeek  int
	Hours       [24]int
	Shipping    []string
}

// Snapshot is everything a frame needs, copied off the loop. A frame reads
// memory only (framedisk law), so the seam hands over values, never a store.
type Snapshot struct {
	// People holds queue identity summaries; repository reads supply the live map.
	People  map[string]PersonSummary
	Now     time.Time
	Repos   []Repo
	Items   []Item
	Benches int
	Daily   float64
	Rail    float64
	Shift   Shift
	Sources []SourceInfo // what is connected; the chat is always one

	// Busy is what is in flight on single items right now, by item id:
	// `reading` while the triage worker is on it, `refreshing` while a
	// [Seam.Refresh] of it runs. An item not in it is idle. Nil is nothing in
	// flight.
	Busy map[int]string
	// BusyAll is the whole floor's refresh while it runs, in the words the
	// floor draws (`refreshing 8 items · 3 done`), and "" otherwise.
	BusyAll string
	// LastReadCost is what the last triage read cost in dollars, the figure
	// the floor's `~$0.0004` note quotes; 0 is not known, and draws nothing.
	LastReadCost float64
	// Marked is the floor's own marks, by item id: the new items the foreman
	// (or anything else behind [Seam.Mark]) proposed for the next launch,
	// kept by the store so every window reads the same ones. Each is also
	// drawn as its item's Marked. Nil is nothing marked.
	Marked []int
	// Checkouts is where this machine has each of the floor's repositories
	// checked out, by the name the floor shows, for the repositories it knows
	// a folder for; a repository not in it has no checkout here, and the
	// first step of a run of its items clones one ([Seam.CheckoutOf] says
	// whether it can). NIL IS A SEAM THAT DOES NOT SAY,
	// which the surface reads as nothing to check; a seam that knows where
	// repositories are checked out answers a map, empty when it knows none.
	Checkouts map[string]string
}

// RepoNamed finds a repo in the snapshot.
func (s Snapshot) RepoNamed(name string) (Repo, bool) {
	for _, r := range s.Repos {
		if r.Name == name {
			return r, true
		}
	}
	return Repo{}, false
}

// Count is how many items stand in a state.
func (s Snapshot) Count(st State) int {
	n := 0
	for _, it := range s.Items {
		if it.State == st {
			n++
		}
	}
	return n
}

// Asked is one question a step of an item's run asked: every such question
// goes to the item's manager first, which answers it from the issue, the
// recipe and the stages, or sends it on to the person.
type Asked struct {
	// Stage is the step that asked, by its one word.
	Stage string
	// Question is the question in one line, and Options its answers when it
	// offered some, with Pick the one the step would take.
	Question string
	Options  []string
	Pick     string
	// At is when the step asked.
	At time.Time
	// With is who holds the question now: [AskedManager] or [AskedYou].
	With string
	// Why is the manager's reason for sending it to the person, "" for none.
	Why string
}

// Who holds a step's question ([Asked.With]).
const (
	AskedManager = "manager"
	AskedYou     = "you"
)

// WorkState names execution that is waiting for its files or carrying an
// approval out. A queued job stays on its machine even while it is offline.
func (it Item) WorkState() string {
	if it.State == StateQueued && it.WorktreeMachine != "" {
		word := "waiting for " + it.WorktreeMachine
		if it.Publishing {
			return "publishing · " + word
		}
		return word
	}
	if it.Publishing {
		return "publishing"
	}
	return ""
}

// ManagerAnswer is an idempotency receipt for one exact manager decision. Phase,
// round and chat keep a delayed redelivery from reaching another step attempt.
type ManagerAnswer struct {
	QuestionID string
	Manager    string
	Words      string
	Stage      string
	Phase      int
	Round      int
	Chat       string
	At         time.Time
}
