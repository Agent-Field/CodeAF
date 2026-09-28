package tui3

import (
	"strings"
	"testing"
)

// barGeometry reads one head row: the row it is on, the column its first word
// starts in, and the blank cells between that word and the next item.
func barGeometry(frame, first string) (y, x, gap int) {
	for i, r := range strings.Split(plain(frame), "\n") {
		at := strings.Index(r, first)
		if at < 0 {
			continue
		}
		rest := r[at+len(first):]
		return i, at, len(rest) - len(strings.TrimLeft(rest, " "))
	}
	return -1, -1, -1
}

// ONE TOP NAV. A conversation and a place draw the same nav: the places on the
// wordmark's row, in the same cells, with the same air between two words. The
// strip of chats is the next row only in a conversation. On a place that row
// is the rule. The one thing that differs on row zero is which word is lit:
// `chats` over a conversation, the place over a place.
func TestOneTopNavOnAChatAndOnAPlace(t *testing.T) {
	for _, width := range []int{80, 110, 160} {
		a, _, _, _ := trafficApp(t)
		a.open = func(workspace, transcript string) (Conversation, error) { return Conversation{}, nil }
		a.welcome.open = false
		a.railAway = true
		a.width, a.height = width, 24
		a.touch()
		chat, _, _ := a.frame()
		cy, cx, cgap := barGeometry(chat, " home ")
		chatSpans := append([]placeTabSpan(nil), a.tabs...)
		walkTo(t, a, pageSpend)
		frame, _, _ := a.frame()
		py, px, pgap := barGeometry(frame, " home ")
		if cy != navRow || py != navRow || a.tabRow != navRow {
			t.Fatalf("at %d the nav is on row %d in the chat and %d on the place", width, cy, py)
		}
		if cx != px || cgap != pgap || pgap != len(tabPad) {
			t.Fatalf("at %d the navs differ: chat x %d gap %d, place x %d gap %d", width, cx, cgap, px, pgap)
		}
		if len(chatSpans) != len(a.tabs) {
			t.Fatalf("at %d the chat's nav has %d buttons and the place's %d", width, len(chatSpans), len(a.tabs))
		}
		for i := range chatSpans {
			if chatSpans[i] != a.tabs[i] {
				t.Fatalf("at %d the button %d moved between the chat and the place: %+v, %+v", width, i, chatSpans[i], a.tabs[i])
			}
		}
		// THE STRIP IS UNDER THE NAV IN THE CHAT, and absent on the place.
		if sy, _, _ := barGeometry(chat, "harbor ▾"); sy != tabStripRow {
			t.Fatalf("at %d the chat's strip is on row %d", width, sy)
		}
		if py2, _, _ := barGeometry(frame, "harbor ▾"); py2 >= 0 {
			t.Fatalf("at %d the place drew the strip on row %d", width, py2)
		}
		placeRows := strings.Split(plain(frame), "\n")
		if len(placeRows) <= placeHeadRows || !strings.HasPrefix(placeRows[placeHeadRows-2], "─") || strings.TrimSpace(placeRows[placeHeadRows-1]) != "" {
			t.Fatalf("at %d the place's head is not the nav, the air row, the rule and a blank", width)
		}
		lit := a.pal.onPlaces()
		if !strings.Contains(frame, lit.bold(lit.accent(tabPad+"spend"+tabPad))) {
			t.Fatalf("at %d the place you stand in is not lit in the accent", width)
		}
		if a.tabActivePaint(" x ") != activeGround(a.pal, " x ") {
			t.Fatal("the chat strip's tab in front has a ground of its own")
		}
	}
}
