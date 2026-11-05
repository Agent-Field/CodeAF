//go:build linux

package cellstore

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

// StopSpawned is an explicit owner boundary, including a group already queued
// by that owner's last capture. A later capture may intentionally start again.
func TestStopSpawnedRejectsPendingGroupCommit(t *testing.T) {
	testStopRejectsPendingGroupCommit(t, true)
}

func TestDaemonStopRejectsPendingGroupCommit(t *testing.T) {
	testStopRejectsPendingGroupCommit(t, false)
}

func testStopRejectsPendingGroupCommit(t *testing.T, spawnedStop bool) {
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	c := newCell(t)
	d := daemonFor(t, bin).(Daemon)
	e := Engine{Binary: bin, DataRoot: t.TempDir(), Transport: Fallback{Primary: d, Secondary: Spawn{Binary: bin, Socket: d.Socket}}}
	// Schedule the real group deterministically, without waiting on wall time.
	previous := commits
	commits = newCommitQueue(syncPath)
	commits.beforeFlush = flushEngines
	var scheduled []func()
	commits.after = func(_ time.Duration, f func()) { scheduled = append(scheduled, f) }
	t.Cleanup(func() {
		verifyOwnedFixtureDaemon(t, d)
		StopSpawned()
		commits = previous
		engineCommits.Lock()
		delete(engineCommits.cells, c.Root)
		engineCommits.Unlock()
	})
	spawnedMu.Lock()
	before := len(spawned)
	spawnedMu.Unlock()
	sealed, err := e.Seal(ctx, c, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRecorder(&stubExec{}, e, c, e.WALPath(c), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Around(ctx, executor.Call{Tool: "bash", Deferred: true}, executor.EffectExternal, AgentRun, func() ([]byte, bool) { return []byte("receipt survives stop"), false }); err != nil {
		t.Fatal(err)
	}
	if len(scheduled) != 1 {
		t.Fatalf("scheduled groups = %d, want 1", len(scheduled))
	}
	spawnedMu.Lock()
	children := append([]spawnedDaemon(nil), spawned[before:]...)
	spawnedMu.Unlock()
	if len(children) != 1 {
		t.Fatalf("initial spawns = %d, want 1", len(children))
	}
	pid := children[0].process.Pid
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil || !bytes.Contains(raw, []byte(d.Socket)) || !bytes.Contains(raw, []byte("serve")) {
		t.Fatalf("cannot verify owned PID %d: %v", pid, err)
	}
	if spawnedStop {
		StopSpawned()
	} else if err := d.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	spawnedMu.Lock()
	stoppedCount := len(spawned)
	spawnedMu.Unlock()
	select {
	case <-children[0].exited:
	default:
		t.Error("original owned PID is still alive after stop")
	}
	for i := 0; i < 3 && i < len(scheduled); i++ {
		scheduled[i]()
	}
	spawnedMu.Lock()
	late := len(spawned) - stoppedCount
	spawnedMu.Unlock()
	if late != 0 {
		t.Errorf("group commit spawned %d daemon(s) after explicit stop", late)
	}
	if pids := aliveUnder(filepath.Dir(d.Socket)); len(pids) != 0 {
		t.Errorf("owned daemons still alive after stop: %v", pids)
	}
	if err := Flush(ctx); err == nil {
		t.Error("exit flush acknowledged stopped engine")
	}
	commits.mu.Lock()
	pending := len(commits.pending)
	commits.mu.Unlock()
	if pending == 0 {
		t.Error("failed flush discarded pending local records")
	}
	head, err := Head(c)
	if err != nil || head == nil || head.Turn.ID != sealed.Turn.ID {
		t.Fatalf("lost receipt after stopped flush: %v", err)
	}
	_, recovery, err := OpenWAL(e.WALPath(c))
	if err != nil || len(recovery.Completed) != 1 || string(recovery.Completed[0].Stdout) != "receipt survives stop" {
		t.Fatalf("stopped flush lost completed WAL output: %+v %v", recovery, err)
	}
	// A fresh recorder explicitly reuses the engine; stopping is not a permanent
	// process-wide ban that would break another chat or the next test fixture.
	if err := r.Drain(ctx); err == nil {
		t.Error("stopped recorder reopened its engine")
	}
	reopened, err := NewRecorder(&stubExec{}, e, c, e.WALPath(c), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if err := Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	head, err = Head(c)
	if err != nil || head == nil || len(head.Receipt.Calls) != 1 || head.Receipt.Calls[0].Tool != "bash" {
		t.Fatalf("reopened recorder lost receipt: %+v %v", head, err)
	}
}

// Verify the live child's command before this fixture asks teardown to signal it.
func verifyOwnedFixtureDaemon(t *testing.T, d Daemon) {
	t.Helper()
	value, ok := daemonOwners.Load(d.Socket)
	if !ok {
		return
	}
	child := value.(*spawnedDaemon)
	select {
	case <-child.exited:
		return
	default:
	}
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(child.process.Pid), "cmdline"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil || !bytes.Contains(raw, []byte(d.Socket+"\x00")) || !bytes.Contains(raw, []byte("serve\x00")) {
		t.Fatalf("cannot verify owned fixture PID %d: %v", child.process.Pid, err)
	}
}
