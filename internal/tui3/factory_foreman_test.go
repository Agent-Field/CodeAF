package tui3

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// foremanSeam is the fake floor with a Foreman door that answers one real
// session folder, and counts how many times it was asked.
func foremanSeam(t *testing.T, f *factoryFake) (factory.Seam, *int, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "foreman-session")
	where := t.TempDir()
	if err := session.SaveMeta(dir, session.Meta{ID: "foreman-session", Workspace: where}); err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(chat, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	asked := 0
	seam := f.seam()
	seam.Foreman = func(context.Context) (string, error) {
		asked++
		_ = f.rec("Foreman")
		return chat, nil
	}
	return seam, &asked, chat
}

// WITH NO FOREMAN DOOR THERE IS NO KEY: `m` asks nothing and the floor stays.
func TestFactoryForemanWithNoDoorDrawsNoKey(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 8)
	if got := a.factoryForemanHint(); len(got) != 0 {
		t.Fatalf("a seam with no foreman names it: %v", got)
	}
	drive(t, a, key("m"))
	if got := f.said(); len(got) != 0 {
		t.Fatalf("m with no door asked %v", got)
	}
	if !a.at(pageFactory) {
		t.Fatal("m with no door left the floor")
	}
}

// `m` OPENS THE FOREMAN AND `esc` COMES BACK to the same row, as `T` does.
func TestFactoryForemanOpensTheChatAndEscReturns(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	seam, asked, chat := foremanSeam(t, f)
	a.factory = seam
	var opened string
	a.open = func(where, file string) (Conversation, error) {
		opened = file
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: file, Workspace: where}, nil
	}
	factoryOn(t, a, 8)
	if got := a.factoryForemanHint(); len(got) != 1 || got[0] != foremanHintWord {
		t.Fatalf("the hint clause is %v, want %q", got, foremanHintWord)
	}
	drive(t, a, key("m"))
	if *asked != 1 || opened != chat {
		t.Fatalf("m asked the door %d times and opened %q, want once and %q", *asked, opened, chat)
	}
	if a.pageShowing() || a.convKey(a.file) != a.convKey(chat) {
		t.Fatalf("m did not land in the foreman: page %v, file %q", a.page, a.file)
	}
	drive(t, a, key("esc"))
	if !a.at(pageFactory) {
		t.Fatalf("esc in the foreman did not go back to the floor (page %v)", a.page)
	}
	if it, ok := a.factoryCursorItem(); !ok || it.ID != 8 {
		t.Fatalf("back on %v, want item 8", it.ID)
	}
	if a.fp.act.talk != "" {
		t.Fatal("the way back was not spent by taking it")
	}
}

// MARKS FROM THE SNAPSHOT DRAW ON ROWS: an item the store marked wears the
// accent lead, even where the person had toggled it off before the store
// marked it, and an item the store unmarks loses it.
func TestFactoryForemanMarksFromTheSnapshotDrawOnRows(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	marked := a.pal.accent(a.icon(tokens.GDoneCell))
	item := func(id int) factory.Item {
		for _, it := range a.fp.snap.Items {
			if it.ID == id {
				return it
			}
		}
		t.Fatalf("item %d is not on the floor", id)
		return factory.Item{}
	}
	if a.factoryLead(item(7)) == marked {
		t.Fatal("item 7 is marked before anything marked it")
	}
	// The person toggled 7 off on the page; then the foreman marks 6 and 7.
	a.fp.marked = map[int]bool{7: false}
	read := func(ids ...int) {
		snap, err := a.factory.Load()
		if err != nil {
			t.Fatal(err)
		}
		snap.Marked = ids
		a.factoryFoldStoreMarks(a.fp.snap, &snap)
		a.factoryFold(snap)
	}
	read(6, 7)
	for _, id := range []int{6, 7} {
		if got := a.factoryLead(item(id)); got != marked {
			t.Errorf("item %d's lead is %q, want the accent mark", id, got)
		}
	}
	if ids := a.factoryMarkedIDs(); len(ids) != 2 {
		t.Errorf("L would launch %v, want items 6 and 7", ids)
	}
	read(6)
	if a.factoryLead(item(7)) == marked {
		t.Error("item 7 kept its mark after the store took it off")
	}
	// A mark on an item that is not new is never drawn as the floor's.
	read(6, 2)
	if item(2).Marked {
		t.Error("a running item took the store's mark")
	}
}
