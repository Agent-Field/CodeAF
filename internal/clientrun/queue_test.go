package clientrun_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/directory"
	"golang.org/x/crypto/nacl/box"
)

func (e *engine) AnswerAsk(agent, id, option, text string) error {
	if agent != chat || id != "consent:1" || option != "allow" {
		return errors.New("wrong ask or option")
	}
	e.answers++
	e.asks = nil
	return nil
}
func (g *rig) enqueue(t *testing.T, grant clientrun.Grant, number int64, faceID bool) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"method": "asks.answer", "params": map[string]string{"agent": chat, "id": "consent:1", "option": "allow"}, "face_id": faceID})
	sealed, err := box.SealAnonymous(nil, payload, (*[32]byte)(grant.BoxKey), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g.request(t, g.client, "POST", "/v1/clients/"+g.client.dev.ID()+"/queue", map[string]any{"number": number, "at": g.now.UnixMilli(), "method": "asks.answer", "target": g.s.dev.ID(), "sealed": sealed}, nil, 204)
}
func TestQueuedAnswerIsExecutedLoggedAndAckedOnce(t *testing.T) {
	g := newRig(t)
	g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat}}
	grant := g.pair(t, "asks.answer")
	g.enqueue(t, grant, 1, false)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.e.answers != 1 {
		t.Fatalf("answered %d times; want 1", g.e.answers)
	}
	var queue struct{ Entries []json.RawMessage }
	g.request(t, g.s, "GET", "/v1/clients/"+g.client.dev.ID()+"/queue", nil, &queue, 200)
	if len(queue.Entries) != 0 {
		t.Fatal("executed call was not acked")
	}
	receipts := g.r.Receipts()
	if len(receipts) != 1 || receipts[0].Client != g.client.dev.ID() || receipts[0].Agent != chat || receipts[0].Method != "asks.answer" || receipts[0].Status != "done" {
		t.Fatalf("receipts: %+v", receipts)
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.e.answers != 1 {
		t.Fatal("call executed twice")
	}
}
func TestIrreversibleQueuedAnswerRequiresFaceID(t *testing.T) {
	g := newRig(t)
	g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat, Irreversible: true}}
	grant := g.pair(t, "asks.answer")
	g.enqueue(t, grant, 1, false)
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("accepted irreversible answer without Face ID")
	}
	if g.e.answers != 0 {
		t.Fatal("executed irreversible answer")
	}
	// A later signed entry cannot jump the rejected sequence number.
	g.enqueue(t, grant, 2, true)
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("accepted unexplained sequence gap")
	}
	if g.e.answers != 0 {
		t.Fatal("skipped unaccepted sequence")
	}
}

func TestFaceIDAllowsAnIrreversibleAnswer(t *testing.T) {
	g := newRig(t)
	g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat, Irreversible: true}}
	grant := g.pair(t, "asks.answer")
	g.enqueue(t, grant, 1, true)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.e.answers != 1 {
		t.Fatal("Face ID answer did not run")
	}
}

func TestAQueuedAnswerNeverTakesAChatLease(t *testing.T) {
	g := newRig(t)
	g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat}}
	grant := g.pair(t, "asks.answer")
	g.enqueue(t, grant, 1, false)
	if err := g.dir.Release(context.Background(), chat, 1); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("executed without lease")
	}
	view, err := g.dir.Cell(context.Background(), chat)
	if err != nil {
		t.Fatal(err)
	}
	if g.e.answers != 0 || view.Cell.Lease.Expires != 0 || view.Cell.Lease.Fence != 1 {
		t.Fatalf("queue acquired a lease: %+v", view)
	}
}

func (e *engine) CreateAgent(appwire.CreateAsk) (appwire.Agent, error) {
	e.created++
	return appwire.Agent{ID: otherChat}, nil
}
func TestAnyTargetCreatesWorkThroughTheDesktopMethod(t *testing.T) {
	g := newRig(t)
	grant := g.pair(t, "agents.create")
	payload, _ := json.Marshal(clientrun.Call{Method: "agents.create", Params: json.RawMessage(`{"text":"start new work"}`)})
	sealed, err := box.SealAnonymous(nil, payload, (*[32]byte)(grant.BoxKey), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g.request(t, g.client, "POST", "/v1/clients/"+g.client.dev.ID()+"/queue", map[string]any{"number": 1, "at": g.now.UnixMilli(), "method": "agents.create", "target": "any", "sealed": sealed}, nil, 204)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	listing, err := g.dir.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	created, ok := listing.Cells[otherChat]
	if !ok || created.Lease.Device != g.s.dev.ID() || created.Lease.Expires <= listing.Now {
		t.Fatalf("created chat has no desktop lease: %+v", created)
	}
	if g.e.created != 1 || g.r.Receipts()[0].Agent != otherChat {
		t.Fatal("creation did not run or was not logged to its chat")
	}
}

func TestSignedSkipRecoversAfterAnUnexecutedEntryExpires(t *testing.T) {
	g := newRig(t)
	grant := g.pair(t, "asks.answer")
	g.e.asks = []appwire.Ask{{ID: "consent:1", Agent: chat}}
	g.enqueue(t, grant, 1, false)
	g.now = g.now.Add(24 * time.Hour)
	g.clock.Store(g.now.UnixMilli())
	if _, err := g.dir.Acquire(context.Background(), chat, directory.AcquireOpts{}); err != nil {
		t.Fatal(err)
	}
	through := int64(1)
	payload, _ := json.Marshal(clientrun.Call{Method: "asks.answer", Params: json.RawMessage(`{"agent":"` + chat + `","id":"consent:1","option":"allow"}`), SkipThrough: &through})
	sealed, err := box.SealAnonymous(nil, payload, (*[32]byte)(grant.BoxKey), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g.request(t, g.client, "POST", "/v1/clients/"+g.client.dev.ID()+"/queue", map[string]any{"number": 2, "at": g.now.UnixMilli(), "method": "asks.answer", "target": g.s.dev.ID(), "sealed": sealed}, nil, 204)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	receipts := g.r.Receipts()
	if g.e.answers != 1 || len(receipts) != 1 || receipts[0].Number != 2 || receipts[0].SkippedFrom != 1 || receipts[0].SkippedThrough != 1 {
		t.Fatalf("recovery = %+v; answers %d", receipts, g.e.answers)
	}
}
