package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

type idleAppContract struct {
	b        *appBackend
	rec      *appRecorder
	model    *recordingModel
	agent    appwire.Agent
	own      *syncsetup.Sync
	peerHome string
}

func newIdleAppContract(t *testing.T, wrappers ...func(http.Handler) http.Handler) *idleAppContract {
	t.Helper()
	celltest.RequireEngine(t)
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	relay := e2WireRelay(t, wrappers...)
	t.Setenv(syncsetup.URLVar, relay.URL)
	b, rec, model, workspace := appSyncBackend(t)
	_, peerHome := outageHomes(t, relay.URL)
	t.Cleanup(func() { _ = b.Close() })
	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := b.Send(agent.ID, "! echo idle-contract-seed", "")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, rec, turn)
	return &idleAppContract{b: b, rec: rec, model: model, agent: agent, own: appSyncOwn(t), peerHome: peerHome}
}

func (f *idleAppContract) released(t *testing.T) directory.CellView {
	t.Helper()
	var view directory.CellView
	appSyncWait(t, "C1 paired app release", func() bool {
		var err error
		view, err = f.own.Dir.Cell(t.Context(), f.agent.ID)
		return err == nil && view.Cell.Head != "" && view.Cell.Lease.Expires == 0
	})
	return view
}

func TestIdleHandoff_DefaultOff_AppRetainsHoldAndRefusesAutoOpen(t *testing.T) {
	celltest.RequireEngine(t)
	t.Setenv("CODEAF_IDLE_HANDOFF", "")
	relay := e2WireRelay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	b, rec, _, workspace := appSyncBackend(t)
	_, peerHome := outageHomes(t, relay.URL)
	t.Cleanup(func() { _ = b.Close() })
	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := b.Send(agent.ID, "! echo default-off-seed", "")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, rec, turn)
	own := appSyncOwn(t)
	appSyncWait(t, "published head on relay", func() bool {
		v, err := own.Dir.Cell(t.Context(), agent.ID)
		return err == nil && v.Cell.Head != ""
	})
	time.Sleep(150 * time.Millisecond)
	v, err := own.Dir.Cell(t.Context(), agent.ID)
	if err != nil || v.Cell.Lease.Expires == 0 || v.Cell.Lease.Expires <= v.Now {
		t.Fatalf("default-off app released its idle hold: %+v, %v", v, err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	peer := e2StartApp(t, peerHome)
	t.Cleanup(func() { e2Quit(t, peer) })
	row := outageChatRow(t, peer, agent.ID, func(row appwire.ChatRow) bool { return row.Status == "idle" })
	if row.CanOpen || !row.CanTake || row.Here {
		t.Fatalf("default-off released remote row offered auto-open: %+v", row)
	}
}

func TestIdleRelease_C1_C3_AppSendReacquiresBeforeModelCall(t *testing.T) {
	f := newIdleAppContract(t)
	before := f.released(t)
	var checked atomic.Int32
	original := f.model.server.Config.Handler
	f.model.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			v, err := f.own.Dir.Cell(r.Context(), f.agent.ID)
			if err != nil || v.Cell.Lease.Device != f.own.Device.ID() || v.Cell.Lease.Expires <= v.Now || v.Cell.Lease.Fence <= before.Cell.Lease.Fence {
				t.Errorf("C3 provider reached without reacquisition: %+v, %v", v, err)
			}
			checked.Add(1)
		}
		original.ServeHTTP(w, r)
	})
	turn, err := f.b.Send(f.agent.ID, "reply after idle release", "")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, f.rec, turn)
	if checked.Load() == 0 {
		t.Fatal("C3 work never reached its scripted model")
	}
}

