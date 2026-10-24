package remote

import "testing"

// TestC31DeliberateLastWindowReleasesAnIdleChat proves C31.1/C31.3: a paired
// idle conversation ends after the last deliberate detach, never before it.
func TestC31DeliberateLastWindowReleasesAnIdleChat(t *testing.T) {
	engine := engineOn(&fakeAgent{})
	engine.ReleaseOnLeave = func(WrappedAgent) bool { return true }
	sess := NewSession(engine, true)
	a, b := viewOn(t, sess), viewOn(t, sess)
	if err := a.Agent().Detach(); err != nil {
		t.Fatal(err)
	}
	_ = a.Close()
	waitFor(t, "first window left", func() bool { return sess.Attached() == 1 })
	if sess.Ended() {
		t.Fatal("one window released another window's chat")
	}
	if err := b.Agent().Detach(); err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
	waitFor(t, "last window released the idle chat", sess.Ended)
}

// TestC31ATornLinkDoesNotRelease proves C31.2: a lid close or dropped pipe is
// never treated as the person's deliberate release, even on a paired chat.
func TestC31ATornLinkDoesNotRelease(t *testing.T) {
	engine := engineOn(&fakeAgent{})
	engine.ReleaseOnLeave = func(WrappedAgent) bool { return true }
	sess := NewSession(engine, true)
	a := viewOn(t, sess)
	_ = a.Close()
	waitFor(t, "torn link left", func() bool { return sess.Attached() == 0 })
	if sess.Ended() {
		t.Fatal("torn link released the chat")
	}
}

// TestC31QuitDuringFinalStreamReleasesWhenItFinishes proves C31.1: a turn
// finishing just after the clean departure must not strand its lease for the
// ordinary idle grace. Arriving again cancels the pending departure (C31.3).
func TestC31QuitDuringFinalStreamReleasesWhenItFinishes(t *testing.T) {
	for _, arriveAgain := range []bool{false, true} {
		engine := engineOn(&fakeAgent{})
		engine.ReleaseOnLeave = func(WrappedAgent) bool { return true }
		sess := NewSession(engine, true)
		a := viewOn(t, sess)
		id, generation := sess.mint()
		if err := a.Agent().Detach(); err != nil {
			t.Fatal(err)
		}
		_ = a.Close()
		waitFor(t, "last window left", func() bool { return sess.Attached() == 0 })
		if sess.Ended() {
			t.Fatal("quit interrupted the final stream")
		}
		if arriveAgain {
			b := viewOn(t, sess)
			defer b.Close()
		}
		sess.finish(id, generation)
		if sess.Ended() == arriveAgain {
			t.Fatalf("ended=%v after arrival=%v", sess.Ended(), arriveAgain)
		}
	}
}

// TestC31ReleaseFollowsTheReplacedConversation proves C31.1 after /new or
// /resume: clean departure concerns the current chat, not its boot ancestor.
func TestC31ReleaseFollowsTheReplacedConversation(t *testing.T) {
	first, second := &fakeAgent{}, &fakeAgent{}
	engine := engineOn(first)
	engine.ReleaseOnLeave = func(agent WrappedAgent) bool { return agent == second }
	engine.Fresh = func() (WrappedAgent, string, error) { return second, "/sessions/second.jsonl", nil }
	sess := NewSession(engine, true)
	a := viewOn(t, sess)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}
	if err := a.Agent().Detach(); err != nil {
		t.Fatal(err)
	}
	_ = a.Close()
	waitFor(t, "replacement released", sess.Ended)
}
