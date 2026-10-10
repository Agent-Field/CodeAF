package placegraph

import (
	"fmt"
	"testing"
	"time"
)

func TestAParentDotComesFromItsChildren(t *testing.T) {
	s, _ := newStore(t)
	top := mk(t, s, "Top")
	mid := mk(t, s, "Mid", top.ID)
	leaf := mk(t, s, "Leaf", mid.ID)
	other := mk(t, s, "Other")
	for chat, pl := range map[string]string{"c1": leaf.ID, "c2": mid.ID, "c3": other.ID} {
		if _, _, err := s.AddChat(chat, pl, AddedByYou); err != nil {
			t.Fatal(err)
		}
	}
	r := Rollup(snap(t, s), map[string]ChatState{
		"c1": {NeedsYou: 2, Failed: true},
		"c2": {NeedsYou: 1, Running: true},
		"c3": {},
	})
	got := r.Places[top.ID]
	if got.NeedsYou != 3 || !got.Failed || !got.Running || got.ChatsAll != 2 || got.Chats != 0 || got.Children != 1 || got.Descendants != 2 {
		t.Fatalf("top = %+v", got)
	}
	if want := []Origin{{mid.ID, 1}, {leaf.ID, 2}}; fmt.Sprint(got.Origins) != fmt.Sprint(want) {
		t.Fatalf("origins = %v want %v", got.Origins, want)
	}
	if o := r.Places[other.ID]; o.NeedsYou != 0 || o.Failed || o.Running || o.Chats != 1 || len(o.Origins) != 0 {
		t.Fatalf("other leaked: %+v", o)
	}
	if root := r.Places[RootID]; root.NeedsYou != 3 || root.ChatsAll != 3 {
		t.Fatalf("root = %+v", root)
	}
}

func TestADiamondCountsAChatOnce(t *testing.T) {
	s, _ := newStore(t)
	top := mk(t, s, "Top")
	a := mk(t, s, "A", top.ID)
	b := mk(t, s, "B", top.ID)
	for _, p := range []string{a.ID, b.ID} {
		if _, _, err := s.AddChat("shared", p, AddedByYou); err != nil {
			t.Fatal(err)
		}
	}
	r := Rollup(snap(t, s), map[string]ChatState{"shared": {NeedsYou: 1}})
	got := r.Places[top.ID]
	if got.NeedsYou != 1 || got.ChatsAll != 1 || len(got.Origins) != 1 || got.Origins[0].NeedsYou != 1 {
		t.Fatalf("diamond double-counted: %+v", got)
	}
	if r.Places[a.ID].NeedsYou != 1 || r.Places[b.ID].NeedsYou != 1 {
		t.Fatal("each side still shows the chat")
	}
}

func TestTotalsCountTopLevelAllAndUnplaced(t *testing.T) {
	s, _ := newStore(t)
	a := mk(t, s, "A")
	mk(t, s, "B")
	mk(t, s, "C", a.ID)
	gone := mk(t, s, "Gone")
	if _, _, err := s.AddChat("c1", a.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddChat("c2", gone.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive(gone.ID); err != nil {
		t.Fatal(err)
	}
	r := Rollup(snap(t, s), map[string]ChatState{"c1": {}, "c2": {}, "c3": {}})
	// c2's only place is archived, so it is unplaced, like Unplaced() says.
	if r.Totals != (Totals{TopLevel: 2, All: 3, Unplaced: 2}) {
		t.Fatalf("totals = %+v", r.Totals)
	}
	if _, ok := r.Places[gone.ID]; ok {
		t.Fatal("archived place got a status")
	}
}

func TestRollupOfTwoHundredPlacesIsLinear(t *testing.T) {
	st := State{}
	states := map[string]ChatState{}
	prev := ""
	for i := 0; i < 200; i++ {
		p := Place{ID: fmt.Sprintf("pl_%d", i), Name: fmt.Sprint(i)}
		if prev != "" {
			p.Parents = []string{prev}
		}
		st.Places = append(st.Places, p)
		chat := fmt.Sprintf("c%d", i)
		st.Memberships = append(st.Memberships, Membership{ChatID: chat, PlaceID: p.ID})
		states[chat] = ChatState{NeedsYou: 1}
		prev = p.ID
	}
	sn := newSnapshot(&st)
	start := time.Now()
	r := Rollup(sn, states)
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("200-place chain took %v", d)
	}
	if got := r.Places["pl_0"]; got.NeedsYou != 200 || got.ChatsAll != 200 || got.Descendants != 199 {
		t.Fatalf("head = %+v", got)
	}
}
