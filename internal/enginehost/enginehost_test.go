package enginehost

// The host is tested without a model, a provider or a session file: a stub
// agent stands in for the conversation, because every question here is about
// the HOST — which socket, which lock, which conversation a hello lands on, and
// what happens when none of it can work — and none of them is a question about
// a model.

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// stubAgent is a conversation that does nothing, which is all a host needs one
// to do: the host never speaks to an agent, it only hands it to
// internal/remote.
type stubAgent struct{}

func (stubAgent) Submit(context.Context, string) (<-chan session.Event, error) { return nil, nil }
func (stubAgent) SubmitStanding(context.Context, string) (<-chan session.Event, error) {
	return nil, nil
}
func (stubAgent) SubmitImage(context.Context, string, []session.Image) (<-chan session.Event, error) {
	return nil, nil
}
func (stubAgent) FollowUp(string) (<-chan session.Event, error)             { return nil, nil }
func (stubAgent) Steer(string) (<-chan session.Event, error)                { return nil, nil }
func (stubAgent) Interrupt()                                                {}
func (stubAgent) InterruptFor(session.StopDoor)                             {}
func (stubAgent) Compact(context.Context) error                             { return nil }
func (stubAgent) Close() error                                              { return nil }
func (stubAgent) Model() string                                             { return "openai/gpt-5" }
func (stubAgent) SetModel(string)                                           {}
func (stubAgent) SetContextWindow(int)                                      {}
func (stubAgent) ReasoningFor(string) string                                { return "" }
func (stubAgent) ReasoningLevels() map[string]string                        { return nil }
func (stubAgent) SetReasoningFor(string, string)                            {}
func (stubAgent) ResolveConsent(uint64, bool)                               {}
func (stubAgent) ResolveConsentRemember(uint64, bool, session.ConsentScope) {}
func (stubAgent) ResolveStanding(uint64, session.StandingAnswer)            {}
func (stubAgent) ResolveHarness(uint64, bool, string)                       {}
func (stubAgent) ResolveConnect(string, bool)                               {}
func (stubAgent) ResolveConnectKey(string, string)                          {}
func (stubAgent) NoteConnected(string, string)                              {}
func (stubAgent) Title() string                                             { return "" }
func (stubAgent) Usage() session.Usage                                      { return session.Usage{} }
func (stubAgent) ContextTokens() int                                        { return 0 }
func (stubAgent) Transcript() []session.DisplayEntry                        { return nil }
func (stubAgent) EarlierHistory() session.EarlierHistory                    { return session.EarlierHistory{} }
func (stubAgent) RewindPoints() []session.RewindPoint                       { return nil }
func (stubAgent) RewindAt(int) ([]session.DisplayEntry, error)              { return nil, nil }

var _ remote.WrappedAgent = stubAgent{}

// shortHome is a state root a socket can actually be named in, and it is what
// every test in this package that builds a real socket path uses for
// CODEAF_HOME. The one test that wants a path too long for a socket
// ([TestASocketPathTooLongIsRefusedAtTheDoor]) builds its own on purpose.
//
// It is NOT t.TempDir, and it does not honour $TMPDIR either, and the reason is
// the point of [SocketLimit] rather than an inconvenience: a unix socket path
// has a hard ceiling of [SocketLimit] bytes, Go names a temp directory after
// the test, this package's test names are sentences, and a Mac's own $TMPDIR
// (/var/folders/…/T/…) spends most of the budget before the test has said
// anything. WHETHER THIS SUITE PASSES MUST NOT BE A FUNCTION OF HOW DEEP
// $TMPDIR IS, so the root is named directly under /tmp, where it costs about
// twenty bytes wherever the test runs. A test that ran out of bytes would be
// failing the same honest refusal a person with a deep CODEAF_HOME gets —
// which is a law with a test of its own, not something the rest of the package
// should keep re-discovering by accident, and not a reason for the ledger in
// .github/known-red.txt to carry a socket test on macOS.
//
// If /tmp cannot be written — a locked-down machine, or Windows — the fallback
// is t.TempDir, which is no worse than what was here before, and the log says
// so, because a socket refusal that follows would otherwise look like a bug in
// the host rather than in the room the test was given.
func shortHome(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "eh-")
	if err != nil {
		root = t.TempDir()
		t.Logf("no short home under /tmp (%v); using %s, which may be too long for a socket", err, root)
	} else {
		t.Cleanup(func() { _ = os.RemoveAll(root) })
	}
	t.Setenv("CODEAF_HOME", root)
	return root
}

