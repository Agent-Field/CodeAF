package enginehost

// stale_test.go is the half of this package that keeps a rebuild from trapping
// somebody: a host that can be asked which build it is, that goes when it is
// holding nothing, that stays when it is holding work, and that lets itself go
// once the file it was started from has been replaced under it.
//
// These stand a REAL host on a REAL unix socket, which is why they go through
// [shortHome] — a socket path has about a hundred bytes to spend and a Mac's
// TMPDIR eats most of them before the test has said anything (CLAUDE.md names
// the workaround: TMPDIR=/tmp/eh).

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/remote"
)

// liveHost stands a host up on its socket and hands back the workspace it holds.
func liveHost(t *testing.T, workspace string) {
	t.Helper()
	stopped := make(chan error, 1)
	go func() {
		stopped <- Run(workspace, Options{
			Boot: func(remote.Hello) (*remote.Engine, error) {
				return &remote.Engine{Agent: stubAgent{}, Workspace: workspace}, nil
			},
		})
	}()
	waitForHostQuietly(t, workspace)
	t.Cleanup(func() {
		// Whatever the test did, nothing is left listening: a host that
		// outlived its test is exactly the thing this file is about.
		_ = Retire(workspace, true)
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
		}
	})
}

// waitForHostQuietly waits for a host to be listening WITHOUT connecting to it,
// which is the whole point of the helper.
//
// A connection is reaped on its own goroutine after the far end closes it, so a
// test that dialled to find out whether the host was up would then race that
// reaping — and a host with a connection it has not finished letting go of
// honestly answers that it is holding something. That answer is the safe side
// of the question in the field (a refusal, never a retirement) and it is a
// coin toss inside a test. So this asks the two things a host publishes without
// being spoken to: it has taken the lock, and its socket file exists.
func waitForHostQuietly(t *testing.T, workspace string) {
	t.Helper()
	dir, err := Dir(workspace)
	if err != nil {
		t.Fatalf("resolve the directory: %v", err)
	}
	socket := filepath.Join(dir, socketName)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			held, err := takeLock(filepath.Join(dir, lockName))
			if err != nil {
				// The lock is taken and the socket file is there, which
				// together are a host that is listening.
				return
			}
			_ = releaseLock(held)
		}
		if time.Now().After(deadline) {
			t.Fatal("no host came up")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ── which build is holding this ─────────────────────────────────────────────

func TestAHostSaysWhichBuildItIsOnItsOwnSocket(t *testing.T) {
	shortHome(t)
	workspace := "/home/somebody/api"
	liveHost(t, workspace)

	self, err := Ask(workspace, remote.WhoIs{})
	if err != nil {
		t.Fatalf("ask the host: %v", err)
	}
	if self.Version != remote.Version {
		t.Fatalf("the host said it speaks %d, want %d", self.Version, remote.Version)
	}
	if self.Workspace != workspace {
		t.Fatalf("the host named %q as its workspace", self.Workspace)
	}
	// THE QUESTION IS NOT THE WORK. A host with nothing else on it must not
	// read its own asker as a reason to say it is busy — that would make every
	// stale host permanently unretirable.
	if self.Busy {
		t.Fatal("a host holding nothing said it was busy")
	}
}

// ── it goes when it is holding nothing ──────────────────────────────────────

func TestAHostRetiresWhenAskedAndItIsHoldingNothing(t *testing.T) {
	shortHome(t)
	workspace := "/home/somebody/api"
	liveHost(t, workspace)

	if err := Retire(workspace, false); err != nil {
		t.Fatalf("retire the host: %v", err)
	}
	if conn, err := Dial(workspace); err == nil {
		_ = conn.Close()
		t.Fatal("the host was still answering after it agreed to go")
	}
	// And the lock is free, which is what lets the next connection start a host
	// from the binary that is on disk now.
	dir, err := Dir(workspace)
	if err != nil {
		t.Fatalf("resolve the directory: %v", err)
	}
	held, err := takeLock(filepath.Join(dir, lockName))
	if err != nil {
		t.Fatalf("the retired host is still holding its lock: %v", err)
	}
	_ = releaseLock(held)
}

// ── and it stays when somebody is using it ──────────────────────────────────

func TestAHostIsNotRetiredOutFromUnderAnAttachedSurface(t *testing.T) {
	shortHome(t)
	workspace := "/home/somebody/api"
	liveHost(t, workspace)

	conn, err := Dial(workspace)
	if err != nil {
		t.Fatalf("dial the host: %v", err)
	}
	surface, err := remote.Dial(conn, "test", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	defer surface.Close()

	self, err := Ask(workspace, remote.WhoIs{StandDown: true})
	if err != nil {
		t.Fatalf("ask the host to go: %v", err)
	}
	if !self.Busy {
		t.Fatal("a host with a surface attached said it was holding nothing")
	}
	if self.Retiring {
		t.Fatal("a host with a surface attached agreed to go anyway")
	}
	if err := Retire(workspace, false); err != ErrHostBusy {
		t.Fatalf("retiring a busy host answered %v, want ErrHostBusy", err)
	}
	// AND IT IS STILL THERE, which is the whole of the promise: nobody's turn
	// ended because somebody else's connection wanted a newer build.
	if _, err := Ask(workspace, remote.WhoIs{}); err != nil {
		t.Fatalf("the host stopped answering after it refused to go: %v", err)
	}
}

// A person who has been told what is running and says stop anyway is obeyed.
// That is what `aforge engine --stop` is, and the turn it catches stops where
// it is and keeps its partial reply — the same thing ctrl+c does locally.
func TestStopEndsAHostEvenWithASurfaceOnIt(t *testing.T) {
	shortHome(t)
	workspace := "/home/somebody/api"
	liveHost(t, workspace)

	conn, err := Dial(workspace)
	if err != nil {
		t.Fatalf("dial the host: %v", err)
	}
	surface, err := remote.Dial(conn, "test", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	defer surface.Close()

	stopped, err := Stop(workspace)
	if err != nil {
		t.Fatalf("stop the host: %v", err)
	}
	if !stopped {
		t.Fatal("stop found nothing to stop")
	}
	if conn, err := Dial(workspace); err == nil {
		_ = conn.Close()
		t.Fatal("the host was still answering after it was stopped")
	}
}

// Stopping nothing is not a failure and says nothing happened, because that is
// the ordinary answer on a machine where nobody has connected today.
func TestStopFindsNothingToStopAndSaysSo(t *testing.T) {
	shortHome(t)
	stopped, err := Stop("/home/somebody/api")
	if err != nil {
		t.Fatalf("stop with no host: %v", err)
	}
	if stopped {
		t.Fatal("stop claimed to have stopped a host that was never there")
	}
}

// ── the binary underneath ───────────────────────────────────────────────────

func TestABinaryRemovedAndRebuiltIsNotTheBinaryThatStarted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aforge")
	if err := os.WriteFile(path, []byte("first"), 0o755); err != nil {
		t.Fatalf("write the binary: %v", err)
	}
	was, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the binary: %v", err)
	}
	binary := hostBinary{path: path, was: was}
	if binary.replaced() {
		t.Fatal("an untouched binary read as replaced")
	}

	// The install this tree allows and the one it forbids, in that order:
	// remove-and-write mints a new inode, and a copy over the top keeps the
	// inode and moves the modification time.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove the binary: %v", err)
	}
	if binary.replaced() != true {
		t.Fatal("a deleted binary read as unchanged")
	}
	if err := os.WriteFile(path, []byte("second"), 0o755); err != nil {
		t.Fatalf("rebuild the binary: %v", err)
	}
	if !binary.replaced() {
		t.Fatal("a rebuilt binary read as the one that started")
	}

	// A machine that could not say which file it is never claims to have been
	// replaced: the capability is absent rather than guessing.
	if (hostBinary{}).replaced() {
		t.Fatal("a host with no idea what file it is claimed to be stale")
	}
}

