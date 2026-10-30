package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// VIEWING A JOB'S PAGE LEAVES ITS CLOSED CHATS CLOSED. The page reads the job's
// conversations (factory.job_chats), and that read held each one open, and
// set the job's effort through an open, so a chat the person closed with ×
// came back out of Closed as soon as they looked at the job. A page view is
// not an open: the read names every chat and step as before, and a closed or
// archived one keeps its mark, unheld.
func TestViewingAJobsPageLeavesItsClosedChatsClosed(t *testing.T) {
	s := newLocalSetup(t)
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	first, firstID := jobConversation(t)
	latest, latestID := jobConversation(t)
	t0 := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 11, Title: "Close the page", Kind: factory.KindIssue, State: factory.StateNeedsYou, Effort: "high",
		Stream: &factory.Stream{Phases: []factory.Phase{
			{Name: "review", Kind: factory.StageChat, State: factory.PhaseDone, Round: 2, Chat: latest, Started: t0.Add(5 * time.Minute), At: t0.Add(8 * time.Minute),
				Rounds: []factory.RoundLine{
					{Round: 1, Started: t0, At: t0.Add(4 * time.Minute), Chat: first},
					{Round: 2, Started: t0.Add(5 * time.Minute), At: t0.Add(8 * time.Minute), Chat: latest},
				}},
		}}})
	if err != nil {
		t.Fatal(err)
	}
	read := func() appwire.FactoryJobChats {
		var chats appwire.FactoryJobChats
		s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &chats)
		return chats
	}
	before := read()
	if before.Agent == "" || len(before.Steps) != 1 {
		t.Fatalf("the first read: %+v", before)
	}
	if err := s.backend.MoveToClosed([]string{before.Agent, latestID}); err != nil {
		t.Fatal(err)
	}
	if err := s.backend.Archive([]string{firstID}); err != nil {
		t.Fatal(err)
	}

	rec := newAppRecorder()
	s.backend.emit = rec
	after := read()
	read()
	// The steps are presented exactly as before: the same chats, rounds and order.
	if after.Agent != before.Agent || len(after.Steps) != 1 || after.Steps[0].Step != "review" || after.Steps[0].Agent != latestID {
		t.Fatalf("the read after the close: %+v", after)
	}
	want := []appwire.FactoryJobRoundChat{{Round: 1, Agent: firstID}, {Round: 2, Agent: latestID}}
	if got := after.Steps[0].Rounds; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("the review's rounds read %+v, want %+v", got, want)
	}
	for _, id := range []string{before.Agent, latestID, firstID} {
		if !s.backend.tidied(id) {
			t.Fatalf("viewing the job's page took %s out of Closed", id)
		}
		if s.backend.isHeld(id) {
			t.Fatalf("viewing the job's page holds the closed chat %s open", id)
		}
	}
	// The app is told each one as it stands, so the page draws it closed, and
	// its own warm open of it is refused, never taken for the person's.
	said := map[string]string{}
	rec.mu.Lock()
	for i, event := range rec.events {
		if changed, ok := rec.data[i].(appwire.AgentChanged); ok && event == appwire.EventAgentChanged {
			said[changed.Agent.ID] = changed.Agent.State
		}
	}
	rec.mu.Unlock()
	for id, state := range map[string]string{before.Agent: appwire.StateClosed, latestID: appwire.StateClosed, firstID: appwire.StateArchived} {
		if said[id] != state {
			t.Fatalf("viewing the page last told the app %s is %q, want %q", id, said[id], state)
		}
	}
	s.mustFail("agents.open", map[string]any{"id": before.Agent, "warm": true}, appwire.CodeConflict)
	if !s.backend.tidied(before.Agent) {
		t.Fatal("the page's warm open took the job's chat out of Closed")
	}
	// Opened by the person, the job's chat comes out of Closed as any agent does.
	if _, err := s.backend.OpenAgent(before.Agent); err != nil {
		t.Fatal(err)
	}
	if s.backend.tidied(before.Agent) || !s.backend.isHeld(before.Agent) {
		t.Fatal("the person's own open left the job's chat closed")
	}
}

// A JOB'S CHAT LEFT CLOSED TAKES THE JOB'S EFFORT WHEN IT IS OPENED AGAIN. The
// page's read sets the job's effort only on a held chat, since setting it
// opens. A send or Reopen later opens the closed chat itself, and it ran at its
// own stored effort, not the job's, which may have changed meanwhile.
func TestAJobsClosedChatTakesTheJobsEffortWhenItIsOpenedAgain(t *testing.T) {
	s := newLocalSetup(t)
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 12, Title: "Effort on reopen", Kind: factory.KindIssue, State: factory.StateNeedsYou, Effort: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	read := func() appwire.FactoryJobChats {
		var chats appwire.FactoryJobChats
		s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &chats)
		return chats
	}
	chat := read().Agent
	if err := s.backend.MoveToClosed([]string{chat}); err != nil {
		t.Fatal(err)
	}
	read()
	if s.backend.isHeld(chat) {
		t.Fatal("the page's read held the closed chat open")
	}
	// The job's effort changes while its chat is closed; the page is not read again.
	if err := floor.Update(it.ID, func(it *factory.Item) error { it.Effort = "high"; return nil }); err != nil {
		t.Fatal(err)
	}
	view, err := s.backend.OpenAgent(chat)
	if err != nil {
		t.Fatal(err)
	}
	if view.Effort != "high" {
		t.Fatalf("the job's chat opened again at effort %q, want the job's %q", view.Effort, "high")
	}
}
