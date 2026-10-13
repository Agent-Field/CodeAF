package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/coder/websocket"
)

const token = "gateway-test-launch-token-0123456789abcdef"

type trackedHub struct {
	svc.Hub
	mu      sync.Mutex
	active  int
	removed chan struct{}
}

func (h *trackedHub) Subscribe(topic string) (<-chan svc.Event, func()) {
	events, cancel := h.Hub.Subscribe(topic)
	h.mu.Lock()
	h.active++
	h.mu.Unlock()
	var once sync.Once
	return events, func() { once.Do(func() { cancel(); h.mu.Lock(); h.active--; h.mu.Unlock(); h.removed <- struct{}{} }) }
}

type socketClient struct {
	t    *testing.T
	ctx  context.Context
	conn *websocket.Conn
	next int
}

func (c *socketClient) send(kind bridge.MessageType, app, method, topic string, params any) string {
	c.t.Helper()
	c.next++
	id := strconv.Itoa(c.next)
	raw, _ := json.Marshal(params)
	data, _ := json.Marshal(bridge.Envelope{ID: id, Type: kind, App: app, Method: method, Topic: topic, Params: raw})
	if err := c.conn.Write(c.ctx, websocket.MessageText, data); err != nil {
		c.t.Fatal(err)
	}
	return id
}
func (c *socketClient) read() bridge.Envelope {
	c.t.Helper()
	_, data, err := c.conn.Read(c.ctx)
	if err != nil {
		c.t.Fatal(err)
	}
	var e bridge.Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		c.t.Fatal(err)
	}
	return e
}
func testServer(t *testing.T, all *svc.Services) (context.Context, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	api := appapi.New(token, appapi.Options{Context: ctx})
	t.Cleanup(api.Close)
	api.Handle(bridge.Path, Handler(ctx, all))
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	return ctx, "ws" + strings.TrimPrefix(server.URL, "http") + bridge.Path
}
func dialSocket(t *testing.T, ctx context.Context, url string) *socketClient {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol, bridge.BearerProtocol + token}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	if conn.Subprotocol() != bridge.Subprotocol {
		t.Fatalf("subprotocol = %q", conn.Subprotocol())
	}
	return &socketClient{t: t, ctx: ctx, conn: conn}
}
func testServices(t *testing.T) *svc.Services {
	t.Helper()
	cfg := svc.Config{Root: t.TempDir()}
	all := &svc.Services{}
	all.Store = store.New(cfg, all)
	all.Caps = caps.New(cfg, all)
	all.Records = records.New(cfg, all)
	if err := all.Store.(*store.Service).SeedGoldenCRM(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Install fixture records before starting the stream whose sequence is tested.
	all.Hub = realtime.New(cfg, all)
	return all
}

func TestSocketCallDenySubscribeDiffAndDisconnect(t *testing.T) {
	all := testServices(t)
	hub := &trackedHub{Hub: all.Hub, removed: make(chan struct{}, 4)}
	all.Hub = hub
	ctx, url := testServer(t, all)
	c := dialSocket(t, ctx, url)
	id := c.send(bridge.TypeCall, "", bridge.MethodAppsList, "", nil)
	if e := c.read(); e.ID != id || e.Type != bridge.TypeResult || !strings.Contains(string(e.Result), `"crm"`) {
		t.Fatal(e)
	}
	id = c.send(bridge.TypeCall, "crm", bridge.MethodAppsGet, "", nil)
	var detail bridge.AppDetail
	e := c.read()
	if err := json.Unmarshal(e.Result, &detail); err != nil {
		t.Fatal(err)
	}
	if e.ID != id || len(detail.Needs) == 0 {
		t.Fatalf("apps.get = %+v", e)
	}
	id = c.send(bridge.TypeCall, "crm", bridge.MethodRecordsCreate, "", bridge.CreateRecordParams{Type: "secrets", Set: map[string]any{"name": "secret"}})
	if e := c.read(); e.ID != id || e.Type != bridge.TypeError || e.Error.Code != bridge.CodeDenied {
		t.Fatal(e)
	}
	id = c.send(bridge.TypeSub, "", "", "app/crm/records/*", nil)
	if e := c.read(); e.ID != id || e.Type != bridge.TypeResult {
		t.Fatal(e)
	}
	// Repeating a subscription must not create a second delivery or leak.
	c.send(bridge.TypeSub, "", "", "app/crm/records/*", nil)
	c.read()
	id = c.send(bridge.TypeCall, "crm", bridge.MethodRecordsUpdate, "", bridge.UpdateRecordParams{Type: "deal", ID: "dl_hooli", Set: map[string]any{"stage": "Qualified"}})
	gotResult, gotEvent := false, false
	for range 2 {
		e := c.read()
		switch e.Type {
		case bridge.TypeResult:
			gotResult = e.ID == id
		case bridge.TypeEvent:
			var diff bridge.RecordDiff
			if err := json.Unmarshal(e.Event, &diff); err != nil {
				t.Fatal(err)
			}
			gotEvent = e.Topic == bridge.TopicRecords("crm", "deal") && e.Seq == 1 && diff.ID == "dl_hooli" && diff.Record.Fields["stage"] == "Qualified"
		default:
			t.Fatal(e)
		}
	}
	if !gotResult || !gotEvent {
		t.Fatalf("result=%v event=%v", gotResult, gotEvent)
	}
	if err := c.conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hub.removed:
	case <-ctx.Done():
		t.Fatal("disconnect did not cancel subscription")
	}
	hub.mu.Lock()
	active := hub.active
	hub.mu.Unlock()
	if active != 0 {
		t.Fatalf("active subscriptions=%d", active)
	}
}

