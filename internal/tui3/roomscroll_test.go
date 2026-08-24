package tui3

// ── A ROOM WITH HEAVY TOOL USE READS LIKE A PLACE, NOT A STUB ───────────────
//
// The screenshot these tests hold shut: a task whose turn had made a hundred
// and twenty calls drew four rows pinned to the top of the frame, a forty-row
// void under them, and a wheel that did nothing. Three causes compounded — the
// conversation's three-call fold applied to a page that IS the cluster, the
// folded calls absent from the row list so there was nothing to scroll to, and
// the slack falling under a page that had been starved rather than under one
// that was short. The fixes are render.go's [deck.toolTail], room.go's
// [app.roomToolTail] and [app.roomScroll]; this file is what they promise.

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// callsJournal is a node whose one turn made n calls in a row and then reported:
// the shape of every long refactor, and the one the fold was built for.
func callsJournal(t *testing.T, n int) string {
	t.Helper()
	lines := []string{`{"type":"message","role":"user","content":"Port the loader"}`}
	for i := 0; i < n; i++ {
		id := "c" + strconv.Itoa(i)
		lines = append(lines,
			`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"`+id+
				`","function":{"name":"read","arguments":"{\"path\":\"file`+strconv.Itoa(i)+`.go\"}"}}]}`,
			`{"type":"message","role":"tool","toolCallId":"`+id+`","content":"12 lines"}`)
	}
	lines = append(lines, `{"type":"message","role":"assistant","content":"All read."}`)
	return roomJournal(t, lines...)
}

// callsRoom opens a room on a node with three screens' worth of calls in one
// turn — more than any tail can keep, so the fold is guaranteed to be drawn.
func callsRoom(t *testing.T) (*app, int) {
	t.Helper()
	a, fake, _ := roomApp(t)
	n := 3 * a.viewHeight()
	fake.journal = callsJournal(t, n)
	a.openRoom(7, "Port the loader")
	a.touch()
	return a, n
}

// foldRows counts the fold lines and the call rows on a page, and returns the
// fold's sentence.
func foldRows(rs []row) (folds, calls int, fold string) {
	for _, r := range rs {
		switch r.hit {
		case hitFold:
			folds++
			fold = plain(r.text)
		case hitTool:
			calls++
		}
	}
	return folds, calls, fold
}

// A FOLD NEVER STARVES THE SCREEN. A room whose one turn holds sixty calls
// keeps a screenful of the newest ones over one fold line — not three calls
// over a void — and the fold's sentence names the scroll that opens it.
func TestARoomWithManyCallsFillsItsFrameAndFoldsOnlyTheOverflow(t *testing.T) {
	a, n := callsRoom(t)
	width, height := a.bodyWidth(), a.viewHeight()

	rows := a.roomRows(width)
	folds, calls, fold := foldRows(rows)
	if folds != 1 {
		t.Fatalf("%d fold lines on the page, want exactly one:\n%s", folds, roomText(a))
	}
	if calls != height {
		t.Fatalf("%d calls on the page at %d rows high — the fold starved the screen:\n%s",
			calls, height, roomText(a))
	}
	if want := strconv.Itoa(n-height) + " earlier tool calls · scroll up or ctrl+o"; !strings.Contains(fold, want) {
		t.Fatalf("the room's fold reads %q, want %q", fold, want)
	}
	visible, pad := a.roomWindow(width, height)
	if pad != 0 || len(visible) != height {
		t.Fatalf("the room's frame is %d rows with %d of slack at %d high — the void is back",
			len(visible), pad, height)
	}
	last := plain(visible[len(visible)-1].text)
	if !strings.Contains(roomText(a), "file"+strconv.Itoa(n-1)+".go") {
		t.Fatalf("the newest call is not on the page; the frame ends on %q", last)
	}
}

