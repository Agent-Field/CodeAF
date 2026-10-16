package appwire

import (
	"encoding/json"
	"strings"
	"time"
	"unicode"
)

// ── session.act: THREE NAMED ACTIONS THE TERMINAL ALREADY HAS, TYPED ────────
//
// `/task <brief>`, `/<program> <brief>` and `/remember <text>` are Bubble Tea
// handlers in the terminal; the app had no verb for them, and `agents.send
// "/review"` is a message to a MODEL, not a command. This file is the wire for
// the three producers underneath those handlers (session.Agent.StartTask,
// StartDelegate, Remember), named for what they do and not for a label or a
// slash line. It adds no runner, catalogue, route, default, cap or policy: the
// engine calls the producers unchanged, so their own refusals are the whole
// authority and an explicit caller is never a bypass of any of them.
//
// WRITES ARE IDEMPOTENT BY A CLIENT-CHOSEN, IMMUTABLE client_id, recorded
// durably BEFORE the producer is called. One client_id is one attempt, ever:
// a repeat is answered from the record and never executed, and an attempt
// whose outcome cannot be known is `uncertain` and is never run again by
// anybody but a person with a new client_id. The engine's policy lives in
// cmd/codeaf/appsessionactions.go; this file is shape, bounds and routing.

// FeatureSessionActions is hello's word for an engine that serves
// session.actions, session.act and session.action.get.
const FeatureSessionActions = "session.actions"

// The three actions. They are the producers' names for what they do.
const (
	ActionTaskStart      = "task.start"
	ActionProgramStart   = "program.start"
	ActionMemoryRemember = "memory.remember"
)

// The bounds of an action. They are checked here, before the engine is asked.
const (
	ActionClientIDMax = 128
	ActionAgentMax    = 256
	ActionBriefMax    = 32 << 10
	ActionTextMax     = 8 << 10
	ActionProgramMax  = 64
)

// The words task.start's effort and memory.remember's scope take.
const (
	EffortBest      = "best"
	EffortCheap     = "cheap"
	ScopeProject    = "project"
	ScopeEverywhere = "everywhere"
)

// The states of an action's receipt. They say what the engine KNOWS and
// nothing more. THERE IS NO "refused" STATE: no producer answers a typed proof
// that its error came before any effect (a task id is reserved, a run is
// registered, a memory line is written before some of their errors), so an
// error from a producer is never read as "nothing happened". A refusal the
// engine can prove — a malformed ask, a program the engine does not carry, a
// door the conversation lacks — is an RPC error raised BEFORE anything is
// claimed or called, and leaves no record.
const (
	// ActionDone: the producer answered success; the ids are the producer's.
	// It means ADMITTED, not finished: the work's own events say the rest.
	ActionDone = "done"
	// ActionUncertain: the producer was called and may have had an effect —
	// it answered an error (ProducerError carries its words) or no answer came
	// back. The engine never runs it again; the caller reads the task list or
	// the memory before asking anew with a new client_id.
	ActionUncertain = "uncertain"
	// ActionInProgress: this process is executing it right now.
	ActionInProgress = "in_progress"
)

// ActionAsk is session.act's params: ONE action, for ONE conversation, under
// ONE client_id. Exactly the input that matches Action is present.
type ActionAsk struct {
	Action   string `json:"action"`
	Agent    string `json:"agent"`
	ClientID string `json:"client_id"`

	Task    *TaskStartInput      `json:"task,omitempty"`
	Program *ProgramStartInput   `json:"program,omitempty"`
	Memory  *MemoryRememberInput `json:"memory,omitempty"`
}

// TaskStartInput is `/task [solo] [--best|--cheap] <brief>`.
type TaskStartInput struct {
	Brief  string `json:"brief"`
	Solo   bool   `json:"solo,omitempty"`
	Effort string `json:"effort,omitempty" enum:"best,cheap"`
}

// ProgramStartInput is `/<program> <brief>`. Program is a NAME in the
// conversation engine's own catalogue: never a path, a command or a model.
type ProgramStartInput struct {
	Program string `json:"program"`
	Brief   string `json:"brief,omitempty"`
}

// MemoryRememberInput is `/remember <text>` (scope project, or empty) and
// `/always <text>` typed on home (scope everywhere): authored text, kept as
// the producer keeps it. No model is asked to rewrite it.
type MemoryRememberInput struct {
	Text  string `json:"text"`
	Scope string `json:"scope,omitempty" enum:"project,everywhere"`
}

// ActionsParams is session.actions's and the like's params: one conversation.
type ActionsParams struct {
	Agent string `json:"agent"`
}

// ActionGetParams is session.action.get's params.
type ActionGetParams struct {
	Agent    string `json:"agent"`
	ClientID string `json:"client_id"`
}

// ActionCapabilities is what one conversation's engine can do of the three,
// always all three in the order above. An action that cannot work is
// unavailable with the engine's reason, never a verb that fails when called.
type ActionCapabilities struct {
	Agent   string             `json:"agent"`
	Actions []ActionCapability `json:"actions"`
}

