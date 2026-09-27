package tui3

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// pasteChipLines is the ONE threshold between an ordinary paste and a held
// document. Three logical lines are enough to make the composer cleaner when
// folded, while one- and two-line clipboard fragments remain ordinary text.
const pasteChipLines = 3

const pasteTokenHead = "[paste "

type pasteChip struct {
	n    int
	text string
}

type pasteEditor struct {
	open bool
	n    int
	box  editor
}

func pasteLineCount(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func pasteToken(n, lines int) string {
	return pasteTokenHead + strconv.Itoa(n) + " · " + strconv.Itoa(lines) + " lines]"
}

func (a *app) pasteText(text string) bool {
	if strings.HasPrefix(strings.TrimSpace(a.input.String()), "/") || pasteLineCount(text) < pasteChipLines {
		return false
	}
	n := len(a.pastes) + 1
	a.pastes = append(a.pastes, pasteChip{n: n, text: text})
	inserted := a.spacedTokens([]string{pasteToken(n, pasteLineCount(text))})
	at := a.input.cursor
	a.input.insert(inserted)
	a.editTags(at, at, len([]rune(inserted)))
	return true
}

func (a *app) pasteSpans() []segment {
	value := string(a.input.value)
	var out []segment
	for _, held := range a.pastes {
		token := pasteToken(held.n, pasteLineCount(held.text))
		fromByte := strings.Index(value, token)
		if fromByte < 0 {
			continue
		}
		from := len([]rune(value[:fromByte]))
		out = append(out, segment{from: from, to: from + len([]rune(token))})
	}
	return out
}

func (a *app) pasteForSpan(s segment) *pasteChip {
	token := string(a.input.value[s.from:s.to])
	for i := range a.pastes {
		if token == pasteToken(a.pastes[i].n, pasteLineCount(a.pastes[i].text)) {
			return &a.pastes[i]
		}
	}
	return nil
}

func (a *app) selectedPaste() (segment, *pasteChip, bool) {
	for _, s := range a.pasteSpans() {
		if a.input.cursor == s.from || a.input.cursor == s.to {
			return s, a.pasteForSpan(s), true
		}
	}
	return segment{}, nil, false
}

func (a *app) openSelectedPaste() bool {
	_, held, ok := a.selectedPaste()
	if !ok || held == nil {
		return false
	}
	a.pasteEdit = pasteEditor{open: true, n: held.n}
	a.pasteEdit.box.setText(held.text)
	return true
}

func (a *app) openPasteAt(at int) bool {
	for _, s := range a.pasteSpans() {
		if at >= s.from && at <= s.to {
			a.input.cursor = s.to
			return a.openSelectedPaste()
		}
	}
	return false
}

func (a *app) pasteChipKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s, held, selected := a.selectedPaste()
	switch msg.String() {
	case "left":
		for _, chip := range a.pasteSpans() {
			if a.input.cursor > chip.from && a.input.cursor <= chip.to {
				a.input.cursor = chip.from
				a.touch()
				return nil, true
			}
		}
	case "right", "ctrl+f":
		for _, chip := range a.pasteSpans() {
			if a.input.cursor >= chip.from && a.input.cursor < chip.to {
				a.input.cursor = chip.to
				a.touch()
				return nil, true
			}
		}
	case "backspace":
		if selected && a.input.cursor == s.to {
			a.removePaste(s, held)
			return a.edited(), true
		}
	case "delete":
		if selected && a.input.cursor == s.from {
			a.removePaste(s, held)
			return a.edited(), true
		}
	case "enter":
		if selected {
			return nil, a.openSelectedPaste()
		}
	default:
		if selected && msg.Key().Text != "" {
			a.input.cursor = s.to
		}
	}
	return nil, false
}

func (a *app) removePaste(s segment, held *pasteChip) {
	a.input.value = append(a.input.value[:s.from], a.input.value[s.to:]...)
	a.input.cursor = s.from
	if held != nil {
		for i := range a.pastes {
			if a.pastes[i].n == held.n {
				a.pastes = append(a.pastes[:i], a.pastes[i+1:]...)
				break
			}
		}
	}
	a.touch()
}

func (a *app) pasteDraftBlock(width, rows int) ([]string, int, int) {
	block, x, y := draftBlockWithTags(&a.input, a.pal, width, rows, a.trafficHint(), a.roomLead(width), a.input.demotedTags, a.draftInk())
	block = a.paintDraftMentions(block)
	for i, line := range block {
		plainLine := ansi.Strip(line)
		for _, held := range a.pastes {
			token := pasteToken(held.n, pasteLineCount(held.text))
			if strings.Contains(plainLine, token) {
				painted := a.pal.chip(token)
				if _, chosen, ok := a.selectedPaste(); ok && chosen != nil && chosen.n == held.n {
					painted = a.pal.mark(a.pal.chip(token), ansi.StringWidth(token))
				} else if a.hot.kind == hoverPaste && a.hot.index == held.n {
					painted = a.pal.cursor(a.pal.chip(token), ansi.StringWidth(token))
				}
				block[i] = strings.Replace(block[i], token, painted, 1)
			}
		}
	}
	return block, x, y
}

func (a *app) pastePointerAt(x, row int) int {
	if len(a.pastes) == 0 || a.pasteEdit.open {
		return 0
	}
	width, height := a.size()
	boxWidth := width - len(inputPad)
	if a.chipStrip(boxWidth) != "" {
		if row == 0 {
			return 0
		}
		row--
	}
	rows := min(draftRows, height-2)
	head := ansi.StringWidth(a.roomLead(boxWidth)) + ansi.StringWidth(prompt)
	room := max(4, boxWidth-head)
	at := draftClickIndex(a.input.value, a.input.cursor, row, x-len(inputPad)-head, room, rows)
	for _, s := range a.pasteSpans() {
		if at >= s.from && at < s.to {
			if held := a.pasteForSpan(s); held != nil {
				return held.n
			}
		}
	}
	return 0
}