// THE CONVERSATION KEEPS ITS THREE. The tail is the room's alone: the
// transcript's deck sets none, and its fold still names only the key.
func TestTheConversationsFoldStillKeepsThreeCallsAndNamesOnlyTheKey(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if got := a.conversation().window(); got != toolWindow {
		t.Fatalf("the conversation's window is %d, want toolWindow (%d)", got, toolWindow)
	}
	if word := foldWord(2, false); word != "2 earlier tool calls · ctrl+o" {
		t.Fatalf("the conversation's fold reads %q", word)
	}
	if word := foldWord(1, true); word != "1 earlier tool call · scroll up or ctrl+o" {
		t.Fatalf("the room's fold reads %q", word)
	}
}

// SCROLLING UP AT THE TOP OPENS THE FOLD, ANCHORED. The call that was on the
// line under the fold is still on that line afterwards, the reader has left the
// live edge, and the fold line is gone.
func TestScrollingUpAtTheTopOfARoomOpensTheFoldWithoutLosingThePlace(t *testing.T) {
	a, _ := callsRoom(t)
	width, height := a.bodyWidth(), a.viewHeight()
	rows := a.roomRows(width)

	a.roomScroll(-len(rows))
	if at := a.roomOffsetFor(len(rows), height); at != 0 {
		t.Fatalf("the room did not reach the top: offset %d", at)
	}
	before, _ := a.roomWindow(width, height)
	foldAt := -1
	for i, r := range before {
		if r.hit == hitFold {
			foldAt = i
		}
	}
	if foldAt < 0 || foldAt+1 >= len(before) {
		t.Fatalf("no fold line with a call under it on the first screen:\n%s", roomText(a))
	}
	turn := before[foldAt].turn
	anchorLine := foldAt + 1
	anchor := plain(before[anchorLine].text)

	a.roomScroll(-3) // one wheel tick, at the top
	if !a.room.unfolded[turn] {
		t.Fatal("scrolling up at the top did not open the fold")
	}
	if a.room.stick {
		t.Fatal("the reader is still stuck to the live edge after opening history")
	}
	after, _ := a.roomWindow(width, height)
	if got := plain(after[anchorLine].text); got != anchor {
		t.Fatalf("the anchor moved: line %d was %q and is now %q", anchorLine, anchor, got)
	}
	if folds, _, _ := foldRows(a.roomRows(width)); folds != 0 {
		t.Fatalf("the fold line survived the unfold:\n%s", roomText(a))
	}
	grown := len(a.roomRows(width)) - len(rows)
	if got := a.roomOffsetFor(len(a.roomRows(width)), height); got != grown {
		t.Fatalf("the offset is %d after an unfold that added %d rows", got, grown)
	}
	// The next tick walks up into the calls that just appeared.
	a.roomScroll(-3)
	if got := a.roomOffsetFor(len(a.roomRows(width)), height); got != grown-3 {
		t.Fatalf("the tick after the unfold landed at %d, want %d", got, grown-3)
	}
}

// THE WHEEL IS THE SAME GESTURE. A wheel-up over the body of a room at its top
// reaches the same unfold pgup and ↑ do.
func TestTheWheelOverARoomAtTheTopOpensTheFold(t *testing.T) {
	a, _ := callsRoom(t)
	width := a.bodyWidth()
	rows := a.roomRows(width)
	a.roomScroll(-len(rows))
	turn := rows[0].turn
	for _, r := range rows {
		if r.hit == hitFold {
			turn = r.turn
		}
	}
	y := a.bodyTop() + 2
	drive(t, a, tea.MouseWheelMsg{X: 2, Y: y, Button: tea.MouseWheelUp})
	if !a.room.unfolded[turn] {
		t.Fatalf("the wheel at the top of a room did not open the fold:\n%s", roomText(a))
	}
}