func TestIdleRelease_C4_AppRefusesMovedChatWithoutJournalOrModelWrite(t *testing.T) {
	f := newIdleAppContract(t)
	seen := f.released(t)
	peer := appDeviceSync(t, f.peerHome)
	_, err := peer.Dir.Acquire(t.Context(), f.agent.ID, directory.AcquireOpts{
		RequireConditional: true,
		Expect:             &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := appTranscriptOf(f.b, f.agent.ID)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var called atomic.Bool
	original := f.model.server.Config.Handler
	f.model.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			called.Store(true)
		}
		original.ServeHTTP(w, r)
	})
	_, err = f.b.Send(f.agent.ID, "must stay unsent", "")
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeConflict {
		t.Fatalf("C4 held-elsewhere refusal=%v", err)
	}
	raw, _ := json.Marshal(wire.Detail)
	var moved appwire.HeldElsewhere
	if err := json.Unmarshal(raw, &moved); err != nil || moved.Kind != "held_elsewhere" || moved.Holder == nil || moved.Holder.ID != peer.Device.ID() {
		t.Fatalf("C4 moved holder detail=%s, %v", raw, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || called.Load() {
		t.Fatal("C4 refused work wrote or called the model")
	}
}

func TestIdleOpen_C6_C8_AppOpensReleasedChatAfterSourceStops(t *testing.T) {
	f := newIdleAppContract(t)
	before := f.released(t)
	if err := f.b.Close(); err != nil {
		t.Fatal(err)
	}
	peer := e2StartApp(t, f.peerHome)
	t.Cleanup(func() { e2Quit(t, peer) })
	row := outageChatRow(t, peer, f.agent.ID, func(row appwire.ChatRow) bool { return row.CanOpen })
	if row.Here || !row.CanTake {
		t.Fatalf("C6 available row=%+v", row)
	}
	opened := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, peer.w, "agents.open", map[string]any{"id": f.agent.ID}).Agent
	if opened.Location == nil || !opened.Location.Here || opened.Location.Seen == nil || opened.Location.Seen.Fence != before.Cell.Lease.Fence+1 {
		t.Fatalf("C6 engine did not open here: %+v", opened)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	view, err := f.own.Dir.Cell(ctx, f.agent.ID)
	if err != nil || view.Cell.Head != before.Cell.Head || view.Cell.Lease.Device == f.own.Device.ID() {
		t.Fatalf("C6/C8 relay state=%+v, %v", view, err)
	}
}

func TestIdleRelease_C2_C7_AppRunningAndQueuedWorkKeepTheLiveHold(t *testing.T) {
	f := newIdleAppContract(t)
	f.released(t)
	pause, entered, resume := appSyncHoldReply(t, f.model)
	pause()
	turn, err := f.b.Send(f.agent.ID, "hold the model reply", "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("C2 model did not start")
	}
	if _, err := f.b.Queue(f.agent.ID, "queued after this answer", ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	before, err := f.own.Dir.Cell(t.Context(), f.agent.ID)
	if err != nil || before.Cell.Lease.Expires <= before.Now {
		t.Fatalf("C2 active hold=%+v, %v", before, err)
	}
	peer := e2StartApp(t, f.peerHome)
	t.Cleanup(func() { e2Quit(t, peer) })
	row := outageChatRow(t, peer, f.agent.ID, func(row appwire.ChatRow) bool { return row.Seen.Fence == before.Cell.Lease.Fence })
	if row.CanOpen || !row.CanTake || row.Here {
		t.Fatalf("C7 running chat lost its explicit Continue here choice: %+v", row)
	}
	after, err := f.own.Dir.Cell(t.Context(), f.agent.ID)
	if err != nil || after.Cell.Lease.Device != before.Cell.Lease.Device || after.Cell.Lease.Fence != before.Cell.Lease.Fence {
		t.Fatalf("C7 watching took ownership: %+v, %v", after, err)
	}
	resume()
	appTurnDoneFor(t, f.rec, turn)
	appSyncWait(t, "C2 queued work to settle", func() bool { return len(f.b.queued(f.b.agent(f.agent.ID))) == 0 })
	f.released(t)
}

func TestIdleRelease_C12_AppSendRacesAutomaticOpen(t *testing.T) {
	f := newIdleAppContract(t)
	seen := f.released(t)
	peer := e2StartApp(t, f.peerHome)
	t.Cleanup(func() { e2Quit(t, peer) })
	outageChatRow(t, peer, f.agent.ID, func(row appwire.ChatRow) bool { return row.CanOpen })
	path := appTranscriptOf(f.b, f.agent.ID)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	resume := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(resume) }) }
	t.Cleanup(unblock)
	var called atomic.Int32
	original := f.model.server.Config.Handler
	f.model.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			v, err := f.own.Dir.Cell(r.Context(), f.agent.ID)
			if err != nil || v.Cell.Lease.Device != f.own.Device.ID() || v.Cell.Lease.Fence != seen.Cell.Lease.Fence+1 || v.Cell.Lease.Expires <= v.Now {
				t.Errorf("C12 model entered without the winning fence: %+v, %v", v, err)
			}
			called.Add(1)
			select {
			case <-resume:
			case <-r.Context().Done():
				return
			}
		}
		original.ServeHTTP(w, r)
	})
	type sendResult struct {
		turn uint64
		err  error
	}
	sent := make(chan sendResult, 1)
	go func() { turn, err := f.b.Send(f.agent.ID, "C12 real send", ""); sent <- sendResult{turn, err} }()
	raw, openErr := peer.w.call("agents.open", map[string]any{"id": f.agent.ID})
	var send sendResult
	select {
	case send = <-sent:
	case <-time.After(3 * time.Second):
		t.Fatal("C12 send admission blocked")
	}
	if (send.err == nil) == (openErr == nil) {
		t.Fatalf("C12 want one winner: send=%v open=%v", send.err, openErr)
	}
	v, err := f.own.Dir.Cell(t.Context(), f.agent.ID)
	if err != nil || v.Cell.Lease.Fence != seen.Cell.Lease.Fence+1 || v.Cell.Lease.Expires <= v.Now {
		t.Fatalf("C12 directory winner=%+v, %v", v, err)
	}
	var loser *appwire.Error
	if send.err == nil {
		if v.Cell.Lease.Device != f.own.Device.ID() {
			t.Fatal("C12 successful send does not own the chat")
		}
		loser = openErr
	} else {
		if v.Cell.Lease.Device == f.own.Device.ID() {
			t.Fatal("C12 refused send still owns the chat")
		}
		var opened struct {
			Agent appwire.Agent `json:"agent"`
		}
		if err := json.Unmarshal(raw, &opened); err != nil || opened.Agent.Location == nil || !opened.Agent.Location.Here {
			t.Fatalf("C12 automatic open=%s, %v", raw, err)
		}
		if !errors.As(send.err, &loser) {
			t.Fatalf("C12 send refusal=%v", send.err)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) || called.Load() != 0 {
			t.Fatalf("C12 losing send wrote or called model: %v", err)
		}
	}
	if loser == nil || loser.Code != appwire.CodeConflict {
		t.Fatalf("C12 losing door=%v", loser)
	}
	detail, _ := json.Marshal(loser.Detail)
	var held appwire.HeldElsewhere
	if err := json.Unmarshal(detail, &held); err != nil || held.Kind != "held_elsewhere" {
		t.Fatalf("C12 losing detail=%s, %v", detail, err)
	}
	unblock()
	if send.err == nil {
		appTurnDoneFor(t, f.rec, send.turn)
		if called.Load() == 0 {
			t.Fatal("C12 winning send never entered its scripted model")
		}
	}
}
