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
	"fmt"
	"time"
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
	CodeNotFound      = "not_found"
	CodeBusy          = "busy"
	CodeEngine        = "engine"
	CodeClosed        = "closed"
	// CodeCycle refuses a collections.add that would make a collection hold
	// itself, at any depth.
	CodeCycle = "cycle"
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
	EventHello        = "hello"
	EventAgentChanged = "agent.changed"
	EventTurnStarted  = "turn.started"
	EventTurnDelta    = "turn.delta"
	EventTurnTool     = "turn.tool"
	EventTurnDone     = "turn.done"
	EventAskOpened    = "ask.opened"
	EventAskClosed    = "ask.closed"
	EventEntryAdded   = "entry.added"
	EventSpendChanged = "spend.changed"
	EventTaskChanged  = "task.changed"
	EventCrewChanged  = "crew.changed"
	EventCrewRemoved  = "crew.removed"
	EventLog          = "log"

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
type Agent struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Workspace string `json:"workspace"`
	Model     string `json:"model,omitempty"`
	// Effort is how hard the model is asked to think: low, medium or high,
	// and empty when the conversation runs on the model's own default.
	Effort    string    `json:"effort,omitempty"`
	State     string    `json:"state" enum:"idle,working,needs_you,stopped,closed,archived"`
	UpdatedAt time.Time `json:"updated_at"`
	// Crews are the crews this agent is a member of, open ones only.
	Crews []string `json:"crews,omitempty"`
	// Line is its live one-liner: the step it is on while it works, or the
	// question it is waiting on.
	Line string `json:"line,omitempty"`
	// ClosedAt is when it was moved to Closed and ArchivedAt when it was put
	// away; LastActiveAt is when the person last spoke in it, and SpentUSD
	// what it has spent in all.
	ClosedAt     *time.Time `json:"closed_at,omitempty"`
	ArchivedAt   *time.Time `json:"archived_at,omitempty"`
	LastActiveAt time.Time  `json:"last_active_at"`
	SpentUSD     float64    `json:"spent_usd,omitempty"`
	// Context is what the conversation weighs against its window
	// (compaction.go); absent when nothing holds the conversation.
	Context *AgentContext `json:"context,omitempty"`
}

// Task is one node of an agent's task graph.
type Task struct {
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
}

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
	Subkind string `json:"subkind,omitempty" enum:"permission,budget,crew_decision,crew_proposal,order_run"`
	Title   string `json:"title"`
	Detail  string `json:"detail,omitempty"`
	// Options carry the engine's own ids; for a permission they are
	// allow_once, deny and, unless the call is irreversible, always.
	Options      []AskOption `json:"options,omitempty"`
	Irreversible bool        `json:"irreversible,omitempty"`
	// The decision dock's facts (V68): where the ask comes from, why now and
	// the evidence, the choices with the asker's suggestion, the clock, the
	// calls asked about together, the tasks it holds up, and whether a reason
	// may go with the answer. Later says the person put it aside.
	Source     *AskSource  `json:"source,omitempty"`
	Reason     string      `json:"reason,omitempty"`
	Evidence   *Evidence   `json:"evidence,omitempty"`
	Choices    []AskChoice `json:"choices,omitempty"`
	ClockUntil *time.Time  `json:"clock_until,omitempty"`
	Batch      []BatchItem `json:"batch,omitempty"`
	HoldingUp  []string    `json:"holding_up,omitempty"`
	SayWhy     bool        `json:"say_why,omitempty"`
	Later      bool        `json:"later,omitempty"`
	// Path is the crews a crew's decision climbed through to the person,
	// "Backend → Release → you".
	Path []string `json:"path,omitempty"`
	// Subject is what the ask is about, so the app draws that thing's own
	// card: an automation waiting to be agreed to, a task, a tool call, a crew
	// proposal or decision. Nil is an ask about nothing in particular.
	Subject *AskSubject `json:"subject,omitempty"`
	// Proposal is a crew proposal's whole shape, on its Bring in helpers ask:
	// the crew's name, the helpers with their handles and roles, the cap, the
	// goal. It is kept in the app layer until it is answered, so the ask is
	// asked again after a restart.
	Proposal *Proposal `json:"proposal,omitempty"`
}

// AskSubject is the thing an ask is about: its kind and the id the app knows
// it by.
type AskSubject struct {
	Kind string `json:"kind" enum:"automation,task,call,run,page,account,crew_proposal,crew_decision,budget"`
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
type Entry struct {
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
	Quiet     bool   `json:"quiet,omitempty"`
	ErrorKind string `json:"error_kind,omitempty"`
	Status    int    `json:"status,omitempty"`
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
	// Op is a durable fact the conversation's record keeps beside its
	// messages, said as the fact and never as words: the app words it.
	Op *EntryOp `json:"op,omitempty"`
	// Compaction is a summary entry's pass, as facts (compaction.go).
	Compaction *Compaction `json:"compaction,omitempty"`
	// A tool entry carries the facts of its call (worklog.go); a note entry
	// names its task there too.
	ToolFacts
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
)

// Model is one model the configured keys can use.
type Model struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	// ProviderName is the provider's own name, as the picker heads its group.
	ProviderName string `json:"provider_name,omitempty"`
	// Default marks the model a new conversation runs on.
	Default bool `json:"default,omitempty"`
}

// The three efforts agents.set_effort takes.
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
type Provider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SignedIn bool   `json:"signed_in"`
	KeyHint  string `json:"key_hint,omitempty"`
	SignIn   bool   `json:"sign_in,omitempty"`
}

// KeyCheck is keys.check's answer. Status is the provider's own status code
// for a rejection, or the word timeout when the provider never answered.
type KeyCheck struct {
	OK      bool    `json:"ok"`
	Models  []Model `json:"models,omitempty"`
	Status  any     `json:"status,omitempty"`
	Message string  `json:"message,omitempty"`
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

// SetupStep is a setup step: 1 to 4 on the wire as a number, or "done".
type SetupStep struct {
	N    int
	Done bool
}

// The number of setup steps, and the one place a setup may say it starts.
const (
	SetupSteps  = 4
	SetupAgents = "agents"
)

func (s SetupStep) MarshalJSON() ([]byte, error) {
	if s.Done {
		return []byte(`"done"`), nil
	}
	return json.Marshal(s.N)
}

func (s *SetupStep) UnmarshalJSON(raw []byte) error {
	var word string
	if json.Unmarshal(raw, &word) == nil {
		if word != "done" {
			return fmt.Errorf("step is 1 to %d or \"done\"", SetupSteps)
		}
		*s = SetupStep{Done: true}
		return nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil || n < 1 || n > SetupSteps {
		return fmt.Errorf("step is 1 to %d or \"done\"", SetupSteps)
	}
	*s = SetupStep{N: n}
	return nil
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
	Workspace   string         `json:"workspace"`
	Model       string         `json:"model"`
	BudgetUSD   *float64       `json:"budget_usd"`
	Text        string         `json:"text"`
	ClientID    string         `json:"client_id"`
	Attachments []AttachSource `json:"attachments,omitempty"`
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
