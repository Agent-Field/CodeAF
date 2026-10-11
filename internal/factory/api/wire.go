// Package api is the factory's wire: the JSON shapes `/v1` speaks, the codec
// between them and the Go floor ([factory.Snapshot], [factory.Item]), and the
// names of the verbs the server answers.
//
// THE CODEC IS THE ONE PLACE THE TWO VOCABULARIES MEET. The API and the
// dashboard say job, step, pipeline and backlog; the Go internals still say
// item, stage, recipe and new (docs/design/factory/ARCHITECTURE.md §2). No
// other package maps one onto the other, so a word changes in one file.
//
// A Job carries two kinds of field. The product fields are what the dashboard
// draws. The desk fields, marked `desk` in their comments, are what the TUI
// page reads and the dashboard does not: the TUI is a client of the same API,
// and a field it draws that the wire dropped would be a blank on the desk that
// nobody notices until a person does. Fields the Go floor cannot supply yet are
// `omitempty` and say so in one line.
package api

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// Contract is the wire contract version `GET /v1/me` and `GET /v1/about` report. Bump it only for
// a breaking wire change (a field removed, renamed or re-typed, a verb's
// meaning changed); additive fields never bump it.
const Contract = 1

// Factory names the server every response comes from.
type Factory struct {
	Address string `json:"address"`
	Version string `json:"version"`
}

// Queue is `GET /v1/queue`: the whole floor as one person sees it.
type Queue struct {
	Factory  Factory    `json:"factory"`
	Since    Since      `json:"since"`
	Jobs     []Job      `json:"jobs"`
	Repos    []Repo     `json:"repos"`
	Strip    []Cell     `json:"strip"`
	Capacity Capacity   `json:"capacity"`
	Me       string     `json:"me"`
	Desk     *QueueDesk `json:"desk,omitempty"`
}

// QueueDesk is what the TUI page reads off the floor that the dashboard does
// not draw. It is one block so a reader of the wire sees at a glance which
// fields are the desk's.
type QueueDesk struct {
	Now          time.Time      `json:"now"`
	Sources      []Source       `json:"sources"`
	Busy         map[int]string `json:"busy,omitempty"`
	BusyAll      string         `json:"busy_all,omitempty"`
	LastReadCost float64        `json:"last_read_cost,omitempty"`
	Marked       []int          `json:"marked,omitempty"`
	// Checkouts has no omitempty ON PURPOSE: null is a seam that does not
	// say, and {} is a seam that knows no checkout ([factory.Snapshot]).
	Checkouts map[string]string `json:"checkouts"`
}

// Since is the handover: what happened since the person last looked.
// SpentToday is the runner's figure for the calendar day, which is not Spent:
// Spent counts only what began during this shift.
type Since struct {
	At         time.Time `json:"at"`
	Shipped    int       `json:"shipped"`
	Arrived    int       `json:"arrived"`
	Asked      int       `json:"asked"`
	Handled    int       `json:"handled"`
	Spent      float64   `json:"spent"`
	SpentToday float64   `json:"spent_today"`
	Hours      [24]int   `json:"hours"`
	Shipping   []string  `json:"shipping,omitempty"`
}

// Cell is one chip of the pipeline strip: a step with jobs in it, and whether
// an approve waits there.
type Cell struct {
	Step    string `json:"step"`
	Jobs    int    `json:"jobs"`
	Waiting bool   `json:"waiting,omitempty"`
}

// Capacity is how much the floor may run and spend.
type Capacity struct {
	Benches int     `json:"benches"`
	Daily   float64 `json:"daily"`
	Rail    float64 `json:"rail"`
	Queued  int     `json:"queued"`
	// MachinesOnline counts machines heard inside the offline window, busy
	// or idle, every handle. Busy counts those holding a job. Both are
	// counts, so zero is sent: an absent field would be an older factory.
	MachinesOnline int `json:"machines_online"`
	Busy           int `json:"busy"`
	// Workers remains for an older reader. Presence is machines_online and
	// busy; this list is not filled.
	Workers []Worker `json:"workers,omitempty"`
}

// Source is a connected source of jobs (desk).
type Source struct {
	Name    string    `json:"name"`
	Writes  bool      `json:"writes,omitempty"`
	Repos   []string  `json:"repos,omitempty"`
	Polled  time.Time `json:"polled"`
	Trouble string    `json:"trouble,omitempty"`
	Polling bool      `json:"polling,omitempty"`
	Reading string    `json:"reading,omitempty"`
	Read    int       `json:"read,omitempty"`
	Of      int       `json:"of,omitempty"`
	Items   int       `json:"items,omitempty"`
}

