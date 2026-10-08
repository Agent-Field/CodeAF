package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE VERBS ON THE RIGHT OF THE ITEM PAGE ─────────────────────────────────
//
// THE ITEM PAGE CARRIES ITS VERBS ON A RAIL (owner decision, 2026-10-08): a
// column on the pane's right, past one more rule, of rows a person can read
// and press, grouped `do` and `also` the way the `?` sheet groups them, each
// row's key at the column's right edge, dim, so nobody has to decode a hint
// line to know what the page can do.
//
//	do                      the item's verbs for its state: the strip's list
//	                        ([app.factoryVerbRows]) without `ask me at`
//	also                    open on github, refresh, dismiss
//
// THE KNOBS ARE NOT HERE: ask me at, thinking, budget and the stages are the
// `settings` row's, on the left (factory_item.go's [app.factorySettingsPane]),
// where the item page is the issue's map (owner decision, 2026-10-08).
//
// EVERY ROW IS ONE THE `?` SHEET WOULD NAME for the item where it stands
// ([app.factorySheet]): a verb whose door is absent or whose state refuses it
// is not drawn, and a group with nothing in it is not drawn either.
//
// A PRESS ON A ROW IS ITS KEY: the key is sent down the place's own key path
// ([app.factoryVerbPress]), so a press and a key cannot do two different
// things.
//
// WHILE A TYPING ROW HAS THE KEYS THE COLUMN IS DIMMED (owner decision,
// 2026-10-08): every row and every group's name in the dim tier, drawn but
// off, and a press on a row does nothing. A dim column means exactly one
// thing, the keys type now; the manager's box (factory_timeline.go) is the
// typing row a person meets most.
//
// THE POINTER RESTING ON A ROW PAINTS IT WITH THE POINTER'S GROUND, as the
// floor's rows wear it, dimmed or not. THE KEYBOARD DOES NOT WALK THIS COLUMN: `↑↓` stay the
// left column's, and every row here already has its own key.
//
// UNDER [factoryVerbRailMinW] THE COLUMN IS NOT DRAWN: the chips come back on
// the head's second row and the pane keeps its action line, so a narrow
// terminal loses nothing it had.

// factoryVerbRow is one row of the column: its word and the key a press on
// it sends.
type factoryVerbRow struct {
	word, key string
}

// factoryVerbGroup is one titled group of the column.
type factoryVerbGroup struct {
	name string
	rows []factoryVerbRow
}

// factoryVerbHit is a row as the last draw placed it: the body row it stands
// on and the row.
type factoryVerbHit struct {
	row  int
	verb factoryVerbRow
}

// factoryVerbsDrawn says whether the item page's last draw put the column on
// the screen: the page is open and was wide enough.
func (a *app) factoryVerbsDrawn() bool { return a.fp.open && a.fp.verbX > 0 }