func TestAHostWhoseBinaryWasReplacedRetiresOnceItIsHoldingNothing(t *testing.T) {
	h := stubHost(t, "/home/somebody/api")
	h.binary = replacedBinary(t)
	h.quiet = time.Now()

	if !h.sweepOnce() {
		t.Fatal("a host started from a file that is gone stayed up with nothing to hold")
	}
	// AND THE DOOR IS SHUT ON THE WAY OUT, so a surface arriving in the moment
	// between the decision and the stop is refused rather than handed a
	// conversation that is about to end.
	if _, err := h.open(remote.Hello{Version: remote.Version}); err == nil {
		t.Fatal("a host on its way out opened a new conversation")
	}
}

func TestAHostWhoseBinaryWasReplacedStaysWhileItIsHoldingWork(t *testing.T) {
	h := stubHost(t, "/home/somebody/api")
	h.binary = replacedBinary(t)
	h.quiet = time.Now()

	// One conversation with a surface on it. It is not idle, and this host is
	// not the thing that ends it.
	if _, err := h.open(remote.Hello{Version: remote.Version}); err != nil {
		t.Fatalf("open a conversation: %v", err)
	}
	h.mu.Lock()
	h.live = 1
	h.mu.Unlock()

	if h.sweepOnce() {
		t.Fatal("a host with a connection on it retired because its binary had moved")
	}
	if h.retiring {
		t.Fatal("a host holding work shut its own door")
	}
}

// replacedBinary is a host binary whose file is already gone, which is the
// simplest true version of "this is not the build that is on disk now".
func replacedBinary(t *testing.T) hostBinary {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aforge")
	if err := os.WriteFile(path, []byte("gone in a moment"), 0o755); err != nil {
		t.Fatalf("write the binary: %v", err)
	}
	was, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the binary: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove the binary: %v", err)
	}
	return hostBinary{path: path, was: was}
}