// Repo is a watched repository and the pipeline it runs.
type Repo struct {
	Name     string    `json:"name"`
	Watched  bool      `json:"watched"`
	Checkout string    `json:"checkout,omitempty"`
	Pipeline *Pipeline `json:"pipeline,omitempty"`
	Team     string    `json:"team,omitempty"`   // desk
	Areas    []string  `json:"areas,omitempty"`  // desk
	Habits   []string  `json:"habits,omitempty"` // desk
	Hue      int       `json:"hue,omitempty"`    // desk
	CIRed    bool      `json:"ci_red,omitempty"` // desk
}

// Pipeline is a repository's steps in order, per kind of job, and the policy
// every proof must show (code: [factory.Recipe]). Text, Start, Size, Origin
// and Personal are the file the pipeline route read; a queue repository's
// pipeline leaves them empty because the floor's recipe does not carry them.
type Pipeline struct {
	Problems []string             `json:"problems,omitempty"`
	Source   string               `json:"source,omitempty"` // file, markdown or defaults, from internal/factory/pipeline
	Steps    []StepDef            `json:"steps"`
	ByKind   map[string][]StepDef `json:"by_kind,omitempty"`
	Policy   []string             `json:"policy,omitempty"`
	Habits   []string             `json:"habits,omitempty"`
	Adapt    map[string]string    `json:"adapt,omitempty"`
	// Text is the repository file as it was read. PersonalText is
	// ~/.codeaf/factory.yaml when that file tightened the read.
	Text         string      `json:"text,omitempty"`
	PersonalText string      `json:"personal_text,omitempty"`
	Personal     bool        `json:"personal,omitempty"`
	Origin       string      `json:"origin,omitempty"` // main, working tree, defaults
	Start        *StartBlock `json:"start,omitempty"`
	Size         *SizeBlock  `json:"size,omitempty"`
}

// StartBlock is the file's start: what may begin without a person.
type StartBlock struct {
	Labels   []string `json:"labels"`
	MaxSize  string   `json:"max_size"`
	MaxCost  float64  `json:"max_cost"`
	Never    []string `json:"never"`
	AskFirst []string `json:"ask_first"`
}

// SizeBlock is the file's size: how S, M and L are decided.
type SizeBlock struct {
	PRLines [2]int `json:"pr_lines"`
	Issue   string `json:"issue"`
}

// StepDef is one step as the pipeline defines it (code: [factory.Stage]).
type StepDef struct {
	Name  string `json:"name"`
	Kind  string `json:"kind,omitempty"`
	Ask   string `json:"ask,omitempty"`
	Until string `json:"until,omitempty"`
	// UntilSentence is the file's until line before it is folded into the
	// runner's word (done, clean, green, proven). The pipeline route sets it.
	UntilSentence string   `json:"until_sentence,omitempty"`
	Max           int      `json:"max,omitempty"`
	Fixed         bool     `json:"fixed,omitempty"`
	Custom        bool     `json:"custom,omitempty"`
	Off           bool     `json:"off,omitempty"`
	When          string   `json:"when,omitempty"`   // desk
	Effort        string   `json:"effort,omitempty"` // desk
	Fanout        string   `json:"fanout,omitempty"` // desk
	Proof         []string `json:"proof,omitempty"`  // desk
	Why           string   `json:"why,omitempty"`    // desk
	By            string   `json:"by,omitempty"`     // desk
}

// Job states, as the API says them (ARCHITECTURE §2).
const (
	StateBacklog  = "backlog"
	StateQueued   = "queued"
	StateRunning  = "running"
	StateNeedsYou = "needs_you"
	StateDone     = "done"
	StateDropped  = "dropped"
)

// What Approve does on a job now ([Job.Gate]).
const (
	GatePlan     = "plan"
	GateProof    = "proof"
	GateQuestion = "question"
	GateCap      = "cap"
)

// Step run states.
const (
	StepPending = "pending"
	StepRunning = "running"
	StepDone    = "done"
	StepFailed  = "failed"
	StepWaiting = "waiting"
	StepSkipped = "skipped"
)

