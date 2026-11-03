package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
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

// The scripted errand charges the same job budget as a real model call.
type untouchedBriefDoor struct {
	*briefDoor
	budget *stageBudget
}

func (d *untouchedBriefDoor) RunErrand(ctx context.Context, ask session.ErrandAsk) (session.ErrandAnswer, error) {
	d.budget.SpendModel(0.002, "scripted", 1, 10, 5)
	return d.briefDoor.RunErrand(ctx, ask)
}

func TestFactoryAutomaticBriefDoesNotTouchJob(t *testing.T) {
	s := newLocalSetup(t)
	door := &untouchedBriefDoor{briefDoor: &briefDoor{fakeConversation: &fakeConversation{}, answer: session.ErrandAnswer{Text: `{"verdict":"Waiting to start.","blocks":[]}`, Model: "scripted", Usage: session.ErrandUsage{CostUSD: 0.002}}}}
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: door, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 74, Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	door.budget = &stageBudget{floor: floor, item: it}
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
	if !factory.UntouchedIssue(after) || factory.JobSpent(after) != 0.002 {
		t.Fatalf("brief billing changed reconciliation eligibility: %+v", after)
	}
	if !written.Kept {
		t.Fatalf("scripted brief was not kept: %+v", written)
	}
	s.must("agents.send", map[string]any{"id": appAgentID(after.Talk), "text": "Please check the ledger"}, nil)
	after, err = floor.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if factory.JobUnworked(after) || !after.PersonTouched {
		t.Fatalf("personal send after automatic brief did not touch job: %+v", after)
	}
}
