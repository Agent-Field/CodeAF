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
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// standInOptions are the two things an ordinary stand-in cannot model, and
// each of them is a moment a real host can genuinely be in.
type standInOptions struct {
	// busyAfter makes the Nth question onward answer busy: a host that looked
	// idle when it was asked what it is and is holding something by the time
	// the stand-down arrives. It is the race the rule must lose gracefully.
	busyAfter int
	// vanishAfter makes the socket go after the Nth answer: a host that retired
	// on its own between the question and the stand-down.
	vanishAfter int
	// ignoresWhenIdle is a host built before the note existed: it answers the
	// question without acknowledging it, exactly as an installed older build
	// does, and cannot retire at a quiet moment nobody told it about.
	ignoresWhenIdle bool
}

// standInHost answers the version exchange with whatever it was told to say,
// and takes down its own socket when it agrees to retire — which is what a real
// host's shutdown looks like from the outside.
//
// IT KEEPS EVERY QUESTION IT WAS ASKED, because part of what is under test is
// WHICH question arrived: an older engine holding work is asked to let go when
// it is quiet and must never be asked to go anyway.
type standInHost struct {
	self  remote.HostSelf
	older bool
	opts  standInOptions

	mu      sync.Mutex
	asks    []remote.WhoIs
	retired bool

	listener net.Listener
	once     sync.Once
}

func standIn(t *testing.T, workspace string, self remote.HostSelf, older bool) *standInHost {
	t.Helper()
	return standInAs(t, workspace, self, older, standInOptions{})
}

func standInAs(t *testing.T, workspace string, self remote.HostSelf, older bool, opts standInOptions) *standInHost {
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
	host := &standInHost{self: self, older: older, opts: opts, listener: listener}
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

	h.mu.Lock()
	h.asks = append(h.asks, ask)
	count := len(h.asks)
	busy := h.self.Busy || (h.opts.busyAfter > 0 && count >= h.opts.busyAfter)
	retired := h.retired
	h.mu.Unlock()

	self := h.self
	self.Busy = busy
	self.StandDownWhenIdle = ask.StandDownWhenIdle && !h.opts.ignoresWhenIdle
	if retired {
		self.Retiring = true
	}
	if ask.StandDown && (ask.Anyway || !busy) {
		self.Retiring = true
		h.mu.Lock()
		h.retired = true
		h.mu.Unlock()
	}
	answer, _ := json.Marshal(remote.Frame{Kind: "whoami", Payload: mustStandInJSON(self)})
	_, _ = conn.Write(append(answer, '\n'))
	if self.Retiring || (h.opts.vanishAfter > 0 && count >= h.opts.vanishAfter) {
		h.close()
	}
}

// questions is every question this stand-in was asked, oldest first.
func (h *standInHost) questions() []remote.WhoIs {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]remote.WhoIs(nil), h.asks...)
}

// waitingSaid reports that a newer build asked this host for the slot without
// asking it to end anything.
func (h *standInHost) waitingSaid() bool {
	for _, ask := range h.questions() {
		if ask.StandDownWhenIdle {
			return true
		}
	}
	return false
}

