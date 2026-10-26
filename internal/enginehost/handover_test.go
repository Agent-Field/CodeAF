package enginehost

// handover_test.go is the moment one host gives way to the next, measured by
// what each side can see rather than by a clock: a host on its way out must
// not keep the workspace while its conversations finish saving, and a launch
// that meets a holder on its way out must start the next host as soon as the
// holder lets go rather than waiting out the birth wait for a socket nobody is
// going to open.

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// savingAgent is a conversation whose close is a save that takes as long as the
// test says: Close reports that it has begun and then waits to be let go.
type savingAgent struct {
	stubAgent
	once    sync.Once
	closing chan struct{}
	release chan struct{}
}

func newSavingAgent() *savingAgent {
	return &savingAgent{closing: make(chan struct{}), release: make(chan struct{})}
}

func (a *savingAgent) Close() error {
	a.once.Do(func() { close(a.closing) })
	<-a.release
	return nil
}

// A HOST ON ITS WAY OUT GIVES THE WORKSPACE UP BEFORE IT SAVES. Its
// conversations keep their own journal locks until their saves finish; the
// workspace's slot is free the moment the host stops serving, so the next
// launch can start the current build at once.
func TestARetiringHostLetsGoOfTheWorkspaceBeforeItsSaveFinishes(t *testing.T) {
	shortHome(t)
	workspace := "/home/somebody/api"
	agent := newSavingAgent()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = Run(workspace, Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: agent, Workspace: workspace, SessionFile: workspace + "/j.jsonl"}, nil
		}})
	}()
	released := false
	release := func() {
		if !released {
			released = true
			close(agent.release)
		}
	}
	t.Cleanup(func() {
		release()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("the first host did not exit")
		}
	})
	waitForHostQuietly(t, workspace)

	// A window opens the conversation and its link is cut, so the host holds a
	// conversation with nobody attached: the state a replaced binary retires in.
	conn, err := Dial(workspace)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := remote.Dial(conn, "", remote.Hello{Version: remote.Version}); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	_ = conn.Close()

	self, err := Ask(workspace, remote.WhoIs{StandDown: true, Anyway: true})
	if err != nil || !self.Retiring {
		t.Fatalf("the host did not agree to retire: %+v, %v", self, err)
	}
	select {
	case <-agent.closing:
	case <-time.After(10 * time.Second):
		t.Fatal("the host never began saving its conversation")
	}

	// The save is still running. The workspace must already be free.
	if hostLockHeld(workspace) {
		t.Fatal("the host kept the workspace lock while its conversation was still saving")
	}
	second := make(chan error, 1)
	go func() {
		second <- Run(workspace, Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: stubAgent{}, Workspace: workspace}, nil
		}})
	}()
	t.Cleanup(func() {
		_, _ = Stop(workspace)
		select {
		case <-second:
		case <-time.After(5 * time.Second):
			t.Error("the second host did not exit")
		}
	})
	waitForHostQuietly(t, workspace)

	// And the first host's late exit does not take the new host's socket with it.
	release()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the first host did not exit after its save")
	}
	if _, err := Ask(workspace, remote.WhoIs{}); err != nil {
		t.Fatalf("the new host stopped answering when the old one left: %v", err)
	}
}

// A LAUNCH THAT MEETS A HOLDER ON ITS WAY OUT STARTS THE NEXT HOST WHEN IT
// LEAVES. A host of an older build keeps the lock through its last save with
// nothing listening; the launch must not wait out the whole birth wait for a
// socket nobody is going to open, and must not fall back to its own process
// when the slot came free in the middle of that wait.
func TestALaunchStartsTheHostOnceAHolderOnItsWayOutLetsGo(t *testing.T) {
	shortHome(t)
	workspace := "/home/somebody/api"
	dir, err := Dir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	// The holder: the lock, and no socket.
	held, err := takeLock(dir + "/" + lockName)
	if err != nil {
		t.Fatalf("take the lock: %v", err)
	}
	waiting := make(chan struct{})
	was := attachWaiting
	attachWaiting = func() { close(waiting) }
	t.Cleanup(func() { attachWaiting = was })

	var mu sync.Mutex
	spawns := 0
	stopped := make(chan error, 1)
	spawn := func() error {
		mu.Lock()
		spawns++
		mu.Unlock()
		go func() {
			stopped <- Run(workspace, Options{Boot: func(remote.Hello) (*remote.Engine, error) {
				return &remote.Engine{Agent: stubAgent{}, Workspace: workspace}, nil
			}})
		}()
		return nil
	}
	type answer struct {
		conn net.Conn
		err  error
	}
	got := make(chan answer, 1)
	go func() {
		conn, err := Attach(workspace, spawn)
		got <- answer{conn, err}
	}()
	<-waiting
	mu.Lock()
	if spawns != 0 {
		mu.Unlock()
		t.Fatalf("a host was started while the lock was held: %d", spawns)
	}
	mu.Unlock()
	_ = releaseLock(held)

	a := <-got
	if a.err != nil {
		t.Fatalf("the launch found no host after the holder left: %v", a.err)
	}
	_ = a.conn.Close()
	t.Cleanup(func() {
		_, _ = Stop(workspace)
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("the host did not exit")
		}
	})
	mu.Lock()
	defer mu.Unlock()
	if spawns != 1 {
		t.Fatalf("hosts started = %d, want exactly one", spawns)
	}
}
