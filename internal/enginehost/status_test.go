package enginehost

// status_test.go pins what `codeaf engine --status` reads, the one guard on
// --stop that keeps it from ending the process asking, and the engine letting go
// of a conversation a window on this machine asked for.

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// A HOST NAMES ITSELF: the process, the file it runs from, when it started,
// which build it is and how many windows are on it. Every one of these is what
// a person had to dig out of `ps` before.
func TestAHostNamesItsProcessBinaryAndStartForStatus(t *testing.T) {
	shortHome(t)
	workspace := "/home/somebody/api"
	before := time.Now()
	liveHost(t, workspace)

	held, err := Inspect(workspace)
	if err != nil {
		t.Fatalf("inspect a live host: %v", err)
	}
	if !held.Answered {
		t.Fatal("a host of this build did not answer the question")
	}
	self := held.Self
	if self.PID != os.Getpid() {
		t.Fatalf("the host named pid %d, want %d", self.PID, os.Getpid())
	}
	if want, _ := os.Executable(); want != "" && self.Binary != want {
		t.Fatalf("the host named binary %q, want %q", self.Binary, want)
	}
	if self.Started.IsZero() || self.Started.Before(before.Add(-time.Second)) || self.Started.After(time.Now()) {
		t.Fatalf("the host's start %v is not the moment it came up (after %v)", self.Started, before)
	}
	if self.Revision == "" {
		t.Fatal("the host did not say which build it is in words a person reads")
	}
	if self.BuiltAt.IsZero() {
		t.Fatal("the host did not say when it was built, which is what orders two builds")
	}
	// THE QUESTION IS NOT A WINDOW. Nothing else is attached.
	if self.Surfaces != 0 || self.Conversations != 0 {
		t.Fatalf("an empty host counted %d windows and %d conversations", self.Surfaces, self.Conversations)
	}
}

func TestInspectFindsNothingWhereNothingIsHolding(t *testing.T) {
	shortHome(t)
	if _, err := Inspect("/home/somebody/api"); !errors.Is(err, ErrNothingHolding) {
		t.Fatalf("an empty workspace answered %v, want ErrNothingHolding", err)
	}
}

// A SOCKET WHOSE OTHER END IS THIS PROCESS IS NEVER SIGNALLED. Stop ends a host
// too old to be asked through the pid the kernel names — and when that pid is
// the asker's own, the stop would end the process that was cleaning up.
func TestStopNeverSignalsTheProcessAsking(t *testing.T) {
	shortHome(t)
	workspace := "/home/somebody/api"
	dir, err := Dir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", dir+"/"+socketName)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				lines := bufio.NewScanner(conn)
				if !lines.Scan() {
					return
				}
				refusal, _ := json.Marshal(remote.Frame{Kind: "fatal", Error: "engine: the first frame was \"whois\", not a hello"})
				_, _ = conn.Write(append(refusal, '\n'))
			}(conn)
		}
	}()
	went, err := Stop(workspace)
	if went || err == nil {
		t.Fatalf("stop of a socket this process holds answered (%v, %v), want a refusal", went, err)
	}
}

// askedAgent is a conversation another window has asked for: the flag is what
// internal/session's heartbeat sets when it finds takeover.json beside the
// journal.
type askedAgent struct {
	stubAgent
	asked  atomic.Bool
	closed atomic.Bool
}

func (a *askedAgent) TakeoverAsked() bool { return a.asked.Load() }
func (a *askedAgent) Close() error        { a.closed.Store(true); return nil }

// AN ENGINE HONOURS THE MOVE-IT-HERE REQUEST. A window with no engine behind it
// can only ask for a conversation an engine holds; the request used to sit there
// for its whole life because nobody was attached to hear the announcement, and
// the asking window was told the other window did not answer. The host looks
// itself now, and lets the one conversation go.
func TestAnEngineLetsGoOfAConversationAnotherWindowAskedFor(t *testing.T) {
	agent := &askedAgent{}
	workspace := hostHolding(t, agent)
	client := dialHost(t, workspace)
	// The window goes away and the conversation stays held, which is the state
	// the request used to die in.
	_ = client.Close()
	time.Sleep(3 * takeoverDoorstep)
	if agent.closed.Load() {
		t.Fatal("the engine closed a conversation nobody had asked for")
	}

	agent.asked.Store(true)
	waitUntil(t, "the engine lets go of the conversation that was asked for", agent.closed.Load)
	held, err := Inspect(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if held.Self.Conversations != 0 {
		t.Fatalf("the engine still counts %d conversations after letting go", held.Self.Conversations)
	}
}
