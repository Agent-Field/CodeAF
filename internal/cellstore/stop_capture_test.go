//go:build linux

package cellstore

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

type pausedEngineCapture struct {
	Engine
	entered, release chan struct{}
	once             sync.Once
}

func (e *pausedEngineCapture) Seal(ctx context.Context, c cell.Cell, info TurnInfo) (Sealed, error) {
	e.once.Do(func() { close(e.entered) })
	<-e.release
	return e.Engine.Seal(ctx, c, info)
}

func TestStoppedRecorderCannotRestartPendingCapture(t *testing.T) {
	testStoppedRecorderCapture(t, true)
}

func TestStopSpawnedFencesFirstRecorderCapture(t *testing.T) {
	testStoppedRecorderCapture(t, false)
}

func testStoppedRecorderCapture(t *testing.T, warm bool) {
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Fatal(err)
	}
	c := newCell(t)
	d := daemonFor(t, bin).(Daemon)
	e := Engine{Binary: bin, DataRoot: t.TempDir(), Transport: d}
	paused := &pausedEngineCapture{Engine: e, entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(paused.release) }); verifyOwnedFixtureDaemon(t, d); StopSpawned() })
	if warm {
		if _, err := e.Seal(t.Context(), c, TurnInfo{}); err != nil {
			t.Fatal(err)
		}
	}
	r, err := NewRecorder(&stubExec{}, paused, c, e.WALPath(c), Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Around(t.Context(), executor.Call{Tool: "bash", Deferred: true}, executor.EffectExternal, AgentRun, func() ([]byte, bool) { return []byte("keep pending capture"), false }); err != nil {
		t.Fatal(err)
	}
	r.Settle(t.Context())
	<-paused.entered
	if !warm {
		spawnedMu.Lock()
		initial := len(spawned)
		spawnedMu.Unlock()
		if initial != 0 {
			t.Fatalf("first capture already spawned %d daemons", initial)
		}
	}
	verifyOwnedFixtureDaemon(t, d)
	StopSpawned()
	release.Do(func() { close(paused.release) })
	if err := WaitCapture(t.Context(), c); err == nil {
		t.Error("stopped capture was acknowledged")
	}
	if pending := Unsaved(c); pending != 1 {
		t.Errorf("stopped capture cleared pending receipt debt: got %d, want 1", pending)
	}
	if err := r.Drain(t.Context()); err == nil {
		t.Error("stopped recorder restarted its pending capture")
	}
	spawnedMu.Lock()
	late := len(spawned)
	spawnedMu.Unlock()
	if late != 0 {
		t.Errorf("pending capture spawned %d daemons after stop", late)
	}
	if pids := aliveUnder(filepath.Dir(d.Socket)); len(pids) != 0 {
		t.Errorf("owned daemons survived stopped capture: %v", pids)
	}
	_, recovery, err := OpenWAL(e.WALPath(c))
	if err != nil || len(recovery.Completed) != 1 || string(recovery.Completed[0].Stdout) != "keep pending capture" {
		t.Errorf("stopped capture lost WAL output: %+v %v", recovery, err)
	}
	fresh, err := NewRecorder(&stubExec{}, e, c, e.WALPath(c), Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := r.Drain(t.Context()); err == nil {
		t.Error("new owner reactivated the old recorder")
	}
	turns, err := Turns(c)
	wantTurns := 1
	if warm {
		wantTurns = 2
	}
	if err != nil || len(turns) != wantTurns {
		t.Errorf("old recorder changed saved receipts: turns=%d err=%v", len(turns), err)
	}
	head, err := Head(c)
	if err != nil || head == nil || len(head.Receipt.Calls) != 1 || head.Receipt.Calls[0].StdoutHash != hashHex([]byte("keep pending capture")) {
		t.Fatalf("replay lost or duplicated receipt: %+v %v", head, err)
	}
	_, replayed, err := OpenWAL(e.WALPath(c))
	if err != nil || len(replayed.Completed) != 0 {
		t.Fatalf("successful replay still owes completed calls: %+v %v", replayed, err)
	}
	if err := fresh.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := Turns(c)
	if err != nil || len(after) != wantTurns {
		t.Fatalf("second drain double-counted replay: turns=%d err=%v", len(after), err)
	}
}

func TestDaemonRequestHonorsDeadlineDuringStop(t *testing.T) {
	socket := testSocket(t)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	exited := make(chan struct{})
	daemonExits.Store(socket, (<-chan struct{})(exited))
	defer daemonExits.Delete(socket)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		raw, _ := bufio.NewReader(conn).ReadBytes('\n')
		var request wireRequest
		_ = json.Unmarshal(raw, &request)
		response, _ := json.Marshal(wireResponse{ID: request.ID, Ok: json.RawMessage(`{"status":"stopping"}`)})
		_, _ = conn.Write(append(response, '\n'))
		var probe [1]byte
		_, _ = conn.Read(probe[:])
	}()
	stopped := make(chan error, 1)
	go func() { stopped <- (Daemon{Socket: socket}).Stop(t.Context()) }()
	<-joined
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan error, 1)
	other := Daemon{Socket: testSocket(t)}
	go func() { _, err := other.Do(ctx, Target{}, syncOp{}); done <- err }()
	received := false
	select {
	case err := <-done:
		received = true
		if !errors.Is(err, context.Canceled) {
			t.Errorf("canceled request = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("canceled request waited for another daemon's stop")
	}
	close(exited)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if !received {
		<-done
	}
}
