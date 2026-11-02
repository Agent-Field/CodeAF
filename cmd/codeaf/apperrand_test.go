package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

type appErrandDoor struct {
	*fakeConversation
	calls int
	fail  error
}

func (d *appErrandDoor) RunErrand(_ context.Context, ask session.ErrandAsk) (session.ErrandAnswer, error) {
	d.calls++
	if d.fail != nil {
		return session.ErrandAnswer{}, d.fail
	}
	return session.ErrandAnswer{Text: "ok", Model: ask.Model}, nil
}

func TestAnErrandUsesOnlyAHeldConversationAndAServedModel(t *testing.T) {
	b := newFakeBackend(newAppRecorder(), func(appOpenAsk) (*appConn, error) {
		t.Error("an errand opened a conversation")
		return nil, nil
	})
	defer b.Close()
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	b.locked(func() { b.agents["a"] = &appAgent{view: appwire.Agent{ID: "a"}, transcript: transcript} })
	ask := session.ErrandAsk{Purpose: "status", Role: "title"}
	if got, err := b.Errand(t.Context(), "a", ask); err != nil || got.Reason != appwire.ErrandNotOpen {
		t.Fatalf("unheld: %+v %v", got, err)
	}
	door := &appErrandDoor{fakeConversation: &fakeConversation{}}
	b.locked(func() { b.agents["a"].live = &appLive{conn: &appConn{Conv: door, Release: func() {}}} })
	if got, err := b.Errand(t.Context(), "a", ask); err != nil || got.Text != "ok" || door.calls != 1 {
		t.Fatalf("held: %+v %v", got, err)
	}
	ask.Model = "vendor/nobody-serves-this"
	if _, err := b.Errand(t.Context(), "a", ask); !errors.Is(err, session.ErrErrandRoute) || door.calls != 1 {
		t.Fatalf("an unserved model reached the model: %v calls=%d", err, door.calls)
	}
	// A link that drops under the errand is a reason, never a wire error.
	door.fail = errors.New("this connection is closed")
	if got, err := b.Errand(t.Context(), "a", session.ErrandAsk{Purpose: "status"}); err != nil || got.Reason != appwire.ErrandUnavailable {
		t.Fatalf("dropped owner: %+v %v", got, err)
	}
	b.locked(func() { b.agents["a"].live.conn.Conv = &fakeConversation{} })
	if got, err := b.Errand(t.Context(), "a", session.ErrandAsk{Purpose: "status"}); err != nil || got.Reason != appwire.ErrandUnavailable {
		t.Fatalf("older owner: %+v %v", got, err)
	}
}
