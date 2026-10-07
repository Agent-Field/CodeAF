package enginehost

// standdown_test.go is the half of the takeover rule that lives on the host:
// a newer build may ask for this slot WITHOUT ending anybody's work, and the
// host decides when the moment has come.
//
// Three things are under test and they are the same three facts stated three
// ways: the note is a note (nothing stops while something is happening), the
// decision is the host's own (work arriving cancels it, under one lock), and the
// slot really does change hands once the work is gone — which is the promise
// every surface line about "picks up this build once it goes quiet" makes.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// quietSweeps makes the host's policy look often enough for a test to watch what
// it decides, and puts it back whatever the test did.
func quietSweeps(t *testing.T, every time.Duration) {
	t.Helper()
	was := sweepEvery.Load()
	sweepEvery.Store(int64(every))
	t.Cleanup(func() { sweepEvery.Store(was) })
}

// THE NOTE IS A NOTE. A host that is asked to let go when idle does not stop
// while it is holding anything — an attached surface is work, exactly as a turn
// or an unanswered question is.
func TestAStandDownWhenIdleWaitsForTheWorkInFlight(t *testing.T) {
	h := stubHost(t, "/home/somebody/api")
	h.dir = t.TempDir()
	if _, err := h.open(remote.Hello{Version: remote.Version}); err != nil {
		t.Fatalf("open: %v", err)
	}
	// Somebody is attached, and this very question is the other connection the
	// host is holding: BEING ASKED IS NOT WORK, so the two are counted side by
	// side and only the window is left over.
	h.live = 2
	if self := h.whois(remote.WhoIs{StandDownWhenIdle: true}); self.Retiring {
		t.Fatal("a host with a window on it agreed to go when it was only asked to wait")
	}
	if h.sweepOnce() {
		t.Fatal("the policy retired a host that was still holding a window")
	}
	if h.retiring {
		t.Fatal("the host closed its door while it was still holding work")
	}

	// AND THE WINDOW GOES. Now the same note is the whole of what stands between
	// this host and the newer build on disk.
	h.live = 1
	if !h.sweepOnce() {
		t.Fatal("the host did not take its quiet moment")
	}
	if !h.retiring {
		t.Fatal("the host decided to leave without closing the door to new conversations")
	}
}

// THE DOOR CLOSES UNDER THE SAME LOCK THAT DECIDED, which is the atomicity the
// whole rule rests on: a conversation arriving in the moment between the
// decision and the stop is refused rather than handed to a process that is
// leaving.
func TestAPastAndRefusedWindowIsRefusedRatherThanHandedAConversation(t *testing.T) {
	h := stubHost(t, "/home/somebody/api")
	h.dir = t.TempDir()

	if self := h.whois(remote.WhoIs{StandDownWhenIdle: true}); self.Retiring {
		t.Fatal("the note was read as a command")
	}
	if !h.sweepOnce() {
		t.Fatal("an idle host that was asked to wait did not take its quiet moment")
	}
	if _, err := h.open(remote.Hello{Version: remote.Version}); err == nil {
		t.Fatal("a conversation was opened into a host that is leaving")
	}
}

// THE NOTE IS DELIVERED, AND IT IS NOT A COMMAND: a real host answers it, keeps
// serving, and is not retiring a moment later.
func TestAStandDownWhenIdleIsDeliveredAndEndsNothingOnTheSpot(t *testing.T) {
	shortHome(t)
	workspace := "/home/somebody/api"
	liveHost(t, workspace)

	if _, err := StandDownWhenIdle(workspace); err != nil {
		t.Fatalf("ask the host to let go when it is quiet: %v", err)
	}
	self, err := Ask(workspace, remote.WhoIs{})
	if err != nil {
		t.Fatalf("the host stopped answering: %v", err)
	}
	if self.Retiring {
		t.Fatal("being asked to wait was answered as agreeing to go")
	}
	if self.Busy {
		t.Fatal("the host called itself busy with nothing but the question on it")
	}
}

