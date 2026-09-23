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
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
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
	// A real host makes the directory it listens in ([enginehost.Run] takes Dir
	// before SocketPath), because naming the socket makes nothing — asking
	// whether a host is there must not build one a house.
	if _, err := enginehost.Dir(workspace); err != nil {
		t.Fatalf("make somewhere for a host to live: %v", err)
	}
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
	t.Setenv("CODEAF_HOME", root)
}

// ── the takeover rule: the older engine gives up the slot ───────────────────

// window is a build of this binary at a moment, for the tests that need two
// builds of one source and a test binary that is linked once.
func window(build string, at time.Time) engineBuild {
	return engineBuild{Build: build, BuiltAt: at}
}

var (
	earlier = time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	later   = earlier.Add(48 * time.Hour)
)

func TestAHostOfThisBuildIsSplicedOntoWithoutAWord(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, remote.HostSelf{Version: remote.Version, Build: buildinfo.Identity()}, false)

	note, err := clearStaleEngineHost(workspace)
	if err != nil {
		t.Fatalf("a host of this build was not attached to: %v", err)
	}
	if note != "" {
		t.Fatalf("a host of this build owed a sentence: %q", note)
	}
	if conn, err := enginehost.Dial(workspace); err != nil {
		t.Fatalf("a host of this build was asked to go: %v", err)
	} else {
		_ = conn.Close()
	}
}

// THE CASE THIS RULE WAS WRITTEN FOR (2026-09-23): an engine two days older,
// from another binary, HOLDING WORK, was deferred to — the new daemon exited
// without a word and every window kept talking to the old one. Now it is
// replaced, and the window is told in one line which process that was.
func TestAnOlderEngineHoldingWorkIsReplacedAndNamed(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, remote.HostSelf{
		Version: remote.Version, Build: "two-days-ago", Busy: true,
		PID: 4242, Binary: "/home/somebody/.codeaf/bin/devaf", Revision: "a1b2c3d4 built 2026-09-21 09:00",
		BuiltAt: earlier, Surfaces: 1, Conversations: 3,
	}, false)

	note, err := clearStaleEngineHostAs(workspace, window("today", later))
	if err != nil {
		t.Fatalf("an older engine holding work was refused rather than replaced: %v", err)
	}
	for _, want := range []string{"replaced the older engine", "pid 4242", "a1b2c3d4", "/home/somebody/.codeaf/bin/devaf", "this build holds the workspace now"} {
		if !strings.Contains(note, want) {
			t.Fatalf("the takeover line %q does not say %q", note, want)
		}
	}
	if conn, err := enginehost.Dial(workspace); err == nil {
		_ = conn.Close()
		t.Fatal("the older engine was still answering after the takeover")
	}
}

// Another wire and older is the same answer: replaced, not refused.
func TestAnOlderEngineOnAnotherWireIsReplaced(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, remote.HostSelf{Version: remote.Version - 1, Busy: true}, false)

	note, err := clearStaleEngineHost(workspace)
	if err != nil {
		t.Fatalf("an older engine on another wire was refused: %v", err)
	}
	if !strings.Contains(note, "replaced the older engine") {
		t.Fatalf("the takeover said nothing: %q", note)
	}
	if conn, err := enginehost.Dial(workspace); err == nil {
		_ = conn.Close()
		t.Fatal("the older engine was still answering")
	}
}

// Idle or busy makes no difference to WHETHER an older engine is replaced, and
// a stamp it does not carry reads as older than every stamp.
func TestASameProtocolOlderBuildIsReplacedBusyOrNot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stamp string
		busy  bool
	}{
		{"idle", "previous-build", false},
		{"busy", "previous-build", true},
		{"no-stamp", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shortEngineHome(t)
			workspace := "/home/somebody/api"
			standIn(t, workspace, remote.HostSelf{Version: remote.Version, Build: tc.stamp, Busy: tc.busy}, false)
			note, err := clearStaleEngineHost(workspace)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(note, "replaced the older engine") {
				t.Fatalf("the notice did not say an older engine was replaced: %q", note)
			}
			if strings.Contains(note, "--stop") {
				t.Fatalf("the notice sends somebody off to stop something by hand: %q", note)
			}
			if conn, err := enginehost.Dial(workspace); err == nil {
				_ = conn.Close()
				t.Fatal("stale same-protocol host still answers")
			}
		})
	}
}

