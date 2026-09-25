package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// navPlaces is the nav's buttons alone, as a reader sees them: the cells from
// the first button's first cell to the last button's last, `more ▾` included,
// off the row [app.navLine] drew. The pulse at the row's end is not in it, so
// a test about the places is not a test about the clock.
func navPlaces(a *app, width int, numbered bool) string {
	was := a.mapShowing
	a.mapShowing = numbered
	defer func() { a.mapShowing = was }()
	line := a.navLine(width, a.pal)
	if len(a.tabs) == 0 {
		return ""
	}
	from, to := a.tabs[0].from, a.tabs[len(a.tabs)-1].to
	if a.navMore.span.pressable() {
		to = a.navMore.span.to
	}
	return strings.TrimSpace(plain(ansi.Cut(line, from, to)))
}

// navWidths are the widths the head is pinned at: a split pane, the classic
// terminal, a laptop and a wide screen.
var navWidths = []int{60, 80, 110, 160}

// navChat is a conversation with three tabs on its strip, a team shown and
// some money spent, the fixture every test here starts from.
func navChat(t *testing.T) *app {
	t.Helper()
	a, _, _, _ := trafficApp(t)
	a.open = func(workspace, transcript string) (Conversation, error) { return Conversation{}, nil }
	a.welcome.open = false
	a.traffic.hidden = true
	a.height = 30
	return a
}

// headRowsOf is the frame's first rows, as a reader sees them.
func headRowsOf(a *app) []string {
	rows := strings.Split(plain(frame(a)), "\n")
	if len(rows) > placeHeadRows+1 {
		rows = rows[:placeHeadRows+1]
	}
	return rows
}

// THE HEAD IS THE SAME FOUR ROWS ON EVERY PAGE, AT EVERY WIDTH. The nav on row
// zero, the strip on row one, the rule, a blank, and the body under exactly
// those rows, on a chat and on a place alike, so nothing moves when a person
// walks between them.
func TestTheHeadIsTheSameFourRowsOnEveryPage(t *testing.T) {
	for _, width := range navWidths {
		a := navChat(t)
		a.width = width
		a.touch()
		chat := headRowsOf(a)
		chatNav := append([]placeTabSpan(nil), a.tabs...)
		if a.tabRow != navRow || a.headHeight() != placeHeadRows {
			t.Fatalf("at %d the chat's nav is on row %d and its head is %d rows", width, a.tabRow, a.headHeight())
		}
		for _, to := range []page{pageHome, pageTeams, pageSpend} {
			walkTo(t, a, to)
			place := headRowsOf(a)
			if a.tabRow != navRow {
				t.Fatalf("at %d %s drew its nav on row %d", width, to.word(), a.tabRow)
			}
			for i := range []int{navRow, tabStripRow} {
				if !strings.HasPrefix(place[i], " ") || ansi.StringWidth(place[i]) > width {
					t.Fatalf("at %d %s's row %d is not inset or runs past the frame: %q", width, to.word(), i, place[i])
				}
			}
			if !strings.Contains(place[tabStripRow], "harbor") || place[2] != chat[2] || strings.TrimSpace(place[3]) != "" {
				t.Fatalf("at %d %s's head is not the nav, the strip, the rule and a blank:\n%s", width, to.word(), strings.Join(place, "\n"))
			}
			// AND THE WORDS STAND IN THE SAME CELLS: every button the chat drew
			// is where the place drew it, wherever both drew one.
			for _, c := range chatNav {
				for _, p := range a.tabs {
					if c.id == p.id && c != p {
						t.Fatalf("at %d %q moved from %+v on the chat to %+v on %s", width, c.id.word(), c, p, to.word())
					}
				}
			}
			a.showPage(pageNone)
		}
		if width == 80 || width == 110 {
			t.Logf("the chat's head at %d:\n%s", width, strings.Join(chat, "\n"))
		}
	}
}