func (a *app) rewritePasteToken(n int, before, after string) {
	old := pasteToken(n, pasteLineCount(before))
	newToken := pasteToken(n, pasteLineCount(after))
	value := strings.Replace(a.input.String(), old, newToken, 1)
	cursor := a.input.cursor + len([]rune(newToken)) - len([]rune(old))
	a.input.rewrite(value)
	a.input.cursor = max(0, min(cursor, len(a.input.value)))
}

func (a *app) pasteEditorKey(msg tea.KeyPressMsg) tea.Cmd {
	var held *pasteChip
	for i := range a.pastes {
		if a.pastes[i].n == a.pasteEdit.n {
			held = &a.pastes[i]
			break
		}
	}
	if held == nil {
		a.pasteEdit = pasteEditor{}
		return nil
	}
	before := held.text
	switch msg.String() {
	case "esc":
		a.pasteEdit = pasteEditor{}
		a.touch()
		return nil
	case "ctrl+x":
		for _, s := range a.pasteSpans() {
			if a.pasteForSpan(s) == held {
				a.removePaste(s, held)
				break
			}
		}
		a.pasteEdit = pasteEditor{}
		return a.edited()
	case "left":
		a.pasteEdit.box.left()
	case "right", "ctrl+f":
		a.pasteEdit.box.right()
	case "up":
		a.pasteEdit.box.up()
	case "down":
		a.pasteEdit.box.down()
	case "home", "ctrl+a", "super+left", "meta+left":
		a.pasteEdit.box.home()
	case "end", "ctrl+e", "super+right", "meta+right":
		a.pasteEdit.box.end()
	case "alt+left", "alt+b", "ctrl+left":
		a.pasteEdit.box.wordLeft()
	case "alt+right", "alt+f", "ctrl+right":
		a.pasteEdit.box.wordRight()
	case "backspace":
		a.pasteEdit.box.deleteBackward()
	case "delete":
		a.pasteEdit.box.deleteForward()
	case "enter", "alt+enter", "ctrl+j":
		a.pasteEdit.box.insert("\n")
	default:
		if msg.Key().Text != "" {
			a.pasteEdit.box.insert(msg.Key().Text)
		}
	}
	held.text = a.pasteEdit.box.String()
	if held.text != before {
		a.rewritePasteToken(held.n, before, held.text)
	}
	a.touch()
	return nil
}

func (a *app) pasteEditorFrame(width, height int) (string, int, int) {
	column := min(76, max(24, width-8))
	bodyRows := max(1, height-6)
	block, x, y := draftBlock(&a.pasteEdit.box, a.pal, column, bodyRows, "", "")
	title := fmt.Sprintf("paste %d · %d lines", a.pasteEdit.n, pasteLineCount(a.pasteEdit.box.String()))
	rows := []string{a.pal.muted(fit(title, column)), ""}
	rows = append(rows, block...)
	rows = append(rows, "", a.pal.dim(fit("esc keeps and closes · ctrl+x discards this paste", column)))
	top := max(0, (height-len(rows))/2)
	left := max(0, (width-column)/2)
	out := make([]string, 0, height)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, row := range rows {
		out = append(out, strings.Repeat(" ", left)+row)
	}
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out, "\n"), left + x, top + 2 + y
}

// ── WHAT THE MODEL READS IS NEVER THE TAG ───────────────────────────────────
//
// A paste chip is TWO texts: the tag the screen shows, `[paste 1 · 42 lines]`,
// and the forty-two lines it stands for. Every door that carries the box's
// words to the model must hand over the second, and every row that draws them
// keeps the first — and there was one function that did the unfolding, called
// from one door. A message parked over a running turn and then steered into it
// went through neither, so the model read the tag, said "I cannot see the
// paste", and worked from what it could guess. So the unfolding is one PURE
// function over one message's own chips, and each door asks it about the text
// it is about to send.

// unfoldPastes replaces every chip's tag in text with the text it holds, fenced
// and numbered so the words around it can still refer to "paste 2". It spends
// nothing: the chips it reads belong to the caller.
func unfoldPastes(text string, pastes []pasteChip) string {
	for _, held := range pastes {
		token := pasteToken(held.n, pasteLineCount(held.text))
		wrapped := fmt.Sprintf("paste %d:\n```text\n%s\n```", held.n, held.text)
		text = strings.ReplaceAll(text, token, wrapped)
	}
	return text
}

// pastesUnfolded is [unfoldPastes] over the box's own chips, WITHOUT spending them: for
// a reader that needs the words as the model would see them while the person is
// still composing (spellout.go).
func (a *app) pastesUnfolded(text string) string {
	return unfoldPastes(text, a.pastes)
}

// composed is THE DOOR between the box and anything that speaks for the person:
// the text as the model reads it, the text as the screen keeps it, and the box's
// chips spent — they went with the message. Every door that sends the box's
// words to the model, wherever that is (a reply, a steer into a running turn, a
// task's room, a card's answer), asks this and nothing else.
func (a *app) composed(text string) (spoken, shown string) {
	spoken = unfoldPastes(text, a.pastes)
	a.pastes = nil
	return spoken, text
}

// spoken is [app.composed]'s spoken half for a door that has already kept the
// shown text in its own hands.
func (a *app) spoken(text string) string {
	spoken, _ := a.composed(text)
	return spoken
}

// expandPastes is [app.composed]'s spoken half, kept under the name the submit
// door has always called it by.
func (a *app) expandPastes(text string) string {
	spoken, _ := a.composed(text)
	return spoken
}
