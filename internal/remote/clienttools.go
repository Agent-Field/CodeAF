package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Client tool methods ride the existing connection without opening a stream.
const (
	MethodClientToolsSet   = "ClientTools.Set"
	MethodClientToolAnswer = "ClientTool.Answer"
	clientToolCallKind     = "clientToolCall"
	clientToolEndedKind    = "clientToolEnded"
	clientToolOwnerMin     = 26
	clientToolOwnerMax     = 256
)

// ErrClientToolsUnsupported means no forwarding capability was advertised.
var ErrClientToolsUnsupported = errors.New("the holder does not forward client tools")

// ErrClientToolDisconnected distinguishes a lost connection from a stopped turn.
var ErrClientToolDisconnected = errors.New("the client tool connection ended")

// ClientToolsRefusal is a holder's atomic refusal of a declaration snapshot.
type ClientToolsRefusal struct{ Reason string }

func (e *ClientToolsRefusal) Error() string { return e.Reason }

type clientToolCallWire struct {
	Call          string          `json:"call"`
	Set           string          `json:"set"`
	Name          string          `json:"name"`
	TurnCall      string          `json:"turn_call"`
	SourceMessage string          `json:"source_message,omitempty"`
	Args          json.RawMessage `json:"args"`
	DeadlineMS    int             `json:"deadline_ms"`
}

type clientToolAnswerWire struct {
	Call    string `json:"call"`
	Text    string `json:"text"`
	IsError bool   `json:"is_error"`
}

type clientToolSetsWire struct {
	Owner string                  `json:"owner"`
	Sets  []session.ClientToolSet `json:"sets"`
}

type clientToolEndedWire struct {
	Call string `json:"call"`
}

type clientToolAck struct {
	Accepted bool `json:"accepted"`
}

type clientToolWait struct {
	ctx      context.Context
	owner    *server
	call     session.ClientToolCall
	answerBy time.Time
	done     chan struct{}
	answer   session.ClientToolAnswer
	sent     bool
}

type clientToolOwner struct {
	token string
	sets  []session.ClientToolSet
	wake  chan struct{}
	ended map[string]bool
}

// ClientToolHub owns one conversation's declarations and routes by connection.
// Its zero value is ready to use; snapshots remain immutable after publication.
type ClientToolHub struct {
	mu       sync.Mutex
	owners   map[*server]*clientToolOwner
	builtins map[string]bool
	version  uint64
	sets     []session.ClientToolSet
	pending  map[string]*clientToolWait
	sequence uint64
	// swapped and conversation record the conversation a swap moved to
	// ([ClientToolHub.forget]), so a joined connection's late snapshot for the
	// one it left is refused.
	swapped      bool
	conversation string
}

var _ session.ClientToolDoor = (*ClientToolHub)(nil)

// ClientToolSets returns the merged snapshot sorted by set id and first tool name.
func (h *ClientToolHub) ClientToolSets() (uint64, []session.ClientToolSet) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.version, h.sets
}

// ClientToolBuiltins includes deferred engine tools in collision checks.
func (h *ClientToolHub) ClientToolBuiltins(names []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.builtins = make(map[string]bool, len(names))
	for _, name := range names {
		h.builtins[name] = true
	}
}

func cloneClientToolSets(sets []session.ClientToolSet) []session.ClientToolSet {
	copySets := make([]session.ClientToolSet, len(sets))
	copy(copySets, sets)
	for i := range copySets {
		copySets[i].Tools = append([]session.ClientTool(nil), sets[i].Tools...)
		for j := range copySets[i].Tools {
			copySets[i].Tools[j].InputSchema = bytes.Clone(sets[i].Tools[j].InputSchema)
		}
		if copySets[i].DeadlineMS == 0 {
			copySets[i].DeadlineMS = session.ClientToolDefaultDeadlineMS
		}
	}
	return copySets
}