// factoryVerbGroups is the column's groups for the item, in the order the
// column draws them, a group with no row left out.
func (a *app) factoryVerbGroups(it factory.Item) []factoryVerbGroup {
	var do, also []factoryVerbRow
	for _, r := range a.factoryVerbRows(it) {
		if r.key == keyAskAt {
			// `ask me at` is a setting, and stands in the settings row.
			continue
		}
		do = append(do, factoryVerbRow{word: r.word, key: r.key})
	}
	var sheet []factorySheetRow
	for _, g := range a.factorySheet() {
		if g.name == wordGroupAlso {
			sheet = g.rows
		}
	}
	for _, word := range []string{wordOpenGitHub, wordRefresh, wordDismiss} {
		for _, r := range sheet {
			if r.word == word {
				also = append(also, factoryVerbRow{word: word, key: r.key})
				break
			}
		}
	}
	var out []factoryVerbGroup
	for _, g := range []factoryVerbGroup{{wordGroupDo, do}, {wordGroupAlso, also}} {
		if len(g.rows) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// factoryVerbLines is the column as at most room lines of [factoryVerbRailW]
// cells, and where each row stands among them: each group a dim name over
// its rows, one blank row between two groups.
func (a *app) factoryVerbLines(it factory.Item, room int) ([]string, []factoryVerbHit) {
	var lines []string
	var hits []factoryVerbHit
	for i, g := range a.factoryVerbGroups(it) {
		if i > 0 {
			for range factoryBlockGap {
				lines = append(lines, "")
			}
		}
		// THE GROUP'S NAME IS DIM, as the chat's task page names its own
		// column's group (`Task setup`, roompanel.go).
		lines = append(lines, factorySpaces(factoryVerbLeadW)+a.pal.dim(g.name))
		for _, r := range g.rows {
			hits = append(hits, factoryVerbHit{row: len(lines), verb: r})
			lines = append(lines, a.factoryVerbRowLine(r))
		}
	}
	if len(lines) > room {
		lines = lines[:max(room, 0)]
	}
	kept := hits[:0]
	for _, h := range hits {
		if h.row < len(lines) {
			kept = append(kept, h)
		}
	}
	return lines, kept
}

// factoryVerbRowLine is one row: the word in ink and the key dim,
// right-aligned at the column's end. The row the pointer rests on wears the
// pointer's ground.
func (a *app) factoryVerbRowLine(r factoryVerbRow) string {
	pal := a.pal
	paint := pal.ink
	if a.factoryVerbsOff() {
		paint = pal.dim
	}
	body := paint(factoryPad(r.word, factoryVerbWordW+factoryVerbValueW))
	key := factorySpaces(factoryVerbKeyW-ansi.StringWidth(r.key)) + pal.dim(r.key)
	content := factorySpaces(factoryVerbIndentW) + body + factorySpaces(factoryVerbKeyGap) + key
	if r.word == a.fp.verbHover {
		content = pal.cursor(content, factoryVerbRailW-factoryVerbLeadW)
	}
	return factorySpaces(factoryVerbLeadW) + content
}

// factoryVerbsOff says whether the column is drawn but off: a typing row
// has the keys.
func (a *app) factoryVerbsOff() bool { return a.fp.act.ask != nil }

// factoryVerbAt is the row the last draw put at screen column x on body row
// row, and false off the column or on a row that is no verb.
func (a *app) factoryVerbAt(x, row int) (factoryVerbRow, bool) {
	if !a.factoryVerbsDrawn() || x < a.fp.verbX || x >= a.fp.verbX+factoryVerbRailW {
		return factoryVerbRow{}, false
	}
	for _, h := range a.fp.verbHits {
		if h.row == row {
			return h.verb, true
		}
	}
	return factoryVerbRow{}, false
}

// factoryVerbHover is the pointer resting at (x, y): the row under it takes
// the pointer's ground and every other gives it up. It reports whether the
// pointer is on a row.
func (a *app) factoryVerbHover(x, y int) bool {
	v, ok := a.factoryVerbAt(x, y-placeHeadRows)
	next := ""
	if ok {
		next = v.word
	}
	if next != a.fp.verbHover {
		a.fp.verbHover = next
		a.touch()
	}
	return ok
}

// factoryVerbPress is a press on a row: ITS KEY, sent down the place's own
// key path (place_factory.go's owns, then key), so the press runs the very
// function the key runs and never a copy of it; while the column is dimmed,
// nothing.
func (a *app) factoryVerbPress(v factoryVerbRow) tea.Cmd {
	// A DIMMED ROW RUNS NOTHING: its key would type into the box.
	if a.factoryVerbsOff() {
		return nil
	}
	msg := factoryKeyPress(v.key)
	if cmd, took := (placeFactory{}).owns(a, msg); took {
		return cmd
	}
	return (placeFactory{}).key(a, msg)
}

// factoryKeyPress is the key press a key's spelling names: `space`, or the
// one character it is.
func factoryKeyPress(k string) tea.KeyPressMsg {
	if k == keySelect {
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	r := []rune(k)
	if len(r) != 1 {
		return tea.KeyPressMsg{}
	}
	return tea.KeyPressMsg{Code: r[0], Text: k}
}
