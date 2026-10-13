package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const readLimit = 4 << 20

// Handler serves bridge.Path. Origin, host and the bearer token are checked
// by the app transport guard before this handler runs.
func Handler(ctx context.Context, all *svc.Services) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols:       []string{bridge.Subprotocol},
			InsecureSkipVerify: true, // the guard already enforced the origin allowlist
		})
		if err != nil {
			return
		}
		conn.SetReadLimit(readLimit)
		session := newSession(ctx, conn, all)
		session.serve()
	})
}

type session struct {
	ctx    context.Context
	cancel context.CancelFunc
	conn   *websocket.Conn
	all    *svc.Services
	out    chan bridge.Envelope
	mu     sync.Mutex
	subs   map[string]func()
}

func newSession(parent context.Context, conn *websocket.Conn, all *svc.Services) *session {
	ctx, cancel := context.WithCancel(parent)
	return &session{ctx: ctx, cancel: cancel, conn: conn, all: all, out: make(chan bridge.Envelope, 64), subs: map[string]func(){}}
}

func (s *session) serve() {
	defer s.close()
	go s.write()
	for {
		_, data, err := s.conn.Read(s.ctx)
		if err != nil {
			return
		}
		var in bridge.Envelope
		if err := json.Unmarshal(data, &in); err != nil {
			s.send(failure("", bridge.Errorf(bridge.CodeInvalid, "envelope: "+err.Error())))
			continue
		}
		s.route(in)
	}
}

func (s *session) route(in bridge.Envelope) {
	switch in.Type {
	case bridge.TypeCall:
		go s.call(in)
	case bridge.TypeSub:
		s.subscribe(in)
	case bridge.TypeUnsub:
		s.unsubscribe(in.Topic)
		s.send(bridge.Envelope{ID: in.ID, Type: bridge.TypeResult})
	default:
		s.send(failure(in.ID, bridge.Errorf(bridge.CodeInvalid, "unknown message type "+string(in.Type))))
	}
}

func (s *session) call(in bridge.Envelope) {
	entry, ok := methods[in.Method]
	if !ok {
		s.send(failure(in.ID, bridge.Errorf(bridge.CodeInvalid, "unknown method "+in.Method)))
		return
	}
	scope, err := s.scope(in.App, entry.global)
	if err == nil {
		err = s.all.Caps.Allow(s.ctx, scope, in.Method, in.Params)
	}
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

// scope resolves the caller and data mode. Wave 1 has one member, the owner,
// holding every role.
func (s *session) scope(app string, global bool) (svc.Scope, error) {
	scope := svc.Scope{App: app, Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, Data: svc.DataSample}
	if global {
		return scope, nil
	}
	if app == "" {
		return scope, bridge.Errorf(bridge.CodeInvalid, "this method needs app")
	}
	detail, err := s.all.Store.Get(s.ctx, app)
	if err != nil {
		return scope, err
	}
	if detail.State == bridge.StateKept {
		scope.Data = svc.DataReal
	}
	return scope, nil
}

func (s *session) subscribe(in bridge.Envelope) {
	app, ok := topicApp(in.Topic)
	if !ok {
		s.send(failure(in.ID, bridge.Errorf(bridge.CodeInvalid, "topics look like app/<id>/...")))
		return
	}
	scope, err := s.scope(app, false)
	if err == nil {
		err = s.all.Caps.Allow(s.ctx, scope, "sub", json.RawMessage(`{"topic":`+quote(in.Topic)+`}`))
	}
	if err != nil {
		s.send(failure(in.ID, err))
		return
	}
	s.mu.Lock()
	if _, exists := s.subs[in.Topic]; !exists {
		events, cancel := s.all.Hub.Subscribe(in.Topic)
		s.subs[in.Topic] = cancel
		go s.forward(events)
	}
	s.mu.Unlock()
	s.send(bridge.Envelope{ID: in.ID, Type: bridge.TypeResult})
}

func (s *session) forward(events <-chan svc.Event) {
	for event := range events {
		data, err := json.Marshal(event.Data)
		if err != nil {
			continue
		}
		s.send(bridge.Envelope{Type: bridge.TypeEvent, Topic: event.Topic, Seq: event.Seq, Event: data})
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
	select {
	case s.out <- e:
	case <-s.ctx.Done():
	}
}

func (s *session) write() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case e := <-s.out:
			data, _ := json.Marshal(e)
			if s.conn.Write(s.ctx, websocket.MessageText, data) != nil {
				s.cancel()
				return
			}
		}
	}
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
	_ = s.conn.Close(websocket.StatusNormalClosure, "")
}

func failure(id string, err error) bridge.Envelope {
	var typed *bridge.Error
	if !errors.As(err, &typed) {
		typed = bridge.Errorf(bridge.CodeInternal, err.Error())
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

func quote(text string) string {
	data, _ := json.Marshal(text)
	return string(data)
}