func TestSocketUnsubscribeCancelsAndChecksTopics(t *testing.T) {
	all := testServices(t)
	hub := &trackedHub{Hub: all.Hub, removed: make(chan struct{}, 4)}
	all.Hub = hub
	ctx, url := testServer(t, all)
	c := dialSocket(t, ctx, url)
	c.send(bridge.TypeSub, "", "", "app/crm/state", nil)
	c.read()
	id := c.send(bridge.TypeUnsub, "", "", "app/crm/state", nil)
	if e := c.read(); e.ID != id || e.Type != bridge.TypeResult {
		t.Fatal(e)
	}
	select {
	case <-hub.removed:
	case <-ctx.Done():
		t.Fatal("unsubscribe did not cancel")
	}
	c.send(bridge.TypeUnsub, "", "", "app/crm/records/secret", nil)
	if e := c.read(); e.Type != bridge.TypeError || e.Error.Code != bridge.CodeDenied {
		t.Fatal(e)
	}
}

func TestSocketGuard(t *testing.T) {
	ctx, url := testServer(t, testServices(t))
	for _, tt := range []struct {
		name      string
		protocols []string
		headers   http.Header
		status    int
	}{
		{"missing token", []string{bridge.Subprotocol}, nil, http.StatusUnauthorized},
		{"wrong token", []string{bridge.Subprotocol, bridge.BearerProtocol + "wrong"}, nil, http.StatusUnauthorized},
		{"foreign origin", []string{bridge.Subprotocol, bridge.BearerProtocol + token}, http.Header{"Origin": []string{"https://foreign.example"}}, http.StatusForbidden},
		{"missing version", []string{bridge.BearerProtocol + token}, nil, http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conn, response, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: tt.protocols, HTTPHeader: tt.headers})
			if conn != nil {
				_ = conn.CloseNow()
			}
			if err == nil || response == nil || response.StatusCode != tt.status {
				t.Fatalf("response=%v error=%v", response, err)
			}
		})
	}
	// Native clients may use the same bearer header as other app routes.
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol}, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.CloseNow()
}

type queryRecorder struct {
	svc.Records
	scope chan svc.Scope
}

func (r queryRecorder) Query(_ context.Context, scope svc.Scope, _ bridge.QueryParams) (bridge.QueryResult, error) {
	r.scope <- scope
	return bridge.QueryResult{}, nil
}

type stateStore struct {
	svc.Store
	state bridge.AppState
}

func (s stateStore) Get(ctx context.Context, app string) (bridge.AppDetail, error) {
	d, err := s.Store.Get(ctx, app)
	d.State = s.state
	return d, err
}
func TestGatewayChoosesDataMode(t *testing.T) {
	for _, tt := range []struct {
		state bridge.AppState
		mode  svc.DataMode
	}{{bridge.StateKept, svc.DataReal}, {bridge.StatePreview, svc.DataSample}, {bridge.StatePlanning, svc.DataSample}} {
		t.Run(string(tt.state), func(t *testing.T) {
			all := testServices(t)
			all.Store = stateStore{all.Store, tt.state}
			scopes := make(chan svc.Scope, 1)
			all.Records = queryRecorder{scope: scopes}
			ctx, url := testServer(t, all)
			c := dialSocket(t, ctx, url)
			c.send(bridge.TypeCall, "crm", bridge.MethodRecordsQuery, "", map[string]any{"type": "deal", "data": "real", "caller": "intruder"})
			if e := c.read(); e.Type != bridge.TypeResult {
				t.Fatal(e)
			}
			scope := <-scopes
			if scope.Data != tt.mode || scope.Caller.Member != svc.Owner || scope.Caller.Role != svc.RoleEditor {
				t.Fatal(scope)
			}
		})
	}
}

func TestBackpressureDisconnectsWithTypedError(t *testing.T) {
	all := testServices(t)
	ctx, url := testServer(t, all)
	c := dialSocket(t, ctx, url)
	c.send(bridge.TypeSub, "", "", "app/crm/state", nil)
	c.read()
	all.Hub.Publish("app/crm/state", bridge.Errorf(bridge.CodeConflict, "slow consumer: reconnect and re-query"))
	if e := c.read(); e.Type != bridge.TypeError || e.Error.Code != bridge.CodeConflict {
		t.Fatal(e)
	}
	_, _, err := c.conn.Read(ctx)
	if err == nil {
		t.Fatal("connection stayed open")
	}
}
func TestOutputQueueNeverBlocksPublisher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newSession(ctx, nil, nil)
	defer s.cancel()
	for range outputBuffer + 1 {
		s.send(bridge.Envelope{Type: bridge.TypeEvent})
	}
	select {
	case err := <-s.fatal:
		if err.Code != bridge.CodeConflict {
			t.Fatal(err)
		}
	default:
		t.Fatal("no slow consumer error")
	}
}

