// Package appwire is the desktop app's door into codeaf: one JSON object per
// line on the process's own stdin and stdout, as docs/design/app-v3/PROTOCOL.md
// states it. This package is the transport and the dispatch and nothing else.
// It knows what a request, a response and an event look like, in what order
// they leave, and which method names exist; it does NOT know what an agent is.
// That half is an [Engine], which cmd/codeaf implements over the conversations
// it holds, and which a test implements in a few lines.
//
// THE SPLIT IS WHAT MAKES THE WIRE PROVABLE WITHOUT A MODEL. Framing, sequence
// numbers, the hello line, the refusal of an unknown method and the exit on
// end-of-input are all facts about bytes, and they are tested here over
// io.Pipe with no conversation anywhere in the process.
package appwire

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/session"
	"time"

	"github.com/Agent-Field/codeaf/internal/workgraph"
)

// Contract is the protocol revision this build speaks, carried on the hello
// line. The app refuses a contract it does not know and kills the child, so a
// change that breaks a reader moves this number and nothing else does.
const Contract = 1

// The error codes a response may carry. They are words rather than numbers so
// the app can branch on them without a table, and the set is closed: a failure
// that is none of the others is [CodeEngine], with the message saying how.
const (
	CodeUnknownMethod = "unknown_method"
	CodeBadParams     = "bad_params"
	CodeBadScope      = "bad_scope"
	CodeNotFound      = "not_found"
	CodeBusy          = "busy"
	CodeEngine        = "engine"
	CodeClosed        = "closed"
	// Factory connection and GitHub grant failures have explicit recovery paths.
	CodeUnreachable   = "unreachable"
	CodeSignedOut     = "signed_out"
	CodeGitHubExpired = "github_expired"
	// CodeCycle refuses a collections.add that would make a collection hold
	// itself, at any depth.
	CodeCycle = "cycle"
	// CodeTooLarge refuses a read whose working set would not fit the engine's
	// memory budget. It is additive: an app that does not know the word treats
	// it as any failure, and one that does reads Error.Detail.
	CodeTooLarge = "too_large"
	// CodeUncertain answers a mutation whose effect the engine itself cannot
	// confirm: it may have happened. A caller MUST NOT retry it blind, which
	// could do it twice; it looks at the state again first. It is the same word
	// the app already uses for a call that was written and never answered.
	CodeUncertain = "uncertain"
	// CodeForbidden and CodeStale refuse a write on an attached factory's job
	// (factory.job_*), both with a FactoryRefusal as Detail. Forbidden: the
	// factory says this person may not (only the assignee approves). Stale:
	// the job changed since the person read it. Neither is retried by anyone
	// but the person, who reads the job again first. A refusal that is neither
	// (already approved, already taken) is CodeConflict.
	CodeForbidden = "forbidden"
	CodeStale     = "stale"
	// CodeHasWork refuses a factory.job_drop whose job holds work on this
	// machine that the drop would delete: commits on its local branch or
	// changes in its folder. The message is the factory's sentence naming
	// them, and Detail a FactoryRefusal. The same drop with discard is the
	// person's yes; nothing sends it on anyone's behalf.
	CodeHasWork = "has_work"
	// CodeHasRunning refuses a factory.repo_set that would turn a repository
	// off while any of its jobs is running, queued or waiting on a person.
	// The message is the factory's sentence naming them; the repository is
	// still on. The same switch with stop is the person's yes to stopping
	// them first; nothing sends it on anyone's behalf. It also answers a stop
	// that failed on the way: the message names the job, and the repository
	// is still on.
	CodeHasRunning = "has_running"
)

// The states an agent can be in, as the app draws them.
const (
	StateIdle     = "idle"
	StateWorking  = "working"
	StateNeedsYou = "needs_you"
	StateStopped  = "stopped"
	StateClosed   = "closed"
)

// The states a task can be in on the wire.
const (
	TaskQueued     = "queued"
	TaskRunning    = "running"
	TaskDone       = "done"
	TaskFailed     = "failed"
	TaskUnverified = "unverified"
)

// The event names this package and its engines send.
const (
	EventHello             = "hello"
	EventAgentChanged      = "agent.changed"
	EventTurnStarted       = "turn.started"
	EventTurnDelta         = "turn.delta"
	EventTurnTool          = "turn.tool"
	EventTurnDone          = "turn.done"
	EventCrewProposalDelta = "crew.proposal.delta"
	EventAskOpened         = "ask.opened"
	EventAskClosed         = "ask.closed"
	EventEntryAdded        = "entry.added"
	EventSpendChanged      = "spend.changed"
	EventTaskChanged       = "task.changed"
	EventCrewChanged       = "crew.changed"
	EventCrewRemoved       = "crew.removed"
	EventLog               = "log"

	EventSettingsChanged = "settings.changed"
	EventProviderChanged = "provider.changed"
)