// SCROLLING BACK DOWN RE-STICKS, AND THE UNFOLD STAYS. ctrl+o remains the
// toggle either way, and folds the page back to a screenful.
func TestScrollingDownReturnsARoomToTheLiveEdgeWithTheHistoryStillOpen(t *testing.T) {
	a, _ := callsRoom(t)
	width, height := a.bodyWidth(), a.viewHeight()
	rows := a.roomRows(width)
	a.roomScroll(-len(rows))
	a.roomScroll(-1)
	if folds, _, _ := foldRows(a.roomRows(width)); folds != 0 {
		t.Fatal("the fold did not open")
	}

	a.roomScroll(len(a.roomRows(width)))
	total := len(a.roomRows(width))
	if !a.room.stick || a.roomOffsetFor(total, height) != total-height {
		t.Fatalf("scrolling down did not re-stick at the live edge: stick=%v offset=%d of %d",
			a.room.stick, a.roomOffsetFor(total, height), total)
	}
	if folds, _, _ := foldRows(a.roomRows(width)); folds != 0 {
		t.Fatal("returning to the live edge folded the history back up")
	}

	drive(t, a, key("ctrl+o"))
	folds, calls, _ := foldRows(a.roomRows(width))
	if folds != 1 || calls != height {
		t.Fatalf("ctrl+o did not fold the room back to a screenful: %d folds, %d calls at %d high",
			folds, calls, height)
	}
}

// A RESIZE RE-DERIVES THE TAIL. The room's row cache is keyed on the height as
// well as the width, because the tail is the height.
func TestAShorterFrameFoldsARoomToItsNewHeight(t *testing.T) {
	a, _ := callsRoom(t)
	width, height := a.bodyWidth(), a.viewHeight()
	if _, calls, _ := foldRows(a.roomRows(width)); calls != height {
		t.Fatalf("%d calls at %d high before the resize", calls, height)
	}
	drive(t, a, tea.WindowSizeMsg{Width: a.width, Height: a.height - 6})
	shorter := a.viewHeight()
	if shorter >= height {
		t.Fatalf("the frame did not get shorter: %d then %d", height, shorter)
	}
	if _, calls, _ := foldRows(a.roomRows(a.bodyWidth())); calls != shorter {
		t.Fatalf("%d calls on the page at %d high after the resize — the cache kept the old tail",
			calls, shorter)
	}
}

// FREEZING A FOLDED ROOM STAYS IN BOUNDS. ctrl+b snapshots the room-sized rows,
// and every cursor the copy keys move lands inside them.
func TestFreezingAFoldedRoomKeepsTheCopyCursorInBounds(t *testing.T) {
	a, _ := callsRoom(t)
	width, height := a.bodyWidth(), a.viewHeight()
	rows := a.roomRows(width)

	a.freezeRoom()
	if !a.copy.on {
		t.Fatal("ctrl+b did not freeze the room")
	}
	if len(a.copy.rows) != len(rows) {
		t.Fatalf("the freeze holds %d rows of a %d-row page", len(a.copy.rows), len(rows))
	}
	inBounds := func(when string) {
		t.Helper()
		if a.copy.at < 0 || a.copy.at >= len(a.copy.rows) {
			t.Fatalf("%s: the copy cursor is at %d of %d rows", when, a.copy.at, len(a.copy.rows))
		}
		if a.copy.top < 0 || a.copy.top > max(len(a.copy.rows)-height, 0) {
			t.Fatalf("%s: the copy window starts at %d of %d rows", when, a.copy.top, len(a.copy.rows))
		}
	}
	inBounds("on freeze")
	if a.copy.top != a.roomOffsetFor(len(rows), height) {
		t.Fatalf("the freeze starts at %d, the room was at %d", a.copy.top, a.roomOffsetFor(len(rows), height))
	}
	for _, k := range []string{"home", "a", "end", "v", "pgup", "pgdown"} {
		drive(t, a, key(k))
		inBounds("after " + k)
	}
	drive(t, a, key("esc"))
	if a.copy.on {
		t.Fatal("esc did not leave copy mode")
	}
}