// THE WHOLE PROMISE, ON A REAL HOST: a window is on it, a newer build asks for
// the slot, the window leaves, and the host takes its own quiet moment. Nothing
// was killed; the host ended itself, which is the one ending that flushes every
// journal on the way out.
func TestAnOlderHostLetsGoAtItsFirstQuietMomentAfterBeingAsked(t *testing.T) {
	shortHome(t)
	quietSweeps(t, 20*time.Millisecond)
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

	if _, err := StandDownWhenIdle(workspace); err != nil {
		t.Fatalf("ask the host to let go when it is quiet: %v", err)
	}
	// It is holding a window, so several of its own sweeps go by and it stays.
	time.Sleep(200 * time.Millisecond)
	if _, err := Ask(workspace, remote.WhoIs{}); err != nil {
		t.Fatalf("the host left with a window still on it: %v", err)
	}

	// The window goes, and the slot is handed over without anything being
	// signalled or forced.
	_ = surface.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := Ask(workspace, remote.WhoIs{}); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the host kept the slot after the window left")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// AND NOTHING A WINDOW ASKS CAN END THE WORK BESIDE IT. Many asks arriving while
// a surface is attached — the note and the stand-down both — leave the host
// holding exactly what it held, refusing every one of them. This is the race a
// status-then-kill pair used to lose, run many times over.
func TestManyAsksAtOnceNeverEndTheWorkInFlight(t *testing.T) {
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

	var waiters sync.WaitGroup
	for i := 0; i < 16; i++ {
		waiters.Add(1)
		go func() {
			defer waiters.Done()
			if _, err := StandDownWhenIdle(workspace); err != nil {
				t.Errorf("a note was refused: %v", err)
			}
			if err := Retire(workspace, false); !errors.Is(err, ErrHostBusy) {
				t.Errorf("a stand-down against a busy host answered %v, want ErrHostBusy", err)
			}
		}()
	}
	waiters.Wait()

	self, err := Ask(workspace, remote.WhoIs{})
	if err != nil {
		t.Fatalf("the host stopped answering: %v", err)
	}
	if !self.Busy || self.Retiring {
		t.Fatalf("after many asks the host said busy=%v retiring=%v, want busy and staying", self.Busy, self.Retiring)
	}
}

// ── the two kinds of work a host holds without a window ─────────────────────

// questionAgent stops on a question: its one turn asks for a bash command and
// ends, leaving the card waiting for somebody to answer it — and says so through
// the same door the real session does ([session.Question]), which is what the
// room reconciles against.
type questionAgent struct {
	stubAgent
	mu   sync.Mutex
	open bool
}

func (a *questionAgent) Submit(context.Context, string) (<-chan session.Event, error) {
	a.mu.Lock()
	a.open = true
	a.mu.Unlock()
	events := make(chan session.Event, 1)
	events <- session.Event{Kind: session.EventConsentRequest, ID: 7, Tool: "bash", CallID: "call-1"}
	close(events)
	return events, nil
}

func (a *questionAgent) OpenQuestions() []session.Question {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.open {
		return nil
	}
	return []session.Question{{ID: 7, Kind: session.QuestionConsent, Ask: session.AskPermission, Head: "run this command?"}}
}

// settled is somebody answering the card, or its turn being over: either way the
// engine has no question open.
func (a *questionAgent) settled() {
	a.mu.Lock()
	a.open = false
	a.mu.Unlock()
}

// handoffAgent is a conversation that handed work off: the turn that started it
// is long over and the work is still going, which is what [WorkingNow] says.
type handoffAgent struct {
	stubAgent
	mu      sync.Mutex
	running bool
}

func (a *handoffAgent) WorkingNow() []session.WorkNode {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running {
		return nil
	}
	return []session.WorkNode{{ID: "job-1", Title: "the handed-off job"}}
}

func (a *handoffAgent) finished() {
	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
}

// liveAgentHost is [liveHost] with a conversation of the test's own, so a test
// can put the host in a state that needs an agent rather than a socket.
func liveAgentHost(t *testing.T, workspace string, agent remote.WrappedAgent) {
	t.Helper()
	stopped := make(chan error, 1)
	go func() {
		stopped <- Run(workspace, Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: agent, Workspace: workspace}, nil
		}})
	}()
	waitForHostQuietly(t, workspace)
	t.Cleanup(func() {
		_ = Retire(workspace, true)
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("the host did not exit")
		}
	})
}

