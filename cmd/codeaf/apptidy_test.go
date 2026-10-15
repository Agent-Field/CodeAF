package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
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
// the title or the folder.
func TestAppAgentsListFiltersByState(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{})
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