// THE NAV LIGHTS WHERE YOU STAND, IN THE ONE ACCENT. The place on a place, and
// `chats` inside a conversation, since the strip under the nav is the chats.
// Every other word is muted, and exactly one is lit.
func TestTheNavLightsWhereYouStand(t *testing.T) {
	a := navChat(t)
	a.width = 160
	lit := func(id page) string { return a.pal.bold(a.pal.accent(tabPad + id.word() + tabPad)) }
	line := a.navLine(a.width, a.pal)
	if !strings.Contains(line, lit(pageChats)) {
		t.Fatalf("inside a conversation `chats` is not lit: %q", line)
	}
	for _, to := range []page{pageHome, pageTeams, pageTasks, pageSpend, pageSettings} {
		walkTo(t, a, to)
		line := a.navLine(a.width, a.pal)
		for _, id := range barPages(to, false) {
			if got := strings.Contains(line, lit(id)); got != (id == to) {
				t.Fatalf("standing in %s, %s is lit %v", to.word(), id.word(), got)
			}
			if id != to && !strings.Contains(line, a.pal.muted(tabPad+id.word()+tabPad)) {
				t.Fatalf("standing in %s, %s is not muted", to.word(), id.word())
			}
		}
	}
	// AND WITH NO COLOUR TO LIGHT IT, the place wears brackets in its pads, in
	// the same cells.
	a.pal = newPalette(tokens.NoColor, false)
	walkTo(t, a, pageSpend)
	if line := plain(a.navLine(a.width, a.pal)); !strings.Contains(line, "[spend]") {
		t.Fatalf("a plain terminal cannot tell which place is lit: %q", line)
	}
}

// EVERY BUTTON'S HOVER GROUND IS ITS TARGET, PADS INCLUDED. Each cell of a
// word's button, its two pad cells too, puts that word under the pointer, the
// cell either side of it does not, and the ground drawn covers exactly those
// cells. On a plain terminal the pointer's word wears `·` in its leading pad.
func TestEveryNavButtonsHoverGroundIsItsTarget(t *testing.T) {
	a := navChat(t)
	for _, width := range navWidths {
		a.width = width
		for _, plainTerm := range []bool{false, true} {
			a.pal = newPalette(tokens.TrueColor, false)
			if plainTerm {
				a.pal = newPalette(tokens.NoColor, false)
			}
			a.navMemo = navMemo{}
			a.navLine(width, a.pal)
			spans := append([]placeTabSpan(nil), a.tabs...)
			if a.navMore.span.pressable() {
				spans = append(spans, placeTabSpan{id: pageNone, from: a.navMore.span.from, to: a.navMore.span.to})
			}
			for _, span := range spans {
				for x := span.from - 1; x <= span.to; x++ {
					a.navHover(x, navRow)
					in := x >= span.from && x < span.to
					under := a.tabHover == span.id && span.id != pageNone || span.id == pageNone && a.navMore.hot
					if under != in {
						t.Fatalf("at %d the pointer at %d is under %v %v, and the button is %d..%d", width, x, span.id.word(), under, span.from, span.to)
					}
				}
				a.navHover(span.from, navRow)
				line := a.navLine(width, a.pal)
				word := strings.TrimSpace(plain(ansi.Cut(line, span.from, span.to)))
				want := a.pal.background(a.pal.ink(tabPad+strings.TrimPrefix(word, "·")+tabPad), 0, a.pal.ramp.mark)
				if plainTerm {
					if got := plain(ansi.Cut(line, span.from, span.to)); !strings.HasPrefix(got, "·") {
						t.Fatalf("at %d on a plain terminal %q has no pointer mark under the pointer", width, got)
					}
				} else if span.id != a.navLit() && !strings.Contains(line, want) {
					t.Fatalf("at %d the ground under %q does not cover its pads", width, word)
				}
				a.navHover(-1, -1)
			}
		}
	}
}