func stubHost(t *testing.T, workspace string) *Host {
	t.Helper()
	return &Host{
		workspace: workspace,
		opts: Options{
			Boot: func(hello remote.Hello) (*remote.Engine, error) {
				return &remote.Engine{
					Agent:       stubAgent{},
					Workspace:   workspace,
					SessionFile: filepath.Join(workspace, hello.Session),
				}, nil
			},
		},
		sessions: map[string]*remote.Session{},
		done:     make(chan struct{}),
	}
}

// ── where a host lives ──────────────────────────────────────────────────────

func TestTheSocketMovesWithTheStateRoot(t *testing.T) {
	root := shortHome(t)

	socket, err := SocketPath("/home/somebody/api")
	if err != nil {
		t.Fatalf("resolve the socket: %v", err)
	}
	if !strings.HasPrefix(socket, filepath.Join(root, "v3", "hosts")) {
		t.Fatalf("the socket landed at %s, outside the state root", socket)
	}
	if filepath.Base(socket) != socketName {
		t.Fatalf("the socket is called %s", filepath.Base(socket))
	}

	// Two workspaces are two hosts, because one process may only be in one
	// directory (the chdir law this package's header states).
	other, err := SocketPath("/home/somebody/notes")
	if err != nil {
		t.Fatalf("resolve the other socket: %v", err)
	}
	if other == socket {
		t.Fatal("two workspaces were given one socket")
	}

	// And the directory says in plain words which workspace it belongs to,
	// because its name is a hash.
	dir, err := Dir("/home/somebody/api")
	if err != nil {
		t.Fatalf("resolve the directory: %v", err)
	}
	if dir != filepath.Dir(socket) {
		t.Fatalf("the socket is not in its own directory: %s vs %s", dir, socket)
	}
}

// A state root so deep that a socket cannot be named there is answered with a
// refusal rather than an opaque syscall error, so the caller falls back to the
// pipe knowing why.
func TestASocketPathTooLongIsRefusedAtTheDoor(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("deep/", 40))
	t.Setenv("CODEAF_HOME", root)
	if _, err := SocketPath("/home/somebody/api"); err == nil {
		t.Fatal("a socket path far past the limit was accepted")
	}
}

func TestOnlyOneProcessHoldsTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), lockName)
	first, err := takeLock(path)
	if err != nil {
		t.Fatalf("take the lock: %v", err)
	}
	if _, err := takeLock(path); err == nil {
		t.Fatal("two holders of one host lock")
	}
	if err := releaseLock(first); err != nil {
		t.Fatalf("release the lock: %v", err)
	}
	second, err := takeLock(path)
	if err != nil {
		t.Fatalf("the lock did not come back: %v", err)
	}
	_ = releaseLock(second)
}

// ── which conversation a hello lands on ─────────────────────────────────────

