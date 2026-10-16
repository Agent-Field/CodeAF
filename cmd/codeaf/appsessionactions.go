package main

// ── codeaf app: THREE NAMED SESSION ACTIONS (session.act) ───────────────────
//
// `/task <brief>`, `/<program> <brief>` and `/remember <text>` are handlers in
// the terminal's Bubble Tea loop; the producers under them are the session's
// StartTask, StartDelegate and Remember. This file gives the app a typed,
// idempotent way to call those SAME producers through the SAME door the
// terminal uses: the open conversation's own handle, which is a remote.Agent,
// so over --host the call runs on the far machine against the far build's
// catalogue. Nothing here starts a runner, reads a catalogue of its own,
// chooses a model, raises a cap or answers a permission: a producer's error
// is carried back in its own words, and a caller that says it means it is
// not a bypass of anything.
//
// THE ORDER IS THE LAW: shape, then the conversation and its capability (all
// refusable with nothing written), then the client_id is CLAIMED ON DISK
// (appsessionactionclaim.go), then the producer is called ONCE, then the
// claim is settled. A client_id is one attempt for ever: a repeat is answered
// from the record, an attempt whose outcome cannot be known is uncertain and
// is never run again by this engine, and a read that fails is never "new".
// AN ERROR FROM A PRODUCER IS UNCERTAIN, NEVER "NOT DONE": only what is
// refused before the claim — and so before the producer — is a refusal.
//
// ADMISSION IS HELD BY THE ONE GOROUTINE THAT CALLS THE PRODUCER. It is taken
// before the claim and released by a defer after the producer returns, so a
// closing engine or a gone caller cannot free a slot while a mutation is still
// running, and no goroutine, timer or poller is added to make any of it work.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The app's backend serves session.actions, session.act and session.action.get.
var _ appwire.SessionActioner = (*appBackend)(nil)

// The bounds of admission: one action at a time on a conversation, a few in
// the process, and a few reads of a record. A full gate is busy at once.
const (
	appActionsPerProcess = 8
	appActionReads       = 16
	appActionErrorMax    = 1 << 10
	appActionWhyMax      = 256
)

// The producers' doors, asked of the conversation's own handle. A fake
// conversation without them has the capability absent, not broken.
type (
	appTaskDoor interface {
		StartTask(ctx context.Context, brief string, solo bool) (uint64, string, string, error)
	}
	appEffortDoor interface {
		StartTaskEffort(ctx context.Context, brief string, solo bool, effort string) (uint64, string, string, error)
	}
	appProgramDoor interface {
		StartDelegate(ctx context.Context, name, brief string) (uint64, string, string, error)
		Delegates() session.DelegateReport
	}
	appMemoryDoor interface {
		Remember(text string) (string, error)
		Remembers() bool
	}
	appAlwaysDoor interface {
		RememberAlways(text string, everywhere bool) (string, error)
	}
)

// appActionGate is the process's admission: which attempts are running, on
// which conversation. The zero value is ready.
type appActionGate struct {
	mu      sync.Mutex
	flights map[string]appActionFlight // by claim name
	byAgent map[string]string          // conversation id → claim name
	reads   int
}

// appActionFlight is one running attempt, as a repeat of it can be answered.
type appActionFlight struct {
	action, agent, digest, clientID string
	at                              time.Time
}

// admit takes the one slot a conversation has and the process's share, or
// answers the attempt already running under this claim name, or busy.
func (g *appActionGate) admit(key string, f appActionFlight) (release func(), running *appActionFlight, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if have, ok := g.flights[key]; ok {
		return nil, &have, nil
	}
	if _, ok := g.byAgent[f.agent]; ok {
		return nil, nil, appwire.Errorf(appwire.CodeBusy, "another action on this conversation is running; ask again when it has answered")
	}
	if len(g.flights) >= appActionsPerProcess {
		return nil, nil, appwire.Errorf(appwire.CodeBusy, "too many actions are running; ask again in a moment")
	}
	if g.flights == nil {
		g.flights, g.byAgent = map[string]appActionFlight{}, map[string]string{}
	}
	g.flights[key], g.byAgent[f.agent] = f, key
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		delete(g.flights, key)
		delete(g.byAgent, f.agent)
	}, nil, nil
}

