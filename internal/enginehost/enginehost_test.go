package enginehost

// The host is tested without a model, a provider or a session file: a stub
// agent stands in for the conversation, because every question here is about
// the HOST — which socket, which lock, which conversation a hello lands on, and
// what happens when none of it can work — and none of them is a question about
// a model.

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
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

// shortHome is a state root a socket can actually be named in.
//
// It is NOT t.TempDir, and the reason is the point of [socketLimit] rather than
// an inconvenience: Go names a temp directory after the test, this package's
// test names are sentences, and a socket path has about a hundred bytes to
// spend. A test that ran out of them would be failing the same honest refusal a
// person with a deep AFORGE_HOME gets.
func shortHome(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "eh")
	if err != nil {
		t.Fatalf("make a state root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("AFORGE_HOME", root)
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
	root := t.TempDir()
	t.Setenv("AFORGE_HOME", root)

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
	t.Setenv("AFORGE_HOME", root)
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
	t.Setenv("AFORGE_HOME", t.TempDir())
	// A spawn that starts nothing is every real way this can fail — no binary,
	// a machine that refuses, a host that died on its first line — and the
	// answer has to be an error the caller can fall back from.
	conn, err := Attach("/home/somebody/api", func() error { return nil })
	if err == nil {
		_ = conn.Close()
		t.Fatal("Attach claimed a host that does not exist")
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
