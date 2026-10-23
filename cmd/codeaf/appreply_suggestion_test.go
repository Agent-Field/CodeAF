package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

type appReplyDoor struct {
	*fakeConversation
	calls int
}

func (d *appReplyDoor) SuggestReply(_ context.Context, material string) (string, error) {
	d.calls++
	if material != "Two approaches are available." {
		return "", nil
	}
	return "Explain the second approach.", nil
}

func TestReplySuggestionAppUsesOnlyHeldConversation(t *testing.T) {
	b := newFakeBackend(newAppRecorder(), func(appOpenAsk) (*appConn, error) {
		t.Error("suggestion opened a conversation")
		return nil, nil
	})
	defer b.Close()
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	b.locked(func() { b.agents["a"] = &appAgent{view: appwire.Agent{ID: "a"}, transcript: transcript} })
	ask := appwire.ReplySuggestionParams{Agent: "a", Context: "Two approaches are available."}
	if got, err := b.SuggestReply(t.Context(), ask); err != nil || got.Text != "" {
		t.Fatalf("unheld: %q %v", got.Text, err)
	}
	door := &appReplyDoor{fakeConversation: &fakeConversation{}}
	b.locked(func() { b.agents["a"].live = &appLive{conn: &appConn{Conv: door, Release: func() {}}} })
	if got, err := b.SuggestReply(t.Context(), ask); err != nil || got.Text != "Explain the second approach." {
		t.Fatalf("held: %q %v", got.Text, err)
	}
	if door.calls != 1 {
		t.Fatalf("suggestion calls: %d", door.calls)
	}
	b.locked(func() { b.agents["a"].live.conn.Conv = &fakeConversation{} })
	if got, err := b.SuggestReply(t.Context(), ask); err != nil || got.Text != "" {
		t.Fatalf("older owner: %q %v", got.Text, err)
	}
}
