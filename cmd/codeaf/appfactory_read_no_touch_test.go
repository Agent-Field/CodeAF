package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestFactoryJobChatsDoesNotTouchJob(t *testing.T) {
	s := newLocalSetup(t)
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 71, Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	version, err := floor.Revision(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var chats appwire.FactoryJobChats
		s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &chats)
		after, err := floor.Get(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		revision, err := floor.Revision(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Talk != "" || !factory.JobUnworked(after) || after.PersonTouched || revision != version || !after.Changed.Equal(it.Changed) {
			t.Fatalf("read touched job: Talk=%q unworked=%t touched=%t version=%d want=%d changed=%v", after.Talk, factory.JobUnworked(after), after.PersonTouched, revision, version, after.Changed)
		}
		if chats.Agent != "" || chats.Steps == nil || len(chats.Steps) != 0 {
			t.Fatalf("no-chat read = %+v", chats)
		}
	}
}

func TestFactoryJobSendTouchesJob(t *testing.T) {
	s := newLocalSetup(t)
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 72, Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Agent string `json:"agent"`
		Turn  uint64 `json:"turn"`
	}
	s.must("factory.job_send", map[string]any{"factory": st.Factory, "job": it.ID, "text": "Please check the ledger"}, &sent)
	after, err := floor.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Talk == "" || factory.JobUnworked(after) || !after.PersonTouched || sent.Agent != appAgentID(after.Talk) || sent.Turn == 0 {
		t.Fatalf("send did not touch job: sent=%+v job=%+v", sent, after)
	}
}

func TestFactoryExistingJobSendTouchesJob(t *testing.T) {
	s := newLocalSetup(t)
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	s.start()
	talk, id := jobConversation(t)
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 73, Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew, Talk: talk})
	if err != nil {
		t.Fatal(err)
	}
	if err := markFactoryChat(floor, it, talk); err != nil {
		t.Fatal(err)
	}
	s.must("agents.send", map[string]any{"id": id, "text": "Please check the ledger"}, nil)
	after, err := floor.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if factory.JobUnworked(after) || !after.PersonTouched {
		t.Fatalf("sent message did not count: unworked=%t touched=%t", factory.JobUnworked(after), after.PersonTouched)
	}
}

func TestFactoryAutomaticBriefDoesNotTouchJob(t *testing.T) {
	s := newLocalSetup(t)
	door := &briefDoor{fakeConversation: &fakeConversation{}, answer: session.ErrandAnswer{Text: `{"verdict":"Waiting to start.","blocks":[]}`, Model: "scripted"}}
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: door, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 74, Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	var written appwire.FactoryJobBriefWritten
	s.must("factory.job_brief_write", map[string]any{"factory": st.Factory, "job": it.ID}, &written)
	after, err := floor.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Talk == "" {
		t.Fatal("brief did not create its manager")
	}
	if !factory.JobUnworked(after) || after.PersonTouched {
		t.Fatalf("automatic brief touched job: unworked=%t PersonTouched=%t", factory.JobUnworked(after), after.PersonTouched)
	}
	if !written.Kept {
		t.Fatalf("scripted brief was not kept: %+v", written)
	}
}
