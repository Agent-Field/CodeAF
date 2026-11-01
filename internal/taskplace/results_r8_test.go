//go:build linux || darwin

package taskplace_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
	"github.com/Agent-Field/codeaf/internal/taskplace"
)

func resultCopyFixture(t *testing.T) (taskdoor.CopyDeps, string, *plandb.Store, session.TaskRemoteRecord) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", home)
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	device, err := identity.LoadDevice(home)
	if err != nil {
		t.Fatal(err)
	}
	bin := taskEngineBinary(t)
	socketHome, err := os.MkdirTemp("", "rn-l1-r8-")
	if err != nil {
		t.Fatal(err)
	}
	daemon := cellstore.Daemon{Binary: bin, Socket: filepath.Join(socketHome, "e.sock")}
	t.Cleanup(func() { _ = daemon.Stop(context.Background()); _ = os.RemoveAll(socketHome) })
	engine := cellstore.EngineFor("")
	engine.Binary = bin
	engine.Transport = daemon
	engine.DataRoot = filepath.Join(home, "stores")
	engine.Guard.Home = home
	copies := taskdoor.CopyDeps{Root: filepath.Join(home, "copies"), Service: "http://127.0.0.1:8900", Identity: id, Device: device, Engine: engine, Store: blobstore.NewMemory()}
	work := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", work, "-c", "user.name=test", "-c", "user.email=test@example.test"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v: %s", err, out)
		}
	}
	git("init", "-qb", "task-copy")
	if err = os.WriteFile(filepath.Join(work, "edit.txt"), []byte("sent baseline"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "--all")
	git("commit", "-qm", "baseline")
	fingerprint, err := taskdoor.CopyFingerprint(t.Context(), work)
	if err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plandb.db"), "task", "1", "repair", "brief")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return copies, work, store, session.TaskRemoteRecord{Executor: session.TaskExecutor{Device: "other", Name: "work computer"}, Attempt: "att_0123456789abcdef", Fingerprint: fingerprint, Finished: true}
}

type delayedResultStore struct {
	blobstore.Store
	entered, release chan struct{}
}

func (s *delayedResultStore) GetMany(ctx context.Context, _ []string) ([]blobstore.Object, error) {
	close(s.entered)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.release:
		return nil, blobstore.ErrUnreachable
	}
}

// R7-14 and contract 11 allow stop to acquire the session's copy fence during a real fetch.
func TestAStopIsNotBlockedByAResultStillDownloading(t *testing.T) {
	copies, work, store, record := resultCopyFixture(t)
	blobs := &delayedResultStore{Store: copies.Store, entered: make(chan struct{}), release: make(chan struct{})}
	copies.Store = blobs
	cellID, err := cell.NewID(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	record.Final, _ = json.Marshal(taskdoor.Finished{Outcome: "done", Result: &taskdoor.Head{Cell: cellID, Head: strings.Repeat("0", 64)}})
	remote := &taskplace.Engine{Copies: copies, Computer: session.TaskComputer{Name: "work computer"}}
	var copyFence sync.Mutex
	done := make(chan session.RunSummary, 1)
	go func() {
		done <- remote.Resume(t.Context(), session.RunSpec{Workspace: work, Store: store, Remote: &record, SaveRemote: func(session.TaskRemoteRecord, bool) error { return nil }, WithRemoteCopy: func(install func() error) error { copyFence.Lock(); defer copyFence.Unlock(); return install() }})
	}()
	defer func() { close(blobs.release); <-done }()
	select {
	case <-blobs.entered:
	case summary := <-done:
		done <- summary
		t.Fatalf("fetch ended before the gate: %+v", summary)
	case <-time.After(5 * time.Second):
		t.Fatal("result never reached its download")
	}
	stopped := make(chan struct{})
	go func() {
		copyFence.Lock()
		defer copyFence.Unlock()
		_ = remote.Stop(t.Context(), session.TaskRemoteRecord{})
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop waited for the result download")
	}
}

// R7-9 and contract 11 preserve the actual sent fingerprint for local fallback.
func TestRunItHereWorksAfterAnInterruptedRemoteRun(t *testing.T) {
	copies, work, store, record := resultCopyFixture(t)
	other, _, _, _ := resultCopyFixture(t)
	other.Identity = copies.Identity
	var err error
	other.Device, err = identity.NewDevice(copies.Identity)
	if err != nil {
		t.Fatal(err)
	}
	other.Store = copies.Store
	input, err := taskdoor.SendCopy(t.Context(), copies, work)
	if err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(t.TempDir(), "partial")
	if err := taskdoor.ReceiveCopy(t.Context(), other, input, partial); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "edit.txt"), []byte("unfinished remote edit"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := taskdoor.SendResult(t.Context(), other, partial, input)
	if err != nil {
		t.Fatal(err)
	}
	record.Final, _ = json.Marshal(taskdoor.Finished{Outcome: "interrupted", Report: "the worker stopped", Result: &result})
	remote := &taskplace.Engine{Copies: copies, Computer: session.TaskComputer{Name: "work computer"}}
	summary := remote.Resume(t.Context(), session.RunSpec{Workspace: work, Store: store, Remote: &record, SaveRemote: func(r session.TaskRemoteRecord, _ bool) error { record = r; return nil }})
	if summary.Outcome != "interrupted" || !strings.Contains(summary.Result, "partial work stayed on the other computer") {
		t.Fatalf("interrupted result = %+v", summary)
	}
	if err := remote.CheckCopy(t.Context(), work, record.Fingerprint); err != nil {
		t.Fatalf("run it here refused its baseline: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(work, "edit.txt"))
	if err != nil || string(raw) != "sent baseline" {
		t.Fatalf("baseline changed: %q, %v", raw, err)
	}
	if record.Restored {
		t.Fatal("partial work marked restored")
	}
}