// A PRESS ON EACH NAV WORD OPENS THAT PLACE, from a conversation and from a
// place, on any cell of its button; a press on the place you are already in
// does nothing at all; and none of it moves the draft or the caret.
func TestAPressOnEachNavWordOpensThatPlace(t *testing.T) {
	a := navChat(t)
	a.width = 160
	a.input.setText("half a sentence")
	caret := a.input.cursor
	frame(a)
	for _, span := range append([]placeTabSpan(nil), a.tabs...) {
		for _, x := range []int{span.from, span.to - 1} {
			a.showPage(pageNone)
			frame(a)
			drive(t, a, tea.MouseClickMsg{X: x, Y: navRow, Button: tea.MouseLeft})
			switch {
			case span.id == pageChats && a.pageShowing():
				t.Fatalf("a press on `chats` inside a conversation opened %s", a.page.word())
			case span.id != pageChats && !a.at(span.id):
				t.Fatalf("a press at %d on %q left the router on %q", x, span.id.word(), a.page.word())
			}
			if span.id != pageChats {
				// AND FROM THE PLACE, `chats` IS THE WAY BACK.
				frame(a)
				for _, back := range a.tabs {
					if back.id == pageChats {
						drive(t, a, tea.MouseClickMsg{X: back.from, Y: navRow, Button: tea.MouseLeft})
					}
				}
				if a.pageShowing() {
					t.Fatalf("`chats` on %s did not go back to the conversation", span.id.word())
				}
			}
		}
	}
	if a.input.String() != "half a sentence" || a.input.cursor != caret {
		t.Fatalf("the nav moved the draft or its caret: %q at %d", a.input.String(), a.input.cursor)
	}
}

// A PRESS ON A TAB FROM A PLACE OPENS THAT CHAT. The strip is on every page;
// on a place no tab is lit, since the place is what is on screen, and a press
// on any tab leaves the place for that conversation.
func TestAPressOnATabFromAPlaceOpensThatChat(t *testing.T) {
	a := navChat(t)
	a.width = 160
	front := a.frontTabKey()
	walkTo(t, a, pageSpend)
	frame(a)
	var other tabHit
	for _, hit := range a.chatTabHits {
		if hit.kind == tabHere {
			t.Fatalf("a tab is lit on a place: %+v", hit.tab.word)
		}
		if hit.kind == tabOther && hit.tab.key != front && other.span.to == 0 {
			other = hit
		}
		if hit.kind == tabOther && hit.tab.key == front {
			// The tab of the conversation under the place is a door back to it.
			defer func(h tabHit) {
				walkTo(t, a, pageSpend)
				frame(a)
				drive(t, a, tea.MouseClickMsg{X: h.span.from, Y: tabStripRow, Button: tea.MouseLeft})
				if a.pageShowing() || a.frontTabKey() != front {
					t.Fatalf("a press on the front conversation's tab from a place left %q up", a.page.word())
				}
			}(hit)
		}
	}
	if other.span.to == 0 {
		t.Fatal("the fixture has no other tab on the place's strip")
	}
	// AND THE POINTER RESTING ON IT PUTS IT ON THE HOVER GROUND, on a place as
	// on a chat.
	drive(t, a, tea.MouseMotionMsg{X: other.span.from, Y: tabStripRow})
	if a.hot.kind != hoverTab || a.hot.index != other.span.from {
		t.Fatalf("the pointer on a tab from a place lights nothing: %+v", a.hot)
	}
	drive(t, a, tea.MouseClickMsg{X: other.span.from, Y: tabStripRow, Button: tea.MouseLeft})
	if a.pageShowing() || a.frontTabKey() != other.tab.key {
		t.Fatalf("a press on %q from a place left %q up with %q in front", other.tab.word, a.page.word(), a.frontTabKey())
	}
}

