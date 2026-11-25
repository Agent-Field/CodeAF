package main

import (
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func installAppAssistantCRM(t *testing.T) store.Meta {
	t.Helper()
	definitions := store.New()
	draft, err := definitions.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS("../../internal/apps/store/testdata/crm")); err != nil {
		t.Fatal(err)
	}
	meta, err := definitions.Promote(draft, "Keep the CRM")
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

func TestAppAssistantOpenReusesConversationAcrossRestart(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	meta := installAppAssistantCRM(t)
	b := newAppBackend(newAppRecorder(), refuseAppOpen)
	wire := wireOver(t, b)
	var opened struct {
		Conversation string `json:"conversation"`
	}
	wire.must("apps.agent_open", map[string]string{"app": meta.ID}, &opened)
	if opened.Conversation == "" {
		t.Fatal("missing conversation")
	}
	view, err := b.find(opened.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	var transcript string
	b.locked(func() { transcript = view.transcript })
	place := session.Place{Dir: session.DirOf(transcript)}
	chat, err := session.LoadMeta(place.Dir)
	if err != nil || chat.AppID != meta.ID {
		t.Fatalf("app binding = %q, error = %v", chat.AppID, err)
	}
	if agent, err := session.New(session.Config{Workspace: chat.Workspace, Place: place, SessionFile: transcript, Model: "test/model"}); err == nil {
		_ = agent.Close()
		t.Fatal("opened an app transcript with the ordinary belt")
	}
	var again struct {
		Conversation string `json:"conversation"`
	}
	wire.must("apps.agent_open", map[string]string{"app": meta.ID}, &again)
	if again != opened {
		t.Fatalf("reopen returned %q, want %q", again.Conversation, opened.Conversation)
	}
	_ = b.Close()
	restarted := newAppBackend(newAppRecorder(), refuseAppOpen)
	t.Cleanup(func() { _ = restarted.Close() })
	wireOver(t, restarted).must("apps.agent_open", map[string]string{"app": meta.ID}, &again)
	if again != opened {
		t.Fatalf("restart returned %q, want %q", again.Conversation, opened.Conversation)
	}
}
