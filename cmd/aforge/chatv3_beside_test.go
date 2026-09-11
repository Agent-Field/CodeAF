package main

// ── THREE CONVERSATIONS, AT ONCE, THROUGH THE ORDINARY ENGINE DOOR ──────────
//
// These drive a REAL session host on a REAL unix socket, speaking the REAL
// protocol, because the claim under test is a claim about the transport: that
// `aforge` and `aforge chat` — the doors that reach this machine's own engine —
// can hold several conversations whose turns overlap, rather than swapping one
// connection between them and ending whatever it was holding.
//
// The only thing faked is the model. Every agent below is a scripted
// conversation whose turn ends when the test says so, which is what makes
// "these two turns were in flight at the same moment" a deterministic
// assertion rather than a race against a language model.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/enginehost"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// scriptedAgent is one conversation on the far machine whose turn runs until
// this test finishes it. It is the deterministic stand-in for a model.
type scriptedAgent struct {
	quietAgent
	file string

	mu         sync.Mutex
	turn       chan session.Event
	said       []string
	finished   int
	interrupts int
	closes     int
}

func (s *scriptedAgent) Submit(_ context.Context, text string) (<-chan session.Event, error) {
	lane := make(chan session.Event, 8)
	s.mu.Lock()
	s.turn = lane
	s.said = append(s.said, text)
	s.mu.Unlock()
	// One delta, so a reader can prove which conversation it is attached to
	// before anything has finished.
	lane <- session.Event{Kind: session.EventTextDelta, Text: "working on " + text}
	return lane, nil
}

// running says a turn this agent started has not been finished yet.
func (s *scriptedAgent) running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turn != nil
}

// finish ends the turn in flight, which is the far machine getting on with it.
func (s *scriptedAgent) finish() {
	s.mu.Lock()
	lane := s.turn
	s.turn = nil
	s.finished++
	s.mu.Unlock()
	if lane == nil {
		return
	}
	lane <- session.Event{Kind: session.EventTurnDone, Text: "done with " + s.file}
	close(lane)
}

func (s *scriptedAgent) Interrupt() { s.InterruptFor(session.StopByPerson) }

func (s *scriptedAgent) InterruptFor(session.StopDoor) {
	s.mu.Lock()
	s.interrupts++
	s.mu.Unlock()
}

func (s *scriptedAgent) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	return nil
}

func (s *scriptedAgent) counts() (interrupts, closes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interrupts, s.closes
}

var _ remote.WrappedAgent = (*scriptedAgent)(nil)

// farMachine is a workspace's conversations as the engine holds them: one
// scripted agent per transcript, minted on demand, remembered so a test can
// look at the conversation rather than at the handle onto it.
type farMachine struct {
	workspace string

	mu     sync.Mutex
	agents map[string]*scriptedAgent
	next   int
}

func (f *farMachine) agentFor(file string) *scriptedAgent {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.agents == nil {
		f.agents = map[string]*scriptedAgent{}
	}
	if held := f.agents[file]; held != nil {
		return held
	}
	agent := &scriptedAgent{file: file}
	f.agents[file] = agent
	return agent
}

// mint is the engine's own /new: the next transcript in this workspace.
func (f *farMachine) mint() string {
	f.mu.Lock()
	f.next++
	at := f.next
	f.mu.Unlock()
	return filepath.Join(f.workspace, fmt.Sprintf("chat-%d.jsonl", at))
}

func (f *farMachine) held(file string) *scriptedAgent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.agents[file]
}

// boot is [enginehost.Options.Boot] for this far machine. It answers the
// transcript the hello asked for and mints one for a hello that named none —
// which is what a hello minting a conversation of its own says too
// ([remote.Hello.New] carries the intent; the file it lands on is the engine's
// to choose, exactly as it is on the local door).
func (f *farMachine) boot(hello remote.Hello) (*remote.Engine, error) {
	file := hello.Session
	if file == "" {
		file = f.mint()
	}
	agent := f.agentFor(file)
	return &remote.Engine{
		Agent:       agent,
		Launch:      hello.Launch,
		Workspace:   f.workspace,
		SessionFile: file,
		Fresh: func() (remote.WrappedAgent, string, error) {
			next := f.mint()
			return f.agentFor(next), next, nil
		},
		Open: func(name string) (remote.WrappedAgent, bool, error) {
			return f.agentFor(name), true, nil
		},
	}, nil
}

