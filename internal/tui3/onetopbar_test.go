package tui3

import (
	"strings"
	"testing"
)

// barGeometry reads one top-bar row: the row it is on, the column its first
// word starts in, and the blank cells between that word and the next item.
func barGeometry(frame, first string) (y, x, gap int) {
	for i, r := range strings.Split(plain(frame), "\n") {
		at := strings.Index(r, first)
		if at < 0 || strings.TrimSpace(r[:at]) != "" {
			continue
		}
		rest := r[at+len(first):]
		return i, at, len(rest) - len(strings.TrimLeft(rest, " "))
	}
	return -1, -1, -1
}

// ONE TOP BAR. The chat strip and the places bar are the same row: the first
// word on the same line and in the same column, the same air between two
// items, and the current item on the same ground, at every width a person uses.
func TestOneTopBarPlacesAndChatsShareGeometry(t *testing.T) {
	for _, width := range []int{80, 110, 160} {
		a, _, _, _ := trafficApp(t)
		a.open = func(workspace, transcript string) (Conversation, error) { return Conversation{}, nil }
		a.welcome.open = false
		a.railAway = true
		a.width, a.height = width, 24
		a.touch()
		chat, _, _ := a.frame()
		cy, cx, cgap := barGeometry(chat, "Home")
		places := placeApp(t)
		places.width = width
		frame, _, _ := places.frame()
		py, px, pgap := barGeometry(frame, "home")
		if cy < 0 || py < 0 {
			t.Fatalf("at %d a bar is missing (chat row %d, places row %d)", width, cy, py)
		}
		if cy != py || py != places.tabRow || cx != px || cx != headLabelAt+1 || cgap != pgap || pgap != 2*len(tabPad)+placeBarGap {
			t.Fatalf("at %d the bars differ: chat row %d x %d gap %d, places row %d x %d gap %d", width, cy, cx, cgap, py, px, pgap)
		}
		if !strings.Contains(frame, activeGround(places.pal, tabPad+"home"+tabPad)) {
			t.Fatalf("at %d the place you stand in is not on the chat strip's current ground", width)
		}
		if a.tabActivePaint(" x ") != activeGround(a.pal, " x ") {
			t.Fatal("the chat strip's tab in front has a ground of its own")
		}
	}
}