// goneWithin waits for nothing to be holding the workspace any more.
func goneWithin(t *testing.T, workspace string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if _, err := Ask(workspace, remote.WhoIs{}); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the host kept the workspace")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// AN UNANSWERED QUESTION HOLDS THE ENGINE, and answering it is what lets the
// newer build in. The card is the case that a status-then-kill rule got worst:
// the turn is over, so a host that only counted turns read as idle, and the
// person's question was closed with the process that was waiting on it.
func TestAnUnansweredQuestionHoldsTheEngineAndItsAnswerLetsItGo(t *testing.T) {
	shortHome(t)
	quietSweeps(t, 20*time.Millisecond)
	workspace := "/home/somebody/api"
	agent := &questionAgent{}
	liveAgentHost(t, workspace, agent)

	conn, err := Dial(workspace)
	if err != nil {
		t.Fatalf("dial the host: %v", err)
	}
	client, err := remote.Dial(conn, "test", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	events, err := client.Agent().Submit(t.Context(), "ask me something")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	deadline := time.After(10 * time.Second)
drain:
	for {
		select {
		case event, ok := <-events:
			if !ok {
				break drain
			}
			if event.Kind == session.EventConsentRequest {
				break drain
			}
		case <-deadline:
			t.Fatal("the question never reached the surface")
		}
	}
	_ = client.Close()

	// A QUESTION, NOT THE WATCH GRACE: asked with the grace ignored, the host
	// still says it is holding something.
	self, err := Ask(workspace, remote.WhoIs{StandDown: true, IgnoreWatchGrace: true})
	if err != nil || !self.Busy {
		t.Fatalf("a host holding an unanswered question said busy=%v: %v", self.Busy, err)
	}
	if err := Retire(workspace, false); !errors.Is(err, ErrHostBusy) {
		t.Fatalf("a stand-down against a waiting question answered %v, want ErrHostBusy", err)
	}
	// AND THE NOTE CANNOT END IT EITHER, for several of its own sweeps.
	if _, err := StandDownWhenIdle(workspace); err != nil {
		t.Fatalf("ask the host to let go when quiet: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := Ask(workspace, remote.WhoIs{}); err != nil {
		t.Fatalf("the host left with a question nobody had answered: %v", err)
	}

	// THE ANSWER IS WHAT OPENS THE QUIET MOMENT.
	agent.settled()
	goneWithin(t, workspace, 5*time.Second)
}

// WORK HANDED OFF OUTLIVES ITS TURN AND ITS WINDOW, and there is no window here
// at all: a task running on after the turn ended is not an idle engine, so the
// newer build waits for it rather than taking it down with the process.
func TestWorkHandedOffHoldsTheEngineAndItsEndLetsItGo(t *testing.T) {
	shortHome(t)
	quietSweeps(t, 20*time.Millisecond)
	workspace := "/home/somebody/api"
	agent := &handoffAgent{running: true}
	liveAgentHost(t, workspace, agent)

	// A conversation has to exist for work to have been handed off from: it is
	// the same road an ordinary window takes, and it closes again — the job
	// outlives both the turn that started it and the window that watched it.
	conn, err := Dial(workspace)
	if err != nil {
		t.Fatalf("dial the host: %v", err)
	}
	client, err := remote.Dial(conn, "test", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	_ = client.Close()

	if err := Retire(workspace, false); !errors.Is(err, ErrHostBusy) {
		t.Fatalf("a stand-down against handed-off work answered %v, want ErrHostBusy", err)
	}
	if _, err := StandDownWhenIdle(workspace); err != nil {
		t.Fatalf("ask the host to let go when quiet: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := Ask(workspace, remote.WhoIs{}); err != nil {
		t.Fatalf("the host left with work it had handed off: %v", err)
	}

	agent.finished()
	goneWithin(t, workspace, 5*time.Second)
}
