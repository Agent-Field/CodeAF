package remote

// The defect these pin (#833): a reply stopped with `nobody was left watching
// this conversation` while the person was sitting in front of it, seconds after
// they had pressed a key on a card in that very window. The unattended door
// reads a presence fact — how many surfaces are attached — and a stalled call
// (#832) tears the pipe under a window that has not gone anywhere, so the
// reading is stale exactly when it matters most.

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A WINDOW THAT ACTED SECONDS AGO IS WATCHING, whatever its pipe is doing.
//
// The window here makes a real call and then loses its link, which is precisely
// what a stalled road does to it: the call gives up after client.go's
// [callDeadline], the connection is torn, and the redial has not landed yet. The
// unattended door must not read that gap as nobody being there.
func TestAWindowThatActedSecondsAgoIsStillWatching(t *testing.T) {
	agent := &fakeAgent{}
	sess := NewSession(engineOn(agent), true)
	client := viewOn(t, sess)

	// The act: one call from the window, through the same door every keystroke
	// takes.
	_ = client.Agent().Model()
	// And its link goes, without the person going anywhere.
	_ = client.Close()
	waitFor(t, "the host noticed the link go", func() bool { return sess.Attached() == 0 })

	if sess.RetireIfIdle(0) {
		t.Fatal("a conversation whose window acted a moment ago was retired for want of a watcher")
	}
	if sess.Ended() {
		t.Fatal("the conversation was ended under a window that is still there")
	}
	if door := agent.door(); door != "" {
		t.Fatalf("the turn was stopped through %q under a window that had just acted", door)
	}
}

// AND A CONVERSATION NOBODY IS IN IS STILL LET GO OF. The grace above is a
// grace and not the door being disabled: nothing ever attached here, nothing
// ever acted, and the sweep must still close it and flush its journal.
func TestAConversationNobodyEverWatchedIsStillRetired(t *testing.T) {
	agent := &fakeAgent{}
	sess := NewSession(engineOn(agent), true)
	if !sess.RetireIfIdle(0) {
		t.Fatal("a conversation with nobody in it and nothing running was kept")
	}
	if door := agent.door(); door != session.StopByRetired {
		t.Fatalf("the unattended door stopped the turn through %q, want %q", door, session.StopByRetired)
	}
}

// AND AN ACT THAT HAS AGED OUT STOPS COUNTING AS PRESENCE. The grace covers a
// stalled road, not a terminal somebody walked away from: past it the room is
// empty again and the sweep may have the conversation.
func TestAnActThatHasAgedOutStopsCountingAsPresence(t *testing.T) {
	agent := &fakeAgent{}
	sess := NewSession(engineOn(agent), true)
	client := viewOn(t, sess)
	_ = client.Agent().Model()
	_ = client.Close()
	waitFor(t, "the host noticed the link go", func() bool { return sess.Attached() == 0 })

	// The act is moved back past the grace, which is what the clock does on its
	// own given the time.
	sess.mu.Lock()
	sess.acted = time.Now().Add(-2 * watchGrace)
	sess.mu.Unlock()

	if !sess.RetireIfIdle(0) {
		t.Fatal("a conversation whose last act aged out was kept for ever")
	}
	if door := agent.door(); door != session.StopByRetired {
		t.Fatalf("the aged-out room was let go of through %q, want %q", door, session.StopByRetired)
	}
	if got := sess.lastWatched(); got == "" {
		t.Fatal("the retirement kept no name for the window it believed had gone")
	}
}

// A STOP TAKEN WHILE SOMEBODY IS WATCHING DOES NOT SAY NOBODY WAS.
//
// Every road that ends a conversation used to go through one door and one
// sentence, so `codeaf engine --stop`, a host shutting down and a person's own
// goodbye all told whoever was reading that nobody had been left watching. The
// sentence is the unattended door's, and only the unattended door may say it.
func TestAStopTakenWithAWindowInTheRoomDoesNotSayNobodyWasWatching(t *testing.T) {
	agent := &fakeAgent{}
	sess := NewSession(engineOn(agent), true)
	viewOn(t, sess)
	waitFor(t, "the window arrived", func() bool { return sess.Attached() == 1 })

	if err := sess.Close(); err != nil {
		t.Fatalf("close the conversation: %v", err)
	}
	if door := agent.door(); door == session.StopByRetired {
		t.Fatal("a conversation closed with a window still in the room said nobody was left watching it")
	}
}
