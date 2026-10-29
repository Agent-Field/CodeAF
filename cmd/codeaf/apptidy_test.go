package main

import (
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// renamed is the name the fake was last given by a person.
var renamed = map[*fakeConversation]string{}

func (f *fakeConversation) RenameByPerson(title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	renamed[f] = title
	return nil
}

// MOVED TO CLOSED, AN AGENT SAYS SO WITH WHEN, AND REOPENING TAKES IT OUT
// AGAIN: the conversation is let go, the moment is kept in the app's state,
// and a reopen forgets it.
func TestAppMoveToClosedAndReopen(t *testing.T) {
	backend, rec, agent := workBackend(t, &fakeConversation{})
	if err := backend.MoveToClosed([]string{agent.ID}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == agent.ID && c.Agent.State == appwire.StateClosed && c.Agent.ClosedAt != nil
	})
	if backend.held(agent.ID) {
		t.Fatal("a closed agent's conversation is still held")
	}
	again, err := backend.Reopen(agent.ID)
	if err != nil || again.State != appwire.StateIdle || again.ClosedAt != nil {
		t.Fatalf("reopened = %+v, %v", again, err)
	}
	closed := false
	_ = backend.state.read(func(f *appStateFile) { _, closed = f.Closed[agent.ID] })
	if closed {
		t.Fatal("the closed mark outlived the reopen")
	}
}

// A RENAME IS THE PERSON'S NAME, given to the conversation itself.
func TestAppRenameGivesTheConversationThePersonsName(t *testing.T) {
	conv := &fakeConversation{}
	backend, _, agent := workBackend(t, conv)
	view, err := backend.Rename(agent.ID, "release notes")
	if err != nil || view.Title != "release notes" {
		t.Fatalf("renamed = %+v, %v", view, err)
	}
	conv.mu.Lock()
	got := renamed[conv]
	conv.mu.Unlock()
	if got != "release notes" {
		t.Fatalf("the conversation was given %q", got)
	}
}

// THE LIST TAKES A STATE AND A QUERY: open by default, and a query matches
// the title or the folder. The agent is spoken in first, because a blank one
// is never listed (appblank.go).
func TestAppAgentsListFiltersByState(t *testing.T) {
	backend, rec, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	if _, err := backend.Send(agent.ID, "hello", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
	open, err := backend.ListAgentsIn("", "", "")
	if err != nil || len(open) != 1 || open[0].ID != agent.ID {
		t.Fatalf("open = %+v, %v", open, err)
	}
	closed, _ := backend.ListAgentsIn("", appwire.ListClosed, "")
	if len(closed) != 0 {
		t.Fatalf("closed = %+v", closed)
	}
	none, _ := backend.ListAgentsIn("", "", "no such words anywhere")
	if len(none) != 0 {
		t.Fatalf("a query matched nothing it names: %+v", none)
	}
}

// A WARM OPEN NEVER TAKES AN AGENT OUT OF CLOSED OR THE ARCHIVE: a window's
// guess at a view (a hover's prefetch) is refused and the mark stays, while
// a person's own open still reopens it.
func TestAWarmOpenLeavesAClosedAgentClosed(t *testing.T) {
	for name, verb := range tidyVerbs {
		t.Run(name, func(t *testing.T) {
			w := newSvcWorld(t)
			w.realTranscript(t)
			backend, agent := w.backend, w.agent
			if err := verb(backend, agent.ID); err != nil {
				t.Fatal(err)
			}
			_, err := backend.WarmAgent(agent.ID)
			var wire *appwire.Error
			if !errors.As(err, &wire) || wire.Code != appwire.CodeConflict {
				t.Fatalf("a warm open of a %s agent answered %v", name, err)
			}
			if !backend.tidied(agent.ID) || backend.held(agent.ID) {
				t.Fatalf("after a warm open: marked=%v held=%v", backend.tidied(agent.ID), backend.held(agent.ID))
			}
			opened, err := backend.OpenAgent(agent.ID)
			if err != nil || opened.State == appwire.StateClosed || opened.State == appwire.StateArchived || backend.tidied(agent.ID) {
				t.Fatalf("a plain open of a %s agent = %+v, %v, marked=%v", name, opened, err, backend.tidied(agent.ID))
			}
		})
	}
}

// A warm open of an open agent opens it, as any open does.
func TestAWarmOpenOpensAnOpenAgent(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{})
	if _, err := backend.WarmAgent(agent.ID); err != nil || !backend.held(agent.ID) {
		t.Fatalf("warm = %v, held=%v", err, backend.held(agent.ID))
	}
}

// NO OPEN LANDS BETWEEN A CLOSE'S RELEASE AND ITS MARK: a warm open asked
// while the close runs waits on the agent's gate, then finds the mark and is
// refused, so the agent is neither held nor reopened.
func TestAWarmOpenWaitingOnACloseFindsItClosed(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{})
	a, err := backend.find(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	warmed := make(chan error, 1)
	err = backend.closeOne(a, func() error {
		go func() { _, err := backend.WarmAgent(agent.ID); warmed <- err }()
		select {
		case err := <-warmed:
			t.Errorf("a warm open ran between the release and the mark: %v", err)
		case <-time.After(150 * time.Millisecond):
		}
		return backend.state.write(func(f *appStateFile) error {
			if f.Closed == nil {
				f.Closed = map[string]time.Time{}
			}
			f.Closed[agent.ID] = backend.now()
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-warmed:
		if err == nil {
			t.Fatal("the warm open that waited on the close opened it")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the warm open never answered")
	}
	if !backend.tidied(agent.ID) || backend.held(agent.ID) {
		t.Fatalf("after the close: marked=%v held=%v", backend.tidied(agent.ID), backend.held(agent.ID))
	}
}
