package main

// The door's decision about a host that is already there, tested against a
// stand-in host on a real socket. What is being checked is the DECISION — splice
// onto this build, replace another build, refuse when neither is possible — and
// not any part of what a conversation is, so the stand-in answers the version
// exchange and nothing else.
//
// A socket path has about a hundred bytes to spend, so the state root is a short
// temp directory rather than one named after the test (CLAUDE.md: a Mac's TMPDIR
// eats the budget on its own, and TMPDIR=/tmp/eh is the way past it).

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/enginehost"
	"github.com/Agent-Field/aforge-v2/internal/remote"
)

// standInHost answers the version exchange with whatever it was told to say,
// and takes down its own socket when it agrees to retire — which is what a real
// host's shutdown looks like from the outside.
type standInHost struct {
	self remote.HostSelf
	// older is a build from before the exchange: it refuses the question the
	// way every one of them always has.
	older bool

	listener net.Listener
	once     sync.Once
}

func standIn(t *testing.T, workspace string, self remote.HostSelf, older bool) *standInHost {
	t.Helper()
	socket, err := enginehost.SocketPath(workspace)
	if err != nil {
		t.Fatalf("resolve the socket: %v", err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen on the socket: %v", err)
	}
	host := &standInHost{self: self, older: older, listener: listener}
	t.Cleanup(host.close)
	go host.serve()
	return host
}

func (h *standInHost) serve() {
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			return
		}
		go h.answer(conn)
	}
}

func (h *standInHost) answer(conn net.Conn) {
	defer conn.Close()
	lines := bufio.NewScanner(conn)
	if !lines.Scan() {
		return
	}
	if h.older {
		refusal, _ := json.Marshal(remote.Frame{
			Kind:  "fatal",
			Error: `engine: the first frame was "whois", not a hello`,
		})
		_, _ = conn.Write(append(refusal, '\n'))
		return
	}
	var frame remote.Frame
	if err := json.Unmarshal(lines.Bytes(), &frame); err != nil {
		return
	}
	var ask remote.WhoIs
	_ = json.Unmarshal(frame.Payload, &ask)

	self := h.self
	if ask.StandDown && (ask.Anyway || !self.Busy) {
		self.Retiring = true
	}
	answer, _ := json.Marshal(remote.Frame{Kind: "whoami", Payload: mustStandInJSON(self)})
	_, _ = conn.Write(append(answer, '\n'))
	if self.Retiring {
		h.close()
	}
}

func (h *standInHost) close() {
	h.once.Do(func() {
		_ = h.listener.Close()
		// A unix socket outlives the process that made it, so a host on its way
		// down takes the file with it — and this stand-in has to as well, or
		// the door would go on finding something to dial.
		if addr, ok := h.listener.Addr().(*net.UnixAddr); ok {
			_ = os.Remove(addr.Name)
		}
	})
}

func mustStandInJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

func shortEngineHome(t *testing.T) {
	t.Helper()
	root, err := os.MkdirTemp("", "eh")
	if err != nil {
		t.Fatalf("make a state root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("AFORGE_HOME", root)
}

// ── the three things the question can find ──────────────────────────────────

func TestAHostOfThisBuildIsSplicedOntoWithoutAWord(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, remote.HostSelf{Version: remote.Version}, false)

	if err := clearStaleEngineHost(workspace); err != nil {
		t.Fatalf("a host of this build was not attached to: %v", err)
	}
}

func TestAHostOfAnotherBuildIsRetiredRatherThanAttachedTo(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, remote.HostSelf{Version: remote.Version - 1}, false)

	// THE WHOLE POINT: the door does not hand a new surface to an old build. It
	// gets that build out of the way, and the line after this one starts a
	// fresh host from the binary that is on disk now.
	if err := clearStaleEngineHost(workspace); err != nil {
		t.Fatalf("a host of another build was not cleared: %v", err)
	}
	if conn, err := enginehost.Dial(workspace); err == nil {
		_ = conn.Close()
		t.Fatal("the older host was still answering")
	}
}

func TestAHostOfAnotherBuildWithWorkInFlightIsRefusedAndNotKilled(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, remote.HostSelf{Version: remote.Version - 1, Busy: true}, false)

	err := clearStaleEngineHost(workspace)
	var stale *staleHost
	if !errors.As(err, &stale) {
		t.Fatalf("a busy older host answered %v, want a refusal", err)
	}
	if !strings.Contains(stale.reason, "something is still going in it") {
		t.Fatalf("the refusal did not say what was true: %q", stale.reason)
	}
	if !strings.Contains(stale.reason, "aforge engine --stop") {
		t.Fatalf("the refusal named no way out: %q", stale.reason)
	}
	// AND IT IS STILL THERE. Nobody's turn ended because another connection
	// wanted a newer build.
	if conn, err := enginehost.Dial(workspace); err != nil {
		t.Fatalf("the busy host was taken down: %v", err)
	} else {
		_ = conn.Close()
	}
}

// The build that trapped somebody for real: one from before the exchange
// existed, which cannot be asked anything at all. It is never ended from here —
// a process that cannot say whether it is busy is a process nobody may guess
// about — so the person is told what is true and what to type.
func TestAHostTooOldToBeAskedIsRefusedInWordsAndLeftAlone(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, remote.HostSelf{}, true)

	err := clearStaleEngineHost(workspace)
	var stale *staleHost
	if !errors.As(err, &stale) {
		t.Fatalf("a host that cannot be asked answered %v, want a refusal", err)
	}
	if !strings.Contains(stale.reason, "an older aforge") {
		t.Fatalf("the refusal did not name the older build: %q", stale.reason)
	}
	if !strings.Contains(stale.reason, "aforge engine --stop") {
		t.Fatalf("the refusal named no way out: %q", stale.reason)
	}
	// THE SENTENCE THE OLD ONE USED TO GIVE IS GONE. Telling somebody to update
	// a machine they updated an hour ago is the bug, not the fix.
	if strings.Contains(stale.reason, "update") {
		t.Fatalf("the refusal still sends somebody off to update something: %q", stale.reason)
	}
	if conn, err := enginehost.Dial(workspace); err != nil {
		t.Fatalf("a host that could not be asked was taken down anyway: %v", err)
	} else {
		_ = conn.Close()
	}
}

// A machine with nothing holding that workspace is the ordinary case, and it is
// not a decision at all: the attach that follows starts a host.
func TestNothingHoldingTheWorkspaceIsNotARefusal(t *testing.T) {
	shortEngineHome(t)
	if err := clearStaleEngineHost("/home/somebody/api"); err != nil {
		t.Fatalf("an empty machine answered %v", err)
	}
}
