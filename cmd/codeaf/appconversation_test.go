package main

import (
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

type actionConversation struct {
	*fakeConversation
	entries  []session.DisplayEntry
	points   []session.RewindPoint
	cut      int
	restored string
	deleted  bool
}

func (c *actionConversation) Transcript() []session.DisplayEntry  { return c.entries }
func (c *actionConversation) RewindPoints() []session.RewindPoint { return c.points }
func (c *actionConversation) RewindKeep(p int) (string, error)    { c.cut = p; return "kept", nil }
func (c *actionConversation) UndoRewind(token string) error       { c.restored = token; return nil }
func (c *actionConversation) DebugConversation() (string, string, error) {
	return "/record", "recording", nil
}
func (c *actionConversation) DeleteOwnConversation() error { c.deleted = true; return nil }

func TestConversationBackendUsesOwningMethods(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	c := &actionConversation{fakeConversation: &fakeConversation{}, entries: []session.DisplayEntry{{Role: "user", Text: "first", At: at}, {Role: "user", Text: "second", At: at.Add(time.Minute)}}, points: []session.RewindPoint{{Index: 1, Entry: 0, Turn: true, Said: "first"}, {Index: 2, Entry: 1, Turn: true, Said: "second"}}}
	var opens, releases int
	var mu sync.Mutex
	opener := fakeOpener(c.fakeConversation, &opens, &releases, &mu)
	b := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		conn, err := opener(ask)
		if conn != nil {
			conn.Conv = c
		}
		return conn, err
	})
	defer b.Close()
	a, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir(), Model: "test/model"})
	if err != nil {
		t.Fatal(err)
	}
	points, err := b.ConversationPoints(a.ID)
	if err != nil || len(points) != 2 || points[0].Point != 2 || points[0].TurnsAfter != 0 || points[1].TurnsAfter != 1 || !points[0].At.Equal(at.Add(time.Minute)) {
		t.Fatalf("points: %+v %v", points, err)
	}
	token, err := b.RewindConversation(a.ID, 1)
	if err != nil || token != "kept" || c.cut != 1 {
		t.Fatalf("rewind: %q %v", token, err)
	}
	if err := b.UndoConversationRewind(a.ID, token); err != nil || c.restored != token {
		t.Fatalf("undo: %v", err)
	}
	debug, err := b.DebugAgent(a.ID)
	if err != nil || debug.Path != "/record" {
		t.Fatalf("debug: %+v %v", debug, err)
	}
	if err := b.DeleteAgent(a.ID); err != nil || !c.deleted {
		t.Fatalf("delete: %v", err)
	}
	if _, err := b.find(a.ID); err == nil {
		t.Fatal("deleted conversation remains registered")
	}
}

func TestConversationManualSendsTerminalInstruction(t *testing.T) {
	c := &fakeConversation{}
	var opens, releases int
	var mu sync.Mutex
	rec := newAppRecorder()
	b := newFakeBackend(rec, fakeOpener(c, &opens, &releases, &mu))
	defer b.Close()
	a, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir(), Model: "test/model"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := b.ManualAgent(a.ID, "how do I rewind?", "manual-1")
	if err != nil || turn == 0 {
		t.Fatalf("manual: %d %v", turn, err)
	}
	rec.waitFor(t, time.Second, appTurnDone)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.texts) != 1 || c.texts[0] != session.ManualQuestionLead+"how do I rewind?" {
		t.Fatalf("sent: %v", c.texts)
	}
}
