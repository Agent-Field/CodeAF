package gateway_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/collab"
	"github.com/Agent-Field/codeaf/internal/apps/gateway"
	"github.com/Agent-Field/codeaf/internal/apps/parts"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type surfaceStore struct{ svc.Store }

func (surfaceStore) Get(context.Context, string) (bridge.AppDetail, error) {
	return bridge.AppDetail{AppSummary: bridge.AppSummary{State: bridge.StateKept}}, nil
}

type surfaceHub struct{ released chan bridge.DocEvent }

func (h surfaceHub) Publish(_ string, value any) {
	event := value.(bridge.DocEvent)
	if event.Kind == bridge.DocEventKindLock && event.Lock == nil {
		h.released <- event
	}
}
func (surfaceHub) Subscribe(string) (<-chan svc.Event, func()) {
	return make(chan svc.Event), func() {}
}

func surfaceSocket(t *testing.T) (context.Context, *svc.Services, svc.Scope, *websocket.Conn, <-chan bridge.DocEvent) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	released := make(chan bridge.DocEvent, 4)
	all := &svc.Services{Store: surfaceStore{}, Hub: surfaceHub{released: released}}
	cfg := svc.Config{Root: t.TempDir(), Ctx: ctx}
	all.Caps = caps.New(cfg, all)
	all.Parts = parts.New(cfg, all)
	collaboration := collab.New(cfg, all)
	t.Cleanup(collaboration.Close)
	all.Collab = collaboration
	scope := svc.Scope{App: "slides", Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, Data: svc.DataReal}
	_, err := all.Parts.CreateDoc(ctx, scope, bridge.CreateDocParams{Doc: "decks/q4", Surface: bridge.SurfaceText, Parts: []bridge.CreateDocPart{{Part: "section-1.md", Text: "# Team"}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(gateway.NewHandler(ctx, all))
	t.Cleanup(server.Close)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return ctx, all, scope, conn, released
}

func callSurface(t *testing.T, ctx context.Context, conn *websocket.Conn, app, method string, params any) bridge.Envelope {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(bridge.Envelope{ID: "surface", Type: bridge.TypeCall, App: app, Method: method, Params: raw})
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.Write(ctx, websocket.MessageText, request); err != nil {
		t.Fatal(err)
	}
	_, raw, err = conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var reply bridge.Envelope
	if err = json.Unmarshal(raw, &reply); err != nil || reply.Type != bridge.TypeResult {
		t.Fatalf("reply=%s error=%v", raw, err)
	}
	return reply
}

func TestSurfaceSocketCloseReleasesRealLocks(t *testing.T) {
	ctx, all, scope, conn, released := surfaceSocket(t)
	callSurface(t, ctx, conn, "slides", bridge.MethodPartsLock, bridge.LockPartParams{Doc: "decks/q4", Part: "section-1.md"})
	if !all.Collab.Holds(scope, "decks/q4", "section-1.md") {
		t.Fatal("socket did not acquire the lock")
	}
	if err := conn.CloseNow(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
	case <-ctx.Done():
		t.Fatal("socket close did not release the lock")
	}
	if all.Collab.Holds(scope, "decks/q4", "section-1.md") {
		t.Fatal("lock remains after socket close")
	}
}

func TestSurfaceDraftEnvelopeWritesOnlyOverlay(t *testing.T) {
	ctx, all, real, conn, _ := surfaceSocket(t)
	app := "slides@draft"
	callSurface(t, ctx, conn, app, bridge.MethodPartsLock, bridge.LockPartParams{Doc: "decks/q4", Part: "section-1.md"})
	reply := callSurface(t, ctx, conn, app, bridge.MethodPartsWrite, bridge.WritePartParams{Doc: "decks/q4", Part: "section-1.md", Text: "# Draft", BaseVersion: 1, Kind: bridge.PartVersionKindNudge})
	var part bridge.Part
	if err := json.Unmarshal(reply.Result, &part); err != nil || part.Text != "# Draft" || part.Version != 2 {
		t.Fatalf("draft=%+v error=%v", part, err)
	}
	applied, err := all.Parts.Get(ctx, real, bridge.GetPartParams{Doc: "decks/q4", Part: "section-1.md"})
	if err != nil || applied.Version != 1 || applied.Text != "# Team" {
		t.Fatalf("applied=%+v error=%v", applied, err)
	}
	reply = callSurface(t, ctx, conn, app, bridge.MethodPartsGet, bridge.GetPartParams{Doc: "decks/q4", Part: "section-1.md"})
	if err := json.Unmarshal(reply.Result, &part); err != nil || part.Text != "# Draft" {
		t.Fatalf("read=%+v error=%v", part, err)
	}
}
