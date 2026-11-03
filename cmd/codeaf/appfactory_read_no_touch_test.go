package main

import (
 "bytes"
 "encoding/json"
 "os"
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

func TestFactoryJobChatsDoesNotMigrateRepositoryOnRead(t *testing.T) {
 s := newLocalSetup(t)
 st := s.start()
 floor := s.backend.local.host.floor
 it, err := floor.Create(factory.Item{Repo: "acme/api", Product: "acme", Num: 74, Kind: factory.KindIssue, Origin: factory.OriginForge, State: factory.StateNew})
 if err != nil { t.Fatal(err) }
 type envelope struct { Schema int `json:"schema"`; Revision int `json:"revision"`; Item factory.Item `json:"item"` }
 raw, err := os.ReadFile(floor.ItemPath(it.ID))
 if err != nil { t.Fatal(err) }
 var doc envelope
 if err := json.Unmarshal(raw, &doc); err != nil { t.Fatal(err) }
 doc.Item.Repo = "api"
 raw, err = json.Marshal(doc)
 if err != nil { t.Fatal(err) }
 if err := os.WriteFile(floor.ItemPath(it.ID), raw, 0600); err != nil { t.Fatal(err) }
 for _, method := range []string{"factory.job_chats", "factory.job", "factory.queue"} {
  params := map[string]any{"factory": st.Factory}
  if method != "factory.queue" { params["job"] = it.ID }
  s.must(method, params, nil)
  kept, err := os.ReadFile(floor.ItemPath(it.ID))
  if err != nil { t.Fatal(err) }
  if !bytes.Equal(raw, kept) { t.Fatalf("%s rewrote repository identity or version", method) }
 }
}
