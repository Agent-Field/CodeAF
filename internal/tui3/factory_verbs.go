package tui3

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE VERBS ON THE RIGHT OF THE ITEM PAGE ─────────────────────────────────
//
// THE ITEM PAGE CARRIES ITS VERBS ON A RAIL (owner decision, 2026-10-08): a
// column on the pane's right, past one more rule, of rows a person can read
// and press, grouped `do`, `set` and `also` the way the `?` sheet groups them,
// each row's key at the column's right edge, dim, so nobody has to decode a
// hint line to know what the page can do.
//
//	do                      the item's verbs for its state: the strip's list
//	                        ([app.factoryVerbRows]) without `ask me at`
//	set                     ask me at, thinking, budget and stages, each with
//	                        its value; the chips that stood on the head's
//	                        second row, which is why that row draws none
//	also                    open on github, refresh, dismiss
//
// EVERY ROW IS ONE THE `?` SHEET WOULD NAME for the item where it stands
// ([app.factorySheet]): a verb whose door is absent or whose state refuses it
// is not drawn, and a group with nothing in it is not drawn either.
//
// A PRESS ON A ROW IS ITS KEY: the key is sent down the place's own key path
// ([app.factoryVerbPress]), so a press and a key cannot do two different
// things. The `stages` row has no one key; a press on it walks the stage rail
// on the left to the first stage, where `1-9` turn stages on and off.
//
// THE POINTER RESTING ON A ROW PAINTS IT WITH THE POINTER'S GROUND, as the
// floor's rows wear it. THE KEYBOARD DOES NOT WALK THIS COLUMN: `↑↓` stay the
// stage rail's on the left, and every row here already has its own key.
//
// UNDER [factoryVerbRailMinW] THE COLUMN IS NOT DRAWN: the chips come back on
// the head's second row and the pane keeps its action line, so a narrow
// terminal loses nothing it had.

// factoryVerbRow is one row of the column: its word, its value in a `set`
// row, and the key a press on it sends. stages marks the `stages n of m`
// row, whose press walks the stage rail instead.
type factoryVerbRow struct {
	word, value, key string
	stages           bool
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
	var do, set, also []factoryVerbRow
	for _, r := range a.factoryVerbRows(it) {
		if r.key == keyAskAt {
			// `ask me at` is a setting, and stands in `set` with its value.
			continue
		}
		do = append(do, factoryVerbRow{word: r.word, key: r.key})
	}
	sheet := map[string][]factorySheetRow{}
	for _, g := range a.factorySheet() {
		sheet[g.name] = g.rows
	}
	find := func(group string, ok func(factorySheetRow) bool) (factorySheetRow, bool) {
		for _, r := range sheet[group] {
			if ok(r) {
				return r, true
			}
		}
		return factorySheetRow{}, false
	}
	values := map[string]string{}
	for _, c := range a.factoryChipList(it) {
		values[c.label] = c.value
	}
	for _, s := range []struct{ key, word string }{{keyAskAt, wordAskAt}, {keyThinking, wordThinking}, {keyBudget, wordBudget}} {
		if r, ok := find(wordGroupSet, func(r factorySheetRow) bool { return r.key == s.key }); ok {
			set = append(set, factoryVerbRow{word: s.word, value: values[s.word], key: r.key})
		}
	}
	if _, ok := find(wordGroupSet, func(r factorySheetRow) bool { return r.key == keyStages }); ok {
		stages := factoryStages(a.fp.snap, it)
		on := 0
		for _, s := range stages {
			if s.On {
				on++
			}
		}
		set = append(set, factoryVerbRow{word: wordStages, value: strconv.Itoa(on) + " " + wordOf + " " + strconv.Itoa(len(stages)), stages: true})
	}
	for _, word := range []string{wordOpenGitHub, wordRefresh, wordDismiss} {
		if r, ok := find(wordGroupAlso, func(r factorySheetRow) bool { return r.word == word }); ok {
			also = append(also, factoryVerbRow{word: word, key: r.key})
		}
	}
	var out []factoryVerbGroup
	for _, g := range []factoryVerbGroup{{wordGroupDo, do}, {wordGroupSet, set}, {wordGroupAlso, also}} {
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

// factoryVerbRowLine is one row: the word in ink, a `set` row's value muted in
// its own column, and the key dim, right-aligned at the column's end. The row
// the pointer rests on wears the pointer's ground.
func (a *app) factoryVerbRowLine(r factoryVerbRow) string {
	pal := a.pal
	body := pal.ink(factoryPad(r.word, factoryVerbWordW+factoryVerbValueW))
	if r.value != "" {
		body = pal.ink(factoryPad(r.word, factoryVerbWordW)) + pal.muted(factoryPad(r.value, factoryVerbValueW))
	}
	key := factorySpaces(factoryVerbKeyW-ansi.StringWidth(r.key)) + pal.dim(r.key)
	content := factorySpaces(factoryVerbIndentW) + body + factorySpaces(factoryVerbKeyGap) + key
	if r.word == a.fp.verbHover {
		content = pal.cursor(content, factoryVerbRailW-factoryVerbLeadW)
	}
	return factorySpaces(factoryVerbLeadW) + content
}

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
// function the key runs and never a copy of it. The `stages` row walks the
// stage rail to the first stage.
func (a *app) factoryVerbPress(v factoryVerbRow) tea.Cmd {
	if v.stages {
		it, ok := a.factoryCursorItem()
		if !ok {
			return nil
		}
		for i, r := range a.factoryItemRows(it) {
			if r.kind == factoryPageStage {
				a.factoryStageSelect(i)
				break
			}
		}
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
