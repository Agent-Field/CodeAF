package main

import (
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// renderConversation is a conversation that keeps renders.
type renderConversation struct {
	*fakeConversation
	page      session.RenderPage
	listErr   error
	records   map[string]session.RenderRecord
	cancelled []string
}

func (c *renderConversation) Renders(limit int, cursor string) (session.RenderPage, error) {
	return c.page, c.listErr
}

func (c *renderConversation) Render(id string) (session.RenderRecord, error) {
	if r, ok := c.records[id]; ok {
		return r, nil
	}
	return session.RenderRecord{}, session.ErrRenderNotFound
}

func (c *renderConversation) CancelRender(id string) (session.RenderCancel, error) {
	c.cancelled = append(c.cancelled, id)
	r, ok := c.records[id]
	if !ok {
		return session.RenderCancel{}, session.ErrRenderNotFound
	}
	r.State = session.RenderCancelled
	return session.RenderCancel{Outcome: session.RenderOutcomeCancelled, Render: r}, nil
}

// plainConversation hides everything but the conversation interface, which is
// what an older host's connection looks like.
type plainConversation struct{ appConversation }

func renderBackend(t *testing.T, conv appConversation) (*appBackend, *appRecorder, appwire.Agent) {
	t.Helper()
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return backend, rec, agent
}

func TestRenderRPCsReachTheEngineAndKeepItsAnswers(t *testing.T) {
	cost := 0.4
	record := session.RenderRecord{ID: "rnd_1", Job: 3, Kind: "video", State: session.RenderRunning, Model: "m", Started: time.Unix(1_700_000_000, 0),
		Charge: session.RenderChargeUnknown, Run: "r", Call: "c"}
	done := record
	done.ID, done.State, done.Charge, done.CostUSD = "rnd_2", session.RenderFinished, session.RenderCharged, &cost
	conv := &renderConversation{
		fakeConversation: &fakeConversation{},
		page:             session.RenderPage{Renders: []session.RenderRecord{record, done}, Next: "r2.12", Incomplete: true},
		records:          map[string]session.RenderRecord{"rnd_1": record, "rnd_2": done},
	}
	backend, _, agent := renderBackend(t, conv)
	if _, _, err := backend.ensure(mustFind(t, backend, agent.ID)); err != nil {
		t.Fatal(err)
	}

	page, err := backend.ListRenders(agent.ID, "", 50)
	if err != nil || len(page.Renders) != 2 || page.Next != "r2.12" || !page.Incomplete || page.Renders[1].CostUSD == nil || *page.Renders[1].CostUSD != cost || page.Renders[0].Run != "r" || page.Renders[0].Call != "c" {
		t.Fatalf("list = %+v, %v", page, err)
	}
	if _, err := backend.GetRender(agent.ID, "rnd_missing"); !isWire(err, appwire.CodeNotFound) {
		t.Fatalf("an absent id = %v, want not_found", err)
	}
	cancelled, err := backend.CancelRender(agent.ID, "rnd_1")
	if err != nil || cancelled.Outcome != "cancelled" || cancelled.Render.State != "cancelled" || len(conv.cancelled) != 1 {
		t.Fatalf("cancel = %+v, %v", cancelled, err)
	}
	conv.listErr = session.ErrRenderBusy
	if _, err := backend.ListRenders(agent.ID, "", 50); !isWire(err, appwire.CodeBusy) {
		t.Fatalf("a busy history = %v, want busy", err)
	}
}

// A host whose connection does not keep renders is UNSUPPORTED, never empty.
func TestRenderRPCsOnAnOldHostAreUnknownMethodNotEmpty(t *testing.T) {
	backend, _, agent := renderBackend(t, plainConversation{&fakeConversation{}})
	if _, _, err := backend.ensure(mustFind(t, backend, agent.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ListRenders(agent.ID, "", 50); !isWire(err, appwire.CodeUnknownMethod) {
		t.Fatalf("list on an old host = %v", err)
	}
	if _, err := backend.GetRender(agent.ID, "rnd_1"); !isWire(err, appwire.CodeUnknownMethod) {
		t.Fatalf("get on an old host = %v", err)
	}
	if _, err := backend.CancelRender(agent.ID, "rnd_1"); !isWire(err, appwire.CodeUnknownMethod) {
		t.Fatalf("cancel on an old host = %v", err)
	}
}

// The same news arrives on the turn's stream and the task lane; the app hears
// each change once, and a late receipt is the SAME id with the same state.
func TestRenderChangedIsSaidOncePerChangeAndAReceiptKeepsTheState(t *testing.T) {
	conv := &renderConversation{fakeConversation: &fakeConversation{}, records: map[string]session.RenderRecord{}}
	backend, rec, agent := renderBackend(t, conv)
	if _, _, err := backend.ensure(mustFind(t, backend, agent.ID)); err != nil {
		t.Fatal(err)
	}
	cancelled := session.RenderRecord{ID: "rnd_9", Kind: "video", State: session.RenderCancelled, Model: "m", Started: time.Unix(1_700_000_000, 0), Charge: session.RenderChargeUnknown, Upstream: "unknown"}
	cost := 0.9
	receipt := cancelled
	receipt.Charge, receipt.CostUSD = session.RenderCharged, &cost

	for _, record := range []session.RenderRecord{cancelled, cancelled, receipt, receipt} {
		record := record
		conv.fakeConversation.lane(session.Event{Kind: session.EventRenderUpdate, Render: &record})
	}
	rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.RenderChanged)
		return ok && changed.Render.Charge == "charged"
	})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var heard []appwire.RenderChanged
	for _, data := range rec.data {
		if changed, ok := data.(appwire.RenderChanged); ok {
			heard = append(heard, changed)
		}
	}
	if len(heard) != 2 || heard[0].Render.State != "cancelled" || heard[0].Render.Charge != "unknown" ||
		heard[1].Render.State != "cancelled" || heard[1].Render.Charge != "charged" || heard[1].Render.ID != "rnd_9" || heard[1].Agent != agent.ID {
		t.Fatalf("render.changed heard = %+v, want one per distinct change", heard)
	}
}

func isWire(err error, code string) bool {
	var wire *appwire.Error
	return errors.As(err, &wire) && wire.Code == code
}

func mustFind(t *testing.T, backend *appBackend, id string) *appAgent {
	t.Helper()
	a, err := backend.find(id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