// forced reports that something asked this host to go regardless of what it was
// holding, which is a person's own `codeaf engine --stop` and must never be
// this door.
func (h *standInHost) forced() bool {
	for _, ask := range h.questions() {
		if ask.StandDown && ask.Anyway {
			return true
		}
	}
	return false
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

// ── the takeover rule: work is kept, and the slot is handed over quietly ────

// window is a build of this binary at a moment, for the tests that need two
// builds of one source and a test binary that is linked once.
func window(build string, at time.Time) engineBuild {
	return engineBuild{Build: build, BuiltAt: at}
}

var (
	earlier = time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	later   = earlier.Add(48 * time.Hour)
)

// hostAnswers is whether anything is still listening on this workspace's socket.
func hostAnswers(workspace string) bool {
	conn, err := enginehost.Dial(workspace)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

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
	if !hostAnswers(workspace) {
		t.Fatal("a host of this build was asked to go")
	}
}

// THE CASE THIS RULE WAS WRITTEN FOR (2026-09-23), turned right way up. An
// engine two days older, from another binary, HOLDING WORK, used to be ended
// whenever a newer window connected — a turn somebody was watching closed
// because a rebuild happened. It is joined now, it keeps everything it has, and
// it is asked to let go at its first quiet moment.
func TestAnOlderEngineHoldingWorkKeepsItAndIsJoined(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	host := standIn(t, workspace, remote.HostSelf{
		Version: remote.Version, Build: "two-days-ago", Busy: true,
		PID: 4242, Binary: "/home/somebody/.codeaf/bin/devaf", Revision: "a1b2c3d4 built 2026-09-21 09:00",
		BuiltAt: earlier, Surfaces: 1, Conversations: 3, Workspace: workspace,
	}, false)

	note, err := clearStaleEngineHostAs(workspace, window("today", later))
	if err != nil {
		t.Fatalf("an older engine holding work was refused rather than joined: %v", err)
	}
	for _, want := range []string{"older codeaf", "still holding work", "picks up this build once it goes quiet"} {
		if !strings.Contains(note, want) {
			t.Fatalf("the busy line %q does not say %q", note, want)
		}
	}
	// NOBODY'S WORK ENDED AND THE PROCESS IS STILL THERE.
	if !hostAnswers(workspace) {
		t.Fatal("an older engine holding work was taken down")
	}
	// AND THE SLOT IS ASKED FOR, QUIETLY: the host is told a newer build is
	// waiting, which is what makes the swap happen at its own first quiet
	// moment rather than half an hour later.
	if !host.waitingSaid() {
		t.Fatal("the older engine was not asked to let go when it is quiet")
	}
	if host.forced() {
		t.Fatal("an older engine holding work was asked to go regardless of it")
	}
}

// THE OTHER HALF OF THE SAME LINE: the older engine was holding nothing, so it
// is asked to go — through the host's own admission check, never a signal — and
// the next connection starts a host from the binary on disk.
func TestAnOlderEngineHoldingNothingIsRetiredWithoutBeingKilled(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	host := standIn(t, workspace, remote.HostSelf{
		Version: remote.Version, Build: "two-days-ago", BuiltAt: earlier,
		PID: 4242, Binary: "/home/somebody/.codeaf/bin/devaf", Revision: "a1b2c3d4 built 2026-09-21 09:00",
	}, false)

	note, err := clearStaleEngineHostAs(workspace, window("today", later))
	if err != nil {
		t.Fatalf("an idle older engine was refused rather than retired: %v", err)
	}
	for _, want := range []string{"replaced the older engine", "pid 4242", "a1b2c3d4", "/home/somebody/.codeaf/bin/devaf", "this build holds the workspace now"} {
		if !strings.Contains(note, want) {
			t.Fatalf("the handover line %q does not say %q", note, want)
		}
	}
	if hostAnswers(workspace) {
		t.Fatal("the older engine was still answering after the handover")
	}
	// ATOMICITY IS IN THE QUESTION THAT WAS ASKED. The stand-down went with
	// Anyway FALSE, so the host measured what it was holding under its own lock
	// and agreed; a kill, or an Anyway, would have decided for it.
	asked := host.questions()
	if len(asked) < 2 || !asked[len(asked)-1].StandDown || asked[len(asked)-1].Anyway {
		t.Fatalf("the stand-down was not the host's own decision: %+v", asked)
	}
}

// THE RACE THE RULE HAS TO LOSE GRACEFULLY: the host looked idle when it was
// asked what it was, and it is holding something by the time the stand-down
// arrives. The second question is the one that counts, the host refuses, and
// this build joins it instead of killing it — which is exactly what a
// status-then-kill pair used to get wrong.
func TestAnOlderEngineThatRefusedTheStandDownKeepsItsWork(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	host := standInAs(t, workspace, remote.HostSelf{
		Version: remote.Version, Build: "two-days-ago", BuiltAt: earlier, Workspace: workspace,
	}, false, standInOptions{busyAfter: 2})

	note, err := clearStaleEngineHostAs(workspace, window("today", later))
	if err != nil {
		t.Fatalf("a host that kept its work was refused rather than joined: %v", err)
	}
	if !strings.Contains(note, "older codeaf") || !strings.Contains(note, "still holding work") {
		t.Fatalf("the busy line %q does not say what happened", note)
	}
	if !hostAnswers(workspace) {
		t.Fatal("a host that refused the stand-down was ended anyway")
	}
	if host.forced() {
		t.Fatal("a host that refused the stand-down was asked to go regardless of it")
	}
	if !host.waitingSaid() {
		t.Fatal("the host that refused was not asked to let go when it is quiet")
	}
}

// AND A HOST OLDER THAN THE NOTE IS NOT PROMISED IT. The ask is additive, so an
// already-installed older build ignores it and cannot retire at a quiet moment
// nobody told it about — which the acknowledgement distinguishes, so the line a
// person reads says what actually gets them onto this build.
func TestAnOldHostThatIgnoresTheNoteIsNotPromisedAQuietMoment(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	host := standInAs(t, workspace, remote.HostSelf{
		Version: remote.Version, Build: "two-days-ago", BuiltAt: earlier, Busy: true, Workspace: workspace,
	}, false, standInOptions{ignoresWhenIdle: true})

	note, err := clearStaleEngineHostAs(workspace, window("today", later))
	if err != nil {
		t.Fatalf("an old host was refused rather than joined: %v", err)
	}
	if strings.Contains(note, "once it goes quiet") {
		t.Fatalf("the line promises a quiet-moment handover this host was never told about: %q", note)
	}
	for _, want := range []string{"older codeaf", "still holding work", "your next launch once the work is done"} {
		if !strings.Contains(note, want) {
			t.Fatalf("the line %q does not say %q", note, want)
		}
	}
	if !hostAnswers(workspace) || host.forced() {
		t.Fatal("an old host that cannot step aside on its own was ended rather than kept")
	}
	if !host.waitingSaid() {
		t.Fatal("the note was not even delivered, so a newer host could not use it")
	}
}

// THE LAUNCH ITSELF REFUSES, and that is the whole of this test: an engine
// holding the workspace on a wire this build cannot speak must NOT fall through
// to a conversation in this process, where it would be a second writer beside
// the journal the engine is holding.
func TestALaunchRefusesAnIncompatibleBusyEngineRatherThanFallingBack(t *testing.T) {
	shortEngineHome(t)
	workspace := t.TempDir()
	standIn(t, workspace, remote.HostSelf{
		Version: remote.Version - 1, Busy: true, Build: "ancient", BuiltAt: earlier, Workspace: workspace,
	}, false)

	err := openChatV3Local(localLaunch{workspace: workspace})
	if err == nil {
		t.Fatal("a launch against a busy engine on another wire came back with nothing to say")
	}
	var unreachable *hostUnreachable
	if errors.As(err, &unreachable) {
		t.Fatalf("the launch fell back to an in-process conversation beside a live engine: %q", unreachable.reason)
	}
	var refusal *hostRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("the launch answered %v, want the host's own refusal", err)
	}
	if !strings.Contains(refusal.sentence, "different protocol") || !strings.Contains(refusal.sentence, "engine --stop") {
		t.Fatalf("the refusal %q does not say what is holding it and the way out", refusal.sentence)
	}
	// AND NOTHING WAS OPENED IN THIS PROCESS: no project tree, no journal.
	if entries, readErr := os.ReadDir(workspace); readErr != nil || len(entries) != 0 {
		t.Fatalf("the refused launch left something in the workspace: %v, %v", entries, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(os.Getenv("CODEAF_HOME"), "v3", "projects")); statErr == nil {
		t.Fatal("the refused launch opened a conversation in this process")
	}
}

// A BUILD WITH NO STAMP IS OLDER THAN EVERY STAMP, and older is not a reason to
// end somebody's turn: it is joined like any other build this wire can speak.
func TestAnOlderBuildWithNoStampHoldingWorkIsJoined(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	host := standIn(t, workspace, remote.HostSelf{Version: remote.Version, Build: "previous-build", Busy: true}, false)

	note, err := clearStaleEngineHost(workspace)
	if err != nil {
		t.Fatalf("a stampless older build holding work was refused: %v", err)
	}
	if !strings.Contains(note, "older codeaf") {
		t.Fatalf("the notice did not say which engine this is: %q", note)
	}
	if strings.Contains(note, "--stop") {
		t.Fatalf("the notice sends somebody off to stop something by hand: %q", note)
	}
	if !hostAnswers(workspace) || !host.waitingSaid() || host.forced() {
		t.Fatal("a stampless older build was ended rather than joined")
	}
}

// A SECOND WINDOW MEETS THE SAME OLDER ENGINE AND THE TWO OF THEM DO NOTHING TO
// IT: no kill, no replacement, one engine still holding the one conversation.
// This is the half of #1788 that is about windows rather than turns — two
// terminals opening codeaf must not race each other into ending anything.
func TestTwoWindowsMeetingAnOlderEngineHoldingWorkBothJoinIt(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	host := standIn(t, workspace, remote.HostSelf{
		Version: remote.Version, Build: "two-days-ago", BuiltAt: earlier, Busy: true, Workspace: workspace,
	}, false)

	for opened := 1; opened <= 2; opened++ {
		note, err := clearStaleEngineHostAs(workspace, window("today", later))
		if err != nil {
			t.Fatalf("window %d was refused: %v", opened, err)
		}
		if !strings.Contains(note, "older codeaf") {
			t.Fatalf("window %d was not told which engine it is on: %q", opened, note)
		}
	}
	if !hostAnswers(workspace) {
		t.Fatal("two windows ended the engine they joined")
	}
	if host.forced() {
		t.Fatal("two windows asked a busy engine to go regardless of its work")
	}
}

// AND A HOST THAT RETIRED ON ITS OWN BETWEEN THE QUESTION AND THE STAND-DOWN IS
// NOT A FAILURE: nothing is holding the workspace, so the launch attaches and
// starts a host from this build. It is the failed-replacement road.
func TestAnIdleOlderEngineThatWentAwayLetsTheLaunchStartItsOwn(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standInAs(t, workspace, remote.HostSelf{Version: remote.Version, Build: "two-days-ago", BuiltAt: earlier}, false, standInOptions{vanishAfter: 1})

	note, err := clearStaleEngineHostAs(workspace, window("today", later))
	if err != nil || note != "" {
		t.Fatalf("a host that had already gone answered %q, %v — want a quiet go-ahead", note, err)
	}
	if hostAnswers(workspace) {
		t.Fatal("something is still holding the workspace after it vanished")
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
	if !hostAnswers(workspace) {
		t.Fatal("an older window took the slot from a newer engine")
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
	if !hostAnswers(workspace) {
		t.Fatal("a newer engine was taken down")
	}
}

// AN OLDER ENGINE ON ANOTHER WIRE, HOLDING WORK, IS REFUSED AND NEVER KILLED.
// This build cannot speak to it, so there is nothing to join; and the work it
// is holding is the reason there is nothing to end either. A person is told
// which command says so.
func TestAnOlderEngineOnAnotherWireHoldingWorkIsRefusedNotKilled(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	host := standIn(t, workspace, remote.HostSelf{
		Version: remote.Version - 1, Busy: true, Build: "ancient", BuiltAt: earlier, Workspace: workspace,
	}, false)

	_, err := clearStaleEngineHostAs(workspace, window("today", later))
	var stale *staleHost
	if !errors.As(err, &stale) {
		t.Fatalf("an older engine on another wire holding work answered %v, want a refusal", err)
	}
	// AND THE COMMAND IS COPY-PASTEABLE: the folder is quoted, because a path
	// with a space in it typed out of a sentence has to be one argument.
	for _, want := range []string{"different protocol", "engine --stop", "--workspace " + shellQuote(workspace)} {
		if !strings.Contains(stale.reason, want) {
			t.Fatalf("the refusal %q does not say %q", stale.reason, want)
		}
	}
	if !hostAnswers(workspace) {
		t.Fatal("an older engine on another wire was taken down while holding work")
	}
	if host.forced() {
		t.Fatal("an older engine on another wire was asked to go regardless of its work")
	}
}

// AND AN OLDER ENGINE ON ANOTHER WIRE THAT IS HOLDING NOTHING IS ASKED TO GO,
// on the host's own terms: it cannot be joined, so the slot has to change hands
// for the launch to work at all.
func TestAnOlderEngineOnAnotherWireHoldingNothingIsRetired(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	host := standIn(t, workspace, remote.HostSelf{Version: remote.Version - 1, Build: "ancient", BuiltAt: earlier}, false)

	note, err := clearStaleEngineHostAs(workspace, window("today", later))
	if err != nil {
		t.Fatalf("an idle older engine on another wire was refused: %v", err)
	}
	if !strings.Contains(note, "replaced the older engine") {
		t.Fatalf("the handover said nothing: %q", note)
	}
	if hostAnswers(workspace) {
		t.Fatal("the older engine was still answering after the handover")
	}
	asked := host.questions()
	if len(asked) == 0 || !asked[len(asked)-1].StandDown || asked[len(asked)-1].Anyway {
		t.Fatalf("the stand-down was not the host's own decision: %+v", asked)
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
	if !hostAnswers(workspace) {
		t.Fatal("the matching host was asked to retire")
	}
}

// SAME SOURCE, ANOTHER FILE, OLDER, HOLDING NOTHING: retired — `~/.codeaf/bin/devaf`
// and `bin/codeaf` built from one commit two days apart are two builds.
func TestTheSameSourceFromAnOlderOtherFileIsRetired(t *testing.T) {
	shortEngineHome(t)
	workspace := "/home/somebody/api"
	standIn(t, workspace, remote.HostSelf{Version: remote.Version, Build: "c85e10a19", Binary: "/somewhere/else/devaf", BuiltAt: earlier}, false)

	note, err := clearStaleEngineHostAs(workspace, engineBuild{Build: "c85e10a19", BuiltAt: later, Binary: "/here/bin/codeaf"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "replaced the older engine") {
		t.Fatalf("an older copy from another file was not retired: %q", note)
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
	if !hostAnswers(workspace) {
		t.Fatal("the copy was asked to go")
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

// THE BUILD THAT TRAPPED SOMEBODY FOR REAL cannot be asked anything at all: it
// says nothing about what it holds or what it is doing. It used to be signalled
// through the pid on its socket, which is a turn somebody may have been watching
// ended by a rebuild. It is refused in words now — and the words are true,
// because the one thing that still ends it is a person typing the stop, which is
// the same test's second half.
func TestAnEngineTooOldToBeAskedIsRefusedAndTheExplicitStopStillEndsIt(t *testing.T) {
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

	_, err = clearStaleEngineHost(workspace)
	var stale *staleHost
	if !errors.As(err, &stale) {
		t.Fatalf("a too-old engine answered %v, want a refusal in words", err)
	}
	for _, want := range []string{"cannot say what it is", "engine --stop", "--workspace " + shellQuote(workspace)} {
		if !strings.Contains(stale.reason, want) {
			t.Fatalf("the refusal %q does not say %q", stale.reason, want)
		}
	}
	select {
	case <-exited:
		t.Fatal("a too-old engine was ended without being asked")
	case <-time.After(300 * time.Millisecond):
	}

	// AND THE EXPLICIT STOP IS UNCHANGED: the pid road is still there for the
	// one caller a person is. `codeaf engine --stop` is [enginehost.Stop].
	stopped, err := enginehost.Stop(workspace)
	if err != nil || !stopped {
		t.Fatalf("the explicit stop answered %v, %v — want the host ended", stopped, err)
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the explicit stop did not end the old engine")
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
