package tui3

import (
	"strings"
	"testing"
)

// Removing a place must remove every advertised way into it while keeping
// Home's search available through the ordinary keyboard route.
func TestSearchIsAbsentFromNavigationAndCommands(t *testing.T) {
	a := placeApp(t)
	drive(t, a, key(placeMapKey))
	if text := placeFrameText(a); strings.Contains(text, "9 search") {
		t.Fatalf("the map still offers Search:\n%s", text)
	}
	for _, p := range placeOrder {
		if p.word() == "search" {
			t.Fatal("the navigation still offers Search")
		}
	}
	for _, chord := range []string{"alt+9", "ctrl+9"} {
		drive(t, a, key(chord))
		if !a.at(pageHome) {
			t.Fatalf("%s left Home for a removed destination", chord)
		}
	}
	if hits := placeMatches("search"); len(hits) != 0 {
		t.Fatalf("Home still offers a Search place: %v", hits)
	}
	if _, ok := parsePageWord("search"); ok {
		t.Fatal("Search is still a typed destination")
	}
	if sheet := helpText("", chordSpelling{meta: chordAltWord}); strings.Contains(sheet, "/search") {
		t.Fatal("help still advertises /search")
	}
	for _, home := range []bool{false, true} {
		b := newTestApp(&fakeAgent{model: "m"})
		box, menu := &b.input, &b.menu
		if home {
			b = placeApp(t)
			box, menu = &b.home.box, &b.home.cmd
		}
		box.setText("/search")
		menu.sync(box)
		if len(menu.hits) != 0 {
			t.Fatalf("/search still has command completions (home=%v)", home)
		}
	}
	b := newTestApp(&fakeAgent{model: "m"})
	typeLine(t, b, "/search")
	if got := lastNote(t, b); got != unknownCommandWord("search") {
		t.Fatalf("/search did not use the normal unknown-command response: %q", got)
	}
}

func TestHomeStillFindsConversationsAfterSearchPlaceRemoval(t *testing.T) {
	a := placeApp(t)
	typeInto(t, a, "pricing")
	text := homeBodyText(a)
	if !strings.Contains(strings.ToLower(text), "pricing research") || strings.Contains(strings.ToLower(text), "porting the picker") {
		t.Fatalf("Home did not filter its conversation list:\n%s", text)
	}
	drive(t, a, key("esc"))
	if !a.at(pageHome) || !a.home.box.empty() {
		t.Fatal("Escape did not clear Home's search in place")
	}
}