// farHost starts a real host on a real socket around one far machine, and does
// not come back until it is listening.
func farHost(t *testing.T, far *farMachine) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "af-beside-")
	if err != nil {
		root = t.TempDir()
		t.Logf("no short home under /tmp (%v); %s may be too long for a socket", err, root)
	} else {
		t.Cleanup(func() { _ = os.RemoveAll(root) })
	}
	t.Setenv("AFORGE_HOME", root)
	t.Setenv("HOME", root)

	stopped := make(chan error, 1)
	go func() {
		stopped <- enginehost.Run(far.workspace, enginehost.Options{
			Boot: far.boot,
			Key:  func(hello remote.Hello) string { return hello.Session },
		})
	}()
	t.Cleanup(func() {
		_, _ = enginehost.Stop(far.workspace)
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Log("the host did not stop within five seconds")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := enginehost.Dial(far.workspace); err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no host answered on the socket")
}

// onePipeFleet is a door that CANNOT open a second connection — a loopback pipe,
// a test double — which is the honest state [tui3.Options.SharedAgent] describes.
func onePipeFleet(dest string, client *remote.Client) *engineFleet {
	return newEngineFleet(dest, client, nil, nil)
}

// besideFleet is the ordinary local door's own fleet: this machine's session
// host, dialled once for the launch and again for every conversation opened
// beside it.
func besideFleet(t *testing.T, far *farMachine) *engineFleet {
	t.Helper()
	link := &localLink{workspace: far.workspace}
	client, err := remote.Roam("", remote.Hello{Workspace: far.workspace},
		remote.Roaming{Dial: link.dial})
	if err != nil {
		t.Fatalf("dial this machine's own engine: %v", err)
	}
	return newEngineFleet("", client, nil, func(ask engineAsk) (*engineConn, error) {
		beside := &localLink{workspace: ask.workspace}
		next, err := remote.Roam("", remote.Hello{
			Workspace: ask.workspace, Session: ask.session, New: ask.mint,
		}, remote.Roaming{Dial: beside.dial})
		if err != nil {
			return nil, err
		}
		return &engineConn{client: next}, nil
	})
}

// besideDoor is the surface's options as the ordinary local door assembles
// them, against the host above.
func besideDoor(t *testing.T, far *farMachine) (tui3.Options, *engineFleet, func()) {
	t.Helper()
	fleet := besideFleet(t, far)
	options := hostOptions(fleet, fleet.client().Welcome(), false)
	return options, fleet, fleet.closeAll
}

func waitUntilBeside(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting until %s", what)
}

// THE DEFECT, AS THE PERSON MET IT. Three Untitled tabs, a question typed into
// the third, and `closed · santoshkumar — a connection holds one conversation
// at a time` where the answer should have been.
//
// The door is asked for three conversations and every one of them has to be a
// conversation of its own: a different transcript, a different agent on the far
// machine, and a turn that goes on running while the next one is opened.
func TestTheOrdinaryEngineDoorHoldsThreeConversationsWhoseTurnsOverlap(t *testing.T) {
	far := &farMachine{workspace: "/home/somebody/api"}
	farHost(t, far)
	options, _, done := besideDoor(t, far)
	defer done()

	if options.Start == nil {
		t.Fatal("the engine door offers no way to START a conversation beside the one it opened, so /new can only swap the one connection")
	}
	if options.Open == nil {
		t.Fatal("the engine door offers no way to OPEN an earlier conversation beside this one")
	}
	if options.SharedAgent {
		t.Fatal("the engine door still says its conversations share one handle, which is the sentence the person read on screen")
	}

	// The conversation the launch opened, with a turn in it.
	first := far.held(options.SessionFile)
	if first == nil {
		t.Fatalf("the engine opened no conversation for %q", options.SessionFile)
	}
	if _, err := options.Agent.Submit(context.Background(), "one"); err != nil {
		t.Fatalf("submit into the first conversation: %v", err)
	}

	// TWO MORE, OPENED WHILE THAT TURN IS STILL RUNNING. Each is asked for the
	// way the surface asks — the workspace it is already in — and each has to
	// come back as a whole conversation with an agent of its own.
	var convs []tui3.Conversation
	for i := 0; i < 2; i++ {
		conv, err := options.Start("")
		if err != nil {
			t.Fatalf("start a conversation beside: %v", err)
		}
		if conv.Agent == nil {
			t.Fatal("the door answered a conversation with no agent in it")
		}
		if conv.Agent == options.Agent {
			t.Fatal("the door handed back the handle it was already holding, so the two tabs are one conversation")
		}
		if conv.SessionFile == options.SessionFile {
			t.Fatalf("the conversation opened beside writes the same transcript as the one it was opened beside: %s", conv.SessionFile)
		}
		convs = append(convs, conv)
		if _, err := conv.Agent.Submit(context.Background(), fmt.Sprintf("beside %d", i)); err != nil {
			t.Fatalf("submit into the conversation beside: %v", err)
		}
	}
	if convs[0].SessionFile == convs[1].SessionFile {
		t.Fatal("two conversations opened beside are writing one transcript")
	}

	// ── THE WHOLE CLAIM: ALL THREE TURNS ARE IN FLIGHT AT ONE MOMENT ─────────
	seconds := []*scriptedAgent{first}
	for _, conv := range convs {
		agent := far.held(conv.SessionFile)
		if agent == nil {
			t.Fatalf("the engine opened no conversation for %q", conv.SessionFile)
		}
		seconds = append(seconds, agent)
	}
	for i, agent := range seconds {
		waitUntilBeside(t, fmt.Sprintf("conversation %d had a turn in flight", i), agent.running)
	}
	for i, agent := range seconds {
		if !agent.running() {
			t.Fatalf("conversation %d stopped running while the others were opened", i)
		}
		if _, closes := agent.counts(); closes != 0 {
			t.Fatalf("conversation %d was closed by opening another one beside it", i)
		}
	}

	// AND CLOSING ONE AFFECTS ONLY THAT ONE. The surface's own close road is an
	// interrupt and a close on the agent it is letting go of.
	convs[0].Agent.Interrupt()
	if err := convs[0].Agent.Close(); err != nil {
		t.Fatalf("close one conversation: %v", err)
	}
	// AT LEAST ONCE, never exactly once: [remote.MethodClose] flushes the journal
	// with somebody still listening for the failure, and the hang-up behind it
	// closes the session again on its way out (internal/remote's server.leave).
	// Both are the engine's own arrangement and a real session's Close is
	// idempotent; what this asserts is that the conversation ENDED.
	waitUntilBeside(t, "the closed conversation ended", func() bool {
		_, closes := seconds[1].counts()
		return closes >= 1
	})
	for _, at := range []int{0, 2} {
		if _, closes := seconds[at].counts(); closes != 0 {
			t.Fatalf("closing one conversation ended another: conversation %d was closed", at)
		}
		if !seconds[at].running() {
			t.Fatalf("closing one conversation stopped another: conversation %d is no longer running", at)
		}
	}

	// AND THE MACHINE-WIDE READINGS SURVIVE IT. The world, the recent list and
	// the ledger are facts about the engine's machine rather than about the
	// conversation that happened to be closed.
	if options.RecentSessions == nil {
		t.Fatal("the door lost its list of the machine's conversations")
	}
	options.RecentSessions()
}

// A chat minted by Ctrl+T must remain compatible with the ordinary launch on
// reconnect. Otherwise the surface falls back and waits for its own held lock.
func TestSiblingChatReconnectKeepsTheOrdinaryLaunchSettings(t *testing.T) {
	far := &farMachine{workspace: "/tmp/af-launch-reconnect"}
	farHost(t, far)
	shape := v3LaunchShape(false, false, false, 0, 0, true)
	launch := localLaunch{workspace: far.workspace, shape: shape}
	link := &localLink{workspace: far.workspace}
	client, err := remote.Roam("", localBesideHello(engineAsk{workspace: far.workspace, mint: true}, launch), remote.Roaming{Dial: link.dial})
	if err != nil {
		t.Fatal(err)
	}
	file := client.Welcome().SessionFile
	if !client.Welcome().Launch.Same(shape) {
		client.Close()
		t.Fatal("new tab lost the launch settings needed to reopen it")
	}
	client.Close()
	reopened, err := remote.Roam("", remote.Hello{Workspace: far.workspace, Session: file, Launch: shape}, remote.Roaming{Dial: link.dial})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !reopened.Welcome().Launch.Same(shape) || reopened.Welcome().SessionFile != file {
		t.Fatal("reconnect requires a fallback or duplicates the conversation")
	}
}