// Error is a refusal the app can read: a code from the closed set above and a
// sentence. An engine returns one when it knows which code applies; any other
// error reaching the dispatcher is answered as [CodeEngine] with its text.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Changed names the files a conflict is about ([CodeConflict]).
	Changed []string `json:"changed,omitempty"`
	// Detail is the typed account of a refusal that has one: a [CodeTooLarge]
	// carries what did not fit, by how much and which smaller scope to ask for.
	Detail any `json:"detail,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Errorf builds an [Error] with a formatted sentence.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Hello is the first line codeaf writes, before it reads anything.
type Hello struct {
	// Features are additive request extensions; absent means unsupported.
	Features []string `json:"features,omitempty"`
	Contract int      `json:"contract"`
	Version  string   `json:"version"`
	PID      int      `json:"pid"`
	Home     string   `json:"home"`
}

// Agent is one conversation as the app sees it. ID is the conversation's
// session id, which is its folder's name on disk and stable across launches.
//
// AN UNKNOWN TIME IS LEFT OUT, NEVER WRITTEN AS THE ZERO TIME. Go's zero time
// reaches a reader as 0001-01-01T00:00:00Z, which a reader draws as a date in
// the first century; the emptiness law says
// an unknown draws nothing, so UpdatedAt and LastActiveAt carry omitzero.
type Agent struct {
	Location      *Location              `json:"location,omitempty"`
	ModelIdentity *session.ModelIdentity `json:"model_identity,omitempty"`
	// Retryable means this backend retained the failed message for agents.retry.
	Retryable    bool   `json:"retryable"`
	FactoryOwned bool   `json:"factory_owned,omitempty"`
	ID           string `json:"id"`
	Title        string `json:"title"`
	Workspace    string `json:"workspace"`
	Model        string `json:"model,omitempty"`
	// Effort is the conversation override: low, medium, high, xhigh or max.
	// It is omitted when unset (auto); other effort scopes can still apply.
	Effort    string    `json:"effort,omitempty"`
	State     string    `json:"state" enum:"idle,working,needs_you,stopped,closed,archived"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	// Crews are the crews this agent is a member of, open ones only.
	Crews []string `json:"crews,omitempty"`
	// Line is its live one-liner: the step it is on while it works, or the
	// question it is waiting on.
	Line string `json:"line,omitempty"`
	// ClosedAt is when it was moved to Closed and ArchivedAt when it was put
	// away; LastActiveAt is when the person last spoke in it, absent for one
	// they never spoke in (a crew member a manager started), and SpentUSD
	// what it has spent in all.
	ClosedAt     *time.Time `json:"closed_at,omitempty"`
	ArchivedAt   *time.Time `json:"archived_at,omitempty"`
	LastActiveAt time.Time  `json:"last_active_at,omitzero"`
	SpentUSD     float64    `json:"spent_usd,omitempty"`
	// Context is what the conversation weighs against its window
	// (compaction.go); absent when nothing holds the conversation.
	Context *AgentContext `json:"context,omitempty"`
	// Budget is this agent's own spend limit (budget.go), which is never
	// settings.budget_usd; absent from an engine that does not say.
	Budget *AgentBudget `json:"budget,omitempty"`
}

// Task is one node of an agent's task graph.
type Task struct {
	// Retryable is supported by this task road. The actual request still validates admission.
	Retryable bool `json:"retryable"`

	ID    string `json:"id"`
	Agent string `json:"agent"`
	// Parent is the task that asked for this one, and empty for a task the
	// agent itself started; Group names the adaptive run a task belongs to,
	// and GroupTitle is that run's own name, the label a person reads over
	// the group: the title of the run's own row.
	Parent     string    `json:"parent,omitempty"`
	Group      string    `json:"group,omitempty"`
	GroupTitle string    `json:"group_title,omitempty"`
	Title      string    `json:"title"`
	State      string    `json:"state"`
	UpdatedAt  time.Time `json:"updated_at"`
	// Proposed is a task waiting for the person's yes; ClockUntil is when it
	// starts on its own, absent while the clock is held and always absent
	// for Irreversible work. YourCall is a finished task waiting for the
	// person's verdict (tasks.decide).
	Proposed     bool       `json:"proposed,omitempty"`
	ClockUntil   *time.Time `json:"clock_until,omitempty"`
	Irreversible bool       `json:"irreversible,omitempty"`
	YourCall     bool       `json:"your_call,omitempty"`
	// Result is what the task said it did, Files what it changed, and
	// BudgetUSD and SpentUSD its cap and what it has spent.
	Result    string       `json:"result,omitempty"`
	Files     []FileChange `json:"files,omitempty"`
	BudgetUSD *float64     `json:"budget_usd,omitempty"`
	SpentUSD  *float64     `json:"spent_usd,omitempty"`
	// Line is the task's live status line, the one the terminal draws under
	// a running task: the phase it is in, the gap it is closing, or what it
	// is waiting on. It is empty when there is nothing to say.
	Line string `json:"line,omitempty"`
	// The task as its own conversation (V65, V66): whether the person paused
	// it, what it waits on and what waits on it, its model, its steps, cost and
	// clock, who started it, its instructions, and its own tasks.
	Paused       bool       `json:"paused,omitempty"`
	WaitsOn      []string   `json:"waits_on,omitempty"`
	HoldingUp    []string   `json:"holding_up,omitempty"`
	Model        string     `json:"model,omitempty"`
	Steps        *int       `json:"steps,omitempty"`
	CostUSD      *float64   `json:"cost_usd,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	StartedBy    string     `json:"started_by,omitempty"`
	Instructions string     `json:"instructions,omitempty"`
	Children     *int       `json:"children,omitempty"`
	ChildrenDone *int       `json:"children_done,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	// Models is the task's models in brief (V80); tasks.routing.get has the
	// rest. Absent from an engine that does not route tasks.
	Models *TaskModelsChip `json:"models,omitempty"`
	// Source is the message or directive that started the task, captured by the
	// engine when the work was admitted (V92). ABSENT IS UNKNOWN: a task recorded
	// before sources were captured has none, and an app must not infer one from
	// its title, its words or its time. Tasks made by a task share their parent's.
	Source *TaskSource `json:"source,omitempty"`
	// Dismissed says the person hid this settled failure from the default view
	// (tasks.dismiss, undone by tasks.restore). The task is otherwise unchanged
	// and is still listed; a surface leaves it out of its default counts.
	Dismissed bool `json:"dismissed,omitempty"`
	// Program, Ending and Branch are the facts a handed-over task's own row
	// persisted, so a COLD list row can name them without a page read. Program is
	// the program's name; Ending is the typed word for why the task ended
	// ("stopped" when the row's stopped flag is set: the work was stopped rather
	// than ending on its own, by a person, a killed job, a closing session or a
	// plan that called it off, and the word does not say who, so an app must not
	// read it as the person's own decision) and rides beside State, which still says
	// "failed" for it; for "interrupted" it is why a still-resumable task was cut; Branch is the branch the work was kept on. ABSENT IS
	// UNKNOWN for all three: a row from before they were recorded has none, and
	// an app must not read one from the title, the nearest launch or a branch
	// name. Branch is context and never the authority for a push, a post, a fix
	// or a land.
	Program string `json:"program,omitempty"`
	Ending  string `json:"ending,omitempty"`
	Branch  string `json:"branch,omitempty"`
}