// Job is one unit of work on the floor (code: [factory.Item]).
type Job struct {
	Change *factory.Change `json:"change,omitempty"`
	// Draft is known only for source PR jobs; published PRs carry pr.draft.
	Draft *bool  `json:"draft,omitempty"`
	ID    int    `json:"id"`
	Repo  string `json:"repo"`
	Num   int    `json:"number"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
	State string `json:"state"`
	// Ready is the needs_you sub-state of a job that landed and waits for
	// its proof to be approved.
	Ready            bool       `json:"ready,omitempty"`
	Gate             string     `json:"gate"`
	WaitingSince     *time.Time `json:"waiting_since,omitempty"`
	AskedFirstReason string     `json:"asked_first_reason,omitempty"`
	AssignedTo       string     `json:"assigned_to,omitempty"`
	Step             *StepAt    `json:"step,omitempty"`
	Steps            []Step     `json:"steps"`
	Claims           []Claim    `json:"claims,omitempty"`
	// PlannedFiles are the paths the plan step named, so the brief can say
	// how many it planned against how many the change touched.
	PlannedFiles []string   `json:"planned_files,omitempty"`
	Questions    []Question `json:"questions,omitempty"`
	Cost         float64    `json:"cost"`
	CreatedAt    time.Time  `json:"created_at"`
	// HeldBy is the machine working this job now. It is set from the live
	// lease, including when that machine belongs to someone else: the roster
	// on /v1/me does not list another person's machines.
	HeldBy     *HeldBy  `json:"held_by,omitempty"`
	PR         *PR      `json:"pr"` // Null when the worker published a comment.
	Publish    string   `json:"publish,omitempty"`
	CommentURL string   `json:"comment,omitempty"`
	Desk       *JobDesk `json:"desk,omitempty"`
}

// JobDesk is what the TUI page reads off a job that the dashboard does not
// draw (DESIGN-BRIEF §5: triage, tier, origin, labels, checks, comments,
// files and activity are code-only on the dashboard).
type JobDesk struct {
	ChangeBase      string    `json:"change_base,omitempty"`
	Thread          []Message `json:"thread,omitempty"`
	WorktreeMachine string    `json:"worktree_machine,omitempty"`
	Publishing      bool      `json:"publishing,omitempty"`
	Product         string    `json:"product,omitempty"`
	Places          []string  `json:"places,omitempty"`
	Body            string    `json:"body,omitempty"`
	Author          string    `json:"author,omitempty"`
	// The routing facts stay separate so a round trip cannot turn a local
	// Take into a forge assignment or forget who started the work.
	Assignee  string    `json:"assignee,omitempty"`
	Starter   string    `json:"starter,omitempty"`
	Taker     string    `json:"taker,omitempty"`
	Tier      string    `json:"tier,omitempty"`
	Origin    string    `json:"origin,omitempty"`
	Synced    bool      `json:"synced,omitempty"`
	ChangedAt time.Time `json:"changed_at"`
	Triage    Triage    `json:"triage"`
	Cap       float64   `json:"cap,omitempty"`
	Policy    []Claim   `json:"policy,omitempty"`
	Marked    bool      `json:"marked,omitempty"`
	Labels    []string  `json:"labels,omitempty"`
	Checks    string    `json:"checks,omitempty"`
	Diff      string    `json:"diff,omitempty"`
	Adapted   []string  `json:"adapted,omitempty"`
	Notes     []string  `json:"notes,omitempty"`
	Talk      string    `json:"talk,omitempty"`
	Heard     time.Time `json:"heard"`
	// Comments has no omitempty ON PURPOSE: null is not read yet, and [] is
	// read with nothing said ([factory.Item.Comments]).
	Comments  []Comment  `json:"comments"`
	Files     []File     `json:"files,omitempty"`
	CheckRuns []CheckRun `json:"check_runs,omitempty"`
	Base      string     `json:"base,omitempty"`
	Head      string     `json:"head,omitempty"`
	HeadSHA   string     `json:"head_sha,omitempty"`
	Activity  []Activity `json:"activity,omitempty"`
	Run       *Run       `json:"run,omitempty"`
}

// Run is a job's bench: the stream's facts that are not per step (desk). Nil
// is a job that never ran.
type Run struct {
	Started  time.Time `json:"started"`
	Ended    time.Time `json:"ended"`
	Activity []int     `json:"activity,omitempty"`
	Log      []LogLine `json:"log,omitempty"`
	Paused   bool      `json:"paused,omitempty"`
	Findings int       `json:"findings,omitempty"`
	Bench    int       `json:"bench,omitempty"`
	Room     string    `json:"room,omitempty"`
}

// StepAt is where a running job stands: step index of count, by name.
type StepAt struct {
	Index int    `json:"index"`
	Count int    `json:"count"`
	Name  string `json:"name"`
}

// Step is one step of one job: its definition and how its run went. A step
// may be in the job's pipeline, in its run, or both; the two flags say which,
// so the codec can give each half back to its own list.
type Step struct {
	Tests *factory.TestCounts `json:"tests,omitempty"`
	StepDef
	State      string     `json:"state"`
	Round      int        `json:"round,omitempty"`
	Result     string     `json:"result,omitempty"`
	Cost       float64    `json:"cost,omitempty"`
	At         *time.Time `json:"at,omitempty"`      // when the step ended; absent until it does
	Note       string     `json:"note,omitempty"`    // desk
	Tasks      int        `json:"tasks,omitempty"`   // desk
	LeftMS     int64      `json:"left_ms,omitempty"` // desk
	Chat       string     `json:"chat,omitempty"`    // desk
	Handle     string     `json:"handle,omitempty"`  // desk
	RunKind    string     `json:"run_kind,omitempty"`
	InPipeline bool       `json:"in_pipeline"`
	InRun      bool       `json:"in_run"`
}

// Claim is one row of a proof sheet.
type Claim struct {
	Text     string    `json:"text"`
	Evidence *Evidence `json:"evidence,omitempty"`
	Proven   bool      `json:"proven"`
}

// Evidence is what shows a claim, in its own medium.
type Evidence struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

// Question kinds the codec gives a question that is not the floor's own
// [factory.Item.QKind].
const (
	// QuestionAsked marks the question a step put to the manager
	// ([factory.Item.Asking]).
	QuestionAsked = "asked"
)

// Question is a question a job waits on.
type Question = factory.Question

// Answer preserves who answered, when, and the factory event.
type Answer = factory.Answer

// PR is the pull request a job opened.
type PR struct {
	Draft  bool   `json:"draft"`
	Number int    `json:"number"`
	URL    string `json:"url"`
	CI     string `json:"ci,omitempty"`
}

// Triage is the cheap read made on arrival (desk; never shown as risk on the
// dashboard).
type Triage struct {
	Type         string    `json:"type,omitempty"`
	Size         string    `json:"size,omitempty"`
	Area         string    `json:"area,omitempty"`
	Readiness    int       `json:"readiness,omitempty"`
	Est          float64   `json:"est,omitempty"`
	DupOf        int       `json:"dup_of,omitempty"`
	Risk         []string  `json:"risk,omitempty"`
	Read         string    `json:"read,omitempty"`
	Questions    []string  `json:"questions,omitempty"`
	Dup          string    `json:"dup,omitempty"`
	Priority     int       `json:"priority,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	TriagedAt    time.Time `json:"triaged_at"`
	StartJudged  bool      `json:"start_judged,omitempty"`
	StartMatched []string  `json:"start_matched,omitempty"`
	StartPaths   []string  `json:"start_paths,omitempty"`
	StartRules   []string  `json:"start_rules,omitempty"`
}

