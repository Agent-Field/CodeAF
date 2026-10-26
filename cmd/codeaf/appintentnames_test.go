package main

import (
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestAppIntentNamesReceiveOtherOpenTitles(t *testing.T) {
	var got appOpenAsk
	b := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		got = ask
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	t.Cleanup(func() { _ = b.Close() })
	b.locked(func() {
		for i := 0; i < 22; i++ {
			id := fmt.Sprintf("sibling-%02d", i)
			a := b.agentLocked(id)
			a.view.Title = fmt.Sprintf("Review %02d", i)
			a.view.State = appwire.StateIdle
		}
		a := b.agentLocked("closed")
		a.view.Title, a.view.State = "Closed title", appwire.StateClosed
		a = b.agentLocked("archived")
		a.view.Title, a.view.State = "Archived title", appwire.StateArchived
	})
	target := b.agent("target")
	target.view.Title = "Own title"
	target.transcript = appFakeTranscript
	if _, err := b.OpenAgent(target.view.ID); err != nil {
		t.Fatal(err)
	}
	names := got.IntentSiblingNames()
	if len(names) != 20 {
		t.Fatalf("sibling titles = %q, want 20", names)
	}
	for i, title := range names {
		if title != fmt.Sprintf("Review %02d", i) {
			t.Fatalf("sibling %d = %q", i, title)
		}
	}
}

func TestAppIntentNamesRefreshOpenTitlesAfterOpening(t *testing.T) {
	var got appOpenAsk
	b := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		got = ask
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	t.Cleanup(func() { _ = b.Close() })
	b.locked(func() {
		for id, title := range map[string]string{"renamed": "Old title", "closed": "Closed later", "archived": "Archived later"} {
			a := b.agentLocked(id)
			a.view.Title, a.view.State = title, appwire.StateIdle
		}
		a := b.agentLocked("target")
		a.transcript, a.view.Title = appFakeTranscript, "Own title"
	})
	if _, err := b.OpenAgent("target"); err != nil {
		t.Fatal(err)
	}
	b.locked(func() {
		b.agents["renamed"].view.Title = "New title"
		b.agents["closed"].view.State = appwire.StateClosed
		b.agents["archived"].view.State = appwire.StateArchived
		a := b.agentLocked("new")
		a.view.Title, a.view.State = "New tab", appwire.StateIdle
	})
	names := got.IntentSiblingNames()
	if len(names) != 2 || names[0] != "New tab" || names[1] != "New title" {
		t.Fatalf("naming request titles = %q, want current new and renamed tabs only", names)
	}
}