// TaskSource is [workgraph.TaskSource]: one spelling on both wires.
type TaskSource = workgraph.TaskSource

// The kinds of [TaskSource].
const (
	SourceMessage   = workgraph.SourceMessage
	SourceDirective = workgraph.SourceDirective
)

// The two kinds of ask, and who closed one.
const (
	AskApproval = "approval"
	AskQuestion = "question"

	ClosedByApp       = "app"
	ClosedByElsewhere = "elsewhere"
	ClosedByStopped   = "stopped"
)

// Ask is one thing an agent is waiting on the person for: a tool call waiting
// to be allowed, or a question the agent put. Options are the engine's own
// choices, keyed by the id an answer names.
type Ask struct {
	ID      string `json:"id"`
	Agent   string `json:"agent"`
	Kind    string `json:"kind" enum:"approval,question"`
	Subkind string `json:"subkind,omitempty" enum:"permission,budget,crew_decision,crew_proposal,order_run,keep_running,service_health"`
	Title   string `json:"title"`
	Detail  string `json:"detail,omitempty"`
	// Options carry the engine's own ids; for a permission they are
	// allow_once, deny and, only when Scopes is non-empty, always.
	Scopes       []string    `json:"scopes,omitempty"`
	Outside      string      `json:"outside,omitempty"`
	AlwaysAsk    string      `json:"always_ask,omitempty" enum:"acts_as_you,critical,compound"`
	Options      []AskOption `json:"options,omitempty"`
	Irreversible bool        `json:"irreversible,omitempty"`
	// The decision dock's facts (V68): where the ask comes from, why now and
	// the evidence, the choices with the asker's suggestion, the clock, the
	// calls asked about together, the tasks it holds up, and whether a reason
	// may go with the answer. Later says the person put it aside.
	//
	// HoldingUp is set only on an ask whose source is a task: that task's id,
	// then every open task whose waits_on reaches it, nearest first, each
	// once, at most 256, read from the engine's task dependencies as they
	// stood when the ask was raised or restated. It is never read from what a
	// model names as blocked, nor from how many tasks are open.
	Source     *AskSource  `json:"source,omitempty"`
	Reason     string      `json:"reason,omitempty"`
	Evidence   *Evidence   `json:"evidence,omitempty"`
	Choices    []AskChoice `json:"choices,omitempty"`
	ClockUntil *time.Time  `json:"clock_until,omitempty"`
	Batch      []BatchItem `json:"batch,omitempty"`
	HoldingUp  []string    `json:"holding_up,omitempty"`
	// Blocking is whether the engine KNOWS something waits on this ask: true
	// for a permission (its call is blocked) and for the model's own question
	// that parks its turn or names work waiting on it; false only for the
	// model's own question whose turn carries on and that names no work, the
	// one kind whose answer arrives later as a message. Nil is unknown, which
	// is every other kind and every older engine, and a surface reads unknown
	// as waiting. An empty HoldingUp never says false: a parked reply waits
	// on no task.
	Blocking *bool `json:"blocking,omitempty"`
	SayWhy   bool  `json:"say_why,omitempty"`
	Later    bool  `json:"later,omitempty"`
	// Path is the crews a crew's decision climbed through to the person,
	// "Backend → Release → you".
	Path []string `json:"path,omitempty"`
	// DecisionKind is, on a crew_decision ask only, the decision packet's
	// kind: question, conflict, cap, judgement or closing (Decision.Kind).
	DecisionKind string `json:"decision_kind,omitempty" enum:"question,conflict,cap,judgement,closing"`
	// Subject is what the ask is about, so the app draws that thing's own
	// card: an automation waiting to be agreed to, a task, a tool call, a crew
	// proposal or decision. Nil is an ask about nothing in particular.
	Subject *AskSubject `json:"subject,omitempty"`
	// Proposal is a crew proposal's whole shape, on its Bring in helpers ask:
	// the crew's name, the helpers with their handles and roles, the cap, the
	// goal. It is kept in the app layer until it is answered, so the ask is
	// asked again after a restart.
	Proposal *Proposal `json:"proposal,omitempty"`
	// KeepRunning is a keep-running ask's whole proposal (services.go): the
	// exact command and folder, its one check, the proposed policy, and the
	// fixed timings it will run under.
	KeepRunning *KeepRunning `json:"keep_running,omitempty"`
}

