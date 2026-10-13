package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const (
	readLimit        = 4 << 20
	outputBuffer     = 64
	maxCalls         = 32
	maxSubscriptions = 128
	writeTimeout     = 2 * time.Second
)

// Handler serves bridge.Path behind the existing app transport guard, which
// checks the launch token, origin and host before upgrading the connection.
func Handler(ctx context.Context, all *svc.Services) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !offersProtocol(r) {
			http.Error(w, "codeaf.apps.v1 subprotocol required", http.StatusBadRequest)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols:       []string{bridge.Subprotocol},
			InsecureSkipVerify: true, // The app guard already enforced the origin allowlist.
		})
		if err != nil {
			return
		}
		conn.SetReadLimit(readLimit)
		newSession(ctx, conn, all).serve()
	})
}

func offersProtocol(r *http.Request) bool {
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, protocol := range strings.Split(header, ",") {
			if strings.TrimSpace(protocol) == bridge.Subprotocol {
				return true
			}
		}
	}
	return false
}

type session struct {
	ctx    context.Context
	cancel context.CancelFunc
	conn   *websocket.Conn
	all    *svc.Services
	out    chan bridge.Envelope
	fatal  chan *bridge.Error
	wg     sync.WaitGroup
	mu     sync.Mutex
	subs   map[string]func()
	calls  map[string]bool
}

func newSession(parent context.Context, conn *websocket.Conn, all *svc.Services) *session {
	ctx, cancel := context.WithCancel(parent)
	return &session{ctx: ctx, cancel: cancel, conn: conn, all: all, out: make(chan bridge.Envelope, outputBuffer), fatal: make(chan *bridge.Error, 1), subs: map[string]func(){}, calls: map[string]bool{}}
}

func (s *session) serve() {
	defer s.close()
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.write() }()
	for {
		kind, data, err := s.conn.Read(s.ctx)
		if err != nil {
			return
		}
		if kind != websocket.MessageText {
			s.send(failure("", bridge.Errorf(bridge.CodeInvalid, "messages must be JSON text frames")))
			continue
		}
		var in bridge.Envelope
		if err := json.Unmarshal(data, &in); err != nil {
			s.send(failure("", bridge.Errorf(bridge.CodeInvalid, "invalid envelope")))
			continue
		}
		s.route(in)
	}
}

func (s *session) route(in bridge.Envelope) {
	if in.ID == "" {
		s.send(failure("", bridge.Errorf(bridge.CodeInvalid, "request id is required")))
		return
	}
	switch in.Type {
	case bridge.TypeCall:
		s.mu.Lock()
		duplicate, full := s.calls[in.ID], len(s.calls) >= maxCalls
		if !duplicate && !full {
			s.calls[in.ID] = true
		}
		s.mu.Unlock()
		if duplicate || full {
			s.send(failure(in.ID, bridge.Errorf(bridge.CodeConflict, "duplicate request id or too many active calls")))
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { s.mu.Lock(); delete(s.calls, in.ID); s.mu.Unlock() }()
			s.call(in)
		}()
	case bridge.TypeSub:
		s.subscribe(in)
	case bridge.TypeUnsub:
		if _, err := s.topicScope(in, "unsub"); err != nil {
			s.send(failure(in.ID, err))
			return
		}
		s.unsubscribe(in.Topic)
		s.send(bridge.Envelope{ID: in.ID, Type: bridge.TypeResult, Result: json.RawMessage(`{}`)})
	default:
		s.send(failure(in.ID, bridge.Errorf(bridge.CodeInvalid, "unknown message type")))
	}
}

func (s *session) call(in bridge.Envelope) {
	entry, scope, err := s.shapeCall(in)
	var result any
	if err == nil {
		result, err = entry.run(s.ctx, s.all, scope, in.Params)
	}
	if err != nil {
		s.send(failure(in.ID, err))
		return
	}
	data, err := json.Marshal(result)
	if err != nil {
		s.send(failure(in.ID, err))
		return
	}
	s.send(bridge.Envelope{ID: in.ID, Type: bridge.TypeResult, Result: data})
}

func ownerScope(app string) svc.Scope {
	return svc.Scope{App: app, Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, Data: svc.DataSample}
}

// resolveScope selects sample records until Keep, regardless of client params.
func (s *session) resolveScope(scope svc.Scope, global bool) (svc.Scope, error) {
	if global {
		return scope, nil
	}
	if scope.App == "" {
		return scope, bridge.Errorf(bridge.CodeInvalid, "this method needs app")
	}
	detail, err := s.all.Store.Get(s.ctx, scope.App)
	if err == nil && detail.State == bridge.StateKept {
		scope.Data = svc.DataReal
	}
	return scope, err
}