// THE NAV FOLDS INTO `more ▾`, AND `more ▾` IS A DOOR. A press opens a menu of
// exactly the folded places; the pointer lights a row, a press on it goes
// there, `↓` and `enter` do the same from the keyboard, and `esc` puts it away
// with the page, the draft and the caret exactly where they were.
func TestTheMoreMenuOpensTheFoldedPlaces(t *testing.T) {
	a := navChat(t)
	a.width = 50
	a.input.setText("a draft")
	frame(a)
	more := a.navMore.span
	folded := append([]page(nil), a.navMore.folded...)
	if !more.pressable() || len(folded) == 0 {
		t.Fatalf("at 50 columns nothing folded: %q", plain(a.navLine(50, a.pal)))
	}
	drive(t, a, tea.MouseClickMsg{X: more.from, Y: navRow, Button: tea.MouseLeft})
	if !a.navMore.on {
		t.Fatal("a press on `more ▾` did not open its menu")
	}
	text := plain(frame(a))
	for _, id := range folded {
		if !strings.Contains(text, id.word()) || !strings.Contains(text, placeChord(id)) {
			t.Fatalf("the menu does not offer %q with its key:\n%s", id.word(), text)
		}
	}
	drive(t, a, key("esc"))
	if a.navMore.on || a.pageShowing() || a.input.String() != "a draft" {
		t.Fatalf("esc did not put the menu away and leave everything else (page %q, draft %q)", a.page.word(), a.input.String())
	}
	// The keyboard.
	drive(t, a, tea.MouseClickMsg{X: more.from, Y: navRow, Button: tea.MouseLeft})
	frame(a)
	if len(folded) > 1 {
		drive(t, a, key("down"))
	}
	drive(t, a, key("enter"))
	if want := folded[min(1, len(folded)-1)]; !a.at(want) || a.navMore.on {
		t.Fatalf("enter in the menu landed on %q, want %q", a.page.word(), want.word())
	}
	// The pointer.
	a.showPage(pageNone)
	frame(a)
	drive(t, a, tea.MouseClickMsg{X: a.navMore.span.from, Y: navRow, Button: tea.MouseLeft})
	frame(a)
	if len(a.navMore.hits) == 0 {
		t.Fatal("the open menu drew no rows")
	}
	last := a.navMore.hits[len(a.navMore.hits)-1]
	target := a.navMore.folded[last.arg]
	drive(t, a, tea.MouseMotionMsg{X: last.x0, Y: last.y0})
	if a.navMore.hover != last.arg {
		t.Fatalf("the pointer on a menu row lights %v", a.navMore.hover)
	}
	drive(t, a, tea.MouseClickMsg{X: last.x0, Y: last.y0, Button: tea.MouseLeft})
	if !a.at(target) {
		t.Fatalf("a press on the menu's %q row landed on %q", target.word(), a.page.word())
	}
	// AND A PRESS OFF IT ONLY PUTS IT AWAY.
	frame(a)
	if a.navMore.span.pressable() {
		drive(t, a, tea.MouseClickMsg{X: a.navMore.span.from, Y: navRow, Button: tea.MouseLeft})
		was := a.page
		drive(t, a, tea.MouseClickMsg{X: 1, Y: a.height - 1, Button: tea.MouseLeft})
		if a.navMore.on || a.page != was {
			t.Fatal("a press off the menu did something besides putting it away")
		}
	}
}