func (h *ClientToolHub) replace(owner *server, token string, sets []session.ClientToolSet) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if owner.clientToolsGone.Load() {
		return nil, ErrClientToolDisconnected
	}
	if h.swapped && owner.joined != "" && !sameTranscript(owner.joined, h.conversation) {
		return nil, ErrJoinedGone
	}
	if len(token) < clientToolOwnerMin || len(token) > clientToolOwnerMax {
		return nil, fmt.Errorf("client tools need an owner token of %d–%d bytes", clientToolOwnerMin, clientToolOwnerMax)
	}
	if state := h.owners[owner]; state != nil && state.token != token {
		return nil, errors.New("client tool owner token cannot change on a connection")
	}
	names := make(map[string]bool)
	ids := make(map[string]bool)
	setCount, toolCount := len(sets), 0
	for other, state := range h.owners {
		if other == owner || state.token == token {
			continue
		}
		setCount += len(state.sets)
		for _, set := range state.sets {
			toolCount += len(set.Tools)
			for _, tool := range set.Tools {
				names[tool.Name] = true
			}
		}
	}
	attached := []string{}
	for _, set := range sets {
		if err := session.ValidateClientToolSet(set); err != nil {
			return nil, err
		}
		if ids[set.ID] {
			return nil, fmt.Errorf("duplicate set id %q", set.ID)
		}
		ids[set.ID] = true
		toolCount += len(set.Tools)
		for _, tool := range set.Tools {
			if h.builtins[tool.Name] || session.ActionCategoryForTool(tool.Name) != session.ActionWork {
				return nil, fmt.Errorf("%s is a built-in engine tool", tool.Name)
			}
			if names[tool.Name] {
				return nil, fmt.Errorf("%s is already in another set", tool.Name)
			}
			names[tool.Name] = true
			attached = append(attached, tool.Name)
		}
	}
	if setCount > session.ClientToolMaxSets || toolCount > session.ClientToolMaxPerAgent {
		return nil, fmt.Errorf("an agent allows at most %d sets and %d client tools", session.ClientToolMaxSets, session.ClientToolMaxPerAgent)
	}
	if h.owners == nil {
		h.owners = make(map[*server]*clientToolOwner)
	}
	for other, state := range h.owners {
		if other != owner && state.token == token {
			other.clientToolsGone.Store(true)
			h.dropLocked(other)
		}
	}
	state := h.owners[owner]
	if state == nil {
		state = &clientToolOwner{token: token, wake: make(chan struct{}, 1), ended: make(map[string]bool)}
		h.owners[owner] = state
		guard.Go("remote/client-tools-sender", func() { h.sendCalls(owner, state) })
	}
	state.sets = cloneClientToolSets(sets)
	h.snapshotLocked()
	return attached, nil
}

func (h *ClientToolHub) snapshotLocked() {
	var sets []session.ClientToolSet
	for _, state := range h.owners {
		sets = append(sets, state.sets...)
	}
	sort.Slice(sets, func(i, j int) bool {
		if sets[i].ID != sets[j].ID {
			return sets[i].ID < sets[j].ID
		}
		return clientToolFirstName(sets[i]) < clientToolFirstName(sets[j])
	})
	h.sets = sets
	h.version++
}

func clientToolFirstName(set session.ClientToolSet) string {
	if len(set.Tools) == 0 {
		return ""
	}
	return set.Tools[0].Name
}

func clientToolDisconnected(name string) session.ClientToolAnswer {
	return session.ClientToolAnswer{Text: fmt.Sprintf("client_unavailable: the app that provides %s disconnected before answering; the outcome is unknown, so check before trying again.", name), IsError: true}
}

func clientToolWaitEnded(call session.ClientToolCall, timedOut bool) session.ClientToolAnswer {
	if timedOut {
		return session.ClientToolAnswer{Text: fmt.Sprintf("client_timeout: the app that provides %s did not answer within %gs; the outcome is unknown, so check before trying again.", call.Name, float64(call.DeadlineMS)/1000), IsError: true}
	}
	return session.ClientToolAnswer{Text: fmt.Sprintf("the turn ended before the app answered %s; the outcome is unknown, so check before trying again.", call.Name), IsError: true}
}

func (h *ClientToolHub) settleLocked(id string, pending *clientToolWait, answer session.ClientToolAnswer, answered bool) bool {
	if h.pending[id] != pending {
		return false
	}
	delete(h.pending, id)
	pending.answer = answer
	if state := h.owners[pending.owner]; state != nil && pending.sent && !answered && !pending.owner.clientToolsGone.Load() {
		state.ended[id] = true
		h.wakeLocked(state)
	}
	close(pending.done)
	return true
}

// forget ends the hub's tie to the conversation a session is leaving. The hub
// outlives [Session.swap], but sets belong to one conversation: every
// connection's sets go, so none reach the replacement, and each pending call
// ends with an ended frame, so the app stops waiting too. A joined connection
// cannot attach again; any other connection may attach to the new one.
func (h *ClientToolHub) forget(file string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.swapped, h.conversation = true, file
	for _, state := range h.owners {
		state.sets = nil
	}
	for id, pending := range h.pending {
		h.settleLocked(id, pending, clientToolWaitEnded(pending.call, false), false)
	}
	h.snapshotLocked()
}

