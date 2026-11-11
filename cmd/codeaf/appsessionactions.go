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
	"github.com/Agent-Field/codeaf/internal/remote"
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

// appActionDoors is what a conversation's handle can be SHOWN to do of the
// three. session.actions advertises from it and session.act refuses from it,
// so an advert and a call can never disagree.
//
// A HANDLE TO A FAR ENGINE HAS EVERY METHOD, SO ITS TYPE PROVES NOTHING. A
// remote.Agent — every conversation the app holds, over a loopback or --host —
// carries StartTaskEffort and RememberAlways whatever the engine behind it can
// do; the far server asks its own agent per call. So a choice is offered over
// such a handle only where a FAR fact says it works, and otherwise it is left
// out rather than guessed:
//
//   - effort: no far fact states that the far agent can choose a task's crew
//     (internal/remote's server refuses "cannot choose a task's crew" per
//     call), so over a far handle no effort is offered and one asked for is
//     refused before anything is claimed.
//   - everywhere: the far server answers Remember and RememberAlways through
//     ONE check of one interface (internal/remote's places.go,
//     MemoryCommands), and the two halves speak one protocol version or do not
//     connect, so a far engine that keeps a project line keeps an everywhere
//     line. Remembers() is that far fact, and it gates both scopes alike.
//   - programs: the far catalogue read answers an empty report when it fails,
//     so an empty report from a far handle is not known to mean "none"
//     ([appActionDoors.programs]).
type appActionDoors struct {
	task    appTaskDoor
	effort  appEffortDoor // nil unless it is proven
	program appProgramDoor
	memory  appMemoryDoor
	always  appAlwaysDoor
	far     bool
}

// appActionDoorsOf is the doors of conv, as far as they can be shown.
func appActionDoorsOf(conv appConversation) appActionDoors {
	_, far := conv.(*remote.Agent)
	d := appActionDoors{far: far}
	d.task, _ = conv.(appTaskDoor)
	if !far {
		d.effort, _ = conv.(appEffortDoor)
	}
	d.program, _ = conv.(appProgramDoor)
	d.memory, _ = conv.(appMemoryDoor)
	d.always, _ = conv.(appAlwaysDoor)
	return d
}

// programs is the engine's catalogue and whether it is KNOWN. A conversation
// in this process answers its own rows, so none is a fact; a far handle
// answers the zero report when its read fails, so none from it is unknown —
// never "carries no programs" and never "no program named …".
func (d appActionDoors) programs() (rows []session.DelegateRow, known bool) {
	rows = d.program.Delegates().Rows
	return rows, len(rows) > 0 || !d.far
}

// The words for a catalogue that could not be known.
const appActionNoCatalogue = "the engine's programs could not be read, or it reported none"

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
	appActionNoteUnsettled  = "the engine could not confirm that this answer was recorded; a later read of this client_id may say uncertain instead of this answer"
)