// ActionCapability is one action's availability on one conversation.
type ActionCapability struct {
	Action    string `json:"action"`
	Available bool   `json:"available"`
	// Why is the engine's own reason, present only when !Available.
	Why string `json:"why,omitempty"`
	// Programs are the conversation engine's catalogue (program.start): over
	// --host, the far build's.
	Programs []ActionProgram `json:"programs,omitempty"`
	// Scopes are the scopes memory.remember takes here; Efforts what task.start
	// takes beside none. Each lists only what the engine can be SHOWN to
	// honour: a choice a far handle's type merely permits is left out.
	Scopes  []string `json:"scopes,omitempty"`
	Efforts []string `json:"efforts,omitempty"`
}

// ActionProgram is one program of the catalogue.
type ActionProgram struct {
	Name          string `json:"name"`
	Args          string `json:"args,omitempty"`
	BriefOptional bool   `json:"brief_optional,omitempty"`
	Lands         string `json:"lands,omitempty"`
}

// ActionReceipt is the durable account of one attempt: session.act's answer
// and session.action.get's read of the same record.
type ActionReceipt struct {
	Action   string `json:"action"`
	Agent    string `json:"agent"`
	ClientID string `json:"client_id"`
	// Digest is the sha256 (hex) of the normalised ask. The same client_id
	// with another digest is a conflict.
	Digest string `json:"digest"`
	State  string `json:"state" enum:"done,uncertain,in_progress"`
	// Replayed is an answer from the record: nothing was executed now.
	Replayed bool      `json:"replayed,omitempty"`
	At       time.Time `json:"at"`

	Task   *ActionTask   `json:"task,omitempty"`
	Memory *ActionMemory `json:"memory,omitempty"`
	// ProducerError is the producer's own words when it answered an error.
	// It is present only when State is uncertain, and it is NOT a claim that
	// nothing happened.
	ProducerError *ActionProducerError `json:"producer_error,omitempty"`
	// Note says, in words, what is known when State is uncertain.
	Note string `json:"note,omitempty"`
}

// ActionTask is the work a start admitted, as the producer named it. ID is the
// wire task id ("<agent>:<n>") tasks.* and task.changed use. Title and Note are
// cut from the person's words, so they are on the answer that ran the producer
// and NOT on a replay of it: the task's own reads carry them by ID.
type ActionTask struct {
	ID      string `json:"id"`
	Number  uint64 `json:"number"`
	Title   string `json:"title,omitempty"`
	Note    string `json:"note,omitempty"`
	Program string `json:"program,omitempty"`
	// Solo is the EFFECTIVE solo handed to the producer, which folds the
	// person's standing `single` answer exactly as the terminal does.
	Solo bool `json:"solo,omitempty"`
}

// ActionMemory is the producer's receipt for a kept line. It carries no id:
// the producer answers a sentence, and none is invented by a second read. The
// sentence quotes the person's words, so it is on the answer that ran the
// producer and NOT on a replay of it, which carries the scope alone.
type ActionMemory struct {
	Receipt string `json:"receipt,omitempty"`
	// Scope is where the line holds, and is present ONLY when the method the
	// engine called fixes that: "everywhere" for an everywhere line. A project
	// line holds for the project, or for the person everywhere when the
	// conversation has no project of its own; the producer says which only in
	// its receipt sentence, so its scope is absent (unknown), never "project".
	Scope string `json:"scope,omitempty"`
}

// ActionProducerError is a producer's error in its own words, secrets taken
// out and bounded. The words are the producer's; nothing reads them for
// meaning.
type ActionProducerError struct {
	Message string `json:"message"`
}

// SessionActioner is the optional engine half of the three methods. An engine
// serves all of them or none, so hello's feature means all three.
type SessionActioner interface {
	// SessionActions says what the conversation's engine can do of the three.
	SessionActions(agent string) (ActionCapabilities, error)
	// SessionAct runs ONE action under its client_id and answers its receipt.
	SessionAct(ask ActionAsk) (ActionReceipt, error)
	// SessionActionGet reads the persisted receipt of a client_id.
	SessionActionGet(agent, clientID string) (ActionReceipt, error)
}

// checkActionAgent is the shape of a conversation id: it is a lookup key, never a path.
func checkActionAgent(agent string) error {
	if !plainText(agent, ActionAgentMax) {
		return Errorf(CodeBadParams, "agent is a conversation id of at most %d bytes without control characters", ActionAgentMax)
	}
	return nil
}

func checkActionClientID(id string) error {
	if !plainText(id, ActionClientIDMax) {
		return Errorf(CodeBadParams, "client_id is required: at most %d bytes, no surrounding space or control characters", ActionClientIDMax)
	}
	return nil
}

// noControl says s holds no control character other than the line breaks and
// tabs a brief may carry.
func noControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t'
	}) < 0
}