// THE RULE CONVERGES: a window of the OLDER build never takes the slot from a
// newer engine, or two windows on two builds would hand it back and forth for
// ever. Same wire: joined, silently.
func TestANewerEngineIsJoinedAndNeverReplacedByAnOlderWindow(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, remote.HostSelf{Version: remote.Version, Build: "today", BuiltAt: later, Busy: true}, false)

	note, err := clearStaleEngineHostAs(workspace, window("two-days-ago", earlier))
	if err != nil {
		t.Fatalf("a newer engine on this wire was refused: %v", err)
	}
	if note != "" {
		t.Fatalf("joining a newer engine owed no sentence, said %q", note)
	}
	if conn, err := enginehost.Dial(workspace); err != nil {
		t.Fatalf("an older window took the slot from a newer engine: %v", err)
	} else {
		_ = conn.Close()
	}
}

// A NEWER ENGINE ON A WIRE THIS BINARY CANNOT SPEAK is refused in words naming
// this binary as the older half, and left where it is.
func TestANewerEngineOnAnotherWireIsRefusedAndLeftAlone(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, remote.HostSelf{Version: remote.Version + 1, Build: "tomorrow", BuiltAt: later, Workspace: workspace}, false)

	_, err := clearStaleEngineHostAs(workspace, window("today", earlier))
	var stale *staleHost
	if !errors.As(err, &stale) {
		t.Fatalf("a newer engine on another wire answered %v, want a refusal", err)
	}
	if !strings.Contains(stale.reason, "newer codeaf") || !strings.Contains(stale.reason, "codeaf engine --status --workspace "+workspace) {
		t.Fatalf("the refusal does not say this binary is the older one and how to see which is newer: %q", stale.reason)
	}
	if conn, err := enginehost.Dial(workspace); err != nil {
		t.Fatalf("a newer engine was taken down: %v", err)
	} else {
		_ = conn.Close()
	}
}

// A HOST THIS BINARY'S OWN SOURCE BUILT IS THIS BINARY'S ENGINE, whatever minute
// the two were linked in (#730), when it does not name another file.
func TestAHostBuiltFromTheSameSourceAnotherMinuteIsSplicedOnto(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	source := "c85e10a19"
	engine := buildinfo.Info{Revision: source, BuiltAt: time.Date(2026, 9, 9, 21, 9, 1, 0, time.UTC)}
	standIn(t, workspace, remote.HostSelf{Version: remote.Version, Build: engine.Identity(), Busy: true, BuiltAt: engine.BuiltAt}, false)

	note, err := clearStaleEngineHostAs(workspace, window(engine.Identity(), engine.BuiltAt.Add(13*time.Second)))
	if err != nil {
		t.Fatalf("a host built from the same source was not attached to: %v", err)
	}
	if note != "" {
		t.Fatalf("a host built from the same source owed a sentence: %q", note)
	}
	if conn, err := enginehost.Dial(workspace); err != nil {
		t.Fatalf("the matching host was asked to retire: %v", err)
	} else {
		_ = conn.Close()
	}
}

// SAME SOURCE, ANOTHER FILE, OLDER: replaced — `~/.codeaf/bin/devaf` and
// `bin/codeaf` built from one commit two days apart are two builds.
func TestTheSameSourceFromAnOlderOtherFileIsReplaced(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, remote.HostSelf{Version: remote.Version, Build: "c85e10a19", Binary: "/somewhere/else/devaf", BuiltAt: earlier}, false)

	note, err := clearStaleEngineHostAs(workspace, engineBuild{Build: "c85e10a19", BuiltAt: later, Binary: "/here/bin/codeaf"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "replaced the older engine") {
		t.Fatalf("an older copy from another file was not replaced: %q", note)
	}
}

// AND A TIE IS NEVER A REPLACEMENT: two copies of one binary with one stamp
// (a shared install beside the checkout it was copied from) join each other.
func TestTwoCopiesOfOneBuildDoNotTakeTheSlotFromEachOther(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, remote.HostSelf{Version: remote.Version, Build: "c85e10a19", Binary: "/shared/bin/codeaf", BuiltAt: earlier}, false)

	note, err := clearStaleEngineHostAs(workspace, engineBuild{Build: "c85e10a19", BuiltAt: earlier, Binary: "/here/bin/codeaf"})
	if err != nil || note != "" {
		t.Fatalf("a copy of the same build was replaced (%q, %v)", note, err)
	}
	if conn, err := enginehost.Dial(workspace); err != nil {
		t.Fatalf("the copy was asked to go: %v", err)
	} else {
		_ = conn.Close()
	}
}

// A machine with nothing holding that workspace is the ordinary case, and it is
// not a decision at all: the attach that follows starts a host.
func TestNothingHoldingTheWorkspaceIsNotARefusal(t *testing.T) {
	shortEngineHome(t)
	if _, err := clearStaleEngineHost("/home/somebody/api"); err != nil {
		t.Fatalf("an empty machine answered %v", err)
	}
}

// ── a build too old to be asked, as a real process ──────────────────────────

