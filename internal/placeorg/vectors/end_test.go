package vectors

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func TestEndTextMatchesTheV2Tail(t *testing.T) {
	for _, tc := range []struct {
		name  string
		users []string
		reply string
		want  string
	}{
		{"no messages", nil, "reply", ""},
		{"one message", []string{"first"}, "reply", ""},
		{"last two later users", []string{"first", "discard", "second", "third"}, "reply", "Title\nsecond third reply"},
		{"no reply", []string{"first", "second"}, "", "Title\nsecond"},
		{"empty tail", []string{"first", " "}, "", ""},
		{"unicode bounds", []string{"first", strings.Repeat("界", 450)}, strings.Repeat("語", 350), "Title\n" + strings.Repeat("界", 299) + " " + strings.Repeat("語", 300)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := EndText("Title", tc.users, tc.reply); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEndAnchorsAreIndependentAndStructureUsesOnlyStart(t *testing.T) {
	dir := t.TempDir()
	c := corpus(2)
	c.Chats[0].EndText = EndText("Title", []string{"first", "tail"}, "reply")
	p := &fake{space: placeorg.SpacePrimary}
	vz := New(dir)
	_, start, _, err := vz.Vectorize(context.Background(), c, p, nil)
	if err != nil || len(start) != 2 || p.texts.Load() != 3 {
		t.Fatalf("first run: vectors=%d texts=%d err=%v", len(start), p.texts.Load(), err)
	}
	cache, err := OpenEnd(dir, p.Space(), p.Dims())
	if err != nil {
		t.Fatal(err)
	}
	end, ok := cache.Get(c.Chats[0].ID, HashText(c.Chats[0].EndText))
	if !ok || end[len(c.Chats[0].EndText)] != 1 {
		t.Fatal("END not separately cached")
	}
	if _, ok := cache.Get(c.Chats[1].ID, HashText("")); ok {
		t.Fatal("single-message chat has END")
	}
	cache.Close()
	warm := &fake{space: p.Space()}
	if _, _, _, err := New(dir).Vectorize(context.Background(), c, warm, nil); err != nil || warm.calls.Load() != 0 {
		t.Fatalf("warm run refetched: calls=%d err=%v", warm.calls.Load(), err)
	}
	c.Chats[0].EndText = "changed ending"
	changed := &fake{space: p.Space()}
	_, again, _, err := vz.Vectorize(context.Background(), c, changed, nil)
	if err != nil || !reflect.DeepEqual(start, again) || !reflect.DeepEqual(changed.seenTxt, []string{"changed ending"}) {
		t.Fatalf("tail edit changed START or refetched it: seen=%v err=%v", changed.seenTxt, err)
	}
	c.Chats[0].Text = "changed start"
	changed = &fake{space: p.Space()}
	if _, _, _, err := vz.Vectorize(context.Background(), c, changed, nil); err != nil || !reflect.DeepEqual(changed.seenTxt, []string{"changed start"}) {
		t.Fatalf("START edit refetched END: seen=%v err=%v", changed.seenTxt, err)
	}
}

func TestEndFailureFallsBackBothAnchorsToOneSpace(t *testing.T) {
	dir, c := t.TempDir(), corpus(1)
	c.Chats[0].EndText = "end"
	p := &fake{space: placeorg.SpacePrimary, failOn: 2}
	local := &fake{space: placeorg.SpaceLocal}
	space, start, fell, err := New(dir).Vectorize(context.Background(), c, p, local)
	if err != nil || !fell || space != placeorg.SpaceLocal || len(start) != 1 || local.texts.Load() != 2 {
		t.Fatalf("space=%s fell=%v local texts=%d err=%v", space, fell, local.texts.Load(), err)
	}
	cache, err := OpenEnd(dir, space, local.Dims())
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	if _, ok := cache.Get(c.Chats[0].ID, HashText(c.Chats[0].EndText)); !ok {
		t.Fatal("fallback END missing")
	}
}
