package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

const clientToolSettledTTL = 10 * time.Minute
const clientToolSettledMax = 1024

var appClientToolsBackend atomic.Pointer[appBackend]
var clientToolFreshID atomic.Uint64

type clientToolAgentSets struct {
	version  uint64
	sets     []session.ClientToolSet
	builtins map[string]bool
}

type clientToolPending struct {
	ctx      context.Context
	done     chan struct{}
	call     session.ClientToolCall
	answerBy time.Time
	answer   session.ClientToolAnswer
	event    *appwire.AgentsToolCall
}

type clientToolSettled struct {
	answer session.ClientToolAnswer
	at     time.Time
}

// The registry belongs to one stdio connection, including its replay memory.
type appClientTools struct {
	mu         sync.Mutex
	agents     map[string]clientToolAgentSets
	pending    map[string]*clientToolPending
	settled    map[string]clientToolSettled
	order      []string
	closed     bool
	senderWake chan struct{}
	senderDone chan struct{}
}

type appClientToolDoor struct {
	backend *appBackend
	agent   string
}

var _ appwire.ClientTooler = (*appBackend)(nil)
var _ session.ClientToolDoor = (*appClientToolDoor)(nil)

func (b *appBackend) clientToolEventCategory(a *appAgent, event session.Event) session.ActionCategory {
	if event.Category != "" {
		return event.Category
	}
	var id string
	var conv appConversation
	b.locked(func() {
		id = a.view.ID
		if a.live != nil {
			conv = a.live.conn.Conv
		}
	})
	if local, ok := conv.(interface {
		ActionCategoryForTool(string) session.ActionCategory
	}); ok {
		return local.ActionCategoryForTool(event.Tool)
	}
	r := &b.clientTools
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agents[id].builtins[event.Tool] {
		return session.ActionCategoryForTool(event.Tool)
	}
	for _, set := range r.agents[id].sets {
		for _, tool := range set.Tools {
			if tool.Name == event.Tool {
				return tool.Category
			}
		}
	}
	return session.ActionCategoryForTool(event.Tool)
}

func appClientToolsDoor(sessionFile string) session.ClientToolDoor {
	b := appClientToolsBackend.Load()
	if b == nil {
		return nil
	}
	return &appClientToolDoor{backend: b, agent: appAgentID(sessionFile)}
}

// Root builders share launch settings, but each opening needs its own conversation door.
func appClientToolsOpener(open func(session.Config) (*session.Agent, error)) func(session.Config) (*session.Agent, error) {
	return func(cfg session.Config) (*session.Agent, error) {
		cfg.ClientTools = appClientToolsDoor(cfg.SessionFile)
		return open(cfg)
	}
}

func (b *appBackend) SetClientTools(p appwire.ClientToolsSetParams) (appwire.ClientToolsSetResult, error) {
	result := appwire.ClientToolsSetResult{Agent: p.Agent, Set: p.Set, Tools: []string{}}
	if err := p.Validate(); err != nil {
		return result, err
	}
	a, err := b.find(p.Agent)
	if err != nil {
		return result, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if err := b.clientToolScope(a); err != nil {
		return result, err
	}
	held := b.clientToolBuiltinNames(a)
	r := &b.clientTools
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return result, appwire.Errorf(appwire.CodeClosed, "the app connection is closed")
	}
	state := r.agents[p.Agent]
	for name := range state.builtins {
		held[name] = true
	}
	if err := clientToolReplacementValid(p, state.sets, held); err != nil {
		return result, err
	}
	sets := make([]session.ClientToolSet, 0, len(state.sets)+1)
	for _, set := range state.sets {
		if set.ID != p.Set {
			sets = append(sets, set)
		}
	}
	if len(p.Tools) > 0 {
		set := clientToolSnapshot(p)
		sets = append(sets, set)
		for _, tool := range set.Tools {
			result.Tools = append(result.Tools, tool.Name)
		}
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].ID < sets[j].ID })
	if r.agents == nil {
		r.agents = make(map[string]clientToolAgentSets)
	}
	r.agents[p.Agent] = clientToolAgentSets{version: state.version + 1, sets: sets, builtins: state.builtins}
	return result, nil
}

