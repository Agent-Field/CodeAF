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

// WITH NO TALK DOOR THERE IS NO KEY: the hint does not name `T chat`, on the
// floor or on the item page, and `T` asks nothing.
func TestFactoryTalkWithNoDoorDrawsNoKey(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 8)
	for _, page := range []bool{false, true} {
		if page {
			drive(t, a, key("enter"))
		}
		if hint := (placeFactory{}).hint(a) + " · " + factoryVerbsShown(t, a); strings.Contains(hint, "T chat") {
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
// and from the floor the person lands in the conversation it answered, and
// `esc` on its empty box is the floor again on the same row; the way back is
// taken once. ON THE ITEM PAGE `T` is the manager's chat in the center, the
// keys in its box, and the page never leaves (factory_host.go).
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
		drive(t, a, key("T"))
		if *made != 1 || opened != chat {
			t.Fatalf("T made %d conversations and opened %q, want one and %q", *made, opened, chat)
		}
		if page {
			if !a.at(pageFactory) || !a.fp.open || !a.factoryHosting() || !a.fp.box {
				t.Fatalf("T on the item page: factory %v, page %v, hosting %v, box %v", a.at(pageFactory), a.fp.open, a.factoryHosting(), a.fp.box)
			}
			continue
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

// THE WAY BACK DOES NOT WAIT FOR AN EMPTY CONVERSATION: once the model has
// answered, the conversation has turns a rewind could cut, and the first `esc`
// would otherwise only arm it (`esc again to rewind`). In the conversation `T`
// opened the first `esc` goes back to the floor all the same.
func TestFactoryTalkEscReturnsAfterTheModelHasAnswered(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	seam, _, chat := talkSeam(t, f)
	a.factory = seam
	a.open = func(where, file string) (Conversation, error) {
		agent := &rewindFake{fakeAgent: &fakeAgent{model: "m", past: rewindPast()}}
		return Conversation{Agent: agent, SessionFile: file, Workspace: where}, nil
	}
	factoryOn(t, a, 8)
	drive(t, a, key("T"))
	if a.pageShowing() || a.convKey(a.file) != a.convKey(chat) {
		t.Fatalf("T did not land in the conversation: page %v", a.page)
	}
	if _, ok := a.rewinder(); !ok || !a.rewindReady() {
		t.Fatal("the lab conversation cannot rewind, so the test proves nothing")
	}
	drive(t, a, key("esc"))
	if !a.at(pageFactory) {
		t.Fatalf("the first esc did not go back to the floor (page %v)", a.page)
	}
	if a.rewindArmed() {
		t.Fatal("the way back left the rewind armed")
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

// THE ITEM CARD folds like the recipe card: one question while it stands —
// the head built from what changes, the body before and after — and one foot
// afterwards: changed, kept as it was, changed in words, expired.
func TestFactoryItemCardSettles(t *testing.T) {
	notice := session.ItemNotice{ID: "g1", Item: 1, Ref: "#1", Cap: 8, Why: "a second review round",
		Before: session.ItemFacts{Stages: []string{"plan", "approve", "write", "test", "proof"}, Cap: 5},
		After:  session.ItemFacts{Stages: []string{"plan", "approve", "write", "test", "proof"}, Cap: 8}}
	for _, c := range []struct {
		name   string
		settle func(a *app)
		want   string
	}{
		{"yes", func(a *app) {
			d := notice
			d.Decided = &session.ItemAnswer{Approved: true}
			a.itemProposal(session.Event{Kind: session.EventItemProposal, FactoryItem: &d})
			n := notice
			a.itemChanged(session.Event{Kind: session.EventItemChanged, FactoryItem: &n})
		}, "changed"},
		{"no", func(a *app) {
			d := notice
			d.Decided = &session.ItemAnswer{}
			a.itemProposal(session.Event{Kind: session.EventItemProposal, FactoryItem: &d})
		}, "kept as it was"},
		{"words", func(a *app) {
			d := notice
			d.Decided = &session.ItemAnswer{Change: "make it $10"}
			a.itemProposal(session.Event{Kind: session.EventItemProposal, FactoryItem: &d})
		}, factoryChangedWord},
		{"window", func(a *app) {
			d := notice
			d.Withdrawn = "nothing changed on the item"
			a.itemProposal(session.Event{Kind: session.EventItemProposal, FactoryItem: &d})
		}, "expired · nothing changed"},
	} {
		a := placeApp(t)
		raised := notice
		a.itemProposal(session.Event{Kind: session.EventItemProposal, FactoryItem: &raised})
		card := a.factoryCardFor("g1", "")
		if card == nil || card.change == nil {
			t.Fatalf("%s: the card was not drawn", c.name)
		}
		rows := plain(strings.Join(FactoryCardRows(a, card, 100, false), "\n"))
		for _, want := range []string{"#1 · raise the budget to $8?", "budget  $5 → $8", "why: a second review round"} {
			if !strings.Contains(rows, want) {
				t.Fatalf("%s: the standing card lacks %q:\n%s", c.name, want, rows)
			}
		}
		if strings.Contains(rows, "now:") {
			t.Fatalf("%s: a card whose stages do not move says them:\n%s", c.name, rows)
		}
		c.settle(a)
		if got := factoryCardWord(card); got != c.want {
			t.Errorf("%s: the card's foot says %q, want %q", c.name, got, c.want)
		}
	}
}

// factoryActionLine is the last words on the item page's right pane: the
// action line under the row the rail stands on.
func factoryActionLine(a *app) string {
	last := ""
	for _, row := range factoryBodyPlain(a, a.width, 30) {
		if _, right, div := factorySplitAt(row); div >= 0 && strings.TrimSpace(right) != "" {
			last = strings.TrimSpace(right)
		}
	}
	return last
}

// factoryIssueHint is the item page's hint with the rail on the issue row.
func factoryIssueHint(t *testing.T, a *app) string {
	t.Helper()
	factoryRowNamed(t, a, "issue")
	return (placeFactory{}).hint(a)
}

// `ENTER` ON THE ISSUE ROW OF A NEW ITEM OPENS ITS OWN CONVERSATION, IN THE
// CENTER (owner, 2026-10-08: "enter does not seem to take me to a
// conversation"; owner's layout, 2026-10-09: the manager's chat is the
// center): the Talk door is asked, the cursor lands on the manager row, the
// chat is the center with the keys in its box, and the page never leaves.
// `esc` gives the keys back to the page.
func TestFactoryEnterOnTheIssueRowOpensTheItemsConversation(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	seam, made, chat := talkSeam(t, f)
	a.factory = seam
	var opened string
	a.open = func(where, file string) (Conversation, error) {
		opened = file
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: file, Workspace: where}, nil
	}
	factoryOn(t, a, 4)
	drive(t, a, key("enter"))
	if !a.fp.open {
		t.Fatal("enter on the floor row did not open the item page")
	}
	hint := factoryIssueHint(t, a)
	if !strings.Contains(hint, " · enter chat · ") {
		t.Fatalf("the issue row's hint does not say enter chat: %q", hint)
	}
	drive(t, a, key("enter"))
	if got := f.said(); len(got) != 1 || !strings.HasPrefix(got[0], "Talk(4") {
		t.Fatalf("enter on the issue row asked %v, want the Talk door for #1662", got)
	}
	it, _ := a.factoryCursorItem()
	if r, _ := a.factoryPageRowAt(it); r.kind != factoryPageManager {
		t.Fatalf("enter on the issue row left the cursor on %v, not the manager", r.kind)
	}
	if *made != 1 || opened != chat || !a.at(pageFactory) || !a.factoryHosting() || !a.fp.box {
		t.Fatalf("enter on the issue row made %d and opened %q (factory %v, hosting %v, box %v), want %q", *made, opened, a.at(pageFactory), a.factoryHosting(), a.fp.box, chat)
	}
	drive(t, a, key("esc"))
	if !a.at(pageFactory) || !a.fp.open || a.fp.box {
		t.Fatalf("esc did not give the keys back on the item page (page %v, open %v, box %v)", a.page, a.fp.open, a.fp.box)
	}
}

// A STAGE WITH A ROOM STILL OPENS THE ROOM with a Talk door on the seam:
// `enter` on the issue row is the item's conversation, on a stage its room.
func TestFactoryEnterOnARoomStillOpensTheRoomWithATalkDoor(t *testing.T) {
	room := factoryStageChat(t)
	f := &factoryFake{}
	factoryShapeItem(f, 2, func(it *factory.Item) { it.Stream.Phases[0].Chat = room })
	a := factoryVerbLab(t, f)
	seam, made, _ := talkSeam(t, f)
	a.factory = seam
	var opened string
	a.open = func(where, file string) (Conversation, error) {
		opened = file
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: file, Workspace: where}, nil
	}
	factoryOn(t, a, 2)
	drive(t, a, key("enter"))
	factoryRowNamed(t, a, "plan")
	if hint := (placeFactory{}).hint(a); strings.Contains(hint, "enter chat") {
		t.Fatalf("a stage's hint names the issue row's enter: %q", hint)
	}
	drive(t, a, key("enter")) // dives into the stage's story
	drive(t, a, key("enter")) // opens the conversation itself
	if opened != room || *made != 0 {
		t.Fatalf("enter on the room opened %q and asked Talk %d times, want the room and none", opened, *made)
	}
}

// WITH NO TALK DOOR AND A GITHUB DOOR, `enter` on the issue row opens the
// item's page through `g`'s door, and the hint says `enter open on github`.
func TestFactoryEnterOnTheIssueRowOpensGitHubWithoutATalkDoor(t *testing.T) {
	a, lab := newFactoryPolishLab(t)
	factoryOn(t, a, 4)
	drive(t, a, key("enter"))
	if hint := factoryIssueHint(t, a); !strings.Contains(hint, " · enter open on github · ") {
		t.Fatalf("the issue row's hint does not say enter open on github: %q", hint)
	}
	drive(t, a, key("enter"))
	if len(lab.opened) != 1 || lab.opened[0] != "https://github.com/agentfield/codeaf/pull/1662" {
		t.Fatalf("enter on the issue row opened %q", lab.opened)
	}
	if a.pageMsg != "opened #1662 on github" {
		t.Fatalf("the note is %q", a.pageMsg)
	}
}

// WITH NEITHER DOOR, `enter` on the issue row says so on the action line, and
// the hint names no `enter` there.
func TestFactoryEnterOnTheIssueRowWithNothingToOpenSaysSo(t *testing.T) {
	a, lab := newFactoryPolishLab(t)
	a.factory.Open = nil
	factoryOn(t, a, 4)
	drive(t, a, key("enter"))
	if hint := factoryIssueHint(t, a); strings.Contains(hint, "enter ") {
		t.Fatalf("an enter that opens nothing is named: %q", hint)
	}
	drive(t, a, key("enter"))
	if got := factoryActionLine(a); got != "nothing to open yet · r run" {
		t.Fatalf("the action line is %q", got)
	}
	if len(lab.opened) != 0 || !a.fp.open {
		t.Fatalf("enter with nothing to open opened %q or left the page", lab.opened)
	}
	drive(t, a, key("down"))
	if got := factoryActionLine(a); got == "nothing to open yet · r runs it" {
		t.Fatal("moving the cursor did not put the keys back")
	}
}