// flight is the attempt running under key, if one is.
func (g *appActionGate) flight(key string) (appActionFlight, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f, ok := g.flights[key]
	return f, ok
}

// reading takes one read slot, or says there is none.
func (g *appActionGate) reading() (release func(), ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.reads >= appActionReads {
		return nil, false
	}
	g.reads++
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.reads--
	}, true
}

// appActionDigest is the sha256 of the ask normalised: strings trimmed, the
// scope defaulted, and only the input of its own action. The same request said
// two ways is one request; any other is another.
func appActionDigest(ask appwire.ActionAsk) string {
	type shape struct {
		Action  string `json:"action"`
		Agent   string `json:"agent"`
		Brief   string `json:"brief,omitempty"`
		Solo    bool   `json:"solo,omitempty"`
		Effort  string `json:"effort,omitempty"`
		Program string `json:"program,omitempty"`
		Text    string `json:"text,omitempty"`
		Scope   string `json:"scope,omitempty"`
	}
	s := shape{Action: ask.Action, Agent: ask.Agent}
	switch {
	case ask.Task != nil:
		s.Brief, s.Solo, s.Effort = strings.TrimSpace(ask.Task.Brief), ask.Task.Solo, ask.Task.Effort
	case ask.Program != nil:
		s.Program, s.Brief = ask.Program.Program, strings.TrimSpace(ask.Program.Brief)
	case ask.Memory != nil:
		s.Text, s.Scope = strings.TrimSpace(ask.Memory.Text), ask.Memory.Scope
		if s.Scope == "" {
			s.Scope = appwire.ScopeProject
		}
	}
	raw, _ := json.Marshal(s)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// The notes of an uncertain receipt, by what is known.
const (
	appActionNoteIntent     = "an earlier attempt may have started this and was never recorded as finished; check the task list or the memory before asking again with a new client_id"
	appActionNoteUnanswered = "the engine did not answer, so it is not known whether this started; check the task list or the memory before asking again with a new client_id"
	appActionNoteFailed     = "the producer answered an error, which does not prove nothing was started or kept; check the task list or the memory before asking again with a new client_id"
)

// receipt is a claim as the wire carries it.
func (c appActionClaim) receipt(replayed bool) appwire.ActionReceipt {
	r := appwire.ActionReceipt{
		Action: c.Action, Agent: c.Agent, ClientID: c.ClientID, Digest: c.Digest, At: c.At,
		Replayed: replayed, Task: c.Task, Memory: c.Memory,
	}
	switch c.State {
	case appActionDone:
		r.State = appwire.ActionDone
	case appActionFailed:
		r.State, r.ProducerError, r.Note = appwire.ActionUncertain, c.ProducerError, appActionNoteFailed
	default:
		r.State, r.Note = appwire.ActionUncertain, appActionNoteIntent
	}
	return r
}

// inProgress is the receipt of an attempt this process is running now.
func (f appActionFlight) inProgress() appwire.ActionReceipt {
	return appwire.ActionReceipt{
		Action: f.action, Agent: f.agent, ClientID: f.clientID, Digest: f.digest,
		State: appwire.ActionInProgress, Replayed: true, At: f.at,
	}
}

// appActionConflict is a client_id used for something else: another
// conversation, another action or other words. It never says which.
func appActionConflict(agent, action, digest, oldAgent, oldAction, oldDigest string) error {
	if oldAgent != agent {
		return appwire.Errorf(appwire.CodeConflict, "that client_id is already used for another conversation")
	}
	if oldAction != action || oldDigest != digest {
		return appwire.Errorf(appwire.CodeConflict, "that client_id was already used for a different request; use a new client_id")
	}
	return nil
}

// appActionUnreadable is a record this engine cannot say anything about. It is
// never "new".
func appActionUnreadable(err error) error {
	return appwire.Errorf(appwire.CodeUncertain, "this engine cannot read the record of that client_id (%s), so it cannot say whether the action ran; look at the task list or the memory before asking again",
		redact.Secrets(plainWords(err.Error())))
}

// SessionActions says what the conversation's engine can do of the three.
func (b *appBackend) SessionActions(agent string) (appwire.ActionCapabilities, error) {
	_, live, err := b.heldLive(agent)
	if err != nil {
		return appwire.ActionCapabilities{}, err
	}
	conv := live.conn.Conv
	out := appwire.ActionCapabilities{Agent: agent}

	task := appwire.ActionCapability{Action: appwire.ActionTaskStart}
	if _, ok := conv.(appTaskDoor); ok {
		task.Available = true
		if _, ok := conv.(appEffortDoor); ok {
			task.Efforts = []string{appwire.EffortBest, appwire.EffortCheap}
		}
	} else {
		task.Why = "this conversation has no task door"
	}

	program := appwire.ActionCapability{Action: appwire.ActionProgramStart}
	if door, ok := conv.(appProgramDoor); !ok {
		program.Why = "this conversation has no program door"
	} else if rows := door.Delegates().Rows; len(rows) == 0 {
		program.Why = "this engine carries no programs"
	} else {
		program.Available = true
		for _, r := range rows {
			program.Programs = append(program.Programs, appwire.ActionProgram{
				Name: r.Name, Args: r.Args, BriefOptional: r.BriefOptional, Lands: r.Lands,
			})
		}
	}

	memory := appwire.ActionCapability{Action: appwire.ActionMemoryRemember}
	if door, ok := conv.(appMemoryDoor); !ok {
		memory.Why = "this conversation has no memory door"
	} else if !door.Remembers() {
		memory.Why = "this conversation keeps no memory"
	} else {
		memory.Available = true
		memory.Scopes = []string{appwire.ScopeProject}
		if _, ok := conv.(appAlwaysDoor); ok {
			memory.Scopes = append(memory.Scopes, appwire.ScopeEverywhere)
		}
	}
	out.Actions = []appwire.ActionCapability{task, program, memory}
	return out, nil
}

// SessionActionGet reads the persisted receipt of a client_id. Not found says
// only that THIS engine's state folder holds no record of it.
func (b *appBackend) SessionActionGet(agent, clientID string) (appwire.ActionReceipt, error) {
	release, ok := b.actions.reading()
	if !ok {
		return appwire.ActionReceipt{}, appwire.Errorf(appwire.CodeBusy, "too many reads of action records are running; ask again in a moment")
	}
	defer release()
	if f, ok := b.actions.flight(appClaimName(clientID)); ok {
		if f.agent != agent {
			return appwire.ActionReceipt{}, appwire.Errorf(appwire.CodeConflict, "that client_id is already used for another conversation")
		}
		return f.inProgress(), nil
	}
	claim, found, err := b.actionClaims().read(clientID)
	if err != nil {
		return appwire.ActionReceipt{}, appActionUnreadable(err)
	}
	if !found {
		return appwire.ActionReceipt{}, appwire.Errorf(appwire.CodeNotFound,
			"this engine holds no record of that client_id; that does not prove nothing was sent, so read the task list or the memory")
	}
	if claim.Agent != agent {
		return appwire.ActionReceipt{}, appwire.Errorf(appwire.CodeConflict, "that client_id is already used for another conversation")
	}
	return claim.receipt(true), nil
}

// SessionAct runs one action under its client_id. See the file's head for the
// order; every return before the claim made nothing.
func (b *appBackend) SessionAct(ask appwire.ActionAsk) (appwire.ActionReceipt, error) {
	digest := appActionDigest(ask)
	key := appClaimName(ask.ClientID)
	claims := b.actionClaims()

	// A REPEAT IS A LOOKUP, never busy and never an admission: what is running
	// now, then what the record says.
	replay := func(own bool) (appwire.ActionReceipt, bool, error) {
		// THE ATTEMPT THIS CALL IS RUNNING IS NOT A REPEAT OF ITSELF: once
		// admitted, only the record can say another attempt got there first.
		if f, ok := b.actions.flight(key); ok && !own {
			if err := appActionConflict(ask.Agent, ask.Action, digest, f.agent, f.action, f.digest); err != nil {
				return appwire.ActionReceipt{}, true, err
			}
			return f.inProgress(), true, nil
		}
		claim, found, err := claims.read(ask.ClientID)
		if err != nil {
			return appwire.ActionReceipt{}, true, appActionUnreadable(err)
		}
		if !found {
			return appwire.ActionReceipt{}, false, nil
		}
		if err := appActionConflict(ask.Agent, ask.Action, digest, claim.Agent, claim.Action, claim.Digest); err != nil {
			return appwire.ActionReceipt{}, true, err
		}
		return claim.receipt(true), true, nil
	}
	if r, done, err := replay(false); done {
		return r, err
	}
	if b.isClosing() {
		return appwire.ActionReceipt{}, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}

	release, running, err := b.actions.admit(key, appActionFlight{
		action: ask.Action, agent: ask.Agent, digest: digest, clientID: ask.ClientID, at: b.now().UTC(),
	})
	if err != nil {
		return appwire.ActionReceipt{}, err
	}
	if running != nil {
		if err := appActionConflict(ask.Agent, ask.Action, digest, running.agent, running.action, running.digest); err != nil {
			return appwire.ActionReceipt{}, err
		}
		return running.inProgress(), nil
	}
	defer release()

	// Another attempt may have finished between the lookup and the admission.
	if r, done, err := replay(true); done {
		return r, err
	}

	_, live, err := b.heldLive(ask.Agent)
	if err != nil {
		return appwire.ActionReceipt{}, err
	}
	run, err := b.actionRun(live.conn.Conv, ask)
	if err != nil {
		return appwire.ActionReceipt{}, err
	}

	claim := appActionClaim{
		V: appActionClaimV, ClientID: ask.ClientID, Action: ask.Action, Agent: ask.Agent,
		Digest: digest, State: appActionIntent, At: b.now().UTC(),
	}
	made, err := claims.create(claim)
	if err != nil {
		return appwire.ActionReceipt{}, appwire.Errorf(appwire.CodeEngine,
			"the engine could not record this attempt, so it did not start it (%s)", redact.Secrets(plainWords(err.Error())))
	}
	if !made {
		// Another process holds the name; what it recorded is the answer.
		r, _, err := replay(true)
		return r, err
	}

	// THE PRODUCER, ONCE. The context is the process's, not the request's: the
	// work it admits must outlive this call, as it does for the terminal.
	task, memory, runErr := run(context.Background())

	settled := claim
	settled.DoneAt = b.now().UTC()
	switch {
	case runErr == nil:
		// THE RECORD KEEPS IDS AND SCOPE, NEVER THE PERSON'S WORDS: a title is
		// cut from the brief and a memory receipt quotes the line. They go back
		// on this answer; a replay carries the ids and tasks.* the rest.
		settled.State = appActionDone
		if task != nil {
			settled.Task = &appwire.ActionTask{ID: task.ID, Number: task.Number, Program: task.Program, Solo: task.Solo}
		}
		if memory != nil {
			settled.Memory = &appwire.ActionMemory{Scope: memory.Scope}
		}
	case errors.Is(runErr, session.ErrSendUnanswered):
		// NO ANSWER CAME BACK: the claim stays at intent, which every later
		// read calls uncertain, and nothing here or anywhere re-runs it.
		r := claim.receipt(false)
		r.Note = appActionNoteUnanswered
		return r, nil
	default:
		// AN ERROR IS NOT A REFUSAL. The producers reserve an id, register a
		// run or write a line before some of their errors, and none of them
		// says by type which errors came first; reading the words for that
		// would be guessing. So the attempt is uncertain, durably, with the
		// producer's words, and it is never run again.
		settled.State = appActionFailed
		settled.ProducerError = &appwire.ActionProducerError{Message: appActionWords(runErr)}
	}
	r := settled.receipt(false)
	if runErr == nil {
		r.Task, r.Memory = task, memory
	}
	if err := claims.settle(settled); err != nil {
		// The caller still gets the producer's real answer; the record stays
		// at intent, so a later read says uncertain and never "not sent".
		r.Note = "the engine could not record this receipt, so a later read of it will say it is uncertain"
	}
	return r, nil
}

// appActionWords is a producer's error in its own words, with secrets taken
// out and bounded.
func appActionWords(err error) string {
	words := redact.Secrets(strings.TrimSpace(err.Error()))
	if len(words) > appActionErrorMax {
		cut := appActionErrorMax
		for cut > 0 && words[cut]&0xC0 == 0x80 {
			cut--
		}
		words = words[:cut]
	}
	return words
}

// appActionRunner is the one producer call an ask maps to.
type appActionRunner func(ctx context.Context) (*appwire.ActionTask, *appwire.ActionMemory, error)

// actionRun checks that the conversation can do what the ask says, with
// nothing written and nothing called, and answers the call to make.
func (b *appBackend) actionRun(conv appConversation, ask appwire.ActionAsk) (appActionRunner, error) {
	agent := ask.Agent
	taskOf := func(program string, solo bool, id uint64, title, note string) *appwire.ActionTask {
		return &appwire.ActionTask{
			ID: agent + ":" + strconv.FormatUint(id, 10), Number: id, Title: title, Note: note, Program: program, Solo: solo,
		}
	}
	switch ask.Action {
	case appwire.ActionTaskStart:
		in := ask.Task
		door, ok := conv.(appTaskDoor)
		if !ok {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this conversation has no task door")
		}
		effortDoor, hasEffort := conv.(appEffortDoor)
		if in.Effort != "" && !hasEffort {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this conversation cannot choose a task's crew")
		}
		brief := strings.TrimSpace(in.Brief)
		// THE PERSON'S STANDING `single` ANSWER IS FOLDED IN as the terminal's
		// own handler folds it: an existing setting, not a new default.
		solo := in.Solo || config.TaskStartAt(b.profileDir) == config.TaskStartSingle
		return func(ctx context.Context) (*appwire.ActionTask, *appwire.ActionMemory, error) {
			var id uint64
			var title, note string
			var err error
			if in.Effort != "" {
				id, title, note, err = effortDoor.StartTaskEffort(ctx, brief, solo, in.Effort)
			} else {
				id, title, note, err = door.StartTask(ctx, brief, solo)
			}
			if err != nil {
				return nil, nil, err
			}
			return taskOf("", solo, id, title, note), nil, nil
		}, nil

	case appwire.ActionProgramStart:
		in := ask.Program
		door, ok := conv.(appProgramDoor)
		if !ok {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this conversation has no program door")
		}
		// THE PROGRAM IS A NAME IN THE ENGINE'S OWN CATALOGUE — the far build's
		// over --host — and the producer checks it again. No second registry.
		var row *session.DelegateRow
		var have []string
		rows := door.Delegates().Rows
		for i := range rows {
			have = append(have, rows[i].Name)
			if rows[i].Name == in.Program {
				row = &rows[i]
			}
		}
		if row == nil {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this engine has no program named %q; it carries: %s", in.Program, strings.Join(have, ", "))
		}
		brief := strings.TrimSpace(in.Brief)
		if brief == "" && !row.BriefOptional {
			return nil, appwire.Errorf(appwire.CodeBadParams, "%s needs a brief: the whole task, in words", in.Program)
		}
		return func(ctx context.Context) (*appwire.ActionTask, *appwire.ActionMemory, error) {
			id, title, note, err := door.StartDelegate(ctx, in.Program, brief)
			if err != nil {
				return nil, nil, err
			}
			return taskOf(in.Program, false, id, title, note), nil, nil
		}, nil

	case appwire.ActionMemoryRemember:
		in := ask.Memory
		door, ok := conv.(appMemoryDoor)
		if !ok {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this conversation has no memory door")
		}
		if !door.Remembers() {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this conversation keeps no memory")
		}
		scope := in.Scope
		if scope == "" {
			scope = appwire.ScopeProject
		}
		always, hasAlways := conv.(appAlwaysDoor)
		if scope == appwire.ScopeEverywhere && !hasAlways {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this conversation cannot keep a line everywhere")
		}
		text := strings.TrimSpace(in.Text)
		return func(context.Context) (*appwire.ActionTask, *appwire.ActionMemory, error) {
			var receipt string
			var err error
			if scope == appwire.ScopeEverywhere {
				receipt, err = always.RememberAlways(text, true)
			} else {
				receipt, err = door.Remember(text)
			}
			if err != nil {
				return nil, nil, err
			}
			return nil, &appwire.ActionMemory{Receipt: receipt, Scope: scope}, nil
		}, nil
	}
	return nil, appwire.Errorf(appwire.CodeBadParams, "unknown action %q", ask.Action)
}
