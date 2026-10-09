package tui3

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE CENTER OPENS OFF THE LOOP, AND SAYS SO WHEN IT CANNOT ───────────────
//
// The owner's run of 2026-10-09: a PR's `read` step running, its transcript
// on disk and growing, and the center saying `opening the chat…` for good on
// the step's row and on the manager's. The open is now asked away from the
// loop and off the door line, a refusal or an open that does not answer is
// said in the center, and a chat whose agent closed under it is opened again.

// embedAgent is a conversation that can say it closed, the way an in-process
// step's agent does when its round ends, and counts its closes from any
// goroutine.
type embedAgent struct {
	*fakeAgent
	ended  atomic.Bool
	closes atomic.Int32
}

func (e *embedAgent) Closed() bool                  { return e.ended.Load() }
func (e *embedAgent) Close() error                  { e.closes.Add(1); return nil }
func (e *embedAgent) InterruptFor(session.StopDoor) {}

// embedLab is item 2 with its review step's chat on disk, the page open and
// the cursor on review, and open the window's door. Nothing has been asked.
func embedLab(t *testing.T, open func(where, file string) (Conversation, error)) (*app, string) {
	t.Helper()
	chat := factoryStageChat(t)
	f := &factoryFake{}
	factoryShapeItem(f, 2, func(it *factory.Item) {
		s := *it.Stream
		s.Phases = append([]factory.Phase(nil), s.Phases...)
		s.Phases[4].Chat = chat
		it.Stream = &s
	})
	a := factoryVerbsLab(t, f, 150)
	a.open = func(string, string) (Conversation, error) {
		return Conversation{}, fmt.Errorf("no chat was selected yet")
	}
	factoryVerbsOpen(t, a, 2)
	a.open = open
	factoryRowNamed(t, a, "review")
	// What the page asked while it opened is forgotten: the lab starts with
	// nothing asked.
	a.fp.host = factoryHost{}
	return a, chat
}

// embedRun runs the commands a sync asked for, the way the runtime does, and
// answers what they said.
func embedRun(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, embedRun(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// SELECTING A STEP NEVER WAITS ON ITS OPEN: the sync that asks for the chat
// returns before the door is called, the center says it is opening while the
// door works, and the chat lands in the center when the door answers.
func TestFactoryEmbedOpensOffTheLoop(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	agent := &embedAgent{fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4-flash"}}
	a, _ := embedLab(t, func(where, file string) (Conversation, error) {
		calls.Add(1)
		<-release
		return Conversation{Agent: agent, SessionFile: file, Workspace: where}, nil
	})
	cmd := a.factoryHostSync()
	if cmd == nil {
		t.Fatal("selecting the step asked for no chat")
	}
	if calls.Load() != 0 {
		t.Fatal("the window's door was called on the loop")
	}
	if got := a.factoryHostWaitWords(); got != "opening the chat…" {
		t.Fatalf("the center says %q while the chat opens", got)
	}
	said := make(chan []tea.Msg, 1)
	go func() { said <- embedRun(cmd) }()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("the door was asked %d times", calls.Load())
	}
	// The loop is free while the door works: a key walks the page.
	if _, cmd := a.Update(key("down")); cmd == nil && a.fp.stage < 0 {
		t.Fatal("the page did not take a key while the chat was opening")
	}
	factoryRowNamed(t, a, "review")
	close(release)
	drive(t, a, (<-said)...)
	if !a.factoryHosting() || a.agent != Agent(agent) {
		t.Fatalf("the chat did not land in the center (host %+v)", a.fp.host)
	}
}

// AN OPEN THAT REFUSES SAYS WHY IN THE CENTER, never `opening…`: a journal
// another holder's lock keeps is said as the lock it is.
func TestFactoryEmbedRefusalIsSaid(t *testing.T) {
	a, _ := embedLab(t, func(string, string) (Conversation, error) {
		return Conversation{}, &session.SessionLockedError{Path: "transcript.jsonl"}
	})
	drive(t, a, embedRun(a.factoryHostSync())...)
	got := a.factoryHostWaitWords()
	if !strings.HasPrefix(got, "the chat did not open") || !strings.Contains(got, sessionBusyWord) {
		t.Fatalf("a locked journal reads %q in the center", got)
	}
	if strings.Contains(factoryFrameText(a), "opening the chat") {
		t.Fatalf("a refused open still says it is opening:\n%s", factoryFrameText(a))
	}
}

// AN OPEN THAT DOES NOT ANSWER IS SAID TO HAVE FAILED after the wait, and
// the conversation it opens late is let go of rather than left holding its
// journal.
func TestFactoryEmbedSlowOpenSaysSoAndLetsGo(t *testing.T) {
	was := factoryHostOpenWait
	factoryHostOpenWait = 40 * time.Millisecond
	defer func() { factoryHostOpenWait = was }()
	release := make(chan struct{})
	agent := &embedAgent{fakeAgent: &fakeAgent{model: "m"}}
	var once sync.Once
	defer once.Do(func() { close(release) })
	a, _ := embedLab(t, func(where, file string) (Conversation, error) {
		<-release
		return Conversation{Agent: agent, SessionFile: file, Workspace: where}, nil
	})
	drive(t, a, embedRun(a.factoryHostSync())...)
	got := a.factoryHostWaitWords()
	if !strings.HasPrefix(got, "the chat did not open") || !strings.Contains(got, "nothing answered") {
		t.Fatalf("an open that never answered reads %q in the center", got)
	}
	once.Do(func() { close(release) })
	deadline := time.Now().Add(2 * time.Second)
	for agent.closes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if agent.closes.Load() != 1 {
		t.Fatalf("the late conversation was closed %d times", agent.closes.Load())
	}
	if a.factoryHosting() {
		t.Fatal("the late conversation landed in the center")
	}
}

// A LIVE STEP'S CHAT WHOSE AGENT CLOSES UNDER THE CENTER (the round ended)
// is let go of and opened again from its journal, so the box in front can
// always take a word.
func TestFactoryEmbedEndedChatIsOpenedAgain(t *testing.T) {
	var opened []*embedAgent
	a, _ := embedLab(t, func(where, file string) (Conversation, error) {
		agent := &embedAgent{fakeAgent: &fakeAgent{model: "m"}}
		opened = append(opened, agent)
		return Conversation{Agent: agent, SessionFile: file, Workspace: where}, nil
	})
	drive(t, a, embedRun(a.factoryHostSync())...)
	if !a.factoryHosting() || len(opened) != 1 {
		t.Fatalf("the step's chat is not in the center (opened %d)", len(opened))
	}
	opened[0].ended.Store(true)
	drive(t, a, tea.WindowSizeMsg{Width: a.width, Height: a.height})
	if len(opened) != 2 {
		t.Fatalf("a chat that ended under the center was asked for again %d times", len(opened)-1)
	}
	if !a.factoryHosting() || a.agent != Agent(opened[1]) {
		t.Fatalf("the reopened chat is not in the center (host %+v)", a.fp.host)
	}
}
