package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// fakeSayAgent is a conversation that records what it was told.
type fakeSayAgent struct {
	tui3.Agent
	said []string
	fail error
}

func (f *fakeSayAgent) Submit(_ context.Context, text string) (<-chan session.Event, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	f.said = append(f.said, text)
	ch := make(chan session.Event)
	close(ch)
	return ch, nil
}

// sayLab is a Say door over one item's conversation at chat, with live as
// the conversation open in this process (nil for none) and open counting
// the window's door.
func sayLab(t *testing.T, live *fakeSayAgent) (*sayDoor, string, *int, *fakeSayAgent) {
	t.Helper()
	chat := filepath.Join(t.TempDir(), "transcript.jsonl")
	opened := 0
	window := &fakeSayAgent{}
	d := &sayDoor{
		talk: func(_ context.Context, id int) (string, error) {
			if id != 7 {
				return "", errors.New("no such item")
			}
			return chat, nil
		},
		live: func(c string) (sayTurn, bool) {
			if live == nil || c != chat {
				return nil, false
			}
			return live, true
		},
		open: func(_, transcript string) (tui3.Conversation, error) {
			opened++
			return tui3.Conversation{Agent: window, SessionFile: transcript}, nil
		},
		folder: func(string) string { return "" },
	}
	return d, chat, &opened, window
}

// A CONVERSATION LIVE IN THIS PROCESS TAKES THE WORDS: nothing is opened.
func TestFactorySaySubmitsToTheLiveConversation(t *testing.T) {
	live := &fakeSayAgent{}
	d, _, opened, _ := sayLab(t, live)
	if err := d.say(context.Background(), 7, "  keep the old flag "); err != nil {
		t.Fatal(err)
	}
	if len(live.said) != 1 || live.said[0] != "keep the old flag" || *opened != 0 {
		t.Fatalf("the live conversation was told %v, the window's door opened %d", live.said, *opened)
	}
}

// A CONVERSATION NOBODY HOLDS IS OPENED through the window's own door, once,
// kept, and told the words; the next say goes to the one kept; and the
// window asking its door for that transcript (`T`) is handed the very same
// conversation instead of opening a second one.
func TestFactorySayOpensOnceAndTheWindowAttaches(t *testing.T) {
	d, chat, opened, window := sayLab(t, nil)
	if err := d.say(context.Background(), 7, "what is this issue about?"); err != nil {
		t.Fatal(err)
	}
	if err := d.say(context.Background(), 7, "and the tests?"); err != nil {
		t.Fatal(err)
	}
	if *opened != 1 || len(window.said) != 2 {
		t.Fatalf("opened %d times, the conversation was told %v", *opened, window.said)
	}
	other := 0
	open := d.adopt(func(_, transcript string) (tui3.Conversation, error) {
		other++
		return tui3.Conversation{Agent: &fakeSayAgent{}, SessionFile: transcript}, nil
	})
	conv, err := open("", chat)
	if err != nil || conv.Agent != window || other != 0 {
		t.Fatalf("the window was handed %v (err %v, its own door asked %d times)", conv.Agent, err, other)
	}
	if _, err := open("", filepath.Join(t.TempDir(), "another.jsonl")); err != nil || other != 1 {
		t.Fatalf("another transcript did not go to the window's own door: %v, %d", err, other)
	}
	if _, err := open("", chat); err != nil || other != 2 {
		t.Fatal("the door kept a conversation it had handed to the window")
	}
}

// NO WORDS, NO ITEM: the door refuses and opens nothing.
func TestFactorySayRefusesNothingAndNoItem(t *testing.T) {
	d, _, opened, _ := sayLab(t, nil)
	if err := d.say(context.Background(), 7, "   "); err == nil {
		t.Fatal("empty words were sent")
	}
	if err := d.say(context.Background(), 9, "hello"); err == nil {
		t.Fatal("words to an item the floor does not have were sent")
	}
	if *opened != 0 {
		t.Fatalf("the window's door opened %d times", *opened)
	}
}

// THE DOOR IS HUNG ONLY WHERE IT CAN WORK: a window with a Talk door and an
// Open door gets Say and an Open that attaches; one without either gets
// neither.
func TestFactoryWireSay(t *testing.T) {
	var bare tui3.Options
	factoryWireSay(&bare)
	if bare.Factory.Say != nil || bare.Open != nil {
		t.Fatal("a window with no doors got a Say door")
	}
	var full tui3.Options
	full.Factory.Talk = func(context.Context, int) (string, error) { return "", nil }
	full.Open = func(string, string) (tui3.Conversation, error) { return tui3.Conversation{}, nil }
	factoryWireSay(&full)
	if full.Factory.Say == nil || !full.Factory.Has("say") || full.Open == nil {
		t.Fatal("a window with Talk and Open got no Say door")
	}
}
