package apps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const testToken = "apps-bridge-test-token-0123456789abcdef"

type client struct {
	t    *testing.T
	ctx  context.Context
	conn *websocket.Conn
	next int
}

func testServices(t *testing.T, ctx context.Context) *svc.Services {
	t.Helper()
	all := Wire(ctx, t.TempDir())
	// Reading through Store starts the demo registry even when lifecycle wraps it.
	if _, err := all.Store.Get(ctx, "crm"); err != nil {
		t.Fatal(err)
	}
	return all
}

func dial(t *testing.T) *client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	api := appapi.New(testToken, appapi.Options{Context: ctx})
	t.Cleanup(api.Close)
	Mount(ctx, api, testServices(t, ctx))
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http") + bridge.Path
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol, bridge.BearerProtocol + testToken}})
	if err != nil {
		t.Fatal(err)
	}
	if conn.Subprotocol() != bridge.Subprotocol {
		t.Fatalf("subprotocol = %q", conn.Subprotocol())
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return &client{t: t, ctx: ctx, conn: conn}
}

func (c *client) send(e bridge.Envelope) string {
	c.t.Helper()
	c.next++
	e.ID = "c" + strconv.Itoa(c.next)
	data, _ := json.Marshal(e)
	if err := c.conn.Write(c.ctx, websocket.MessageText, data); err != nil {
		c.t.Fatal(err)
	}
	return e.ID
}

func (c *client) read() bridge.Envelope {
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

func (c *client) call(app, method string, params any, into any) *bridge.Error {
	c.t.Helper()
	raw, _ := json.Marshal(params)
	id := c.send(bridge.Envelope{Type: bridge.TypeCall, App: app, Method: method, Params: raw})
	reply := c.read()
	if reply.ID != id {
		c.t.Fatalf("reply id %q, want %q", reply.ID, id)
	}
	if reply.Type == bridge.TypeError {
		return reply.Error
	}
	if into != nil {
		if err := json.Unmarshal(reply.Result, into); err != nil {
			c.t.Fatal(err)
		}
	}
	return nil
}

func TestBridgeServesTheGoldenCRM(t *testing.T) {
	c := dial(t)
	var list bridge.AppsListResult
	if err := c.call("", bridge.MethodAppsList, nil, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Apps) != 1 || list.Apps[0].ID != "crm" || list.Apps[0].State != bridge.StateKept {
		t.Fatalf("apps = %+v", list.Apps)
	}
	var detail bridge.AppDetail
	if err := c.call("crm", bridge.MethodAppsGet, nil, &detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail.Files["screens/pipeline.json"], `"RecordList"`) {
		t.Fatal("apps.get should return the screen files")
	}
	var page bridge.QueryResult
	if err := c.call("crm", bridge.MethodRecordsQuery, bridge.QueryParams{Type: "deal", Query: "sort:-amount", CountBy: "stage"}, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 6 || page.Rows[0].ID != "dl_northwind" || page.Counts["Proposal"] != 2 {
		t.Fatalf("page = %+v", page)
	}
}

func TestBridgePublishesRowDiffs(t *testing.T) {
	c := dial(t)
	topic := bridge.TopicRecords("crm", "deal")
	id := c.send(bridge.Envelope{Type: bridge.TypeSub, Topic: topic})
	if reply := c.read(); reply.ID != id || reply.Type != bridge.TypeResult {
		t.Fatalf("sub reply = %+v", reply)
	}
	raw, _ := json.Marshal(bridge.UpdateRecordParams{Type: "deal", ID: "dl_hooli", Set: map[string]any{"stage": "Qualified"}})
	c.send(bridge.Envelope{Type: bridge.TypeCall, App: "crm", Method: bridge.MethodRecordsUpdate, Params: raw})
	var sawResult, sawEvent bool
	for !(sawResult && sawEvent) {
		e := c.read()
		switch e.Type {
		case bridge.TypeResult:
			sawResult = true
		case bridge.TypeEvent:
			var diff bridge.RecordDiff
			_ = json.Unmarshal(e.Event, &diff)
			if e.Topic != topic || diff.Op != bridge.DiffUpsert || diff.Record.Fields["stage"] != "Qualified" {
				t.Fatalf("event = %+v diff = %+v", e, diff)
			}
			sawEvent = true
		default:
			t.Fatalf("unexpected %+v", e)
		}
	}
}

func TestBridgeTypedErrors(t *testing.T) {
	c := dial(t)
	if err := c.call("crm", bridge.MethodRunsStart, bridge.StartRunParams{Run: "not-a-run"}, nil); err == nil || err.Code != bridge.CodeNotFound {
		t.Fatalf("runs.start err = %+v", err)
	}
	if err := c.call("nope", bridge.MethodAppsGet, nil, nil); err == nil || err.Code != bridge.CodeNotFound {
		t.Fatalf("apps.get err = %+v", err)
	}
	if err := c.call("crm", "records.explode", nil, nil); err == nil || err.Code != bridge.CodeInvalid {
		t.Fatalf("unknown method err = %+v", err)
	}
}

func TestBridgeNeedsTheToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	api := appapi.New(testToken, appapi.Options{Context: ctx})
	defer api.Close()
	Mount(ctx, api, testServices(t, ctx))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + bridge.Path
	_, response, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol, bridge.BearerProtocol + "wrong"}})
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("dial with a wrong token: err=%v response=%v", err, response)
	}
}
