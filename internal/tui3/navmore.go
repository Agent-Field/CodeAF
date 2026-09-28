package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE NAV'S FOLD: `more ▾` AND THE PLACES BEHIND IT ───────────────────────
//
// A row too narrow for every place folds the trailing ones into `more ▾`
// (topnav.go), and a press on it hangs this menu under it, listing exactly the
// places it folded, each with the key that reaches it:
//
//	╭──────────────────╮
//	│ settings  alt+6  │
//	│ standing  alt+7  │
//	╰──────────────────╯
//
// It is modal as every menu here is (teammenu.go): while it is up it has the
// keyboard, `↑` `↓` walk it, `enter` goes to the place under the cursor and
// `esc` puts it away. A press on a row goes there; a press anywhere off it,
// `more ▾` included, only puts it away. IT NEVER MOVES FOCUS ANYWHERE ELSE:
// putting it away leaves the cursor, the draft and the page exactly as they
// were when it opened.

// navMore is the fold's state: where its word was drawn and what it folded,
// whether its menu is up, the row the keyboard is on and the row the pointer
// is on, and where the last frame drew the menu, in frame cells.
type navMore struct {
	// span is where `more ▾` was drawn on the nav's row, empty with nothing
	// folded; folded is the places behind it, in the bar's order.
	span   hudSpan
	folded []page
	// hot is the pointer resting on `more ▾` itself.
	hot    bool
	on     bool
	cursor int
	// hover is the row the pointer is on, -1 for none.
	hover int
	card  wallRect
	hits  []wallHit
}

// navMoreFootWords is the hint line while the menu is up: its own keys, since
// they are the only keys that do anything.
const navMoreFootWords = "↑↓ choose · enter go · esc close"

func (a *app) openNavMore() {
	if len(a.navMore.folded) == 0 {
		return
	}
	a.navMore.on, a.navMore.cursor, a.navMore.hover = true, 0, -1
	a.touch()
}

func (a *app) closeNavMore() {
	a.navMore.on, a.navMore.hover, a.navMore.card, a.navMore.hits = false, -1, wallRect{}, nil
	a.touch()
}

// navMoreHot records the pointer on or off `more ▾`, repainting only when
// that is news.
func (a *app) navMoreHot(on bool) {
	if a.navMore.hot == on {
		return
	}
	a.navMore.hot = on
	a.touch()
}

// navMoreGo is one place chosen from the menu.
func (a *app) navMoreGo(id page) tea.Cmd {
	a.closeNavMore()
	if a.headCovers() {
		a.headUncover()
	}
	if id == a.navLit() {
		return nil
	}
	return a.showPage(id)
}

// navMoreKey is a key while the menu is up.
func (a *app) navMoreKey(msg tea.KeyPressMsg) tea.Cmd {
	m := &a.navMore
	switch msg.String() {
	case "esc":
		a.closeNavMore()
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
		a.touch()
	case "down", "j":
		m.cursor = min(m.cursor+1, len(m.folded)-1)
		a.touch()
	case "enter", "space":
		if m.cursor >= 0 && m.cursor < len(m.folded) {
			return a.navMoreGo(m.folded[m.cursor])
		}
	}
	return nil
}

// navMoreHitAt is the menu's row under the pointer on the last frame.
func (a *app) navMoreHitAt(x, y int) (wallHit, bool) {
	for _, hit := range a.navMore.hits {
		if x >= hit.x0 && x < hit.x1 && y >= hit.y0 && y < hit.y1 {
			return hit, true
		}
	}
	return wallHit{}, false
}

// navMorePress answers a left press while the menu is up.
func (a *app) navMorePress(x, y int) tea.Cmd {
	if hit, ok := a.navMoreHitAt(x, y); ok && hit.arg < len(a.navMore.folded) {
		return a.navMoreGo(a.navMore.folded[hit.arg])
	}
	if !a.navMore.card.holds(x, y) {
		a.closeNavMore()
	}
	return nil
}

// navMoreMotion lights the row under the pointer.
func (a *app) navMoreMotion(x, y int) {
	hit, ok := a.navMoreHitAt(x, y)
	under := -1
	if ok {
		under = hit.arg
	}
	if under != a.navMore.hover {
		a.navMore.hover = under
		a.touch()
	}
}

// navMoreOver lays the menu over a finished frame, hung from `more ▾`, and
// writes down where its rows landed. With the menu down it hands the frame
// back as it was given.
func (a *app) navMoreOver(frame string) string {
	if !a.navMore.on {
		return frame
	}
	// A WIDER WINDOW UNFOLDS THE PLACES AND THE MENU GOES WITH THEM: a menu
	// of nothing, or of words the row now shows, is not one to leave up.
	if len(a.navMore.folded) == 0 || !a.navMore.span.pressable() {
		a.navMore.on, a.navMore.card, a.navMore.hits = false, wallRect{}, nil
		return frame
	}
	a.navMore.cursor = min(max(a.navMore.cursor, 0), len(a.navMore.folded)-1)
	width, height := a.size()
	card := a.navMoreCard(width, height)
	a.navMore.hits = card.hits
	if len(card.rows) == 0 {
		a.navMore.card = wallRect{}
		return frame
	}
	a.navMore.card = wallRect{card.x, card.y, card.x + card.w, card.y + len(card.rows)}
	rows := strings.Split(frame, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	for dy, cr := range card.rows {
		if y := card.y + dy; y >= 0 && y < len(rows) {
			rows[y] = wallSplice(rows[y], cr, card.x, width)
		}
	}
	return strings.Join(rows[:height], "\n")
}

// navMoreCard is the menu as a card: one row per folded place, its word and
// its key, the row under the keyboard's cursor or the pointer on the cursor
// ground. It is hung on the row under the nav, its left edge under the fold's
// word and kept a cell inside the frame, and not drawn on a frame too small
// to hold it whole.
func (a *app) navMoreCard(width, height int) wallCard {
	pal := a.pal
	words, keys := 0, 0
	for _, id := range a.navMore.folded {
		words = max(words, ansi.StringWidth(id.word()))
		keys = max(keys, ansi.StringWidth(a.chords.say(placeChord(id))))
	}
	const padX = 1
	inner := 1 + words + 2 + keys + 1
	w := inner + 2 + 2*padX
	top := navRow + 1
	if w > width-2 || top+len(a.navMore.folded)+2 > height {
		return wallCard{}
	}
	x := min(max(a.navMore.span.from, 1), width-1-w)
	lines := make([]wallCardLine, 0, len(a.navMore.folded))
	for i, id := range a.navMore.folded {
		word := id.word()
		key := a.chords.say(placeChord(id))
		ink := pal.ink
		if id == a.navLit() {
			ink = func(s string) string { return pal.bold(pal.accent(s)) }
		}
		s := " " + ink(word) + strings.Repeat(" ", words-ansi.StringWidth(word)+2) + pal.dim(key) + " "
		lit := a.navMore.hover == i || (a.navMore.hover < 0 && a.navMore.cursor == i)
		lines = append(lines, wallCardLine{
			s:    wallPopRowPaint(pal, s, inner, lit),
			hits: []wallHit{{x0: 0, y0: 0, x1: inner, y1: 1, kind: wallHitPopRow, arg: i}},
		})
	}
	return wallCardBuild(pal, "", lines, x, top, w, padX, 0)
}