func TestTwoSurfacesAskingForTheSameThingGetTheSameConversation(t *testing.T) {
	h := stubHost(t, "/home/somebody/api")

	first, err := h.open(remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	second, err := h.open(remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("open again: %v", err)
	}
	if first != second {
		t.Fatal("the second surface was handed a different conversation")
	}

	other, err := h.open(remote.Hello{Version: remote.Version, Session: "yesterday.jsonl"})
	if err != nil {
		t.Fatalf("open a named session: %v", err)
	}
	if other == first {
		t.Fatal("a named session opened the same conversation as the default one")
	}
}

// A JOIN TAKES A CONVERSATION THAT IS ALREADY HERE AND STARTS NOTHING.
//
// This is what makes it safe for a task page to look into work running next door.
// The keys and the transcripts deliberately DISAGREE here, because that is the
// ordinary case: the surface that opened the conversation said nothing at all
// about a session — the empty string, meaning "this workspace's latest" — so it is
// filed under "" while the file it is writing has a name. A join asking by key
// would miss it and be handed a whole new conversation, which is a model started
// so that somebody could read a transcript.
func TestAJoinFindsTheOpenConversationByItsFileAndNeverStartsOne(t *testing.T) {
	h := stubHost(t, "/home/somebody/api")
	booted := 0
	boot := h.opts.Boot
	h.opts.Boot = func(hello remote.Hello) (*remote.Engine, error) {
		booted++
		return boot(hello)
	}

	// The ordinary launch: no session named, so the key is "" and the file is
	// whatever the engine opened.
	open, err := h.open(remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if booted != 1 {
		t.Fatalf("the first hello booted %d conversations", booted)
	}

	joined, err := h.open(remote.Hello{Version: remote.Version, Session: open.File(), Join: true})
	if err != nil {
		t.Fatalf("join the conversation that is already open: %v", err)
	}
	if joined != open {
		t.Fatal("a join was handed a different conversation from the one running")
	}
	if booted != 1 {
		t.Fatalf("a join booted a conversation: %d boots", booted)
	}

	// AND A JOIN ONTO A CONVERSATION NOBODY IS RUNNING IS A SENTENCE, NOT A BOOT.
	// A stale row on somebody's screen must not be able to start a session.
	stale, err := h.open(remote.Hello{
		Version: remote.Version,
		Session: "/home/somebody/api/gone-an-hour-ago.jsonl",
		Join:    true,
	})
	if err == nil {
		t.Fatal("a join onto a conversation nobody is running opened something")
	}
	if stale != nil {
		t.Fatalf("a refused join handed back a conversation: %v", stale)
	}
	if booted != 1 {
		t.Fatalf("a stale join booted a conversation: %d boots", booted)
	}
	if !strings.Contains(err.Error(), "gone-an-hour-ago.jsonl") {
		t.Fatalf("the refusal does not say what was asked for: %v", err)
	}
}

// A conversation somebody deliberately ended is not handed out again: the next
// hello opens a new one, which is what /new and a fresh window both mean.
func TestAClosedConversationIsNotHandedOutAgain(t *testing.T) {
	h := stubHost(t, "/home/somebody/api")

	first, err := h.open(remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = first.Close()

	second, err := h.open(remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("open again: %v", err)
	}
	if second == first {
		t.Fatal("a conversation that was closed was handed to the next surface")
	}
}

// ── the whole road, socket and all ──────────────────────────────────────────

func TestAHostAnswersOnItsSocketAndHoldsTheConversation(t *testing.T) {
	shortHome(t)
	workspace := "/home/somebody/api"

	stopped := make(chan error, 1)
	go func() {
		stopped <- Run(workspace, Options{
			Boot: func(remote.Hello) (*remote.Engine, error) {
				return &remote.Engine{Agent: stubAgent{}, Workspace: workspace}, nil
			},
		})
	}()

	conn, err := waitForHost(workspace, 5*time.Second)
	if err != nil {
		t.Fatalf("no host answered: %v", err)
	}
	client, err := remote.Dial(conn, "test", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	welcome := client.Welcome()
	if !welcome.Persistent {
		t.Fatal("a host said its conversations were not persistent")
	}
	if welcome.Attached != 0 {
		t.Fatalf("the first surface was told %d others were attached", welcome.Attached)
	}

	// A second surface on the same socket joins the SAME conversation, which is
	// the whole of "sit down somewhere else and be in it".
	next, err := Dial(workspace)
	if err != nil {
		t.Fatalf("dial the host again: %v", err)
	}
	other, err := remote.Dial(next, "test", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("second handshake: %v", err)
	}
	if other.Welcome().Attached != 1 {
		t.Fatalf("the second surface was told %d others were attached, want 1", other.Welcome().Attached)
	}
	_ = other.Close()
	_ = client.Close()

	// A second host on the same workspace finds the lock taken and says so
	// without complaint.
	if err := Run(workspace, Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: stubAgent{}}, nil
	}}); err != ErrHostRunning {
		t.Fatalf("a second host on one workspace returned %v", err)
	}

	// And a lock that cannot be taken is the whole of the refusal: the socket
	// is still there and still answering.
	if _, err := Dial(workspace); err != nil {
		t.Fatalf("the host stopped answering: %v", err)
	}
}

// ── the fallback is not optional ────────────────────────────────────────────

func TestAttachGivesUpQuietlyWhenNoHostCanStart(t *testing.T) {
	// The home is short so that the refusal below is the one this test is
	// about: with a home too deep for a socket, Attach would refuse at the
	// door for the wrong reason and the test would pass without having asked
	// its question.
	shortHome(t)
	// A spawn that starts nothing is every real way this can fail — no binary,
	// a machine that refuses, a host that died on its first line — and the
	// answer has to be an error the caller can fall back from.
	conn, err := Attach("/home/somebody/api", func() error { return nil })
	if err == nil {
		_ = conn.Close()
		t.Fatal("Attach claimed a host that does not exist")
	}
	if !errors.Is(err, ErrNoHostAnswered) {
		t.Fatalf("Attach answered with %v, want the no-host answer", err)
	}
	if errors.Is(err, ErrSocketPathTooLong) {
		t.Fatalf("a short state root was refused as too long: %v", err)
	}
}

// A SOCKET PATH THAT CAN NEVER BE NAMED IS ANSWERED BEFORE ANYTHING IS
// STARTED. Paying the host's whole birth wait cannot change that answer, and
// starting a process there would only make a child that was born unable to
// listen.
func TestAStatePathTooLongForASocketIsAnsweredBeforeAnythingIsStarted(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("deep/", 20))
	t.Setenv("CODEAF_HOME", root)
	spawned := false
	started := time.Now()

	conn, err := Attach("/home/somebody/api", func() error {
		spawned = true
		return nil
	})
	took := time.Since(started)
	if conn != nil {
		_ = conn.Close()
		t.Fatal("a state root too deep for a socket returned a connection")
	}
	if !errors.Is(err, ErrSocketPathTooLong) {
		t.Fatalf("the refusal was %v, want the socket-path answer", err)
	}
	if took >= spawnWait/10 {
		t.Fatalf("a question settled at the door took %v of the %v host wait", took, spawnWait)
	}
	if spawned {
		t.Fatal("a host was started where its socket could never be named")
	}
	if _, statErr := os.Stat(filepath.Join(root, "v3", "hosts")); !os.IsNotExist(statErr) {
		t.Fatalf("the refusal left a hosts directory behind: %v", statErr)
	}
}