// LogLine is one line of a run's grain (desk).
type LogLine struct {
	At    time.Time `json:"at"`
	Glyph string    `json:"glyph,omitempty"`
	Tone  string    `json:"tone,omitempty"`
	Text  string    `json:"text"`
}

// Comment is one thing said on a job's source (desk).
type Comment struct {
	Author string    `json:"author"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
}

// File is one file a pull request changes (desk).
type File struct {
	Path    string `json:"path"`
	Added   int    `json:"added,omitempty"`
	Removed int    `json:"removed,omitempty"`
}

// CheckRun is one check on a pull request's head (desk).
type CheckRun struct {
	Name  string `json:"name"`
	State string `json:"state"`
	URL   string `json:"url,omitempty"`
}

// Activity is one line of a job's own short history (desk).
type Activity struct {
	At   time.Time `json:"at"`
	What string    `json:"what"`
}

// Message is one line of a job's manager thread.
type Message = factory.Message

// Event is one line of the record: who did what to which job, at what cost.
type Event = record.Event

// Machine status words on GET /v1/me.
const (
	MachineBusy    = "busy"
	MachineIdle    = "idle"
	MachineOffline = "offline"
)

// Machine is one of the caller's own codeaf processes, as GET /v1/me lists it.
// Another person's machine is not a row here; it shows up only as a job's
// [HeldBy] when the caller can see that job.
type Machine struct {
	Name          string    `json:"name"`
	OS            string    `json:"os"`
	CodeafVersion string    `json:"codeaf_version"`
	Status        string    `json:"status"`
	Job           int       `json:"job,omitempty"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// HeldBy is the live hold on a job: which machine, whose, when it last
// renewed, and when the hold began.
type HeldBy struct {
	Machine     string    `json:"machine"`
	Handle      string    `json:"handle"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
	Since       time.Time `json:"since"`
}

// Worker is a codeaf that pulls jobs from the factory. GET /v1/me lists
// [Machine] rows instead; this shape is the older capacity list and stays
// so a field already on the wire is not renamed.
type Worker struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Handle string `json:"handle"`
	Status string `json:"status"`
	Job    int    `json:"job,omitempty"`
}

// DryRun is what a repository's pipeline file would do to the jobs on the
// floor now. WouldWait is every backlog job that would neither start nor
// ask, with the reason it is waiting.
type DryRun struct {
	WouldStart []int `json:"would_start"`
	WouldAsk   []Ask `json:"would_ask"`
	WouldWait  []Ask `json:"would_wait"`
}

// Ask is one job the pipeline would ask about first, and why.
type Ask struct {
	Job    int    `json:"job"`
	Reason string `json:"reason"`
}