func (s *session) topicScope(in bridge.Envelope, method string) (svc.Scope, error) {
	app, ok := topicApp(in.Topic)
	if !ok {
		return svc.Scope{}, bridge.Errorf(bridge.CodeInvalid, "topics look like app/<id>/...")
	}
	if in.App != "" && in.App != app {
		return svc.Scope{}, bridge.Errorf(bridge.CodeInvalid, "topic and app disagree")
	}
	scope := ownerScope(app)
	raw, _ := json.Marshal(map[string]string{"topic": in.Topic})
	if err := s.all.Caps.Allow(s.ctx, scope, method, raw); err != nil {
		return scope, err
	}
	return s.resolveScope(scope, false)
}

func (s *session) subscribe(in bridge.Envelope) {
	if _, err := s.topicScope(in, "sub"); err != nil {
		s.send(failure(in.ID, err))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.subs[in.Topic]; !exists {
		if len(s.subs) >= maxSubscriptions {
			s.send(failure(in.ID, bridge.Errorf(bridge.CodeConflict, "too many subscriptions")))
			return
		}
		events, cancel := s.all.Hub.Subscribe(in.Topic)
		s.subs[in.Topic] = cancel
		// Queue the receipt before allowing this subscription's first event.
		s.send(bridge.Envelope{ID: in.ID, Type: bridge.TypeResult, Result: json.RawMessage(`{}`)})
		s.wg.Add(1)
		go func() { defer s.wg.Done(); s.forward(events) }()
		return
	}
	s.send(bridge.Envelope{ID: in.ID, Type: bridge.TypeResult, Result: json.RawMessage(`{}`)})
}

func (s *session) forward(events <-chan svc.Event) {
	for {
		select {
		case <-s.ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err, ok := event.Data.(*bridge.Error); ok {
				s.fail(err)
				return
			}
			data, err := json.Marshal(event.Data)
			if err != nil {
				s.fail(bridge.Errorf(bridge.CodeInternal, "cannot encode subscription event"))
				return
			}
			s.send(bridge.Envelope{Type: bridge.TypeEvent, Topic: event.Topic, Seq: event.Seq, Event: data})
		}
	}
}

func (s *session) unsubscribe(topic string) {
	s.mu.Lock()
	cancel := s.subs[topic]
	delete(s.subs, topic)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *session) send(e bridge.Envelope) {
	if s.ctx.Err() != nil {
		return
	}
	select {
	case s.out <- e:
	default:
		s.fail(bridge.Errorf(bridge.CodeConflict, "slow consumer: reconnect and re-query"))
	}
}
func (s *session) fail(err *bridge.Error) {
	select {
	case s.fatal <- err:
	default:
	}
}

func (s *session) write() {
	for {
		select {
		case err := <-s.fatal:
			s.finish(err)
			return
		default:
		}
		select {
		case <-s.ctx.Done():
			return
		case err := <-s.fatal:
			s.finish(err)
			return
		case e := <-s.out:
			if err := s.writeOne(e); err != nil {
				s.finish(bridge.Errorf(bridge.CodeConflict, "slow consumer or disconnected socket"))
				return
			}
		}
	}
}

func (s *session) writeOne(e bridge.Envelope) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.ctx, writeTimeout)
	defer cancel()
	return s.conn.Write(ctx, websocket.MessageText, data)
}

func (s *session) finish(err *bridge.Error) {
	_ = s.writeOne(failure("", err))
	s.cancel()
	_ = s.conn.Close(websocket.StatusPolicyViolation, err.Message)
}

func (s *session) close() {
	s.cancel()
	s.mu.Lock()
	subs := s.subs
	s.subs = map[string]func(){}
	s.mu.Unlock()
	for _, cancel := range subs {
		cancel()
	}
	_ = s.conn.CloseNow()
	s.wg.Wait()
}

func failure(id string, err error) bridge.Envelope {
	var typed *bridge.Error
	if !errors.As(err, &typed) {
		typed = bridge.Errorf(bridge.CodeInternal, "app service failed")
	}
	return bridge.Envelope{ID: id, Type: bridge.TypeError, Error: typed}
}

func topicApp(topic string) (string, bool) {
	parts := strings.SplitN(topic, "/", 3)
	if len(parts) < 3 || parts[0] != "app" || parts[1] == "" || parts[2] == "" {
		return "", false
	}
	return parts[1], true
}