// EVERY NAV WORD SAYS WHAT IT OPENS AND ITS KEY while the pointer rests on it,
// on the hint line of a place and of a conversation. `chats` and `▦ All` say
// two different things: the place, and the grid of the tabs open here.
func TestEveryNavWordSaysWhatItOpens(t *testing.T) {
	for _, id := range placeOrder {
		if strings.TrimSpace(placeFor(id).about()) == "" {
			t.Fatalf("%s has nothing to say on the hint line", id.word())
		}
	}
	a := navChat(t)
	a.width = 160
	walkTo(t, a, pageSpend)
	frame(a)
	for _, span := range a.tabs {
		a.navHover(span.from, navRow)
		hint := a.placeHintSaid()
		if !strings.Contains(hint, a.chords.say(placeChord(span.id))) || !strings.Contains(hint, placeFor(span.id).about()) {
			t.Fatalf("hovering %q, the place's hint line reads %q", span.id.word(), hint)
		}
	}
	a.navHover(-1, -1)
	a.showPage(pageNone)
	frame(a)
	for _, span := range a.tabs {
		if span.id != pageChats {
			continue
		}
		a.navHover(span.from, navRow)
		if hint := a.footHint(a.width); !strings.Contains(hint, placeFor(pageChats).about()) {
			t.Fatalf("hovering `chats` in a conversation, the hint reads %q", hint)
		}
	}
	a.navHover(-1, -1)
	if !a.wall.door.pressable() {
		t.Fatal("the strip drew no `▦ All`")
	}
	walkTo(t, a, pageSpend)
	frame(a)
	drive(t, a, tea.MouseMotionMsg{X: a.wall.door.from, Y: tabStripRow})
	all := a.placeHintSaid()
	if !strings.Contains(all, "grid of your open tabs") || strings.Contains(all, placeFor(pageChats).about()) {
		t.Fatalf("`▦ All` does not say it is the grid of open tabs: %q", all)
	}
}

// THE FOLD IS A WORD DOOR, NEVER A COUNT. The places' bar used to end a narrow
// row in `▸ 3`, a count nobody could press; `more ▾` is a word that opens the
// places it stands for, and its menu's caret is the one every menu here wears.
func TestTheNavsFoldIsAWordDoorNotACount(t *testing.T) {
	a := placeApp(t)
	for width := 30; width <= 200; width++ {
		bar := navPlaces(a, width, false)
		if strings.Contains(bar, tokens.GlyphCollapsed) || strings.Contains(bar, "+") {
			t.Fatalf("at %d the nav's fold is a count: %q", width, bar)
		}
		if a.navMore.span.pressable() != strings.HasSuffix(bar, "more ▾") {
			t.Fatalf("at %d the fold's word and its door disagree: %q", width, bar)
		}
	}
}

// THE COUNT THAT WANTS YOU NEVER LEAVES THE ROW. At every width from a split
// pane to a wide screen the nav carries the number of things stopped on the
// person, in amber: `2 want you` where it fits and `2 ?` where it does not.
// And at eighty columns the short count costs no place: all six words stay.
func TestTheCountThatWantsYouNeverLeavesTheNav(t *testing.T) {
	a := navChat(t)
	a.machine = machineFacts{wants: 2, hands: 1, spent: 1.2, ceiling: 20}
	long := a.pal.warn("2" + pulseWantWord)
	short := a.pal.warn("2 " + tabSignalGlyph(tabNeedsPerson, a.pal.ascii))
	for width := 44; width <= 200; width++ {
		a.width = width
		a.navMemo = navMemo{}
		line := a.navLine(width, a.pal)
		if ansi.StringWidth(line) != width {
			t.Fatalf("at %d the nav is %d wide", width, ansi.StringWidth(line))
		}
		if !strings.Contains(line, long) && !strings.Contains(line, short) {
			t.Fatalf("at %d the nav lost the count that wants you: %q", width, plain(line))
		}
	}
	a.width = 80
	a.navMemo = navMemo{}
	row := plain(a.navLine(80, a.pal))
	if !placeWordsInOrder(row, "home", "teams", "chats", "sessions", "spend", "settings") || a.navMore.span.pressable() {
		t.Fatalf("at 80 the short count cost a place: %q", row)
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), "2 ? · $1.20 / "+railFigure(20)) {
		t.Fatalf("at 80 the pulse is not the short count and the allowance: %q", row)
	}
	// AND NOTHING WANTING YOU DRAWS NOTHING: no `0 ?`.
	a.machine.wants = 0
	a.navMemo = navMemo{}
	if row := plain(a.navLine(60, a.pal)); strings.Contains(row, "?") {
		t.Fatalf("an empty count drew a mark: %q", row)
	}
}
