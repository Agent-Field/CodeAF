package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	answered bool
}

// ClientToolHub owns one conversation's declarations and routes by connection.
// Its zero value is ready to use; snapshots remain immutable after publication.
type ClientToolHub struct {
	mu       sync.Mutex
	owners   map[*server][]session.ClientToolSet
	builtins map[string]bool
	version  uint64
	sets     []session.ClientToolSet
	pending  map[string]*clientToolWait
	sequence uint64
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

func (h *ClientToolHub) replace(owner *server, sets []session.ClientToolSet) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if owner.clientToolsGone.Load() {
		return nil, ErrClientToolDisconnected
	}
	names := make(map[string]bool)
	ids := make(map[string]bool)
	setCount, toolCount := len(sets), 0
	for other, attached := range h.owners {
		if other == owner {
			continue
		}
		setCount += len(attached)
		for _, set := range attached {
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
		h.owners = make(map[*server][]session.ClientToolSet)
	}
	if len(sets) == 0 {
		delete(h.owners, owner)
	} else {
		h.owners[owner] = cloneClientToolSets(sets)
	}
	h.snapshotLocked()
	return attached, nil
}

func (h *ClientToolHub) snapshotLocked() {
	var sets []session.ClientToolSet
	for _, attached := range h.owners {
		sets = append(sets, attached...)
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
	pending.answer, pending.answered = answer, answered
	close(pending.done)
	return true
}

func (h *ClientToolHub) drop(owner *server) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.owners[owner]; ok {
		delete(h.owners, owner)
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
	h.mu.Lock()
	var owner *server
	for candidate, sets := range h.owners {
		for _, set := range sets {
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
		h.mu.Unlock()
		return session.ClientToolUnavailable(call.Name), nil
	}
	h.sequence++
	id := fmt.Sprintf("ct_remote_%x", h.sequence)
	pending := &clientToolWait{ctx: ctx, owner: owner, call: call, done: make(chan struct{}),
		answerBy: time.Now().Add(time.Duration(call.DeadlineMS) * time.Millisecond)}
	if h.pending == nil {
		h.pending = make(map[string]*clientToolWait)
	}
	h.pending[id] = pending
	h.mu.Unlock()
	guard.Go("remote/client-tool", func() {
		select {
		case <-pending.done:
			return
		default:
		}
		wire := clientToolCallWire{Call: id, Set: call.Set, Name: call.Name, TurnCall: call.TurnCall,
			SourceMessage: call.SourceMessage, Args: call.Args, DeadlineMS: call.DeadlineMS}
		if err := owner.send(Frame{Kind: clientToolCallKind, Payload: mustJSON(wire)}); err != nil {
			owner.dropClientTools()
			return
		}
		<-pending.done
		if !pending.answered && !owner.clientToolsGone.Load() {
			_ = owner.send(Frame{Kind: clientToolEndedKind, Payload: mustJSON(clientToolEndedWire{Call: id})})
		}
	})
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
	h.mu.Lock()
	h.settleLocked(id, pending, clientToolWaitEnded(call, timedOut), false)
	h.mu.Unlock()
	return pending.answer, nil
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
		sets, err := arg[[]session.ClientToolSet](call)
		if err != nil {
			return nil, err
		}
		names, err := hub.replace(s, sets)
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
	sets     []session.ClientToolSet
	attached bool
	handler  func(context.Context, session.ClientToolCall) (session.ClientToolAnswer, error)
	incoming map[string]*clientToolIncoming
}

// SupportsClientTools reports the welcome capability without a wire call.
func (a *Agent) SupportsClientTools() bool {
	if a == nil || a.c == nil {
		return false
	}
	a.c.mu.Lock()
	defer a.c.mu.Unlock()
	return a.c.welcome.ClientTools
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
	payload, answered, err := c.clientToolRequest(ctx, MethodClientToolsSet, sets, nil)
	if err != nil {
		if answered {
			return nil, &ClientToolsRefusal{Reason: err.Error()}
		}
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(payload, &names); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.clientTools.sets, c.clientTools.attached = sets, true
	c.mu.Unlock()
	return names, nil
}

// SetClientToolHandler replaces the one handler and cancels its previous calls.
// Passing nil releases the handler when a surface stops holding the connection.
func (a *Agent) SetClientToolHandler(handler func(context.Context, session.ClientToolCall) (session.ClientToolAnswer, error)) {
	if a == nil || a.c == nil {
		return
	}
	c := a.c
	c.mu.Lock()
	c.endClientToolCallsLocked()
	c.clientTools.handler = handler
	c.mu.Unlock()
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

func (c *Client) clientToolFrame(frame Frame) {
	if frame.Kind == clientToolEndedKind {
		var ended clientToolEndedWire
		if json.Unmarshal(frame.Payload, &ended) == nil {
			c.mu.Lock()
			if incoming := c.clientTools.incoming[ended.Call]; incoming != nil {
				incoming.cancel(context.Canceled)
				delete(c.clientTools.incoming, ended.Call)
			}
			c.mu.Unlock()
		}
		return
	}
	var wire clientToolCallWire
	if json.Unmarshal(frame.Payload, &wire) != nil || wire.Call == "" {
		return
	}
	c.mu.Lock()
	if c.closing || c.dead != nil || c.reconnecting || c.clientTools.incoming[wire.Call] != nil {
		c.mu.Unlock()
		return
	}
	handler, conn := c.clientTools.handler, c.conn
	ctx, cancel := context.WithCancelCause(context.Background())
	incoming := &clientToolIncoming{cancel: cancel}
	if c.clientTools.incoming == nil {
		c.clientTools.incoming = make(map[string]*clientToolIncoming)
	}
	c.clientTools.incoming[wire.Call] = incoming
	c.mu.Unlock()
	guard.Go("remote/client-tool-handler", func() {
		defer cancel(context.Canceled)
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
		c.mu.Lock()
		active := c.clientTools.incoming[wire.Call] == incoming
		c.mu.Unlock()
		if active && ctx.Err() == nil {
			_, _, _ = c.clientToolRequest(ctx, MethodClientToolAnswer,
				clientToolAnswerWire{Call: wire.Call, Text: answer.Text, IsError: answer.IsError}, conn)
		}
		c.mu.Lock()
		if c.clientTools.incoming[wire.Call] == incoming {
			delete(c.clientTools.incoming, wire.Call)
		}
		c.mu.Unlock()
	})
}

// A pinned pipe prevents an answer to an old call from crossing a redial.
// The writer runs separately so an unresponsive pipe cannot defeat the wait bound.
func (c *Client) clientToolRequest(ctx context.Context, method string, args any, pinned io.ReadWriteCloser) (json.RawMessage, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, callDeadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	payload, err := json.Marshal(args)
	if err != nil {
		return nil, false, err
	}
	id := c.seq.Add(1)
	waiting := make(chan result, 1)
	writeFailed := make(chan error, 1)
	c.mu.Lock()
	conn := c.conn
	if pinned != nil {
		conn = pinned
	}
	if conn == nil || c.dead != nil || c.closing || c.reconnecting {
		c.mu.Unlock()
		return nil, false, ErrClientToolDisconnected
	}
	if !c.welcome.ClientTools {
		c.mu.Unlock()
		return nil, false, ErrClientToolsUnsupported
	}
	c.calls[id] = waiting
	c.mu.Unlock()
	defer c.forget(id)
	guard.Go("remote/client-tool-write", func() {
		line, err := json.Marshal(Frame{Kind: "call", ID: id, Method: method, Payload: payload})
		if err == nil {
			c.writeMu.Lock()
			if ctx.Err() != nil {
				err = ctx.Err()
			} else {
				_, err = conn.Write(append(line, '\n'))
				if err == nil {
					err = flushFrame(conn)
					c.made.Add(1)
				}
			}
			c.writeMu.Unlock()
		}
		if err != nil {
			writeFailed <- err
		}
	})
	select {
	case answer := <-waiting:
		return answer.payload, answer.answered, answer.err
	case err := <-writeFailed:
		return nil, false, err
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

func (c *Client) resendClientTools() {
	c.mu.Lock()
	attached := c.clientTools.attached && c.welcome.ClientTools
	c.mu.Unlock()
	if !attached {
		return
	}
	// The reader must resume before a synchronous snapshot can receive its reply.
	guard.Go("remote/client-tools-reattach", func() {
		c.clientTools.setMu.Lock()
		defer c.clientTools.setMu.Unlock()
		c.mu.Lock()
		sets := c.clientTools.sets
		c.mu.Unlock()
		if _, _, err := c.clientToolRequest(context.Background(), MethodClientToolsSet, sets, nil); err != nil {
			c.note("client tools could not be reattached: " + err.Error())
		}
	})
}