func (h *ClientToolHub) drop(owner *server) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropLocked(owner)
}

func (h *ClientToolHub) dropLocked(owner *server) {
	if state := h.owners[owner]; state != nil {
		delete(h.owners, owner)
		close(state.wake)
		h.snapshotLocked()
	}
	for id, pending := range h.pending {
		if pending.owner == owner {
			h.settleLocked(id, pending, clientToolDisconnected(pending.call.Name), false)
		}
	}
}

// CallClientTool waits for only the connection that attached this declaration.
func (h *ClientToolHub) CallClientTool(ctx context.Context, call session.ClientToolCall) (session.ClientToolAnswer, error) {
	if ctx.Err() != nil {
		return clientToolWaitEnded(call, false), nil
	}
	id, pending := h.begin(ctx, call)
	if pending == nil {
		return session.ClientToolUnavailable(call.Name), nil
	}
	timer := time.NewTimer(time.Until(pending.answerBy))
	defer timer.Stop()
	timedOut := false
	select {
	case <-pending.done:
		return pending.answer, nil
	case <-timer.C:
		timedOut = true
	case <-ctx.Done():
	}
	return h.endWait(id, pending, timedOut), nil
}

func (h *ClientToolHub) begin(ctx context.Context, call session.ClientToolCall) (string, *clientToolWait) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var owner *server
	for candidate, state := range h.owners {
		for _, set := range state.sets {
			if set.ID != call.Set {
				continue
			}
			for _, tool := range set.Tools {
				if tool.Name == call.Name {
					owner, call.DeadlineMS = candidate, set.DeadlineMS
				}
			}
		}
	}
	if owner == nil || owner.clientToolsGone.Load() {
		return "", nil
	}
	h.sequence++
	id := fmt.Sprintf("ct_remote_%x", h.sequence)
	pending := &clientToolWait{ctx: ctx, owner: owner, call: call, done: make(chan struct{}),
		answerBy: time.Now().Add(time.Duration(call.DeadlineMS) * time.Millisecond)}
	if h.pending == nil {
		h.pending = make(map[string]*clientToolWait)
	}
	h.pending[id] = pending
	h.wakeLocked(h.owners[owner])
	return id, pending
}

func (h *ClientToolHub) endWait(id string, pending *clientToolWait, timedOut bool) session.ClientToolAnswer {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.settleLocked(id, pending, clientToolWaitEnded(pending.call, timedOut), false)
	return pending.answer
}

func (h *ClientToolHub) wakeLocked(state *clientToolOwner) {
	select {
	case state.wake <- struct{}{}:
	default:
	}
}

// Each connection has one sender. Calls waiting behind a blocked write live
// only in pending, so their waits remove both their arguments and queued work.
func (h *ClientToolHub) sendCalls(owner *server, state *clientToolOwner) {
	for range state.wake {
		for {
			sent, err := h.sendNext(owner, state)
			if err != nil {
				owner.dropClientTools()
				return
			}
			if !sent {
				break
			}
		}
	}
}

func (h *ClientToolHub) sendNext(owner *server, state *clientToolOwner) (bool, error) {
	owner.write.Lock()
	defer owner.write.Unlock()
	// Check after taking the writer: another frame may have held it past the wait.
	frame, ok := h.nextEmission(owner, state)
	if !ok {
		return false, nil
	}
	return true, owner.sendLocked(frame)
}

func (h *ClientToolHub) nextEmission(owner *server, state *clientToolOwner) (Frame, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.owners[owner] != state || owner.clientToolsGone.Load() {
		return Frame{}, false
	}
	for id := range state.ended {
		delete(state.ended, id)
		return Frame{Kind: clientToolEndedKind, Payload: mustJSON(clientToolEndedWire{Call: id})}, true
	}
	for id, pending := range h.pending {
		if pending.owner != owner || pending.sent {
			continue
		}
		if pending.ctx.Err() != nil || !time.Now().Before(pending.answerBy) {
			h.settleLocked(id, pending, clientToolWaitEnded(pending.call, pending.ctx.Err() == nil), false)
			continue
		}
		pending.sent = true
		call := pending.call
		wire := clientToolCallWire{Call: id, Set: call.Set, Name: call.Name, TurnCall: call.TurnCall,
			SourceMessage: call.SourceMessage, Args: call.Args, DeadlineMS: call.DeadlineMS}
		return Frame{Kind: clientToolCallKind, Payload: mustJSON(wire)}, true
	}
	return Frame{}, false
}

