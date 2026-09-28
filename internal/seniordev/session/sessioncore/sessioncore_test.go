//go:build !windows

package sessioncore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/storage"
)

func newTestService(t *testing.T) (*Service, *bus.Bus) {
	t.Helper()
	b := bus.New(bus.Context{}, bus.WithIDGenerator(func() string { return "evt_test" }))
	s, err := New(Options{
		Store: storage.New(t.TempDir()), Bus: b, ProjectID: "p", Worktree: "/work",
		Directory: "/work/sub", WorkspaceID: "wrk", Version: "v",
		Now:  func() time.Time { return time.UnixMilli(1_722_124_923_004) },
		Slug: func() string { return "slug" },
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, b
}

func TestLifecycleAndEventOrder(t *testing.T) {
	s, b := newTestService(t)
	var mu sync.Mutex
	var events []string
	unsub := b.SubscribeAllCallback(func(p bus.Payload) {
		mu.Lock()
		events = append(events, p.Type)
		mu.Unlock()
	})
	defer unsub()
	ctx := context.Background()
	original, err := s.Create(ctx, CreateInput{ID: "ses_original"})
	if err != nil {
		t.Fatal(err)
	}
	user := msgmodel.User{
		MessageBase: msgmodel.MessageBase{ID: "msg_1", SessionID: original.ID},
		Time:        msgmodel.TimeCreated{Created: 1}, Agent: "coder",
		Model: msgmodel.UserModel{ProviderID: "p", ModelID: "m"},
	}
	if err := s.UpdateMessage(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePart(ctx, msgmodel.TextPart{
		PartBase: msgmodel.PartBase{ID: "prt_1", SessionID: original.ID, MessageID: user.ID},
		Text:     "hello",
	}); err != nil {
		t.Fatal(err)
	}
	messages, err := s.Messages(ctx, original.ID)
	if err != nil || len(messages) != 1 || len(messages[0].Parts) != 1 {
		t.Fatalf("messages = %#v, %v", messages, err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{
		EventCreated.Type, EventUpdated.Type,
		msgmodel.EventMessageUpdated, msgmodel.EventMessagePartUpdated,
	}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for index := range want {
		if events[index] != want[index] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}
}
func TestPartDeltaIsBusOnly(t *testing.T) {
	s, b := newTestService(t)
	seen := make(chan msgmodel.PartDeltaEvent, 1)
	unsub := b.SubscribeCallback(EventMessagePartDelta, func(p bus.Payload) {
		raw, _ := json.Marshal(p.Properties)
		var event msgmodel.PartDeltaEvent
		_ = json.Unmarshal(raw, &event)
		seen <- event
	})
	defer unsub()
	s.UpdatePartDelta(context.Background(), msgmodel.PartDeltaEvent{
		SessionID: "s", MessageID: "m", PartID: "p", Field: "text", Delta: "<&",
	})
	select {
	case event := <-seen:
		if event.Delta != "<&" {
			t.Fatalf("event %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing delta")
	}
}

func TestMessageWithPartsNeverLeavesEmptyUserTurnContract(t *testing.T) {
	// A crash/failure boundary may leave an orphan part, but never a visible
	// user message without its part.
	s, _ := newTestService(t)
	blocker := filepath.Join(s.store.Dir, "message", "ses")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	user := msgmodel.User{
		MessageBase: msgmodel.MessageBase{ID: "msg", SessionID: "ses"},
		Time:        msgmodel.TimeCreated{Created: 1}, Agent: "coder",
	}
	part := msgmodel.TextPart{
		PartBase: msgmodel.PartBase{ID: "part", SessionID: "ses", MessageID: "msg"},
		Text:     "prompt",
	}
	if err := s.UpdateMessageWithParts(context.Background(), user, part); err == nil {
		t.Fatal("blocked message write succeeded")
	}
	if _, err := os.Stat(filepath.Join(s.store.Dir, "part", "ses", "msg", "part.json")); err != nil {
		t.Fatalf("part was not written first: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.store.Dir, "message", "ses", "msg.json")); err == nil {
		t.Fatalf("empty message became visible: %v", err)
	}
}

func TestSessionPatchAtomicAcrossServicesContract(t *testing.T) {
	// Two session services sharing flat storage cannot lose independent
	// fields through an unlocked read/modify/write.
	root := t.TempDir()
	makeService := func() *Service {
		service, err := New(Options{
			Store: storage.New(root), Bus: bus.New(bus.Context{}), ProjectID: "p",
			Directory: "/work", Worktree: "/work", Slug: func() string { return "slug" },
		})
		if err != nil {
			t.Fatal(err)
		}
		return service
	}
	first, second := makeService(), makeService()
	created, err := first.Create(context.Background(), CreateInput{ID: "ses_atomic", Title: "base"})
	if err != nil {
		t.Fatal(err)
	}
	firstEntered, releaseFirst, firstDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	secondEntered, releaseSecond, secondDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		firstDone <- first.patch(context.Background(), created.ID, func(info *Info) {
			close(firstEntered)
			<-releaseFirst
			info.Title = "preserved title"
		})
	}()
	<-firstEntered
	archived := 42.0
	go func() {
		secondDone <- second.patch(context.Background(), created.ID, func(info *Info) {
			close(secondEntered)
			<-releaseSecond
			info.Time.Archived = &archived
		})
	}()
	select {
	case <-secondEntered:
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	<-secondEntered
	close(releaseSecond)
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	got, err := first.Get(context.Background(), created.ID)
	if err != nil || got.Title != "preserved title" || got.Time.Archived == nil || *got.Time.Archived != archived {
		t.Fatalf("atomic session patch = %+v, %v", got, err)
	}
}
