package remote

import "testing"

// viewOn dials a real client at one served conversation over an in-memory pipe.
// It is [Loopback] for a session the test built itself, which is the only way to
// choose whether the far end is persistent.
func viewOn(t *testing.T, sess *Session) *Client {
	t.Helper()
	surface, engine := Pipe()
	go func() {
		_ = ServeAttach(engine, engine, AttachOptions{
			Open: func(Hello) (*Session, error) { return sess, nil },
		})
		_ = engine.Close()
	}()
	client, err := Dial(surface, "loopback", Hello{Version: Version})
	if err != nil {
		_ = surface.Close()
		t.Fatalf("dial the conversation: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// A WINDOW CLOSING ON A HOSTED CONVERSATION LEAVES THE WORK RUNNING. This is the
// case a terminal exit actually is: the surface goes, the engine does not, and
// the conversation must not be told it is over — [MethodClose] would end a
// running task on behalf of somebody who only shut a window.
func TestDetachingFromAHostLeavesTheConversationRunning(t *testing.T) {
	agent := &fakeAgent{}
	sess := NewSession(engineOn(agent), true)
	client := viewOn(t, sess)

	if !client.Welcome().Persistent {
		t.Fatal("a session host did not say its engine outlives the connection")
	}
	if err := client.Agent().Detach(); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if sess.Ended() {
		t.Fatal("detaching ended the conversation")
	}
	if closes := agent.closed(); closes != 0 {
		t.Fatalf("detaching closed the engine's agent %d times", closes)
	}

	// AND THE PIPE GOING AWAY IS A SURFACE LEAVING AND NOT AN ENDING, which is
	// what the process exiting looks like from the host's side.
	_ = client.Close()
	waitFor(t, "the host noticed the surface leave", func() bool { return sess.Attached() == 0 })
	if sess.Ended() {
		t.Fatal("a dropped connection ended a hosted conversation")
	}
	if closes := agent.closed(); closes != 0 {
		t.Fatalf("a dropped connection closed the engine's agent %d times", closes)
	}
}

// AND A ONE-SHOT ENGINE HAS NO LIFE APART FROM THE PIPE, so leaving it is
// ending it: the flush is what keeps its session file readable.
func TestDetachingFromAOneShotEngineEndsIt(t *testing.T) {
	agent := &fakeAgent{}
	sess := NewSession(engineOn(agent), false)
	client := viewOn(t, sess)

	if client.Welcome().Persistent {
		t.Fatal("a one-shot engine claimed to outlive its connection")
	}
	if err := client.Agent().Detach(); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if closes := agent.closed(); closes == 0 {
		t.Fatal("detaching from a one-shot engine did not flush its session")
	}
}

// AND AN EXPLICIT CLOSE IS STILL AN ENDING wherever it is sent: /close and a
// stop are the person saying so, and hosting does not soften them.
func TestClosingAHostedConversationStillEndsIt(t *testing.T) {
	agent := &fakeAgent{}
	sess := NewSession(engineOn(agent), true)
	client := viewOn(t, sess)

	if err := client.Agent().Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if closes := agent.closed(); closes == 0 {
		t.Fatal("an explicit close did not reach the conversation")
	}
}

// THE LIFETIME IS A FACT THE ENGINE STATES, AND IT IS NOT THE DETACH SEAM. Every
// remote agent answers Detach; only a hosted one keeps working afterwards, so a
// surface that read the seam as survival would promise it to exactly the
// connection that cannot offer it.
func TestAnAgentSaysWhetherItsWorkOutlivesTheView(t *testing.T) {
	hosted := viewOn(t, NewSession(engineOn(&fakeAgent{}), true))
	if !hosted.Agent().WorkOutlivesExit() {
		t.Error("a hosted conversation said its work ends with the view")
	}
	oneShot := viewOn(t, NewSession(engineOn(&fakeAgent{}), false))
	if oneShot.Agent().WorkOutlivesExit() {
		t.Error("a one-shot engine claimed its work outlives the view")
	}
}
