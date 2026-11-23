package factory_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE ITEM'S CONVERSATION IS MADE ONCE: the first ask makes it and keeps it on
// the item, and every later ask answers the same file without making another.
func TestTalkMakesTheItemsConversationOnceAndAnswersItAgain(t *testing.T) {
	_, st := openLocal(t)
	made := 0
	var seen factory.Item
	seam := factory.LocalSeam(st, time.Now(), factory.WithTalk(func(_ context.Context, it factory.Item) (string, error) {
		made++
		seen = it
		return "/sessions/talk-1/transcript.jsonl", nil
	}))
	id, err := seam.New("agentfield/codeaf", "fix the ledger double count")
	if err != nil {
		t.Fatal(err)
	}
	first, err := seam.Talk(context.Background(), id)
	if err != nil || first != "/sessions/talk-1/transcript.jsonl" {
		t.Fatalf("the first ask answered %q, %v", first, err)
	}
	second, err := seam.Talk(context.Background(), id)
	if err != nil || second != first {
		t.Fatalf("the second ask answered %q, %v, want %q", second, err, first)
	}
	if made != 1 {
		t.Fatalf("the conversation was made %d times, want once", made)
	}
	if seen.ID != id || seen.Title != "fix the ledger double count" {
		t.Fatalf("the maker was handed %+v", seen)
	}
	snap, err := seam.Load()
	if err != nil || len(snap.Items) != 1 || snap.Items[0].Talk != first {
		t.Fatalf("the item does not keep its conversation: %+v, %v", snap.Items, err)
	}
}

// NO CONVERSATION BY DEFAULT, AND NO DOOR WITHOUT A MAKER: an item is made
// without one, and a seam built with no maker has no Talk door at all.
func TestTalkIsNeverMadeByDefaultAndAbsentWithoutAMaker(t *testing.T) {
	_, st := openLocal(t)
	seam := factory.LocalSeam(st, time.Now())
	if seam.Has("talk") || seam.Talk != nil {
		t.Fatal("a seam with no maker has a Talk door")
	}
	id, err := seam.New("agentfield/codeaf", "fix the ledger double count")
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := seam.Load()
	if len(snap.Items) != 1 || snap.Items[0].ID != id || snap.Items[0].Talk != "" {
		t.Fatalf("a new item already has a conversation: %+v", snap.Items)
	}
}