func (b *appBackend) clientToolScope(a *appAgent) error {
	remoteHeld := false
	b.locked(func() {
		if a.live != nil {
			_, remoteHeld = a.live.conn.Conv.(*remote.Agent)
			remoteHeld = remoteHeld && !a.live.conn.Assembled && !a.live.conn.Lent
		}
		remoteHeld = remoteHeld || a.view.Location != nil && !a.view.Location.Here
	})
	if remoteHeld {
		return appwire.Errorf(appwire.CodeBadScope, "client tools need a conversation this engine runs")
	}
	return nil
}

// Local app conversations also use a transport proxy. Their session reports its
// built-in inventory here, so validation still sees the actual engine belt.
func (d *appClientToolDoor) ClientToolBuiltins(names []string) {
	r := &d.backend.clientTools
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	state := r.agents[d.agent]
	state.builtins = make(map[string]bool, len(names))
	for _, name := range names {
		state.builtins[name] = true
	}
	if r.agents == nil {
		r.agents = make(map[string]clientToolAgentSets)
	}
	r.agents[d.agent] = state
}

func (b *appBackend) clientToolBuiltinNames(a *appAgent) map[string]bool {
	var conv appConversation
	b.locked(func() {
		if a.live != nil {
			conv = a.live.conn.Conv
		}
	})
	held := make(map[string]bool)
	if live, ok := conv.(interface{ ClientToolBuiltinNames() []string }); ok {
		for _, name := range live.ClientToolBuiltinNames() {
			held[name] = true
		}
	}
	return held
}

func clientToolReplacementValid(p appwire.ClientToolsSetParams, sets []session.ClientToolSet, builtins map[string]bool) error {
	names := make(map[string]bool)
	count, setCount := len(p.Tools), 0
	for _, set := range sets {
		if set.ID == p.Set {
			continue
		}
		setCount++
		count += len(set.Tools)
		for _, tool := range set.Tools {
			names[tool.Name] = true
		}
	}
	if len(p.Tools) > 0 {
		setCount++
	}
	if setCount > session.ClientToolMaxSets || count > session.ClientToolMaxPerAgent {
		return appwire.Errorf(appwire.CodeBadParams, "an agent allows at most %d sets and %d client tools", session.ClientToolMaxSets, session.ClientToolMaxPerAgent)
	}
	for _, tool := range p.Tools {
		if builtins[tool.Name] || session.ActionCategoryForTool(tool.Name) != session.ActionWork {
			return appwire.Errorf(appwire.CodeBadParams, "%s is a built-in engine tool", tool.Name)
		}
		if names[tool.Name] {
			return appwire.Errorf(appwire.CodeBadParams, "%s is already in another set", tool.Name)
		}
	}
	return nil
}

func clientToolSnapshot(p appwire.ClientToolsSetParams) session.ClientToolSet {
	set := session.ClientToolSet{ID: p.Set, Instructions: p.Instructions, DeadlineMS: p.DeadlineMS,
		Tools: append([]session.ClientTool(nil), p.Tools...)}
	if set.DeadlineMS == 0 {
		set.DeadlineMS = session.ClientToolDefaultDeadlineMS
	}
	for i := range set.Tools {
		set.Tools[i].InputSchema = bytes.Clone(set.Tools[i].InputSchema)
	}
	return set
}

func (d *appClientToolDoor) ClientToolSets() (uint64, []session.ClientToolSet) {
	r := &d.backend.clientTools
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.agents[d.agent]
	return state.version, state.sets
}

func clientToolCallID(agent, provider string) string {
	if provider == "" {
		return fmt.Sprintf("ct_fresh_%x", clientToolFreshID.Add(1))
	}
	sum := sha256.Sum256([]byte(agent + "\x00" + provider))
	return "ct_" + hex.EncodeToString(sum[:])
}