// receipt is a claim as the wire carries it.
func (c appActionClaim) receipt(replayed bool) appwire.ActionReceipt {
	r := appwire.ActionReceipt{
		Action: c.Action, Agent: c.Agent, ClientID: c.ClientID, Digest: c.Digest, At: c.At,
		Replayed: replayed, Task: c.Task, Memory: c.Memory,
	}
	if c.Memory != nil && c.Memory.Scope != appwire.ScopeEverywhere {
		// A record an earlier build of this branch wrote carries the scope
		// that was ASKED; only "everywhere" was ever where the line held.
		r.Memory = &appwire.ActionMemory{}
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
//
// IT IS A READ AND IT IS BOUNDED LIKE ONE: it may open the conversation, and
// over --host it asks the far engine for its catalogue, so it holds one of the
// read slots session.action.get shares until both have answered. A full gate
// is busy at once; nothing queues, and no goroutine is started to wait.
func (b *appBackend) SessionActions(agent string) (appwire.ActionCapabilities, error) {
	release, ok := b.actions.reading()
	if !ok {
		return appwire.ActionCapabilities{}, appActionReadsBusy()
	}
	defer release()
	_, live, err := b.heldLive(agent)
	if err != nil {
		return appwire.ActionCapabilities{}, err
	}
	return appActionDoorsOf(live.conn.Conv).capabilities(agent), nil
}

// capabilities is the advert of these doors: always the three, in order, each
// available or absent with its reason.
func (d appActionDoors) capabilities(agent string) appwire.ActionCapabilities {
	task := appwire.ActionCapability{Action: appwire.ActionTaskStart}
	if d.task != nil {
		task.Available = true
		if d.effort != nil {
			task.Efforts = []string{appwire.EffortBest, appwire.EffortCheap}
		}
	} else {
		task.Why = "this conversation has no task door"
	}

	program := appwire.ActionCapability{Action: appwire.ActionProgramStart}
	if d.program == nil {
		program.Why = "this conversation has no program door"
	} else if rows, known := d.programs(); !known {
		program.Why = appActionNoCatalogue
	} else if len(rows) == 0 {
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
	if d.memory == nil {
		memory.Why = "this conversation has no memory door"
	} else if !d.memory.Remembers() {
		memory.Why = "this conversation keeps no memory"
	} else {
		memory.Available = true
		memory.Scopes = []string{appwire.ScopeProject}
		if d.always != nil {
			memory.Scopes = append(memory.Scopes, appwire.ScopeEverywhere)
		}
	}
	return appwire.ActionCapabilities{Agent: agent, Actions: []appwire.ActionCapability{task, program, memory}}
}

// appActionReadsBusy is a full read gate.
func appActionReadsBusy() error {
	return appwire.Errorf(appwire.CodeBusy, "too many reads of actions are running; ask again in a moment")
}

// SessionActionGet reads the persisted receipt of a client_id. Not found says
// only that THIS engine's state folder holds no record of it.
func (b *appBackend) SessionActionGet(agent, clientID string) (appwire.ActionReceipt, error) {
	release, ok := b.actions.reading()
	if !ok {
		return appwire.ActionReceipt{}, appActionReadsBusy()
	}
	defer release()
	key := appClaimName(clientID)
	claim, found, err := b.actionRecord(key, clientID, true)
	if err != nil {
		return appwire.ActionReceipt{}, appActionUnreadable(err)
	}
	if f, ok := b.actions.flight(key); ok && (!found || claim.State == appActionIntent) {
		if f.agent != agent {
			return appwire.ActionReceipt{}, appwire.Errorf(appwire.CodeConflict, "that client_id is already used for another conversation")
		}
		return f.inProgress(), nil
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

// actionRecord reads clientID's record for a lookup. THE RECORD IS READ BEFORE
// THE FLIGHT IS LOOKED AT, and that order is what makes the pair true: an
// attempt registers its flight before it claims and releases it only after it
// has settled, so a record at intent read first, with this process's flight
// still there after, is that flight running. A flight looked at first could
// be missed by an attempt that is admitted and claims a moment later, and its
// intent then read as somebody else's. When watch is set and the record says
// intent with no flight here, it is read once more, because the flight may
// have settled and gone between the two looks.
func (b *appBackend) actionRecord(key, clientID string, watch bool) (appActionClaim, bool, error) {
	claims := b.actionClaims()
	claim, found, err := claims.read(clientID)
	if err != nil || !found || !watch || claim.State != appActionIntent {
		return claim, found, err
	}
	if _, running := b.actions.flight(key); running {
		return claim, found, nil
	}
	if again, ok, err := claims.read(clientID); err == nil && ok {
		return again, true, nil
	}
	return claim, found, nil
}

// SessionAct runs one action under its client_id. See the file's head for the
// order; every return before the claim made nothing.
func (b *appBackend) SessionAct(ask appwire.ActionAsk) (appwire.ActionReceipt, error) {
	digest := appActionDigest(ask)
	key := appClaimName(ask.ClientID)
	claims := b.actionClaims()

	// A REPEAT IS A LOOKUP, never busy and never an admission: what the record
	// says, and whether this process is running it.
	replay := func(own bool) (appwire.ActionReceipt, bool, error) {
		claim, found, err := b.actionRecord(key, ask.ClientID, !own)
		if err != nil {
			return appwire.ActionReceipt{}, true, appActionUnreadable(err)
		}
		// THE ATTEMPT THIS CALL IS RUNNING IS NOT A REPEAT OF ITSELF: once
		// admitted, only the record can say another attempt got there first.
		if f, ok := b.actions.flight(key); ok && !own && (!found || claim.State == appActionIntent) {
			if err := appActionConflict(ask.Agent, ask.Action, digest, f.agent, f.action, f.digest); err != nil {
				return appwire.ActionReceipt{}, true, err
			}
			return f.inProgress(), true, nil
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

	a, err := b.find(ask.Agent)
	if err != nil {
		return appwire.ActionReceipt{}, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	_, live, err := b.heldLiveLocked(a)
	if err != nil {
		return appwire.ActionReceipt{}, err
	}
	if ask.Action == appwire.ActionTaskStart || ask.Action == appwire.ActionProgramStart {
		if err := b.workAllowed(a); err != nil {
			return appwire.ActionReceipt{}, err
		}
	}
	run, err := b.actionRun(live.conn.Conv, ask)
	if err != nil {
		return appwire.ActionReceipt{}, err
	}

	claim := appActionClaim{
		V: appActionClaimV, ClientID: ask.ClientID, Action: ask.Action, Agent: ask.Agent,
		Digest: digest, State: appActionIntent, At: b.now().UTC(),
	}
	create := appActionClaims.create
	if b.createAction != nil {
		create = b.createAction
	}
	made, err := create(claims, claim)
	if err != nil {
		return appwire.ActionReceipt{}, appwire.Errorf(appwire.CodeEngine,
			"the engine could not record this attempt, so it did not start it (%s)", redact.Secrets(plainWords(err.Error())))
	}
	if !made {
		// Another process holds the name; what it recorded is the answer.
		r, found, err := replay(true)
		if err == nil && !found {
			// THE NAME WAS TAKEN AND ITS RECORD IS GONE: somebody claimed this
			// client_id, so its absence now is not "nothing was sent", and an
			// empty answer is no receipt at all. Nothing ran here.
			return appwire.ActionReceipt{}, appwire.Errorf(appwire.CodeUncertain,
				"another attempt claimed that client_id and its record could not be read back, so this engine cannot say whether it ran; look at the task list or the memory before asking again with a new client_id")
		}
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
	settle := appActionClaims.settle
	if b.settleAction != nil {
		settle = b.settleAction
	}
	if err := settle(claims, settled); err != nil {
		// The caller still gets the producer's real answer. WHAT THE RECORD
		// SAYS NOW IS NOT KNOWN: the rename may have landed before the folder
		// sync failed, so it may read as this answer, or still as the claim,
		// which a later read calls uncertain — never "not sent".
		r.Note = appActionNoteUnsettled
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
	return b.actionRunWith(appActionDoorsOf(conv), ask)
}

// actionRunWith is [appBackend.actionRun] over doors already shown.
func (b *appBackend) actionRunWith(doors appActionDoors, ask appwire.ActionAsk) (appActionRunner, error) {
	agent := ask.Agent
	taskOf := func(program string, solo bool, id uint64, title, note string) *appwire.ActionTask {
		return &appwire.ActionTask{
			ID: agent + ":" + strconv.FormatUint(id, 10), Number: id, Title: title, Note: note, Program: program, Solo: solo,
		}
	}
	switch ask.Action {
	case appwire.ActionTaskStart:
		in := ask.Task
		door, effortDoor := doors.task, doors.effort
		if door == nil {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this conversation has no task door")
		}
		if in.Effort != "" && effortDoor == nil {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this conversation cannot be shown to choose a task's crew, so no effort is offered here")
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
		door := doors.program
		if door == nil {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this conversation has no program door")
		}
		// THE PROGRAM IS A NAME IN THE ENGINE'S OWN CATALOGUE — the far build's
		// over --host — and the producer checks it again. No second registry.
		// A catalogue that could not be known refuses nothing by name: it is
		// the engine's failure, and nothing is claimed or started.
		rows, known := doors.programs()
		if !known {
			return nil, appwire.Errorf(appwire.CodeEngine, "%s, so nothing was started", appActionNoCatalogue)
		}
		var row *session.DelegateRow
		var have []string
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
		door := doors.memory
		if door == nil {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this conversation has no memory door")
		}
		if !door.Remembers() {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this conversation keeps no memory")
		}
		scope := in.Scope
		if scope == "" {
			scope = appwire.ScopeProject
		}
		always := doors.always
		if scope == appwire.ScopeEverywhere && always == nil {
			return nil, appwire.Errorf(appwire.CodeBadParams, "this conversation cannot keep a line everywhere")
		}
		text := strings.TrimSpace(in.Text)
		return func(context.Context) (*appwire.ActionTask, *appwire.ActionMemory, error) {
			// THE SCOPE IS STATED ONLY WHERE THE METHOD FIXES IT. RememberAlways
			// with everywhere keeps the line for the person in every project
			// whatever the conversation is. Remember keeps it for the project,
			// or for the person everywhere when the conversation has none, and
			// answers only a sentence, so which one it did is not known here
			// and the scope is left out rather than read from those words.
			if scope == appwire.ScopeEverywhere {
				receipt, err := always.RememberAlways(text, true)
				if err != nil {
					return nil, nil, err
				}
				return nil, &appwire.ActionMemory{Receipt: receipt, Scope: appwire.ScopeEverywhere}, nil
			}
			receipt, err := door.Remember(text)
			if err != nil {
				return nil, nil, err
			}
			return nil, &appwire.ActionMemory{Receipt: receipt}, nil
		}, nil
	}
	return nil, appwire.Errorf(appwire.CodeBadParams, "unknown action %q", ask.Action)
}
