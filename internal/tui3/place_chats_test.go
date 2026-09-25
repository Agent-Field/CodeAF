package tui3

import (
	"strings"
	"testing"
)

// THE BAR HAS A WAY BACK TO THE CHATS, second after home, and a press on it,
// its digit and enter on it with the bar's cursor all do one thing: the place
// closes and the conversation that was in front is in front again.
func TestTheChatsOnTheBarGoBackToTheConversation(t *testing.T) {
	a := placeApp(t)
	front := a.file
	bar := plain(a.placeTabBar(a.width, false, a.pal))
	if h, c, k := strings.Index(bar, "home"), strings.Index(bar, "chats"), strings.Index(bar, pageTasks.word()); h < 0 || c < h || k < c {
		t.Fatalf("the bar does not read home, chats: %q", bar)
	}
	placeFrameText(a)
	var chats placeTabSpan
	for _, span := range a.tabs {
		if span.id == pageChats {
			chats = span
		}
	}
	if chats.to == 0 {
		t.Fatalf("the chats word has no span: %+v", a.tabs)
	}
	cmd, took := a.placeTabPress(chats.from+1, a.tabRow)
	if !took {
		t.Fatal("the press on chats was not taken")
	}
	spend(t, a, cmd)
	if a.pageShowing() || a.file != front {
		t.Fatalf("the press left the router on %q with %q in front", a.page.word(), a.file)
	}
	a.openHome()
	drive(t, a, key(placeChord(pageChats)))
	if a.pageShowing() || a.file != front || placeChord(pageChats) != "alt+2" {
		t.Fatalf("%s left the router on %q", placeChord(pageChats), a.page.word())
	}
	// And `tab`, which walks the rooms, steps over it.
	a.openHome()
	drive(t, a, key("tab"))
	if a.page != pageTasks {
		t.Fatalf("tab from home landed on %q", a.page.word())
	}
}

// WITH NO CONVERSATION OPEN THE WAY BACK IS A NEW CHAT.
func TestTheChatsWithNothingOpenIsANewChat(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.app("")
	a.width, a.height = 120, 30
	a.start = func(string) (Conversation, error) { return Conversation{}, nil }
	a.openHome()
	if n := len(a.tabList()); n != 0 {
		t.Fatalf("the fixture holds %d conversations", n)
	}
	drive(t, a, key(placeChord(pageChats)))
	if a.pageShowing() || !a.startingChat() {
		t.Fatalf("with nothing open the chats left %q up and no new chat (starting %v)", a.page.word(), a.startingChat())
	}
}