// A HOST THAT DIES AT BIRTH IS THE ONE PROCESS THAT KNOWS WHY. Its last words
// belong in the workspace's host log, while stdout remains empty because the
// process that asked for the host may be carrying the wire protocol there.
func TestASpawnedHostsLastWordsLandInItsLog(t *testing.T) {
	shortHome(t)
	workspace := "/home/somebody/api"
	dir, err := Dir(workspace)
	if err != nil {
		t.Fatalf("make somewhere for the host log: %v", err)
	}

	readStdout, writeStdout, err := os.Pipe()
	if err != nil {
		t.Fatalf("make a stdout witness: %v", err)
	}
	originalStdout := os.Stdout
	os.Stdout = writeStdout
	if err := Spawn(workspace, "sh", "-c", "echo the host could not start 1>&2"); err != nil {
		os.Stdout = originalStdout
		_ = writeStdout.Close()
		_ = readStdout.Close()
		t.Fatalf("spawn the short-lived host: %v", err)
	}
	os.Stdout = originalStdout
	if err := writeStdout.Close(); err != nil {
		_ = readStdout.Close()
		t.Fatalf("close the stdout witness: %v", err)
	}
	stdout, err := io.ReadAll(readStdout)
	_ = readStdout.Close()
	if err != nil {
		t.Fatalf("read the stdout witness: %v", err)
	}
	if len(stdout) != 0 {
		t.Fatalf("the spawned host wrote %q to this process's stdout", stdout)
	}

	logPath := filepath.Join(dir, logName)
	deadline := time.Now().Add(2 * time.Second)
	for {
		logged, readErr := os.ReadFile(logPath)
		if readErr == nil && strings.Contains(string(logged), "the host could not start") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the host's last words did not reach %s: %q (%v)", logPath, logged, readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ── the splice ──────────────────────────────────────────────────────────────

func TestTheSpliceCarriesBytesBothWaysAndEndsWithThePipe(t *testing.T) {
	surface, engine := net.Pipe()
	fromSurface, toSplice := io.Pipe()

	var wait sync.WaitGroup
	wait.Add(1)
	spliced := make(chan error, 1)
	go func() {
		defer wait.Done()
		spliced <- Splice(fromSurface, writerFunc(func(p []byte) (int, error) {
			return len(p), nil
		}), surface)
	}()

	// What the surface writes reaches the engine, unread and unchanged.
	go func() { _, _ = toSplice.Write([]byte("hello\n")) }()
	got := make([]byte, 6)
	if _, err := io.ReadFull(engine, got); err != nil {
		t.Fatalf("read the spliced bytes: %v", err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("the splice carried %q", got)
	}

	// The surface's side ending ends the splice, which is what tells the engine
	// its surface has gone.
	_ = toSplice.Close()
	select {
	case err := <-spliced:
		if err != nil {
			t.Fatalf("the splice ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the splice did not end when the pipe did")
	}
	wait.Wait()
	_ = engine.Close()
}

type writerFunc func([]byte) (int, error)

func (w writerFunc) Write(p []byte) (int, error) { return w(p) }

// ASKING WHETHER A HOST IS THERE MAKES NOTHING. Every plain `codeaf chat` puts
// this question before it opens anything, so a Dial that made a directory would
// leave one under every workspace anybody ever ran codeaf in.
func TestAskingWhetherAHostIsThereLeavesNothingBehind(t *testing.T) {
	root := shortHome(t)
	workspace := t.TempDir()

	if _, err := SocketPath(workspace); err != nil {
		t.Fatalf("name this workspace's socket: %v", err)
	}
	if conn, err := Dial(workspace); err == nil {
		_ = conn.Close()
		t.Fatal("something answered a socket nothing is listening on")
	}
	if _, err := os.Stat(filepath.Join(root, "v3", "hosts")); !os.IsNotExist(err) {
		t.Fatalf("the question left a hosts directory behind: %v", err)
	}

	// And the doors that are about to write something still make it themselves.
	if _, err := Dir(workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "v3", "hosts")); err != nil {
		t.Fatalf("Dir did not make the directory a host lives in: %v", err)
	}
}

// A PLAIN HELLO NEVER COLLIDES WITH THE HOST'S OWN CONVERSATION, and this is
// the shape that made it: the conversation a nameless hello landed on ends —
// moved to another window, left behind by /new, closed — while the host goes on
// holding a different one. The next plain launch used to find nothing under the
// empty key and BOOT, the boot resolved this workspace's latest, and that is a
// journal this very process holds the flock on; the host then refused its own
// conversation with "this conversation is open in another window".
//
// The door answers "which conversation does a hello that named nothing want"
// with the workspace's latest, spelled as a transcript path (cmd/codeaf's
// [engineHelloKey]), and `newest` below is that reading — moved by hand exactly
// as the disk would move it.
func TestAPlainHelloJoinsTheConversationThisHostStillHolds(t *testing.T) {
	workspace := "/home/somebody/api"
	first := filepath.Join(workspace, "first.jsonl")
	second := filepath.Join(workspace, "second.jsonl")

	newest, mints, boots := "", first, 0
	h := &Host{
		workspace: workspace,
		opts: Options{
			Boot: func(hello remote.Hello) (*remote.Engine, error) {
				boots++
				file := strings.TrimSpace(hello.Session)
				if file == "" {
					// A hello that named nothing gets whatever the workspace's
					// resume law would open, which is what the door resolves.
					file = mints
				}
				return &remote.Engine{Agent: stubAgent{}, Workspace: workspace, SessionFile: file}, nil
			},
			Key: func(hello remote.Hello) string {
				if named := strings.TrimSpace(hello.Session); named != "" {
					return named
				}
				return newest
			},
		},
		sessions: map[string]*remote.Session{},
		done:     make(chan struct{}),
	}

	// The first plain hello, in a workspace with nothing in it yet: the door has
	// no latest to name, so this one boots.
	one, err := h.open(remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	newest = first

	// A second conversation beside it, opened by name, and now the newest.
	two, err := h.open(remote.Hello{Version: remote.Version, Session: second})
	if err != nil {
		t.Fatalf("open a second conversation: %v", err)
	}
	if two == one {
		t.Fatal("a named session opened the same conversation as the plain one")
	}
	newest = second
	if boots != 2 {
		t.Fatalf("%d boots for two conversations", boots)
	}

	// And the first one ends, while this host goes on holding the second.
	if !one.RetireIfIdle(0) {
		t.Fatal("the first conversation would not end")
	}

	// THE WHOLE POINT: a plain hello now lands on the conversation this host is
	// still holding, and opens nothing.
	again, err := h.open(remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("open again: %v", err)
	}
	if again != two {
		t.Fatal("a plain hello was handed a conversation this host was not holding")
	}
	if boots != 2 {
		t.Fatalf("a plain hello booted a %d conversation onto a journal this host already holds", boots)
	}

	// And nothing is ever filed under what a hello did not say.
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, found := h.sessions[""]; found {
		t.Fatal("a conversation is filed under the empty string")
	}
}

// A HOSTED CONVERSATION READS THE ENGINE'S REAL PLAN STORE. This test keeps
// every boundary in the production path: PlanDB on disk, session.Agent reading
// it, the host's remote server, and remote.Agent on the surface side.
func TestHostedAgentReadsSeededPlanTasksEndToEnd(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	store, err := session.OpenRunPlan(workspace, "Hosted run", "prove the plan crosses the host")
	if err != nil {
		t.Fatalf("open run plan: %v", err)
	}
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "seeded", Title: "Seeded task", Description: "the real row"}}); err != nil {
		t.Fatalf("seed run plan: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seeded plan: %v", err)
	}

	engine, err := session.New(session.Config{
		Workspace: workspace,
		Model:     "test/model",
		APIKey:    "fixture",
		BaseURL:   "http://127.0.0.1:1/v1",
		System:    "Test only.",
	})
	if err != nil {
		t.Fatalf("build real session agent: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	host := &Host{
		workspace: workspace,
		opts: Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: engine, Workspace: workspace}, nil
		}},
		sessions: map[string]*remote.Session{},
		done:     make(chan struct{}),
	}
	surface, hosted := net.Pipe()
	go host.attach(hosted)
	client, err := remote.Dial(surface, "", remote.Hello{Version: remote.Version, Workspace: workspace})
	if err != nil {
		t.Fatalf("dial hosted session: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	rows := client.Agent().PlanTasks()
	for _, row := range rows {
		if row.ID == "t-seeded" && row.Title == "Seeded task" {
			return
		}
	}
	t.Fatalf("PlanTasks over host = %+v, want seeded real-store row", rows)
}

// A path of exactly 104 bytes does not fit: macOS's socket name holds 104
// bytes and its terminating NUL is one of them, so bind refuses it with
// "invalid argument". Answering "fits" there left codeaf with no host and no
// fallback, and it did not open.
func TestASocketPathFitsOnlyWithRoomForItsEnd(t *testing.T) {
	if !SocketPathFits(strings.Repeat("a", 103)) {
		t.Fatal("a 103-byte path was refused; it binds on every platform")
	}
	if SocketPathFits(strings.Repeat("a", 104)) {
		t.Fatal("a 104-byte path was said to fit; macOS refuses to bind it")
	}
}

// THE TASK PAGE'S MODEL IS THE LEDGER'S, and it has to cross the host: the
// room head reads it off the page the engine served, and a page that arrives
// without it draws the price and the tokens and never the model.
func TestHostedPlanPageCarriesTheLedgersModel(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	store, err := session.OpenRunPlan(workspace, "Hosted run", "prove the model crosses the host")
	if err != nil {
		t.Fatalf("open run plan: %v", err)
	}
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "seeded", Title: "Seeded task", Description: "the real row"}}); err != nil {
		t.Fatalf("seed run plan: %v", err)
	}
	if err := store.AddSpend("seeded", "z-ai/glm-5.3-flash", "worker", 0.0017, 20000, 1000); err != nil {
		t.Fatalf("write spend: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seeded plan: %v", err)
	}

	engine, err := session.New(session.Config{
		Workspace: workspace,
		Model:     "test/model",
		APIKey:    "fixture",
		BaseURL:   "http://127.0.0.1:1/v1",
		System:    "Test only.",
	})
	if err != nil {
		t.Fatalf("build real session agent: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	host := &Host{
		workspace: workspace,
		opts: Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: engine, Workspace: workspace}, nil
		}},
		sessions: map[string]*remote.Session{},
		done:     make(chan struct{}),
	}
	surface, hosted := net.Pipe()
	go host.attach(hosted)
	client, err := remote.Dial(surface, "", remote.Hello{Version: remote.Version, Workspace: workspace})
	if err != nil {
		t.Fatalf("dial hosted session: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	page, ok := client.Agent().PlanTaskPage("t-seeded")
	if !ok {
		t.Fatal("PlanTaskPage over the host answered no page")
	}
	if page.Row.Model != "z-ai/glm-5.3-flash" {
		t.Fatalf("page model = %q, want the ledger's z-ai/glm-5.3-flash (tokens %d usd %v)", page.Row.Model, page.Row.Tokens, page.Row.USD)
	}
	if page.Row.Tokens != 21000 {
		t.Fatalf("page tokens = %d, want 21000", page.Row.Tokens)
	}
	work, found := client.Agent().PlanTaskWork("t-seeded")
	if !found || work.NoDoor {
		t.Fatalf("PlanTaskWork over the host = (%+v, %v), want the door to answer", work, found)
	}
}
