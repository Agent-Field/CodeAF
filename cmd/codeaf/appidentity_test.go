package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
)

func TestAppIndependentChatsKeepTheirOwnTranscripts(t *testing.T) {
	for _, cells := range []bool{false, true} {
		name := "legacy"
		if cells {
			name = "cells"
		}
		t.Run(name, func(t *testing.T) { testAppIndependentChats(t, cells) })
	}
}

func testAppIndependentChats(t *testing.T, cells bool) {
	_, workspace := wakeEnvironment(t, resolvedTempDir(t))
	t.Cleanup(cell.UseAvailabilityInTests(cells))
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	defer backend.Close()

	first, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := backend.Send(first.ID, "explain billing migrations", "billing-first")
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 10*time.Second, func(_ string, data any) bool {
		done, ok := data.(appwire.TurnDone)
		return ok && done.Agent == first.ID && done.Turn == turn && done.OK
	})
	second, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("two new chats share identity %q", first.ID)
	}
	turn, err = backend.Send(second.ID, "explain sourdough starters", "sourdough-first")
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 10*time.Second, func(_ string, data any) bool {
		done, ok := data.(appwire.TurnDone)
		return ok && done.Agent == second.ID && done.Turn == turn && done.OK
	})
	if err := backend.CloseAgent(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.OpenAgent(first.ID); err != nil {
		t.Fatalf("reopen billing: %v", err)
	}
	turn, err = backend.Send(first.ID, "which billing risk comes first?", "billing-followup")
	if err != nil {
		t.Fatalf("billing follow-up: %v", err)
	}
	rec.waitFor(t, 10*time.Second, func(_ string, data any) bool {
		done, ok := data.(appwire.TurnDone)
		return ok && done.Agent == first.ID && done.Turn == turn && done.OK
	})
	turn, err = backend.Send(second.ID, "how often should I feed the starter?", "sourdough-followup")
	if err != nil {
		t.Fatalf("sourdough follow-up: %v", err)
	}
	rec.waitFor(t, 10*time.Second, func(_ string, data any) bool {
		done, ok := data.(appwire.TurnDone)
		return ok && done.Agent == second.ID && done.Turn == turn && done.OK
	})
	for _, chat := range []struct{ id, text, followup string }{{first.ID, "explain billing migrations", "which billing risk comes first?"}, {second.ID, "explain sourdough starters", "how often should I feed the starter?"}} {
		entries, _, err := backend.Entries(chat.id, "", appwire.MaxEntries)
		if err != nil {
			t.Fatal(err)
		}
		var words []string
		for _, entry := range entries {
			if entry.Kind == appwire.EntryUser {
				words = append(words, entry.Text)
			}
		}
		if len(words) != 2 || words[0] != chat.text || words[1] != chat.followup {
			t.Fatalf("chat %s has user messages %q, want %q and %q", chat.id, words, chat.text, chat.followup)
		}
	}
}