// CheckActionAsk is the shape of a session.act: the action is one of three,
// exactly its own input is present, and every bound holds. It never reaches the
// engine malformed. It checks no program and no capability — those are the
// engine's to know.
func CheckActionAsk(p ActionAsk) error {
	if err := checkActionAgent(p.Agent); err != nil {
		return err
	}
	if err := checkActionClientID(p.ClientID); err != nil {
		return err
	}
	set := 0
	for _, present := range []bool{p.Task != nil, p.Program != nil, p.Memory != nil} {
		if present {
			set++
		}
	}
	if set != 1 {
		return Errorf(CodeBadParams, "session.act carries exactly the one input of its action")
	}
	switch p.Action {
	case ActionTaskStart:
		if p.Task == nil {
			return Errorf(CodeBadParams, "%s takes the task input", p.Action)
		}
		t := p.Task
		if brief := strings.TrimSpace(t.Brief); brief == "" || len(t.Brief) > ActionBriefMax || !noControl(t.Brief) {
			return Errorf(CodeBadParams, "a task needs a brief of at most %d bytes", ActionBriefMax)
		}
		if t.Effort != "" && t.Effort != EffortBest && t.Effort != EffortCheap {
			return Errorf(CodeBadParams, "effort is %q or %q", EffortBest, EffortCheap)
		}
	case ActionProgramStart:
		if p.Program == nil {
			return Errorf(CodeBadParams, "%s takes the program input", p.Action)
		}
		in := p.Program
		if !slashWord(in.Program) || len(in.Program) > ActionProgramMax {
			return Errorf(CodeBadParams, "program is the name of a program, at most %d bytes of letters, digits and '-'", ActionProgramMax)
		}
		if len(in.Brief) > ActionBriefMax || !noControl(in.Brief) {
			return Errorf(CodeBadParams, "a brief is at most %d bytes", ActionBriefMax)
		}
	case ActionMemoryRemember:
		if p.Memory == nil {
			return Errorf(CodeBadParams, "%s takes the memory input", p.Action)
		}
		m := p.Memory
		if text := strings.TrimSpace(m.Text); text == "" || len(m.Text) > ActionTextMax || !noControl(m.Text) {
			return Errorf(CodeBadParams, "memory needs text of at most %d bytes", ActionTextMax)
		}
		if m.Scope != "" && m.Scope != ScopeProject && m.Scope != ScopeEverywhere {
			return Errorf(CodeBadParams, "scope is %q or %q", ScopeProject, ScopeEverywhere)
		}
	default:
		return Errorf(CodeBadParams, "action is %s, %s or %s", ActionTaskStart, ActionProgramStart, ActionMemoryRemember)
	}
	return nil
}

func init() {
	methods["session.actions"] = shaped[ActionsParams, ActionCapabilities](func(e Engine, raw json.RawMessage) (*ActionCapabilities, error) {
		a, ok := e.(SessionActioner)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve session.actions")
		}
		var p ActionsParams
		if err := decodeStrict("session.actions", raw, &p); err != nil {
			return nil, err
		}
		if err := checkActionAgent(p.Agent); err != nil {
			return nil, err
		}
		got, err := a.SessionActions(p.Agent)
		if err != nil {
			return nil, err
		}
		return &got, nil
	})
	// THE WRITE READS ITS PARAMS STRICTLY: a field this engine does not know —
	// a "force", a "skip_budget" — is refused, never ignored, because a caller
	// that thinks it asked for something the engine silently dropped has been
	// told yes to a question nobody answered.
	methods["session.act"] = shaped[ActionAsk, ActionReceipt](func(e Engine, raw json.RawMessage) (*ActionReceipt, error) {
		a, ok := e.(SessionActioner)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve session.act")
		}
		var p ActionAsk
		if err := decodeStrict("session.act", raw, &p); err != nil {
			return nil, err
		}
		if err := CheckActionAsk(p); err != nil {
			return nil, err
		}
		got, err := a.SessionAct(p)
		if err != nil {
			return nil, err
		}
		return &got, nil
	})
	methods["session.action.get"] = shaped[ActionGetParams, ActionReceipt](func(e Engine, raw json.RawMessage) (*ActionReceipt, error) {
		a, ok := e.(SessionActioner)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve session.action.get")
		}
		var p ActionGetParams
		if err := decodeStrict("session.action.get", raw, &p); err != nil {
			return nil, err
		}
		if err := checkActionAgent(p.Agent); err != nil {
			return nil, err
		}
		if err := checkActionClientID(p.ClientID); err != nil {
			return nil, err
		}
		got, err := a.SessionActionGet(p.Agent, p.ClientID)
		if err != nil {
			return nil, err
		}
		return &got, nil
	})
}

// withSessionActionsFeature is hello carrying [FeatureSessionActions] when the
// engine serves the three methods.
func withSessionActionsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(SessionActioner); !ok {
		return hello
	}
	return withFeature(hello, FeatureSessionActions)
}
