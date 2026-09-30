package remote

import (
	"testing"
	"testing/synctest"

	"github.com/Agent-Field/codeaf/internal/session"
)

// closeGatedAgent is a conversation whose close does not finish until the test
// says so. It is how the engine's way out is held open on purpose, rather than
// by hoping a slow disk or a busy scheduler holds it open for us — which is the
// only way #1647 ever showed itself.
type closeGatedAgent struct {
	*fakeAgent
	entered chan struct{}
	release chan struct{}
}

func newCloseGatedAgent() *closeGatedAgent {
	return &closeGatedAgent{fakeAgent: &fakeAgent{}, entered: make(chan struct{}), release: make(chan struct{})}
}

func (a *closeGatedAgent) Close() error {
	close(a.entered)
	<-a.release
	return a.fakeAgent.Close()
}

// A CLOSED LOOP HAS NO ENGINE LEFT RUNNING. Close is asked while the
// conversation's own close is held open, and it must still be waiting once
// every goroutine in the bubble has gone as far as it can go.
//
// THE BUBBLE IS WHAT MAKES "STILL WAITING" A FACT RATHER THAN A GUESS.
// [synctest.Wait] returns only when every goroutine the test started is durably
// blocked, so a Close that does not join has certainly returned by then and a
// Close that does has certainly not — there is no window to size and no clock.
// On the Close that did not join, this test failed every time (#1647).
func TestLoopCloseWaitsForTheEngineToFinishLeaving(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		agent := newCloseGatedAgent()
		loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
			return &Engine{Agent: agent}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		returned := make(chan error, 1)
		go func() { returned <- loop.Close() }()
		<-agent.entered
		synctest.Wait()
		select {
		case err := <-returned:
			close(agent.release)
			<-loop.Served
			t.Fatalf("Close returned (%v) while the conversation behind it was still closing", err)
		default:
		}
		close(agent.release)
		if err := <-returned; err != nil {
			t.Fatalf("Close: %v", err)
		}
		// THE ANSWER IS ALREADY THERE. Close waited for the engine goroutine,
		// which puts Serve's answer on Served before it lets Close go, so a
		// receive that has to wait here is a Close that let go too early.
		select {
		case err := <-loop.Served:
			if err != nil {
				t.Fatalf("Serve after Close: %v", err)
			}
		default:
			t.Fatal("Close returned before Serve had answered")
		}
	})
}

// CUT IS STILL A LOST LINK, AND A CLOSE AFTER IT STILL JOINS. Cut shuts the
// pipe without a goodbye and does not wait, because the surface it stages is
// one that is about to redial; the Close a test's cleanup calls afterwards must
// wait all the same, and must not turn the retirement Cut caused into a person
// leaving.
func TestLoopCloseAfterCutStillJoinsAndKeepsTheLostLink(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		agent := newCloseGatedAgent()
		loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
			return &Engine{Agent: agent}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := loop.Cut(); err != nil {
			t.Fatal(err)
		}
		<-agent.entered
		returned := make(chan error, 1)
		go func() { returned <- loop.Close() }()
		synctest.Wait()
		select {
		case err := <-returned:
			close(agent.release)
			<-loop.Served
			t.Fatalf("Close after Cut returned (%v) while the conversation was still closing", err)
		default:
		}
		close(agent.release)
		if err := <-returned; err != nil {
			t.Fatalf("Close after Cut: %v", err)
		}
		if err := <-loop.Served; err != nil {
			t.Fatalf("Serve after Cut and Close: %v", err)
		}
		if agent.stopDoor != session.StopByRetired {
			t.Fatalf("the cut link ended through door %q, want the retired door a lost link takes", agent.stopDoor)
		}
		if err := loop.Close(); err != nil {
			t.Fatalf("a second Close: %v", err)
		}
	})
}

// SERVED IS THE CALLER'S, EVEN AFTER CLOSE HAS JOINED. Close waits on its own
// signal and never receives from Served, so the error an engine died of is
// still there for the test that wants to read it.
func TestLoopCloseLeavesServedErrorReadable(t *testing.T) {
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loop.surface.Write([]byte("{bad frame}\n")); err != nil {
		t.Fatal(err)
	}
	if err := loop.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-loop.Served; err == nil {
		t.Fatal("Serve's bad-frame error was lost")
	}
	if err := loop.Close(); err != nil {
		t.Fatalf("a second Close: %v", err)
	}
}