// oldHostEnv names the socket the helper below listens on. The helper IS the
// build from before the version exchange: it refuses every first frame the way
// those builds did, and it answers SIGTERM by taking its socket down and
// exiting, which is what every host build has always done with that signal.
const oldHostEnv = "CODEAF_TEST_OLD_HOST_SOCKET"

func TestHelperOldEngineHost(t *testing.T) {
	socket := os.Getenv(oldHostEnv)
	if socket == "" {
		t.Skip("the stand-in old engine runs only as a child of the takeover test")
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		os.Exit(3)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM)
	go func() {
		<-stop
		_ = listener.Close()
		_ = os.Remove(socket)
		os.Exit(0)
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			os.Exit(0)
		}
		go func(conn net.Conn) {
			defer conn.Close()
			lines := bufio.NewScanner(conn)
			if !lines.Scan() {
				return
			}
			refusal, _ := json.Marshal(remote.Frame{Kind: "fatal", Error: `engine: the first frame was "whois", not a hello`})
			_, _ = conn.Write(append(refusal, '\n'))
		}(conn)
	}
}

// The build that trapped somebody for real cannot be asked anything at all, and
// it used to be left in place with a sentence telling the person to stop it by
// hand. It is older than everything, so it is replaced: the kernel names the
// process on the other end of its socket and it is sent the signal it has
// always answered by flushing and exiting.
func TestAnEngineTooOldToBeAskedIsReplacedThroughItsPid(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	if _, err := enginehost.Dir(workspace); err != nil {
		t.Fatal(err)
	}
	socket, err := enginehost.SocketPath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestHelperOldEngineHost$")
	child.Env = append(os.Environ(), oldHostEnv+"="+socket)
	if err := child.Start(); err != nil {
		t.Fatalf("start the old engine: %v", err)
	}
	pid := child.Process.Pid
	exited := make(chan struct{})
	go func() { _ = child.Wait(); close(exited) }()
	// THE TEST ENDS EVERY PROCESS IT STARTED, by the pid it recorded, whatever
	// the assertions below decided.
	t.Cleanup(func() {
		select {
		case <-exited:
		default:
			if process, err := os.FindProcess(pid); err == nil {
				_ = process.Kill()
			}
			<-exited
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if conn, err := enginehost.Dial(workspace); err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the old engine never listened")
		}
		time.Sleep(20 * time.Millisecond)
	}

	held, err := enginehost.Inspect(workspace)
	if err != nil || held.Answered || held.Self.PID != pid {
		t.Fatalf("status of a too-old engine: %+v, %v — want unanswered, pid %d", held, err, pid)
	}

	note, err := clearStaleEngineHost(workspace)
	if err != nil {
		t.Fatalf("a too-old engine was refused rather than replaced: %v", err)
	}
	if !strings.Contains(note, "replaced the older engine") || !strings.Contains(note, fmt.Sprintf("pid %d", pid)) {
		t.Fatalf("the takeover line did not name the process it replaced: %q", note)
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the old engine was not ended")
	}
}

// ── what a person types: --status and --stop ────────────────────────────────

func TestStatusNamesTheEngineHoldingTheWorkspace(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	started := time.Now().Add(-43 * time.Hour)
	standIn(t, workspace, remote.HostSelf{
		Version: remote.Version, Build: "two-days-ago", PID: 4242, Binary: "/home/somebody/.codeaf/bin/devaf",
		Revision: "a1b2c3d4 built 2026-09-21 09:00", Started: started, BuiltAt: earlier,
		Surfaces: 2, Conversations: 3, Busy: true, Workspace: workspace,
	}, false)
	held, err := enginehost.Inspect(workspace)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	writeEngineStatus(&out, workspace, held, window("today", later), time.Now())
	said := out.String()
	for _, want := range []string{
		workspace + " is held by an engine: an older build",
		"pid        4242",
		"binary     /home/somebody/.codeaf/bin/devaf",
		"build      a1b2c3d4 built 2026-09-21 09:00",
		"(43h00m ago)",
		"2 attached · 3 conversations open",
		"stop it    codeaf engine --stop --workspace " + workspace,
	} {
		if !strings.Contains(said, want) {
			t.Fatalf("status does not say %q:\n%s", want, said)
		}
	}
	// ASKING IS NOT STOPPING: the engine is still there after --status.
	if conn, err := enginehost.Dial(workspace); err != nil {
		t.Fatalf("--status took the engine down: %v", err)
	} else {
		_ = conn.Close()
	}
}

func TestStatusSaysNoneWhenNothingHoldsTheWorkspace(t *testing.T) {
	shortEngineHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out strings.Builder
	if err := runEngineStatus(&out, home); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "no engine is holding "+home+" on this machine" {
		t.Fatalf("status of an empty workspace said %q", got)
	}
}
