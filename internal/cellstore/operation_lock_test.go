package cellstore

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

func TestCanceledDaemonCallKeepsStoreGateUntilAnswer(t *testing.T) {
	socket := testSocket(t)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	entered := make(chan struct{})
	finish := make(chan struct{})
	defer close(finish)
	guard.Go("pending daemon gate fixture", func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		raw, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			return
		}
		var req wireRequest
		if json.Unmarshal(raw, &req) != nil {
			return
		}
		close(entered)
		<-finish
		reply, _ := json.Marshal(wireResponse{ID: req.ID, Ok: json.RawMessage(`{"snapshot":"saved"}`)})
		_, _ = conn.Write(append(reply, '\n'))
	})
	target := Target{DataDir: t.TempDir()}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	guard.Go("cancel daemon gate fixture", func() { _, err := (Daemon{Socket: socket}).Do(ctx, target, sealOp{}); done <- err })
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not receive the seal")
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatalf("canceled caller: %v", err)
	}
	ran := false
	other := Spawn{Binary: "fixture", Run: func(context.Context, string, []string, ...string) ([]byte, error) { ran = true; return nil, nil }}
	waiting, stop := context.WithTimeout(t.Context(), 100*time.Millisecond)
	_, err = other.Do(waiting, target, capturePlanOp{})
	stop()
	if ran || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("new reader overlapped unfinished daemon request: ran=%v err=%v", ran, err)
	}
	// Let the accepted request answer before testing that another operation can enter.
	finish <- struct{}{}
	resumed, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	if _, err := other.Do(resumed, target, capturePlanOp{}); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("reader did not run after the daemon answered")
	}
}

func TestStoreOperationCoordinatesProcessesAndAliases(t *testing.T) {
	dir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "same-store")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	holder := Spawn{Binary: "fixture", Run: func(context.Context, string, []string, ...string) ([]byte, error) {
		close(entered)
		<-release
		return nil, nil
	}}
	guard.Go("store gate process fixture", func() { _, err := holder.Do(t.Context(), Target{DataDir: dir}, capturePlanOp{}); done <- err })
	<-entered
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestStoreOperationHelperProcess$") //codeaf:plumbing The helper is a separate contender for the same physical store.
	cmd.Env = append(os.Environ(), "CODEAF_OPERATION_LOCK_HELPER="+alias)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cross-process store exclusion: %v\n%s", err, out)
	}
	// A different store must still run while this store's request is held.
	other := Spawn{Binary: "fixture", Run: func(context.Context, string, []string, ...string) ([]byte, error) { return nil, nil }}
	if _, err := other.Do(ctx, Target{DataDir: t.TempDir()}, capturePlanOp{}); err != nil {
		t.Fatal(err)
	}
}

func TestStoreOperationHelperProcess(t *testing.T) {
	dir := os.Getenv("CODEAF_OPERATION_LOCK_HELPER")
	if dir == "" {
		t.Skip("only run by the store gate process fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	other := Spawn{Binary: "fixture", Run: func(context.Context, string, []string, ...string) ([]byte, error) {
		return nil, errors.New("overlapping store operation")
	}}
	if _, err := other.Do(ctx, Target{DataDir: dir}, capturePlanOp{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("another process entered the held store: %v", err)
	}
}
