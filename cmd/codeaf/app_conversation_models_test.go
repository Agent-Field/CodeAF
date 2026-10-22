package main

import (
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

type appPairConversation struct {
	*fakeConversation
	pair session.ConversationModels
}

func (c *appPairConversation) ModelIdentity() session.ModelIdentity {
	return session.ModelIdentity{Selected: c.Model(), Models: &c.pair}
}
func (c *appPairConversation) SetConversationModels(pair session.ConversationModels) ([]session.ModelSwitch, error) {
	old := c.pair
	c.pair = pair
	c.fakeConversation.SetModel(pair.Main)
	notes := []session.ModelSwitch{}
	if old.Main != pair.Main {
		notes = append(notes, session.ModelSwitch{ID: session.NewSessionID(), At: time.Now(), Text: "Main model changed."})
	}
	if old.Fallback != pair.Fallback {
		notes = append(notes, session.ModelSwitch{ID: session.NewSessionID(), At: time.Now(), Text: "Fallback model changed."})
	}
	return notes, nil
}
func TestAppConversationModelsAllowsLocalLentSelectionOnly(t *testing.T) {
	var mu sync.Mutex
	c := &appPairConversation{fakeConversation: &fakeConversation{model: "main/old"}, pair: session.ConversationModels{Main: "main/old", Fallback: "backup/old"}}
	rec := newAppRecorder()
	b := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		mu.Lock()
		defer mu.Unlock()
		return &appConn{Conv: c, Workspace: ask.Workspace, Transcript: ask.Transcript, Lent: true, Release: func() {}}, nil
	})
	defer b.Close()
	a, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.SetConversationModels(a.ID, session.ConversationModels{Main: "main/new", Fallback: "backup/new"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "main/new" || got.ModelIdentity == nil || got.ModelIdentity.Models.Fallback != "backup/new" {
		t.Fatalf("ack: %+v", got)
	}
	// Existing per-stage budget/posture fences are unchanged by the new model door.
	rec.waitFor(t, time.Second, func(_ string, data any) bool {
		e, ok := data.(appwire.EntryAdded)
		return ok && e.Entry.Kind == appwire.EntryNotice && e.Entry.NoticeKind == "model_switch"
	})
}

func TestAppConversationModelsNoticesAreDurableChronologicalAndPaged(t *testing.T) {
	record := session.ReadTranscriptBytes([]byte("{\"type\":\"message\",\"role\":\"user\",\"content\":\"go\",\"timestamp\":\"2026-10-22T10:00:00Z\"}\n{\"type\":\"model_switch\",\"modelSwitch\":{\"id\":\"switch1\",\"at\":\"2026-10-22T10:00:01Z\",\"text\":\"Using fallback model C.\"}}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"answer\",\"timestamp\":\"2026-10-22T10:00:02Z\"}\n"))
	entries, more, err := appEntryPage("chat", record, time.Time{}, nil, "", 10, nil)
	if err != nil || more {
		t.Fatalf("page: %v %v", more, err)
	}
	if len(entries) != 3 || entries[1].ID != "model-switch-switch1" || entries[1].Kind != appwire.EntryNotice || !entries[1].Quiet || entries[2].Text != "answer" {
		t.Fatalf("order: %+v", entries)
	}
	page, more, err := appEntryPage("chat", record, time.Time{}, nil, entries[2].ID, 1, nil)
	if err != nil || !more || len(page) != 1 || page[0].ID != entries[1].ID {
		t.Fatalf("cursor: %+v %v %v", page, more, err)
	}
}

func TestAppConversationModelsLiveNoticeKeepsItsJournalInstant(t *testing.T) {
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	n := session.ModelSwitch{ID: "source-instant", At: at, Text: "Using fallback model C."}
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventNotice, Text: n.Text, ModelSwitch: &n}}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	b := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer b.Close()
	_, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir(), Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, time.Second, func(_ string, data any) bool {
		e, ok := data.(appwire.EntryAdded)
		return ok && e.Entry.ID == "model-switch-source-instant" && e.Entry.At.Equal(at)
	})
}

func TestAppDefaultFallbackUsesSharedDefaultAndKeepsExplicitChoice(t *testing.T) {
	dir := t.TempDir()
	if got := appDefaultFallback(dir); got != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("default=%q", got)
	}
	if err := applyRow(dir, "models.fallbacks", "custom/backup,custom/second"); err != nil {
		t.Fatal(err)
	}
	if got := appDefaultFallback(dir); got != "custom/backup" {
		t.Fatalf("explicit fallback=%q", got)
	}
}
