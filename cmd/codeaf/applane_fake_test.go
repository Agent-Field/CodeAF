package main

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A FAKE'S LANES HOLD WHAT IS PUT ON THEM BEFORE ANYBODY WATCHES. The backend
// opens a conversation after create returns, so a test's first event can come
// first. A send on the lane the fake has not made yet blocks for ever, and all
// of cmd/codeaf then waits for the suite's timeout.
func TestAFakeLaneHoldsAnEventUntilItIsWatched(t *testing.T) {
	for _, tc := range []struct {
		name  string
		put   func(*fakeConversation, session.Event)
		watch func(*fakeConversation) <-chan session.Event
	}{
		{"questions", (*fakeConversation).raise, func(f *fakeConversation) <-chan session.Event {
			lane, _ := f.WatchQuestions()
			return lane
		}},
		{"tasks", (*fakeConversation).lane, func(f *fakeConversation) <-chan session.Event {
			lane, _ := f.WatchTaskUpdates()
			return lane
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conv := &fakeConversation{}
			put := make(chan struct{})
			go func() {
				defer close(put)
				tc.put(conv, session.Event{Text: "first"})
				tc.put(conv, session.Event{Text: "second"})
			}()
			select {
			case <-put:
			case <-time.After(5 * time.Second):
				t.Fatal("an event put before the watch blocked its sender")
			}
			lane := tc.watch(conv)
			tc.put(conv, session.Event{Text: "third"})
			for _, want := range []string{"first", "second", "third"} {
				select {
				case got := <-lane:
					if got.Text != want {
						t.Fatalf("got %q, want %q", got.Text, want)
					}
				case <-time.After(5 * time.Second):
					t.Fatalf("%q never reached the watcher", want)
				}
			}
		})
	}
}

// lateWatch is a conversation whose question lane opens only when told, which
// is the order a loaded machine can produce: the backend publishes the agent as
// live, and only afterwards starts watching.
type lateWatch struct {
	*fakeConversation
	open chan struct{}
}

func (c *lateWatch) WatchQuestions() (<-chan session.Event, func()) {
	<-c.open
	return c.fakeConversation.WatchQuestions()
}

// AN ASK RAISED AS SOON AS THE AGENT IS LIVE REACHES THE INBOX, through the real
// backend, even when the backend has not started watching yet.
func TestAnAskRaisedBeforeTheBackendWatchesStillArrives(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	conv := &lateWatch{fakeConversation: &fakeConversation{}, open: make(chan struct{})}
	var once sync.Once
	opened := func() { once.Do(func() { close(conv.open) }) }
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	t.Cleanup(opened)
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	backend.profileDir = t.TempDir()
	if _, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	q := session.Question{ID: 9, Kind: session.QuestionFuel, Head: "What next?", Options: session.AnswerOptions(session.QuestionFuel)}
	raised := make(chan struct{})
	go func() {
		defer close(raised)
		conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	}()
	select {
	case <-raised:
	case <-time.After(5 * time.Second):
		t.Fatal("raise blocked on a question lane the backend had not watched yet")
	}
	opened()
	rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		_, ok := data.(appwire.InboxAddedEvent)
		return ok
	})
}
