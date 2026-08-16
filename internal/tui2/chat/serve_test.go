package chat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The real Serve path, end to end, with nothing faked between the composer and
// the journal except the provider itself.
//
// 13.2's P0 is a divergence between what the store holds and what the screen
// holds, and the only honest way to look for one is to put the real writer
// (internal/head, tailing its own cursors) on one side of the journal and the
// real reader (this package's poll) on the other. Everything the two disagree
// about — which session a reply is filed under, which watermark the next read
// starts from, whether the row that ends a turn is the row the surface was
// waiting for — is exercised here and nowhere else.

// scriptedModel is the provider, and only the provider. It answers in the
// head's own reply object so the surface's partial decoder sees the bytes it
// would see live.
type scriptedModel struct {
	mu        sync.Mutex
	replies   []string
	calls     int
	delay     time.Duration
	afterCall func()
}

func (m *scriptedModel) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	m.mu.Lock()
	m.calls++
	var text string
	if len(m.replies) > 0 {
		text, m.replies = m.replies[0], m.replies[1:]
	}
	delay, after := m.delay, m.afterCall
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	if after != nil {
		after()
	}
	if text == "" {
		return nil, errors.New("the script is spent")
	}
	return &ai.Response{Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
		FinishReason: "stop",
	}}}, nil
}

// waitForScreen polls the app the way the runtime does until want is on screen
// or the deadline passes. It returns the last frame either way, so a failure
// reports what the reader would actually have been looking at.
func waitForScreen(t *testing.T, app *App, want string) (string, bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	out := ""
	for time.Now().Before(deadline) {
		app.polling = true
		if cmd := app.pollCmd(); cmd != nil {
			if result, ok := cmd().(pollResultMsg); ok {
				app.applyPoll(result)
			}
		}
		app.polling = false
		out = frame(app)
		if strings.Contains(out, want) {
			return out, true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return out, false
}

func TestARealHeadsReplyReachesTheScreen(t *testing.T) {
	graph := openJournal(t)
	model := &scriptedModel{replies: []string{`{"reply":"the plan is to ship"}`}}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = head.New(model, graph).Serve(ctx)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})

	app := newJournalApp(t, graph, &fakeCommander{}, nil)
	send(t, app, "what is the plan")

	out, found := waitForScreen(t, app, "the plan is to ship")
	if !found {
		t.Fatalf("the head's reply never reached the screen:\n%s", out)
	}
	assertJournalOnScreen(t, graph, app)
	if app.turn.active {
		t.Fatalf("the awaiting line outlived the reply:\n%s", out)
	}
}

// The head answers a room it was never told about — a session id the surface
// chose — and the reply must come back filed under that same room.
func TestARealHeadsReplyIsFiledUnderTheRoomTheSurfaceIsWatching(t *testing.T) {
	graph := openJournal(t)
	model := &scriptedModel{replies: []string{`{"reply":"filed here"}`}}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = head.New(model, graph).Serve(ctx)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})

	app := newJournalApp(t, graph, &fakeCommander{}, nil)
	send(t, app, "which room is this")
	if _, found := waitForScreen(t, app, "filed here"); !found {
		t.Fatal("the reply never arrived")
	}

	messages, err := graph.Messages(testSession, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.SessionID != testSession {
			t.Fatalf("seq %d is filed under %q, not the room on screen", message.Seq, message.SessionID)
		}
	}
	if len(messages) < 2 {
		t.Fatalf("the room holds %d rows; the reply is not in it", len(messages))
	}
}

// 13.2's P0 on the real Serve path: a room with more history than one page, a
// real head answering into it, and a reply that must arrive on screen rather
// than only in the store.
func TestARealHeadsReplyIntoARoomWithAHistoryReachesTheScreen(t *testing.T) {
	graph := openJournal(t)
	for i := 0; i < 3*messagePage; i++ {
		reply(t, graph, "an older row")
	}
	model := &scriptedModel{replies: []string{`{"reply":"the answer at last"}`}}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = head.New(model, graph).Serve(ctx)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})

	app := newJournalApp(t, graph, &fakeCommander{}, nil)
	send(t, app, "a fresh question")

	out, found := waitForScreen(t, app, "the answer at last")
	if !found {
		t.Fatalf("the head's reply never reached the screen behind a room's history "+
			"(current-through=%d read-through=%d, turn still live=%v):\n%s",
			app.journal, app.watermark, app.turn.active, out)
	}
	if app.turn.active {
		t.Fatal("the awaiting line outlived the reply")
	}
	assertJournalOnScreen(t, graph, app)
}

// Two turns through the real head, back to back: the second reply must not be
// stranded behind the first turn's watermark.
func TestTwoRealTurnsBackToBackBothReachTheScreen(t *testing.T) {
	graph := openJournal(t)
	model := &scriptedModel{replies: []string{
		`{"reply":"first answer"}`,
		`{"reply":"second answer"}`,
	}}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = head.New(model, graph).Serve(ctx)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})

	app := newJournalApp(t, graph, &fakeCommander{}, nil)
	send(t, app, "first question")
	if out, found := waitForScreen(t, app, "first answer"); !found {
		t.Fatalf("the first reply never arrived:\n%s", out)
	}
	send(t, app, "second question")
	out, found := waitForScreen(t, app, "second answer")
	if !found {
		t.Fatalf("the second reply never arrived:\n%s", out)
	}
	assertJournalOnScreen(t, graph, app)
}