func (h *ClientToolHub) answer(owner *server, wire clientToolAnswerWire) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	pending := h.pending[wire.Call]
	if pending == nil || pending.owner != owner {
		return false
	}
	if owner.clientToolsGone.Load() {
		h.settleLocked(wire.Call, pending, clientToolDisconnected(pending.call.Name), false)
		return false
	}
	if pending.ctx.Err() != nil {
		h.settleLocked(wire.Call, pending, clientToolWaitEnded(pending.call, false), false)
		return false
	}
	if !time.Now().Before(pending.answerBy) {
		h.settleLocked(wire.Call, pending, clientToolWaitEnded(pending.call, true), false)
		return false
	}
	// The app already bounded this text; cutting again would replace its byte count.
	return h.settleLocked(wire.Call, pending, session.ClientToolAnswer{Text: wire.Text, IsError: wire.IsError}, true)
}

func (s *server) dropClientTools() {
	s.clientToolsGone.Store(true)
	if s.session != nil && s.session.engine != nil && s.session.engine.ClientTools != nil {
		s.session.engine.ClientTools.drop(s)
	}
}

func (s *server) invokeClientTools(call Frame) (json.RawMessage, error) {
	hub := s.session.engine.ClientTools
	if hub == nil {
		return nil, ErrClientToolsUnsupported
	}
	switch call.Method {
	case MethodClientToolsSet:
		snapshot, err := arg[clientToolSetsWire](call)
		if err != nil {
			return nil, err
		}
		names, err := hub.replace(s, snapshot.Owner, snapshot.Sets)
		return mustJSON(names), err
	case MethodClientToolAnswer:
		answer, err := arg[clientToolAnswerWire](call)
		if err != nil {
			return nil, err
		}
		return mustJSON(clientToolAck{Accepted: hub.answer(s, answer)}), nil
	}
	return nil, errors.New("unknown client tool method")
}

type clientToolIncoming struct {
	cancel context.CancelCauseFunc
}

type clientToolLink struct {
	setMu    sync.Mutex
	owner    string
	sets     []session.ClientToolSet
	attached bool
	welcome  Welcome

	handler  func(context.Context, session.ClientToolCall) (session.ClientToolAnswer, error)
	incoming map[string]*clientToolIncoming
}

// SupportsClientTools reports the welcome capability without a wire call.
func (a *Agent) SupportsClientTools() bool {
	if a == nil || a.c == nil {
		return false
	}
	return a.c.Welcome().ClientTools
}

// SetClientTools atomically replaces this connection's entire snapshot.
func (a *Agent) SetClientTools(ctx context.Context, sets []session.ClientToolSet) ([]string, error) {
	if !a.SupportsClientTools() {
		return nil, ErrClientToolsUnsupported
	}
	c := a.c
	c.clientTools.setMu.Lock()
	defer c.clientTools.setMu.Unlock()
	sets = cloneClientToolSets(sets)
	welcome := c.Welcome()
	payload, answered, err := c.callAnswered(ctx, MethodClientToolsSet,
		clientToolSetsWire{Owner: c.clientTools.owner, Sets: sets}, callDeadline)
	if err != nil {
		if answered && c.clientToolHolderAnswered() {
			return nil, &ClientToolsRefusal{Reason: err.Error()}
		}
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(payload, &names); err != nil {
		return nil, err
	}
	if !c.rememberClientTools(sets, welcome) {
		return nil, ErrClientToolDisconnected
	}
	return names, nil
}

func (c *Client) clientToolHolderAnswered() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dead == nil && !c.reconnecting && !c.closing
}

// Durable transcript identity survives a holder restart. Without a transcript,
// the live instance is the only conversation identity the welcome supplies.
func clientToolConversationChanged(left, now Welcome) bool {
	if left.SessionFile != "" {
		return conversationSwitched(left.SessionFile, now.SessionFile)
	}
	return left.SessionInstance != "" && left.SessionInstance != now.SessionInstance
}

func (c *Client) rememberClientTools(sets []session.ClientToolSet, welcome Welcome) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if clientToolConversationChanged(welcome, c.welcome) {
		c.forgetClientToolsLocked()
		return false
	}
	c.clientTools.sets, c.clientTools.attached = sets, true
	c.clientTools.welcome = Welcome{SessionFile: welcome.SessionFile, SessionInstance: welcome.SessionInstance}
	return true
}