// AskSubject is the thing an ask is about: its kind and the id the app knows
// it by.
type AskSubject struct {
	Kind string `json:"kind" enum:"automation,task,call,run,page,account,crew_proposal,crew_decision,budget,service"`
	Ref  string `json:"ref,omitempty"`
}

// The kinds of an ask's subject.
const (
	SubjectAutomation   = "automation"
	SubjectTask         = "task"
	SubjectCall         = "call"
	SubjectRun          = "run"
	SubjectPage         = "page"
	SubjectAccount      = "account"
	SubjectCrewProposal = "crew_proposal"
	SubjectCrewDecision = "crew_decision"
	SubjectBudget       = "budget"
)

// The subkinds of an approval, and the option ids a permission offers.
const (
	SubkindPermission = "permission"
	SubkindBudget     = "budget"

	OptionAllowOnce = "allow_once"
	OptionDeny      = "deny"
	OptionAlways    = "always"

	// A budget ask about one agent offers raise (its text is the new figure),
	// stop and tell (its text is what to tell the agent); one about the day cap
	// offers raise_today and wait.
	OptionRaise      = "raise"
	OptionStop       = "stop"
	OptionTell       = "tell"
	OptionRaiseToday = "raise_today"
	OptionWait       = "wait"
)

// Entry is one item of a conversation's transcript, in order. Which of the
// optional fields are set depends on Kind.
// EntryLink points from a receipt to the setting that controls its rule.
type EntryLink struct {
	Label  string `json:"label"`
	Target string `json:"target"`
}