func TestTypedErrorsAndInternalErrors(t *testing.T) {
	wrapped := fmt.Errorf("wrapped: %w", bridge.Errorf(bridge.CodeBudget, "budget reached"))
	for _, tt := range []struct {
		err     error
		code    bridge.ErrorCode
		message string
	}{{wrapped, bridge.CodeBudget, "budget reached"}, {errors.New("private backend path"), bridge.CodeInternal, "app service failed"}} {
		e := failure("request", tt.err)
		if e.ID != "request" || e.Error.Code != tt.code || e.Error.Message != tt.message {
			t.Fatal(e)
		}
	}
}

type rejectingCaps struct{ seen []string }

func (c *rejectingCaps) Allow(_ context.Context, _ svc.Scope, method string, _ json.RawMessage) error {
	c.seen = append(c.seen, method)
	return bridge.Errorf(bridge.CodeDenied, "test denial")
}
func TestCapabilityCheckPrecedesEveryDispatch(t *testing.T) {
	for _, method := range []string{bridge.MethodAppsList, bridge.MethodAppsCreate, bridge.MethodRecordsQuery, "unknown.method"} {
		t.Run(method, func(t *testing.T) {
			checker := &rejectingCaps{}
			s := newSession(context.Background(), nil, &svc.Services{Caps: checker})
			defer s.cancel()
			// No store or other service is present: reaching one would panic.
			s.call(bridge.Envelope{ID: "request", Type: bridge.TypeCall, App: "crm", Method: method})
			reply := <-s.out
			if len(checker.seen) != 1 || checker.seen[0] != method || reply.Type != bridge.TypeError {
				t.Fatalf("checks=%v reply=%+v", checker.seen, reply)
			}
		})
	}
	for _, kind := range []bridge.MessageType{bridge.TypeSub, bridge.TypeUnsub} {
		t.Run(string(kind), func(t *testing.T) {
			checker := &rejectingCaps{}
			s := newSession(context.Background(), nil, &svc.Services{Caps: checker})
			defer s.cancel()
			s.route(bridge.Envelope{ID: "request", Type: kind, Topic: "app/crm/state"})
			reply := <-s.out
			if len(checker.seen) != 1 || checker.seen[0] != string(kind) || reply.Error.Code != bridge.CodeDenied {
				t.Fatalf("checks=%v reply=%+v", checker.seen, reply)
			}
		})
	}
}

type waitingRecords struct {
	svc.Records
	entered chan struct{}
	release chan struct{}
}

func (r waitingRecords) Query(ctx context.Context, _ svc.Scope, _ bridge.QueryParams) (bridge.QueryResult, error) {
	close(r.entered)
	select {
	case <-r.release:
		return bridge.QueryResult{}, nil
	case <-ctx.Done():
		return bridge.QueryResult{}, ctx.Err()
	}
}
func TestCallsResolveOutOfOrderWithTheirRequestIDs(t *testing.T) {
	all := testServices(t)
	r := waitingRecords{entered: make(chan struct{}), release: make(chan struct{})}
	all.Records = r
	ctx, url := testServer(t, all)
	c := dialSocket(t, ctx, url)
	first := c.send(bridge.TypeCall, "crm", bridge.MethodRecordsQuery, "", bridge.QueryParams{Type: "deal"})
	select {
	case <-r.entered:
	case <-ctx.Done():
		t.Fatal("query did not start")
	}
	second := c.send(bridge.TypeCall, "", bridge.MethodAppsList, "", nil)
	if reply := c.read(); reply.ID != second || reply.Type != bridge.TypeResult {
		t.Fatal(reply)
	}
	close(r.release)
	if reply := c.read(); reply.ID != first || reply.Type != bridge.TypeResult {
		t.Fatal(reply)
	}
}

func TestInvalidFramesAndEnvelopes(t *testing.T) {
	ctx, url := testServer(t, testServices(t))
	c := dialSocket(t, ctx, url)
	for _, tt := range []struct {
		name string
		kind websocket.MessageType
		data string
	}{
		{"binary", websocket.MessageBinary, `{"id":"x","type":"call","method":"apps.list"}`},
		{"broken json", websocket.MessageText, `{"id":`},
		{"missing id", websocket.MessageText, `{"type":"call","method":"apps.list"}`},
		{"unknown kind", websocket.MessageText, `{"id":"x","type":"other"}`},
		{"unknown method", websocket.MessageText, `{"id":"x","type":"call","method":"missing","app":"crm"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := c.conn.Write(ctx, tt.kind, []byte(tt.data)); err != nil {
				t.Fatal(err)
			}
			if reply := c.read(); reply.Type != bridge.TypeError || reply.Error.Code != bridge.CodeInvalid {
				t.Fatal(reply)
			}
		})
	}
}
