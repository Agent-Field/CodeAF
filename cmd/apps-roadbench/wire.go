package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

// wire is one bridge WebSocket, spoken the way the desk speaks it: envelope
// calls with ids, subscriptions, events pushed to the collector.
type wire struct {
	conn    *websocket.Conn
	next    atomic.Int64
	mu      sync.Mutex
	pending map[string]chan bridge.Envelope
	hear    *collector
}

func dial(ctx context.Context, url, token string, hear *collector) (*wire, error) {
	conn, _, err := websocket.Dial(ctx, "ws"+url[len("http"):]+bridge.Path,
		&websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol, bridge.BearerProtocol + token}})
	if err != nil {
		return nil, fmt.Errorf("dial bridge: %w", err)
	}
	conn.SetReadLimit(8 << 20)
	w := &wire{conn: conn, pending: map[string]chan bridge.Envelope{}, hear: hear}
	go w.read(ctx)
	return w, nil
}

func (w *wire) close() { _ = w.conn.CloseNow() }

// read routes answers to their callers and events to the collector.
func (w *wire) read(ctx context.Context) {
	for {
		_, data, err := w.conn.Read(ctx)
		if err != nil {
			w.failAll(err)
			return
		}
		var env bridge.Envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		if env.Type == bridge.TypeEvent {
			w.hear.add(env.Topic, env.Event, time.Now())
			continue
		}
		w.mu.Lock()
		ch := w.pending[env.ID]
		delete(w.pending, env.ID)
		w.mu.Unlock()
		if ch != nil {
			ch <- env
		}
	}
}

func (w *wire) failAll(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, ch := range w.pending {
		ch <- bridge.Envelope{Type: bridge.TypeError, Error: bridge.Errorf(bridge.CodeInternal, "socket closed: "+err.Error())}
		delete(w.pending, id)
	}
}

func (w *wire) send(ctx context.Context, env bridge.Envelope) (json.RawMessage, error) {
	env.ID = strconv.FormatInt(w.next.Add(1), 10)
	ch := make(chan bridge.Envelope, 1)
	w.mu.Lock()
	w.pending[env.ID] = ch
	w.mu.Unlock()
	data, _ := json.Marshal(env)
	if err := w.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return nil, err
	}
	select {
	case reply := <-ch:
		if reply.Error != nil {
			return nil, reply.Error
		}
		return reply.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (w *wire) call(ctx context.Context, app, method string, params, into any) error {
	raw, _ := json.Marshal(params)
	result, err := w.send(ctx, bridge.Envelope{Type: bridge.TypeCall, App: app, Method: method, Params: raw})
	if err != nil || into == nil {
		return err
	}
	return json.Unmarshal(result, into)
}

func (w *wire) subscribe(ctx context.Context, topic string) error {
	_, err := w.send(ctx, bridge.Envelope{Type: bridge.TypeSub, Topic: topic})
	return err
}