func (d *appClientToolDoor) CallClientTool(ctx context.Context, call session.ClientToolCall) (session.ClientToolAnswer, error) {
	if err := ctx.Err(); err != nil {
		return clientToolTurnEnded(call.Name), nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(call.Args, &object) != nil || object == nil {
		return session.ClientToolAnswer{Text: "Invalid arguments: expected a JSON object", IsError: true}, nil
	}
	id := clientToolCallID(d.agent, call.TurnCall)
	r := &d.backend.clientTools
	pending, answer, emit := r.begin(ctx, d.agent, id, call)
	if pending == nil {
		return answer, nil
	}
	if emit {
		var turn uint64
		d.backend.locked(func() {
			if a := d.backend.agents[d.agent]; a != nil && a.live != nil {
				turn = a.live.turn
			}
		})
		r.queueEmission(d.backend.emit, pending, appwire.AgentsToolCall{Agent: d.agent, Turn: turn,
			Call: id, TurnCall: call.TurnCall, SourceMessageID: call.SourceMessage, Set: call.Set, Name: call.Name, Args: bytes.Clone(call.Args), DeadlineMS: pending.call.DeadlineMS})
	}
	return r.wait(ctx, id, pending), nil
}

// One sender owns all writer waits. Queued events live only in pending calls,
// so calls that end behind a blocked writer leave no stale queue to grow.
func (r *appClientTools) queueEmission(emit appwire.Emitter, pending *clientToolPending, event appwire.AgentsToolCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.pending[event.Call] != pending {
		return
	}
	pending.event = &event
	if r.senderWake == nil {
		r.senderWake = make(chan struct{}, 1)
		r.senderDone = make(chan struct{})
		guard.Go("app/client-tools", func() { r.sendEvents(emit) })
	}
	select {
	case r.senderWake <- struct{}{}:
	default:
	}
}

func (r *appClientTools) sendEvents(emit appwire.Emitter) {
	defer close(r.senderDone)
	for range r.senderWake {
		for {
			event := r.nextEmission()
			if event == nil {
				break
			}
			emit.Emit(appwire.EventAgentsToolCall, *event)
		}
	}
}

func (r *appClientTools) nextEmission() *appwire.AgentsToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	for _, pending := range r.pending {
		event := pending.event
		pending.event = nil
		if event != nil && pending.ctx.Err() == nil && time.Now().Before(pending.answerBy) {
			return event
		}
	}
	return nil
}

// begin takes the registry lock for [appClientTools.beginLocked] alone, so a
// panic inside it cannot leave the lock held.
func (r *appClientTools) begin(ctx context.Context, agent, id string, call session.ClientToolCall) (*clientToolPending, session.ClientToolAnswer, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.beginLocked(ctx, agent, id, call)
}

// Register before emitting, so even a synchronous client answer finds its waiter.
func (r *appClientTools) beginLocked(ctx context.Context, agent, id string, call session.ClientToolCall) (*clientToolPending, session.ClientToolAnswer, bool) {
	r.pruneLocked(time.Now())
	if settled, ok := r.settled[id]; ok {
		return nil, settled.answer, false
	}
	if pending := r.pending[id]; pending != nil {
		return pending, session.ClientToolAnswer{}, false
	}
	waitMS, attached := r.attachedLocked(agent, call.Set, call.Name)
	if r.closed || !attached {
		return nil, session.ClientToolUnavailable(call.Name), false
	}
	if call.DeadlineMS >= session.ClientToolMinDeadlineMS && call.DeadlineMS <= session.ClientToolMaxDeadlineMS {
		waitMS = call.DeadlineMS
	}
	call.DeadlineMS = waitMS
	pending := &clientToolPending{ctx: ctx, done: make(chan struct{}), call: call, answerBy: time.Now().Add(time.Duration(waitMS) * time.Millisecond)}
	if r.pending == nil {
		r.pending = make(map[string]*clientToolPending)
	}
	r.pending[id] = pending
	return pending, session.ClientToolAnswer{}, true
}

func (r *appClientTools) attachedLocked(agent, setID, name string) (int, bool) {
	for _, set := range r.agents[agent].sets {
		if set.ID != setID {
			continue
		}
		for _, tool := range set.Tools {
			if tool.Name == name {
				return set.DeadlineMS, true
			}
		}
	}
	return 0, false
}

