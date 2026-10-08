package tui3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// talkSeam is the fake floor with a Talk door that makes one conversation per
// item, in a real session folder whose meta names a real workspace, and counts
// how many it made.
func talkSeam(t *testing.T, f *factoryFake) (factory.Seam, *int, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "talk-session")
	where := t.TempDir()
	if err := session.SaveMeta(dir, session.Meta{ID: "talk-session", Workspace: where}); err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(chat, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	made := 0
	seam := f.seam()
	seam.Talk = func(_ context.Context, id int) (string, error) {
		made++
		_ = f.rec("Talk", id)
		return chat, nil
	}
	return seam, &made, chat
}

// WITH NO TALK DOOR THERE IS NO KEY: the hint does not name `T talk`, on the
// floor or on the item page, and `T` asks nothing.
func TestFactoryTalkWithNoDoorDrawsNoKey(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 8)
	for _, page := range []bool{false, true} {
		if page {
			drive(t, a, key("enter"))
		}
		if hint := (placeFactory{}).hint(a); strings.Contains(hint, "T talk") {
			t.Fatalf("a seam with no talk door names it (item page %v): %q", page, hint)
		}
		drive(t, a, key("T"))
		if got := f.said(); len(got) != 0 {
			t.Fatalf("T with no door asked %v", got)
		}
		if !a.at(pageFactory) {
			t.Fatal("T with no door left the floor")
		}
	}
}

// `T` OPENS THE ITEM'S CONVERSATION AND `esc` COMES BACK: the door is asked,
// the person lands in the conversation it answered, and `esc` on its empty box
// is the floor again on the same row, with the item page still open when `T`
// was pressed there. The way back is taken once.
func TestFactoryTalkOpensTheChatAndEscReturnsToTheSameRow(t *testing.T) {
	for _, page := range []bool{false, true} {
		f := &factoryFake{}
		a := factoryVerbLab(t, f)
		seam, made, chat := talkSeam(t, f)
		a.factory = seam
		var opened string
		a.open = func(where, file string) (Conversation, error) {
			opened = file
			return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: file, Workspace: where}, nil
		}
		factoryOn(t, a, 8)
		if page {
			drive(t, a, key("enter"))
		}
		if hint := (placeFactory{}).hint(a); !strings.Contains(hint, "T talk") {
			t.Fatalf("the hint does not name T talk (item page %v): %q", page, hint)
		}
		drive(t, a, key("T"))
		if *made != 1 || opened != chat {
			t.Fatalf("T made %d conversations and opened %q, want one and %q", *made, opened, chat)
		}
		if a.pageShowing() || a.convKey(a.file) != a.convKey(chat) {
			t.Fatalf("T did not land in the conversation: page %v, file %q", a.page, a.file)
		}
		drive(t, a, key("esc"))
		if !a.at(pageFactory) {
			t.Fatalf("esc in the item's conversation did not go back to the floor (page %v)", a.page)
		}
		if it, ok := a.factoryCursorItem(); !ok || it.ID != 8 || a.fp.open != page {
			t.Fatalf("back on %v, item page %v; want item 8, page %v", it.ID, a.fp.open, page)
		}
		if a.fp.act.talk != "" {
			t.Fatal("the way back was not spent by taking it")
		}
	}
}

// THE `factory` TEAM IS ONE FOLD: a hundred items' teams under it are one row
// on the open-team walk (the teams rail and the chats' team menu), until the
// person stands in it.
func TestFactoryTeamsFoldUnderOneRow(t *testing.T) {
	a := placeApp(t)
	a.wall.teams = []team{{ID: "factory-team", Name: "factory"}, {ID: "other", Name: "web"}}
	for i := 1; i <= 100; i++ {
		a.wall.teams = append(a.wall.teams, team{ID: "item-" + itoa(i), Name: "#" + itoa(i) + " · work", Parent: "factory-team"})
	}
	a.wall.loaded = true
	rows := a.teamsOpenTree()
	if len(rows) != 2 {
		t.Fatalf("the open-team walk has %d rows, want the factory row and web", len(rows))
	}
	a.tp.sel = "factory-team"
	if rows := a.teamsOpenTree(); len(rows) != 102 {
		t.Fatalf("standing on factory shows %d rows, want all 102", len(rows))
	}
	a.tp.sel = ""
	a.teamViews.id = "item-7"
	if rows := a.teamsOpenTree(); len(rows) != 102 {
		t.Fatalf("an item's team in front shows %d rows, want all 102", len(rows))
	}
}

// THE STAGES CARD folds like the recipe card: a question while it stands, and
// one foot afterwards — changed, not now, changed in words, expired.
func TestFactoryStagesCardSettles(t *testing.T) {
	notice := session.StagesNotice{ID: "g1", Item: 12, Ref: "#12", Add: []string{"after review, read it for auth holes"}, Skip: []string{"neaten"}, Why: "touches billing"}
	for _, c := range []struct {
		name   string
		settle func(a *app)
		want   string
	}{
		{"yes", func(a *app) {
			d := notice
			d.Decided = &session.StagesAnswer{Approved: true}
			a.stagesProposal(session.Event{Kind: session.EventStagesProposal, Stages: &d})
			n := notice
			n.Now = []string{"plan", "write"}
			a.stagesChanged(session.Event{Kind: session.EventStagesChanged, Stages: &n})
		}, "changed"},
		{"no", func(a *app) {
			d := notice
			d.Decided = &session.StagesAnswer{}
			a.stagesProposal(session.Event{Kind: session.EventStagesProposal, Stages: &d})
		}, "not now"},
		{"words", func(a *app) {
			d := notice
			d.Decided = &session.StagesAnswer{Change: "keep neaten"}
			a.stagesProposal(session.Event{Kind: session.EventStagesProposal, Stages: &d})
		}, factoryChangedWord},
		{"window", func(a *app) {
			d := notice
			d.Withdrawn = "nothing changed in the item's stages"
			a.stagesProposal(session.Event{Kind: session.EventStagesProposal, Stages: &d})
		}, "expired · nothing changed"},
	} {
		a := placeApp(t)
		raised := notice
		a.stagesProposal(session.Event{Kind: session.EventStagesProposal, Stages: &raised})
		card := a.factoryCardFor("g1", "")
		if card == nil || card.stages == nil {
			t.Fatalf("%s: the card was not drawn", c.name)
		}
		rows := strings.Join(FactoryCardRows(a, card, 100, false), "\n")
		if !strings.Contains(plain(rows), "wants to change #12's stages: +") || !strings.Contains(plain(rows), "−neaten") ||
			!strings.Contains(plain(rows), "stages · #12 · touches billing") {
			t.Fatalf("%s: the standing card reads %q", c.name, plain(rows))
		}
		c.settle(a)
		if got := factoryCardWord(card); got != c.want {
			t.Errorf("%s: the card's foot says %q, want %q", c.name, got, c.want)
		}
	}
}
