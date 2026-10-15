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
)

// Error is a refusal the app can read: a code from the closed set above and a
// sentence. An engine returns one when it knows which code applies; any other
// error reaching the dispatcher is answered as [CodeEngine] with its text.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Errorf builds an [Error] with a formatted sentence.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Hello is the first line codeaf writes, before it reads anything.
type Hello struct {
	Contract int    `json:"contract"`
	Version  string `json:"version"`
	PID      int    `json:"pid"`
	Home     string `json:"home"`
}

// Agent is one conversation as the app sees it. ID is the conversation's
// session id, which is its folder's name on disk and stable across launches.
type Agent struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Workspace string    `json:"workspace"`
	Model     string    `json:"model,omitempty"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Task is one node of an agent's task graph.
type Task struct {
	ID        string    `json:"id"`
	Agent     string    `json:"agent"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Crew is one codeaf team, with its members named by agent id.
type Crew struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Manager string       `json:"manager,omitempty"`
	Members []CrewMember `json:"members"`
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
	Kind    string `json:"kind"`
	Subkind string `json:"subkind,omitempty"`
	Title   string `json:"title"`
	Detail  string `json:"detail,omitempty"`
	// Options carry the engine's own ids; for a permission they are
	// allow_once, deny and, unless the call is irreversible, always.
	Options      []AskOption `json:"options,omitempty"`
	Irreversible bool        `json:"irreversible,omitempty"`
}

// The subkinds of an approval, and the option ids a permission offers.
const (
	SubkindPermission = "permission"
	SubkindBudget     = "budget"

	OptionAllowOnce = "allow_once"
	OptionDeny      = "deny"
	OptionAlways    = "always"
)

// Entry is one item of a conversation's transcript, in order. Which of the
// optional fields are set depends on Kind.
type Entry struct {
	ID    string    `json:"id"`
	Agent string    `json:"agent"`
	Turn  uint64    `json:"turn,omitempty"`
	Kind  string    `json:"kind"`
	At    time.Time `json:"at"`
	// Text is the words of a user, answer, receipt, notice or error entry.
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	// Call, Name, OK and Summary describe a tool entry.
	Call    string `json:"call,omitempty"`
	Name    string `json:"name,omitempty"`
	OK      *bool  `json:"ok,omitempty"`
	Summary string `json:"summary,omitempty"`
	// Ref is what a notice points at; ErrorKind and Status sort an error.
	Ref       string `json:"ref,omitempty"`
	ErrorKind string `json:"error_kind,omitempty"`
	Status    int    `json:"status,omitempty"`
}

// The kinds of entry.
const (
	EntryUser    = "user"
	EntryAnswer  = "answer"
	EntryTool    = "tool"
	EntryReceipt = "receipt"
	EntryNotice  = "notice"
	EntryError   = "error"
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
}

// Settings are the few defaults the app shows. A figure nobody set is null.
type Settings struct {
	DefaultModel string   `json:"default_model"`
	BudgetUSD    *float64 `json:"budget_usd"`
	AsksBefore   string   `json:"asks_before"`
	DayCapUSD    *float64 `json:"day_cap_usd"`
}

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

// CrewMember is one conversation in a crew.
type CrewMember struct {
	Agent  string `json:"agent"`
	Handle string `json:"handle,omitempty"`
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
		Kind  string `json:"kind"`
		Text  string `json:"text"`
	}
	TurnTool struct {
		Agent   string `json:"agent"`
		Turn    uint64 `json:"turn"`
		Call    string `json:"call"`
		Name    string `json:"name"`
		Phase   string `json:"phase"`
		OK      *bool  `json:"ok,omitempty"`
		Summary string `json:"summary,omitempty"`
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
	Send(id, text string) (uint64, error)
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
	// Close releases every conversation the engine holds. It is called once,
	// when the process is leaving, and returns when they are all let go.
	Close() error
}

// CreateAsk is agents.create's params. An empty workspace is the person's home
// folder; a non-empty Text starts the first turn at once.
type CreateAsk struct {
	Workspace string   `json:"workspace"`
	Model     string   `json:"model"`
	BudgetUSD *float64 `json:"budget_usd"`
	Text      string   `json:"text"`
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
