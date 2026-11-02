package main

import (
	"testing"
	"time"

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
