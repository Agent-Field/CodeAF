package tui3

import (
	"errors"
	"testing"
)

type wholeStopAgent struct {
	*asyncAgent
	calls   int
	refusal error
}

func (a *wholeStopAgent) StopWork() error { a.calls++; return a.refusal }

// The engine, not the last rendered roster, is the authority for a close stop.
func TestCloseStopUsesWholeConversationDoor(t *testing.T) {
	a, _, first := asyncApp(t)
	turning(a, first)
	door := &wholeStopAgent{asyncAgent: first}
	a.agent = door
	drain(t, a, a.tabDismiss(a.frontChatTab()))
	takeAnswer(t, a, "2")
	if door.calls != 1 {
		t.Fatalf("whole stop calls = %d", door.calls)
	}
	if len(first.cancelled) != 0 {
		t.Fatal("surface also cancelled a stale roster")
	}
}

func TestCloseStopRefusalKeepsTheTabAndWork(t *testing.T) {
	a, _, first := asyncApp(t)
	turning(a, first)
	door := &wholeStopAgent{asyncAgent: first, refusal: errors.New("connection unavailable")}
	a.agent = door
	before := a.frontTabKey()
	drain(t, a, a.tabDismiss(a.frontChatTab()))
	takeAnswer(t, a, "2")
	if a.frontTabKey() != before || first.stops != 0 {
		t.Fatal("failed stop hid or interrupted the conversation")
	}
}