func (r *appClientTools) wait(ctx context.Context, id string, pending *clientToolPending) session.ClientToolAnswer {
	timer := time.NewTimer(time.Until(pending.answerBy))
	defer timer.Stop()
	var failed session.ClientToolAnswer
	select {
	case <-pending.done:
		return pending.answer
	case <-timer.C:
		failed = clientToolTimeout(pending.call)
	case <-ctx.Done():
		failed = clientToolTurnEnded(pending.call.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settleLocked(id, pending, failed)
	return pending.answer
}

func clientToolTimeout(call session.ClientToolCall) session.ClientToolAnswer {
	return session.ClientToolAnswer{Text: fmt.Sprintf("client_timeout: the app that provides %s did not answer within %gs; the outcome is unknown, so check before trying again.", call.Name, float64(call.DeadlineMS)/1000), IsError: true}
}

func clientToolTurnEnded(name string) session.ClientToolAnswer {
	return session.ClientToolAnswer{Text: fmt.Sprintf("the turn ended before the app answered %s; the outcome is unknown, so check before trying again.", name), IsError: true}
}

func (r *appClientTools) settleLocked(id string, pending *clientToolPending, answer session.ClientToolAnswer) bool {
	if r.pending[id] != pending {
		return false
	}
	delete(r.pending, id)
	pending.answer = answer
	if r.settled == nil {
		r.settled = make(map[string]clientToolSettled)
	}
	r.pruneLocked(time.Now())
	if len(r.order) == clientToolSettledMax {
		delete(r.settled, r.order[0])
		r.order = r.order[1:]
	}
	r.settled[id] = clientToolSettled{answer: answer, at: time.Now()}
	r.order = append(r.order, id)
	close(pending.done)
	return true
}

func (r *appClientTools) pruneLocked(now time.Time) {
	for len(r.order) > 0 {
		id := r.order[0]
		if now.Sub(r.settled[id].at) < clientToolSettledTTL {
			break
		}
		delete(r.settled, id)
		r.order = r.order[1:]
	}
}

func (b *appBackend) ClientToolResult(p appwire.ClientToolResultParams) (appwire.ClientToolResultAck, error) {
	if err := p.Validate(); err != nil {
		return appwire.ClientToolResultAck{}, err
	}
	var result string
	if p.Error == nil {
		var compact bytes.Buffer
		_ = json.Compact(&compact, p.Result)
		result = session.BoundClientToolOutput(compact.String())
	}
	r := &b.clientTools
	r.mu.Lock()
	defer r.mu.Unlock()
	pending := r.pending[p.Call]
	if pending == nil {
		return appwire.ClientToolResultAck{}, nil
	}
	if pending.ctx.Err() != nil {
		r.settleLocked(p.Call, pending, clientToolTurnEnded(pending.call.Name))
		return appwire.ClientToolResultAck{}, nil
	}
	if !time.Now().Before(pending.answerBy) {
		r.settleLocked(p.Call, pending, clientToolTimeout(pending.call))
		return appwire.ClientToolResultAck{}, nil
	}
	answer := session.ClientToolAnswer{}
	if p.Error != nil {
		answer.Text = fmt.Sprintf("%s could not do %s: %s", pending.call.Set, pending.call.Name, *p.Error)
		answer.IsError = true
	} else {
		answer.Text = result
	}
	return appwire.ClientToolResultAck{Accepted: r.settleLocked(p.Call, pending, answer)}, nil
}

// Disconnect precedes conversation shutdown, so pending calls receive the owner-loss answer.
func (r *appClientTools) disconnect() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	if r.senderWake != nil {
		close(r.senderWake)
	}
	for agent, state := range r.agents {
		r.agents[agent] = clientToolAgentSets{version: state.version + 1}
	}
	for id, pending := range r.pending {
		answer := session.ClientToolAnswer{Text: fmt.Sprintf("client_unavailable: the app that provides %s disconnected before answering; the outcome is unknown, so check before trying again.", pending.call.Name), IsError: true}
		r.settleLocked(id, pending, answer)
	}
}