type Entry struct {
	Link *EntryLink `json:"link,omitempty"`
	Why  string     `json:"why,omitempty"`

	RunID string    `json:"run_id,omitempty"`
	ID    string    `json:"id"`
	Agent string    `json:"agent"`
	Turn  uint64    `json:"turn,omitempty"`
	Kind  string    `json:"kind" enum:"user,answer,tool,receipt,notice,error,update,steer,note,summary,footer"`
	At    time.Time `json:"at"`
	// Text is the words of a user, answer, receipt, notice or error entry.
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	// ClientID echoes the id the app sent with the person's message, on that
	// message's user entry.
	ClientID string `json:"client_id,omitempty"`
	// Queued is true on a user entry the engine sent from the person's queue
	// (agents.queue) when the turn it waited behind ended, as the record keeps
	// it on the message's own line. Absent is "not known to be queued": a
	// message sent at once, or a line written before the mark existed.
	Queued bool `json:"queued,omitempty"`
	// Msg is an answer's place among the assistant messages of its turn,
	// counted from zero; the turn.delta lines of that message carry the same.
	Msg *int `json:"msg,omitempty"`
	// Call, Name, Title, Detail, OK and Summary describe a tool entry: Title
	// is the one line the terminal shows for the call, Detail its short result.
	Call    string `json:"call,omitempty"`
	Name    string `json:"name,omitempty"`
	Title   string `json:"title,omitempty"`
	Detail  string `json:"detail,omitempty"`
	OK      *bool  `json:"ok,omitempty"`
	Summary string `json:"summary,omitempty"`
	// Ref is what a notice points at, and Quiet marks a notice the terminal
	// shows as one dim line; ErrorKind and Status sort an error.
	Ref       string `json:"ref,omitempty"`
	Quiet     bool   `json:"quiet"`
	ErrorKind string `json:"error_kind,omitempty"`
	Status    int    `json:"status,omitempty"`
	// NoticeKind, Skills and Unrecorded are typed facts the engine attached to a
	// notice at its producer: "skills" (the skills a turn carried, named in
	// Skills), "memory" (a memory line), "factory_status", or a local "sync",
	// "moved" or "interrupted" sentence. Unrecorded says no transcript record
	// redraws it. A consumer never derives these facts from Text.
	NoticeKind string   `json:"notice_kind,omitempty" enum:"skills,memory,factory_status,sync,moved,interrupted"`
	Skills     []string `json:"skills,omitempty"`
	Unrecorded bool     `json:"unrecorded,omitempty"`
	// A steer's and a note's state, the step a steer landed at and the step a
	// note was read at; a note's direction; a footer's kind and figures; the
	// files and links a message of the person's carried (convo.go).
	State       string       `json:"state,omitempty"`
	Step        *int         `json:"step,omitempty"`
	From        string       `json:"from,omitempty"`
	ReadAtStep  *int         `json:"read_at_step,omitempty"`
	Footer      string       `json:"footer_kind,omitempty"`
	RetryInS    *int         `json:"retry_in_s,omitempty"`
	ModelFrom   string       `json:"model_from,omitempty"`
	ModelTo     string       `json:"model_to,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	// Reads, on a person's message, are the reads of references its words
	// were written from, as the app found them when it sent it (convo.go's
	// [MessageRead]); absent on every message sent without them.
	Reads []MessageRead `json:"reads,omitempty"`
	// MessageID is the durable identity the engine minted when it accepted a
	// person's message that carried pictures; Delivery says how the pictures
	// reached a model and SeenBy names the vision model that looked when it was
	// not the one talking; Annotation is what the person marked on them
	// (media_submission.go). All absent on every other entry.
	MessageID  string      `json:"message_id,omitempty"`
	Delivery   string      `json:"delivery,omitempty" enum:"text,image,description"`
	SeenBy     string      `json:"seen_by,omitempty"`
	Annotation *Annotation `json:"annotation,omitempty"`
	// Op is a durable fact the conversation's record keeps beside its
	// messages, said as the fact and never as words: the app words it.
	Op *EntryOp `json:"op,omitempty"`
	// Progress is, on an answer the factory runner wrote into a job's own
	// conversation, the step round that line reports on and what happened to
	// it. It is the runner's word, never read out of Text, which keeps the
	// line's words for a surface that shows them; absent on every other entry,
	// and on the runner's lines about the job as a whole.
	Progress *EntryProgress `json:"progress,omitempty"`
	// Compaction is a summary entry's pass, as facts (compaction.go).
	Compaction *Compaction `json:"compaction,omitempty"`
	// Team is, on a notice that is a crew's delivery, its lines as facts: who
	// sent each, to whom, its kind and its log entry. The notice's Text is the
	// same lines worded.
	Team []TeamLine `json:"team,omitempty"`
	// A tool entry carries the facts of its call (worklog.go); a note entry
	// names its task there too.
	ToolFacts
}

// EntryProgress is the step a factory runner's line in a job's conversation
// reports on: its name as the job's steps spell it, the round (absent for a
// step with no rounds, an approve step), and what the line says happened.
// started and done are the round beginning and ending; failed and stopped are
// the run ending under it, on the step's error or a person's stop; waiting is
// the step waiting on a question put to the person.
type EntryProgress struct {
	Step  string `json:"step"`
	Round int    `json:"round,omitempty"`
	Event string `json:"event" enum:"started,done,failed,stopped,waiting"`
}

// TeamLine is one line of a crew's delivery to a member. From is "manager",
// "you", "system" or a member's handle; To is "", "room", "everyone",
// "manager" or a handle; Kind is note, directive or start (the brief the
// member was started with); Post is the line's log entry, "" when the
// delivery did not number it.
type TeamLine struct {
	CrewName string `json:"crew_name,omitempty"`
	From     string `json:"from"`
	To       string `json:"to,omitempty"`
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	Post     string `json:"post,omitempty"`
}

// EntryOp is an operation the person or an agent did that the conversation
// remembers. It rides a receipt entry whose Text is empty; the entry's id and
// At are the same live and after a restart.
type EntryOp struct {
	Kind string `json:"kind" enum:"crew.formed"`
	// Crew is the crew's id and Name its name; the entry's Agent is the
	// manager it was made under.
	Crew string `json:"crew,omitempty"`
	Name string `json:"name,omitempty"`
}

// OpCrewFormed is a crew made under this conversation's agent as its manager.
const OpCrewFormed = "crew.formed"

// The kinds of entry.
const (
	EntryUser    = "user"
	EntryAnswer  = "answer"
	EntryTool    = "tool"
	EntryReceipt = "receipt"
	EntryNotice  = "notice"
	EntryError   = "error"
	// Wave 3b: an interim message mid-turn, a steer, a note to or from a
	// task, an earlier stretch summarized, and a turn's footer.
	EntryUpdate  = "update"
	EntrySteer   = "steer"
	EntryNote    = "note"
	EntrySummary = "summary"
	EntryFooter  = "footer"
)

// The kinds a turn's failure is sorted into.
const (
	ErrorProvider = "provider"
	ErrorBudget   = "budget"
	ErrorStopped  = "stopped"
	ErrorOther    = "other"
	// ErrorUncertain is a turn whose words were handed to the conversation and
	// no answer came back: they MAY HAVE BEEN SENT, so no retry is offered for
	// them. It is additive: an app that does not know the word reads a failure.
	ErrorUncertain = "uncertain"
)

// Model is one model the configured keys can use.
type Model struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	// ListedAt is the provider catalog listing time in Unix seconds; absent is unknown.
	ListedAt int64 `json:"listed_at,omitempty"`
	// ProviderName is the provider's own name, as the picker heads its group.
	ProviderName string `json:"provider_name,omitempty"`
	// Default marks the model a new conversation runs on.
	Default bool `json:"default,omitempty"`
}

// FeatureAgentEffortRungs announces the terminal's full conversation effort dial.
const FeatureAgentEffortRungs = "agents.set_effort.rungs"

// These original effort spellings remain available to existing callers.
const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
)

// Settings are the few defaults the app shows. A figure nobody set is null.
// WorkFolder is where an agent created with no workspace works;
// ProposedTaskClockS is how long a proposed task waits before it starts on its
// own; IdleCloseDays is how long an agent nobody touches stays open.
type Settings struct {
	DefaultModel       string   `json:"default_model"`
	BudgetUSD          *float64 `json:"budget_usd"`
	AsksBefore         string   `json:"asks_before"`
	DayCapUSD          *float64 `json:"day_cap_usd"`
	WorkFolder         string   `json:"work_folder"`
	TaskCapUSD         *float64 `json:"task_cap_usd"`
	ProposedTaskClockS int      `json:"proposed_task_clock_s"`
	IdleCloseDays      int      `json:"idle_close_days"`
	Effort             string   `json:"effort,omitempty" enum:"low,medium,high"`
	// The General rows the window draws by (Wave 8): kept here so every window
	// reads the same answer.
	Appearance  string `json:"appearance" enum:"system,light,dark"`
	TextSize    string `json:"text_size" enum:"small,medium,large"`
	OpenAtLogin bool   `json:"open_at_login"`
	DockBadge   bool   `json:"dock_badge"`
	// The Models rows: the pinned models (⌥⌘1–3), the model checks run on and
	// the one plans are made on (empty follows the default), and the model a
	// call falls to when its own cannot answer (empty for none).
	PinnedModels  []string `json:"pinned_models"`
	ChecksModel   string   `json:"checks_model"`
	PlannerModel  string   `json:"planner_model"`
	FallbackModel string   `json:"fallback_model"`
}

// SettingsPatch is settings.set's params: only the fields present change. A
// money figure sent as null is cleared, which the settings that always have a
// figure refuse.
type SettingsPatch struct {
	DefaultModel       *string
	BudgetUSD          *Figure
	AsksBefore         *string
	DayCapUSD          *Figure
	WorkFolder         *string
	TaskCapUSD         *Figure
	ProposedTaskClockS *int
	IdleCloseDays      *int
	Effort             *string
	Appearance         *string
	TextSize           *string
	OpenAtLogin        *bool
	DockBadge          *bool
	ChecksModel        *string
	PlannerModel       *string
	FallbackModel      *string
}

// Figure is one money field of a patch: USD is nil for a figure sent as null.
type Figure struct {
	USD *float64
}

// Provider is one service a key or a sign-in connects. KeyHint is the last
// four characters of the key in use, and SignIn says the provider can be
// connected by a browser sign-in instead of a pasted key.
//
// The rest says what connecting it asks for, which is what the terminal's own
// connect flow asks for that provider and nothing more.
type Provider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SignedIn bool   `json:"signed_in"`
	KeyHint  string `json:"key_hint,omitempty"`
	SignIn   bool   `json:"sign_in,omitempty"`
	// Kind is the shape of connecting it: a pasted key (key, which a reader
	// that predates this field assumes of every provider), a browser sign-in
	// and nothing else (browser), or where the service runs, with a key only if
	// it wants one (address).
	Kind string `json:"kind,omitempty" enum:"key,browser,address"`
	// KeyOptional says the key may be left blank: the service answers with none.
	KeyOptional bool `json:"key_optional,omitempty"`
	// Address is an address provider's address: the one in use when it is
	// connected, otherwise the one its field starts with (empty for a custom
	// endpoint, which has no usual place).
	Address string `json:"address,omitempty"`
	// Regions are a provider's separate hosts, each a separate account with its
	// own keys, of which one is chosen before the key is checked; Region is the
	// one in use.
	Regions []ProviderRegion `json:"regions,omitempty"`
	Region  string           `json:"region,omitempty"`
	// Plan is how a connected key is billed, in the provider's own word for it
	// ("coding plan", "pay-as-you-go"), for a provider that has more than one.
	Plan string `json:"plan,omitempty"`
	// AuthKind is account for a provider connected by signing in to an
	// account rather than by a key; SignInAttempt, SignInState and SignInError
	// are its sign-in in progress or just ended, if any
	// (providers.sign_in_start).
	AuthKind      string `json:"auth_kind,omitempty" enum:"account"`
	SignInAttempt string `json:"sign_in_attempt,omitempty"`
	SignInState   string `json:"sign_in_state,omitempty" enum:"pending,saving,signed_in,failed,cancelled"`
	SignInError   string `json:"sign_in_error,omitempty"`
}

// The three shapes of connecting a provider ([Provider.Kind]).
const (
	ProviderKey     = "key"
	ProviderBrowser = "browser"
	ProviderAddress = "address"
)

// ProviderRegion is one of a provider's separate hosts.
type ProviderRegion struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// KeyCheck is keys.check's answer. Status is the provider's own status code
// for a rejection, the word timeout when the provider never answered in time,
// unreachable when it could not be reached at all, or shape when codeaf
// refused the key before asking (Message then says what shape it wants).
type KeyCheck struct {
	OK      bool    `json:"ok"`
	Models  []Model `json:"models,omitempty"`
	Status  any     `json:"status,omitempty"`
	Message string  `json:"message,omitempty"`
	// Connection is what a passing check proved beyond the key, handed back to
	// keys.set as it came so the provider is kept as it was proved. Absent for
	// OpenRouter, whose key is the whole of its connection.
	Connection *Connection `json:"connection,omitempty"`
}

// Connection is a provider's connection without its key. Nothing in it is a
// secret, and codeaf keeps it in its own profile: the region asked for, the
// billing road the key opened (Door, with Plan its word), whether the provider
// answered with its list of models, and the address an address provider is at.
type Connection struct {
	Region  string `json:"region,omitempty"`
	Door    string `json:"door,omitempty"`
	Plan    string `json:"plan,omitempty"`
	Listed  *bool  `json:"listed,omitempty"`
	Address string `json:"address,omitempty"`
}

// Setup is how far the person has come through first-run setup, kept by
// codeaf so quitting halfway resumes at the same step.
type Setup struct {
	Step         SetupStep `json:"step"`
	Provider     string    `json:"provider,omitempty"`
	DefaultModel string    `json:"default_model,omitempty"`
	DayCapUSD    *float64  `json:"day_cap_usd,omitempty"`
	Start        string    `json:"start,omitempty"`
}

// SetupStep is a setup step: 1 to [SetupSteps] on the wire as a number, or
// "done".
type SetupStep struct {
	N    int
	Done bool
}

// The number of setup steps, and the one place a setup may say it starts.
//
// SETUP IS THREE STEPS: connecting providers (behind the welcome), the
// defaults, and how to start. It was four until the welcome and the connecting
// step became one, and a setup kept by a build from before then counts its
// step in four; codeaf reads such a step into three where it keeps it
// (cmd/codeaf/appstate.go), so the wire only ever carries the three.
const (
	SetupSteps  = 3
	SetupAgents = "agents"
)

// setupStepWords is what a step that is not one says, on the wire and in the
// schema alike.
var setupStepWords = fmt.Sprintf("step is 1 to %d or \"done\"", SetupSteps)

func (s SetupStep) MarshalJSON() ([]byte, error) {
	if s.Done {
		return []byte(`"done"`), nil
	}
	return json.Marshal(s.N)
}

// UnmarshalJSON reads any step from one up, and "done". IT DOES NOT REFUSE A
// STEP PAST THE LAST, because the same type is read back from the file codeaf
// keeps, and a step a four-step build kept there must be read to be moved
// into three, never refused along with the whole file; setup.set refuses a
// step past [SetupSteps] itself ([SetupStep.Valid]).
func (s *SetupStep) UnmarshalJSON(raw []byte) error {
	var word string
	if json.Unmarshal(raw, &word) == nil {
		if word != "done" {
			return errors.New(setupStepWords)
		}
		*s = SetupStep{Done: true}
		return nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil || n < 1 {
		return errors.New(setupStepWords)
	}
	*s = SetupStep{N: n}
	return nil
}

// Valid says the step is one a setup can be at now: "done", or 1 to
// [SetupSteps].
func (s SetupStep) Valid() bool {
	return s.Done || s.N >= 1 && s.N <= SetupSteps
}

// wireSchema is the step as it travels, a number or the word, because its
// JSON is not its fields.
func (SetupStep) wireSchema() map[string]any {
	return map[string]any{
		"anyOf": []any{
			map[string]any{"type": "integer", "minimum": 1, "maximum": SetupSteps},
			map[string]any{"const": "done"},
		},
		"description": setupStepWords,
	}
}

// CapRaise is caps.raise's params: which cap, to what, and whether the raise
// lasts until midnight (for: today) or from now on (for: good).
type CapRaise struct {
	Scope string  `json:"scope"`
	ID    string  `json:"id"`
	ToUSD float64 `json:"to_usd"`
	For   string  `json:"for"`
}

// The two scopes a cap is raised in, and how long a raise lasts.
const (
	CapAgent = "agent"
	CapDay   = "day"

	RaiseToday = "today"
	RaiseGood  = "good"
)

// Spend is today's spending on this machine against the daily cap, which is
// null when there is none.
type Spend struct {
	TodayUSD  float64  `json:"today_usd"`
	DayCapUSD *float64 `json:"day_cap_usd"`
}

// AskOption is one answer an ask offers.
type AskOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// The data of each event, one struct per name so the field spellings live in
// one place.
type (
	AgentChanged struct {
		Agent Agent `json:"agent"`
	}
	TurnStarted struct {
		Agent string `json:"agent"`
		Turn  uint64 `json:"turn"`
	}
	TurnDelta struct {
		Agent string `json:"agent"`
		Turn  uint64 `json:"turn"`
		Msg   int    `json:"msg"`
		Kind  string `json:"kind"`
		Text  string `json:"text"`
	}
	TurnTool struct {
		RunID   string `json:"run_id,omitempty"`
		Agent   string `json:"agent"`
		Turn    uint64 `json:"turn"`
		Call    string `json:"call"`
		Name    string `json:"name"`
		Title   string `json:"title,omitempty"`
		Detail  string `json:"detail,omitempty"`
		Phase   string `json:"phase"`
		OK      *bool  `json:"ok,omitempty"`
		Summary string `json:"summary,omitempty"`
		ToolFacts
	}
	TurnDone struct {
		Agent     string `json:"agent"`
		Turn      uint64 `json:"turn"`
		OK        bool   `json:"ok"`
		Error     string `json:"error,omitempty"`
		ErrorKind string `json:"error_kind,omitempty"`
		Status    int    `json:"status,omitempty"`
	}
	AskOpened struct {
		Ask Ask `json:"ask"`
	}
	AskClosed struct {
		Agent  string    `json:"agent"`
		ID     string    `json:"id"`
		By     string    `json:"by"`
		Option string    `json:"option,omitempty"`
		At     time.Time `json:"at"`
	}
	EntryAdded struct {
		Entry Entry `json:"entry"`
	}
	SpendChanged struct {
		Spend Spend `json:"spend"`
	}
	TaskChanged struct {
		Task Task `json:"task"`
	}
	CrewChanged struct {
		Crew Crew `json:"crew"`
	}
	CrewRemoved struct {
		ID string `json:"id"`
	}
	Log struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	SettingsChanged struct {
		Settings Settings `json:"settings"`
		// TaskModels is settings.models.get's answer as it reads now, sent by
		// an engine that serves Task models whenever settings change; absent
		// from an engine that does not ([TaskModelsLister]).
		TaskModels *TaskModels `json:"task_models,omitempty"`
	}
	// ProviderChanged carries the key a browser sign-in produced, once, so
	// the app can keep it in the keychain and hand it back at the next launch.
	ProviderChanged struct {
		Provider Provider `json:"provider"`
		Key      string   `json:"key,omitempty"`
	}
)

// The two delta kinds.
const (
	DeltaText     = "text"
	DeltaThinking = "thinking"
)

// The two tool phases.
const (
	PhaseBegin = "begin"
	PhaseEnd   = "end"
)

// Emitter is how an engine puts an event on the wire. Emit may block while the
// writer is behind, so an engine never calls it from a loop that must keep
// reading; it hands agent-scoped events to an [Outbox] instead, which does.
type Emitter interface {
	Emit(event string, data any)
}

// Engine is everything behind the methods. Each call is made on its own
// goroutine, so a slow create never holds up a ping, and each answers either a
// result or an error ([Error] for a code the app can act on).
type Engine interface {
	ListAgents(workspace string) ([]Agent, error)
	CreateAgent(ask CreateAsk) (Agent, error)
	// Entries answers up to limit transcript entries before the one named
	// (the newest when before is empty), oldest first, and whether there are
	// older ones still. It reads the disk for an agent nothing holds.
	Entries(id, before string, limit int) ([]Entry, bool, error)
	SetModel(id, model string) (Agent, error)
	Retry(id string) (uint64, error)
	OpenAgent(id string) (Agent, error)
	// Send starts a turn; clientID is echoed on the person's entry.
	Send(id, text, clientID string) (uint64, error)
	SetEffort(id, effort string) (Agent, error)
	Stop(id string) error
	CloseAgent(id string) error
	ListAsks(agent string) ([]Ask, error)
	// AnswerAsk answers one open ask with an option, words, or both. An ask
	// that is no longer open answers not_found.
	AnswerAsk(agent, id, option, text string) error
	ListTasks(agent string) ([]Task, error)
	ListCrews() ([]Crew, error)
	ListModels() ([]Model, error)
	Settings() (Settings, error)
	SpendToday() (Spend, error)
	// Wave 2: providers and keys, first-run setup, settings and caps. A key
	// is held in memory only; CheckKey makes exactly one provider request.
	ListProviders() ([]Provider, error)
	CheckKey(provider, key string) (KeyCheck, error)
	SetKey(provider, key string) error
	RemoveKey(provider string) error
	SignIn(provider string) (string, error)
	Setup() (Setup, error)
	SetSetup(setup Setup) (Setup, error)
	SetSettings(patch SettingsPatch) (Settings, error)
	RaiseCap(raise CapRaise) error
	// Close releases every conversation the engine holds. It is called once,
	// when the process is leaving, and returns when they are all let go.
	Close() error
}

// ProviderConnector is an engine that connects every kind of provider the
// terminal connects: a key on one of a provider's regions, an address with or
// without a key. keys.check and keys.set hand it the whole request, an empty
// key included, and it says which providers take none; an engine that is not
// one is asked the two-field questions above and both need a key.
type ProviderConnector interface {
	CheckProvider(params KeyParams) (KeyCheck, error)
	SetProvider(params KeyParams) error
}

// CreateAsk is agents.create's params. An empty workspace is the person's home
// folder; a non-empty Text, or any Attachments, starts the first turn at once.
//
// Attachments ride on that first message exactly as staged ones ride on
// agents.send: the person's entry carries them as [Attachment]s and the model
// is sent their words. They are checked whole — shape, bounds, and that each
// file is there — BEFORE the conversation is made, so a refused create leaves
// no agent behind. With attachments the text may be empty.
//
// It answers at once: the conversation is assembled behind the answer.
type CreateAsk struct {
	Models          *session.ConversationModels `json:"models,omitempty"`
	AutomaticModels *bool                       `json:"automatic_models,omitempty"`
	Workspace       string                      `json:"workspace"`
	Model           string                      `json:"model"`
	BudgetUSD       *float64                    `json:"budget_usd"`
	Text            string                      `json:"text"`
	ClientID        string                      `json:"client_id"`
	Attachments     []AttachSource              `json:"attachments,omitempty"`
	// Said is the person's own words for the first message: what its entry and
	// the record show in place of Text, as agents.send's said does. Absent is Text.
	Said *string `json:"said,omitempty"`
}

// Warmer is an engine with something slow it can start on before anything is
// asked of it. The server calls Warm once, on its own goroutine, after the
// hello line is queued, so warming never delays the hello.
type Warmer interface {
	Warm()
}

// frame is one line on the way out: a response (ID set) or an event (Event
// set). The event's sequence number is assigned by the writer, at the moment
// it is written, so the numbers are in the order the app reads them.
type frame struct {
	id     uint64
	result any
	err    *Error
	event  string
	data   any
}

type responseLine struct {
	ID     uint64 `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

type eventLine struct {
	Event string `json:"event"`
	Seq   uint64 `json:"seq"`
	Data  any    `json:"data"`
}

// encode is the one json encoding a frame gets.
func (f frame) encode(seq uint64) ([]byte, error) {
	if f.event != "" {
		data := f.data
		if data == nil {
			data = struct{}{}
		}
		return json.Marshal(eventLine{Event: f.event, Seq: seq, Data: data})
	}
	if f.err != nil {
		return json.Marshal(responseLine{ID: f.id, Error: f.err})
	}
	result := f.result
	if result == nil {
		result = struct{}{}
	}
	return json.Marshal(responseLine{ID: f.id, Result: result})
}
