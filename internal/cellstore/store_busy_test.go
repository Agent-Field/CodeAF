package cellstore

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
)

type testBusyOp struct{}

func (testBusyOp) Verb() string   { return "want" }
func (testBusyOp) Args() []string { return []string{"want"} }

func TestStoreBusyHelperProcess(t *testing.T) {
	lockPath := os.Getenv("CODEAF_TEST_HOLD_STORE_LOCK")
	if lockPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		os.Exit(2)
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		os.Exit(3)
	}
	if err := filelock.Lock(file, true, true); err != nil {
		os.Exit(4)
	}
	if payload := os.Getenv("CODEAF_TEST_HOLD_PAYLOAD"); payload != "" {
		_, _ = file.WriteString(payload)
		_ = file.Sync()
	}
	_, _ = os.Stdout.WriteString("LOCKED\n")
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
	_ = filelock.Unlock(file)
	_ = file.Close()
	os.Exit(0)
}

func startLockHolderSubprocess(t *testing.T, lockPath, payload string) (*exec.Cmd, func()) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestStoreBusyHelperProcess$", "chat") //codeaf:plumbing spawns this test binary to hold an advisory store lock in another process
	cmd.Env = append(os.Environ(),
		"CODEAF_TEST_HOLD_STORE_LOCK="+lockPath,
		"CODEAF_TEST_HOLD_PAYLOAD="+payload,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "LOCKED" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("helper failed to lock %s: line=%q err=%v", lockPath, line, err)
	}
	var stopped bool
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_, _ = stdin.Write([]byte("release\n"))
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	t.Cleanup(stop)
	return cmd, stop
}

func TestStoreBusyReportsHolderWithinSeconds(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "v3", "stores", "shared")
	lockPath := filepath.Join(dataDir, "store-v1", "locks", "pack.lock")
	payload := ""
	if runtime.GOOS == "windows" {
		raw, _ := json.Marshal(StoreHolder{Command: "codeaf chat"})
		payload = string(raw)
	}
	child, stopChild := startLockHolderSubprocess(t, lockPath, payload)
	defer stopChild()

	// A held lock never pre-empts a request that the engine itself handles.
	ran := 0
	var engineErr error
	tr := Spawn{
		Binary: "/bin/echo",
		Run: func(context.Context, string, []string, ...string) ([]byte, error) {
			ran++
			if engineErr != nil {
				return nil, engineErr
			}
			return []byte("{}"), nil
		},
	}

	if _, err := tr.Do(context.Background(), Target{Tree: t.TempDir(), DataDir: dataDir}, testBusyOp{}); err != nil {
		t.Fatalf("normal request failed while lock was held: %v", err)
	}
	if ran != 1 {
		t.Fatalf("engine invocations = %d, want 1", ran)
	}

	// Non-busy engine errors are left untouched even when a lock file exists.
	engineErr = errors.New("furrow: exit status 1: remote writer lock is already held")
	if _, err := tr.Do(context.Background(), Target{Tree: t.TempDir(), DataDir: dataDir}, testBusyOp{}); errors.Is(err, ErrStoreBusy) {
		t.Fatalf("remote lock error misclassified as ErrStoreBusy: %v", err)
	}

	// When the engine itself returns its busy error, the transport classifies it
	// as ErrStoreBusy and names the foreign holder's command and pid.
	engineErr = errors.New("furrow: exit status 1: store writer is still busy after waiting 3 seconds; retry the operation")
	began := time.Now()
	_, err := tr.Do(context.Background(), Target{Tree: t.TempDir(), DataDir: dataDir}, testBusyOp{})
	elapsed := time.Since(began)

	if !errors.Is(err, ErrStoreBusy) {
		t.Fatalf("err = %v, want errors.Is(..., ErrStoreBusy)", err)
	}
	var busy *StoreBusyError
	if !errors.As(err, &busy) {
		t.Fatalf("err = %T (%v), want *StoreBusyError", err, err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("busy store classification took %v, want within a few seconds", elapsed)
	}
	if runtime.GOOS != "windows" {
		if busy.Holder.PID != child.Process.Pid {
			t.Fatalf("holder pid = %d, want child pid %d", busy.Holder.PID, child.Process.Pid)
		}
		if busy.Holder.Command == "" {
			t.Fatalf("holder command is empty; want child command")
		}
	}

	// Releasing the foreign writer lock lets the next operation proceed.
	stopChild()
	engineErr = nil
	if _, err := tr.Do(context.Background(), Target{Tree: t.TempDir(), DataDir: dataDir}, testBusyOp{}); err != nil {
		t.Fatalf("after lock release, Do returned %v", err)
	}
}

func TestStoreBusyIgnoresHomesOwnDaemon(t *testing.T) {
	bin, err := sealingEngine()
	if err != nil {
		t.Skipf("no owned engine: %v", err)
	}
	stateRoot := t.TempDir()
	t.Setenv(home.EnvVar, stateRoot)
	t.Cleanup(StopSpawned)

	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	e := Engine{Binary: bin, Workspace: work, Transport: Spawn{Binary: bin}}
	sealed, err := e.Seal(t.Context(), c, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}

	// The home's own furrow serve daemon is the normal writer path and must
	// never be reported as a busy-store blocker or pre-empt a spawned verb.
	if holder, busy := InspectStoreBusy(e.StoreDir(c), ""); busy {
		t.Fatalf("home's own daemon reported as busy holder: %+v", holder)
	}
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.Sync(SyncKeys{}, "test").Materialize(t.Context(), c, sealed.Turn.ID); err != nil {
		t.Fatalf("Materialize failed while home's own daemon was active: %v", err)
	}
}