func (c *Client) forgetClientToolsLocked() {
	c.clientTools.sets = nil
	c.clientTools.attached = false
	c.clientTools.welcome = Welcome{}
	c.endClientToolCallsLocked()
}

// SetClientToolHandler replaces the one handler and cancels its previous calls.
// Passing nil releases the handler when a surface stops holding the connection.
func (a *Agent) SetClientToolHandler(handler func(context.Context, session.ClientToolCall) (session.ClientToolAnswer, error)) {
	if a == nil || a.c == nil {
		return
	}
	c := a.c
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endClientToolCallsLocked()
	c.clientTools.handler = handler
}

func (c *Client) endClientToolCalls() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endClientToolCallsLocked()
}

func (c *Client) endClientToolCallsLocked() {
	for id, incoming := range c.clientTools.incoming {
		incoming.cancel(ErrClientToolDisconnected)
		delete(c.clientTools.incoming, id)
	}
}

func (c *Client) endClientToolCall(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if incoming := c.clientTools.incoming[id]; incoming != nil {
		incoming.cancel(context.Canceled)
		delete(c.clientTools.incoming, id)
	}
}

func (c *Client) beginClientToolCall(wire clientToolCallWire) (context.Context, *clientToolIncoming, func(context.Context, session.ClientToolCall) (session.ClientToolAnswer, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing || c.dead != nil || c.reconnecting || c.clientTools.incoming[wire.Call] != nil {
		return nil, nil, nil
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	incoming := &clientToolIncoming{cancel: cancel}
	if c.clientTools.incoming == nil {
		c.clientTools.incoming = make(map[string]*clientToolIncoming)
	}
	c.clientTools.incoming[wire.Call] = incoming
	return ctx, incoming, c.clientTools.handler
}

func (c *Client) clientToolCallActive(id string, incoming *clientToolIncoming) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clientTools.incoming[id] == incoming
}

func (c *Client) finishClientToolCall(id string, incoming *clientToolIncoming) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.clientTools.incoming[id] == incoming {
		delete(c.clientTools.incoming, id)
	}
}

func (c *Client) clientToolFrame(frame Frame) {
	if frame.Kind == clientToolEndedKind {
		var ended clientToolEndedWire
		if json.Unmarshal(frame.Payload, &ended) == nil {
			c.endClientToolCall(ended.Call)
		}
		return
	}
	var wire clientToolCallWire
	if json.Unmarshal(frame.Payload, &wire) != nil || wire.Call == "" {
		return
	}
	ctx, incoming, handler := c.beginClientToolCall(wire)
	if incoming == nil {
		return
	}
	guard.Go("remote/client-tool-handler", func() {
		defer incoming.cancel(context.Canceled)
		defer c.finishClientToolCall(wire.Call, incoming)
		call := session.ClientToolCall{Set: wire.Set, Name: wire.Name, TurnCall: wire.TurnCall,
			SourceMessage: wire.SourceMessage, Args: wire.Args, DeadlineMS: wire.DeadlineMS}
		answer := session.ClientToolUnavailable(call.Name)
		if handler != nil {
			var err error
			answer, err = handler(ctx, call)
			if err != nil {
				answer = session.ClientToolAnswer{Text: err.Error(), IsError: true}
			}
		}
		if c.clientToolCallActive(wire.Call, incoming) && ctx.Err() == nil {
			_, _ = c.call(ctx, MethodClientToolAnswer,
				clientToolAnswerWire{Call: wire.Call, Text: answer.Text, IsError: answer.IsError})
		}
	})
}

func (c *Client) replayClientToolSets() ([]session.ClientToolSet, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.clientTools.attached {
		return nil, false
	}
	if clientToolConversationChanged(c.clientTools.welcome, c.welcome) {
		c.forgetClientToolsLocked()
		return nil, false
	}
	return c.clientTools.sets, c.welcome.ClientTools
}

func (c *Client) resendClientTools() {
	// The reader must resume before a synchronous snapshot can receive its reply.
	guard.Go("remote/client-tools-reattach", func() {
		c.clientTools.setMu.Lock()
		defer c.clientTools.setMu.Unlock()
		sets, attached := c.replayClientToolSets()
		if !attached {
			return
		}
		if _, err := c.call(context.Background(), MethodClientToolsSet,
			clientToolSetsWire{Owner: c.clientTools.owner, Sets: sets}); err != nil {
			c.note("client tools could not be reattached: " + err.Error())
		}
	})
}
