package cellstore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// TestW3SocketIdentityIncludesHomeAndBuild proves B1, including equivalent
// absolute home spellings and symlink aliases where the platform allows them.
func TestW3SocketIdentityIncludesHomeAndBuild(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	bin := "/engine/furrow-0.2.0-aaaaaaaaaaaa"
	t.Setenv("LOCALAPPDATA", filepath.Dir(testSocket(t)))
	t.Setenv("CODEAF_HOME", first)
	a := socketFor(bin)
	if a != socketFor(bin) || a == socketFor("/engine/furrow-0.2.0-bbbbbbbbbbbb") {
		t.Fatal("same home/build must share; distinct builds must not")
	}
	t.Setenv("CODEAF_HOME", second)
	if a == socketFor(bin) {
		t.Fatal("distinct engine homes shared a socket")
	}
	relative, err := filepath.Rel(mustWorkingDir(t), first)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_HOME", relative)
	if a != socketFor(bin) {
		t.Fatal("relative and absolute spellings of one home did not share")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(first, alias); err == nil {
		t.Setenv("CODEAF_HOME", alias)
		if a != socketFor(bin) {
			t.Fatal("home symlink alias did not share its daemon")
		}
	} else {
		t.Logf("symlink alias unavailable: %v", err)
	}
}

func mustWorkingDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestW3HomeDaemonsSealConcurrentlyAndRestart proves B1/B2 with real furrow:
// different homes seal concurrently; same-home clients share; a killed daemon
// restarts at the next seal without disrupting the other home.
func TestW3HomeDaemonsSealConcurrentlyAndRestart(t *testing.T) {
	bin, err := sealingEngine()
	if err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	t.Setenv("LOCALAPPDATA", filepath.Dir(testSocket(t)))
	engines := make([]Engine, 2)
	daemons := make([]Daemon, 2)
	cells := []cell.Cell{newCell(t), newCell(t)}
	for i := range engines {
		t.Setenv("CODEAF_HOME", t.TempDir())
		e := engineFor(t, (Engine{}).Root())
		e.Binary, e.Workspace = bin, t.TempDir()
		e.Transport = transportFor(bin, nil)
		engines[i] = e
		daemons[i] = Daemon{Socket: socketFor(bin), Binary: bin}
		d := daemons[i]
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), SealWithin)
			defer cancel()
			if err := d.Stop(ctx); err != nil {
				t.Errorf("stop home daemon: %v", err)
			}
		})
	}
	if daemons[0].Socket == daemons[1].Socket {
		t.Fatal("two engines share one socket")
	}
	errors := make(chan error, 2)
	for i, e := range engines {
		go func() {
			sealed, err := e.Seal(t.Context(), cells[i], TurnInfo{})
			if err == nil && sealed.Turn.ID == "" {
				err = os.ErrInvalid
			}
			errors <- err
		}()
	}
	for range engines {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	pids := []int{homeDaemonPID(t, daemons[0]), homeDaemonPID(t, daemons[1])}
	if pids[0] == pids[1] {
		t.Fatal("distinct homes used the same daemon process")
	}
	if _, err := engines[0].Seal(t.Context(), newCell(t), TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if homeDaemonPID(t, daemons[0]) != pids[0] {
		t.Fatal("same-home clients restarted instead of sharing")
	}
	exited, ok := daemonExits.Load(daemons[0].Socket)
	if !ok {
		t.Fatal("test's daemon has no owned exit channel")
	}
	process, err := os.FindProcess(pids[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = process.Release()
	select {
	case <-exited.(<-chan struct{}):
	case <-time.After(5 * time.Second):
		t.Fatal("killed daemon did not exit")
	}
	if _, err := engines[0].Seal(t.Context(), cells[0], TurnInfo{}); err != nil {
		t.Fatalf("next seal did not restart the dead daemon: %v", err)
	}
	if homeDaemonPID(t, daemons[0]) == pids[0] || homeDaemonPID(t, daemons[1]) != pids[1] {
		t.Fatal("restart did not stay within its own home")
	}
}

func homeDaemonPID(t *testing.T, d Daemon) int {
	t.Helper()
	conn, err := dial(t.Context(), d.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	response, err := exchange(conn, wireRequest{V: wireVersion, ID: nextID.Add(1), Verb: "health"})
	if err != nil {
		t.Fatal(err)
	}
	var health struct{ PID int }
	if err := json.Unmarshal(response.Ok, &health); err != nil || health.PID == 0 {
		t.Fatalf("no daemon pid: %s %v", response.Ok, err)
	}
	return health.PID
}
