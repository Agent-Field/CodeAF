package tui3

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestAQueuedTitleCannotRenameTheConversationAfterDetach(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.titleGen = 17
	a.titleLane = make(chan session.Event)
	stale := titleEventMsg{gen: a.titleGen, ev: session.Event{Kind: session.EventTitleChanged, Text: "parser migration"}}
	a.detachConversation()
	a.title = "database recovery"
	if a.titleLane != nil {
		t.Fatal("detach retained the old title lane")
	}
	_, cmd := a.Update(stale)
	if a.title != "database recovery" || cmd != nil {
		t.Fatalf("old title crossed detach: title=%q command=%v", a.title, cmd != nil)
	}
}

func TestTheTitleLaneKeepsReadingUntilItsOwnerClosesIt(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	lane := make(chan session.Event, 1)
	lane <- session.Event{Kind: session.EventTitleChanged, Text: "parser migration"}
	close(lane)
	a.titleLane, a.titleGen = lane, 4
	first := titleEventMsg{gen: 4, ev: session.Event{Kind: session.EventTitleChanged, Text: "parser migration"}}
	_, next := a.Update(first)
	// Find the next title event among the returned commands without running the
	// screen's unrelated clock commands. The lane is buffered, so this never
	// waits for a provider or a timer.
	var titleNext tea.Msg
	var inspect func(tea.Cmd)
	inspect = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			for _, child := range msg {
				inspect(child)
			}
		case titleEventMsg, titleLaneClosedMsg:
			titleNext = msg
		}
	}
	inspect(next)
	if _, ok := titleNext.(titleEventMsg); !ok {
		t.Fatalf("title pump stopped after first name: %T", titleNext)
	}
	_, next = a.Update(titleNext)
	titleNext = nil
	inspect(next)
	if _, ok := titleNext.(titleLaneClosedMsg); !ok {
		t.Fatalf("title pump did not drain owner close: %T", titleNext)
	}
	a.Update(titleNext)
	if a.titleLane != nil {
		t.Fatal("closed title lane remained attached")
	}
}

type titleWaitingAgent struct {
	*countingAgent
	name string
}

func (a *titleWaitingAgent) Title() string { return a.name }

func TestAHeldWaitingChatTitleRedrawsWithoutAnotherAttentionNotice(t *testing.T) {
	a, watch, counted := signalLab(t)
	counted.waiting = true
	agent := &titleWaitingAgent{countingAgent: counted, name: "parser migration"}
	held := a.behind[watch.key]
	held.conv.Agent, watch.agent = agent, agent
	lane := make(chan behindStirMsg, 1)
	watch.out = lane
	watch.armed.Store(true)
	watch.landed.Store(true)
	watch.finished.Store(2)
	a.dirty = false
	watch.stirName()
	note := <-lane
	if !note.quiet || note.key != watch.key {
		t.Fatalf("name became a work notification: %+v", note)
	}
	a.behindStir(note)
	if !a.dirty {
		t.Fatal("late name did not invalidate the frame")
	}
	if counted.asked.Load() != 0 {
		t.Fatal("title update entered the attention-notification path")
	}
	if !watch.armed.Load() || !watch.landed.Load() || watch.finished.Load() != 2 {
		t.Fatal("title update consumed a real work signal")
	}
	if got := hopRawTitle(agent, held.side); got != agent.name {
		t.Fatalf("held tab retained %q", got)
	}
	if a.title != "Shipping the parser" {
		t.Fatalf("background title renamed foreground: %q", a.title)
	}
}
