package tui3

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

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
	// from and to identify THIS inserted token in the draft. The visible token
	// is ordinary user text too, so searching or replacing by its spelling can
	// never prove which occurrence owns the held document.
	from int
	to   int
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
	if isCommandLine(strings.TrimSpace(a.input.String())) || pasteLineCount(text) < pasteChipLines {
		return false
	}
	n := 1
	for _, held := range a.pastes {
		if held.n >= n {
			n = held.n + 1
		}
	}
	token := pasteToken(n, pasteLineCount(text))
	inserted := a.spacedTokens([]string{token})
	at := a.input.cursor
	a.input.insert(inserted)
	a.editTags(at, at, len([]rune(inserted)))
	inside := strings.Index(inserted, token)
	from := at + len([]rune(inserted[:inside]))
	a.pastes = append(a.pastes, pasteChip{n: n, text: text, from: from, to: from + len([]rune(token))})
	return true
}

// editPastes carries token identities through ordinary draft edits. Editing
// before a token shifts it; touching the token dissolves the association rather
// than letting a now-different piece of prose unfold as hidden content.
func (a *app) editPastes(from, to, inserted int) {
	delta := inserted - (to - from)
	out := a.pastes[:0]
	for _, held := range a.pastes {
		switch {
		case to <= held.from:
			held.from += delta
			held.to += delta
			out = append(out, held)
		case from >= held.to:
			out = append(out, held)
		default:
			// The edit touched this chip. What remains is literal draft text.
		}
	}
	a.pastes = out
}

// replaceInput changes one rune range in the main draft and carries every
// identity attached to that draft through the same edit. Programmatic
// completions and chips use this instead of editor surgery so they obey the
// same positional law as keystrokes.
func (a *app) replaceInput(from, to int, replacement string) {
	from = max(0, min(from, len(a.input.value)))
	to = max(from, min(to, len(a.input.value)))
	runes := []rune(replacement)
	oldCursor := a.input.cursor
	a.editTags(from, to, len(runes))
	a.input.value = append(append(append([]rune(nil), a.input.value[:from]...), runes...), a.input.value[to:]...)
	switch {
	case oldCursor <= from:
		a.input.cursor = oldCursor
	case oldCursor >= to:
		a.input.cursor = oldCursor + len(runes) - (to - from)
	default:
		a.input.cursor = from + len(runes)
	}
	a.input.cursor = max(0, min(a.input.cursor, len(a.input.value)))
}

func (a *app) replaceAllInput(old, replacement string) {
	if old == "" {
		return
	}
	for {
		value := a.input.String()
		at := strings.LastIndex(value, old)
		if at < 0 {
			return
		}
		from := len([]rune(value[:at]))
		a.replaceInput(from, from+len([]rune(old)), replacement)
	}
}

func (a *app) pasteSpans() []segment {
	var out []segment
	for _, held := range a.pastes {
		token := pasteToken(held.n, pasteLineCount(held.text))
		if held.from < 0 || held.to > len(a.input.value) || held.from >= held.to ||
			string(a.input.value[held.from:held.to]) != token {
			continue
		}
		out = append(out, segment{from: held.from, to: held.to})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].from < out[j].from })
	return out
}

func (a *app) pasteForSpan(s segment) *pasteChip {
	for i := range a.pastes {
		if s.from == a.pastes[i].from && s.to == a.pastes[i].to {
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
	n := 0
	if held != nil {
		n = held.n
	}
	a.editTags(s.from, s.to, 0)
	a.input.value = append(a.input.value[:s.from], a.input.value[s.to:]...)
	a.input.cursor = s.from
	if n != 0 {
		for i := range a.pastes {
			if a.pastes[i].n == n {
				a.pastes = append(a.pastes[:i], a.pastes[i+1:]...)
				break
			}
		}
	}
	a.touch()
}

func (a *app) pasteDraftBlock(width, rows int) ([]string, int, int) {
	return a.pasteDraftBlockWith(a.pal, width, rows)
}

func (a *app) pasteDraftBlockWith(pal palette, width, rows int) ([]string, int, int) {
	return draftBlockPainted(&a.input, pal, width, rows, "", a.roomLead(width), func(row segment, _ bool) string {
		return a.paintPasteDraftRow(pal, row)
	})
}

// paintPasteDraftRow paints only the source spans that own held documents.
// A literal lookalike remains ordinary ink even when it appears before the real
// chip, and a chip split by soft wrapping keeps its style on both visible parts.
func (a *app) paintPasteDraftRow(pal palette, row segment) string {
	value := a.input.value
	var b strings.Builder
	at := row.from
	paintWords := func(from, to int) {
		if from >= to {
			return
		}
		boundary := from == 0 || unicode.IsSpace(value[from-1])
		b.WriteString(paintDraftCommands(string(value[from:to]), pal, pal.ink, from, boundary, a.input.demotedTags))
	}
	for _, span := range a.pasteSpans() {
		if span.to <= row.from {
			continue
		}
		if span.from >= row.to {
			break
		}
		from, to := max(span.from, row.from), min(span.to, row.to)
		paintWords(at, from)
		piece := string(value[from:to])
		painted := pal.chip(piece)
		held := a.pasteForSpan(span)
		if _, chosen, ok := a.selectedPaste(); ok && chosen == held {
			painted = pal.mark(painted, ansi.StringWidth(piece))
		} else if held != nil && a.hot.kind == hoverPaste && a.hot.index == held.n {
			painted = pal.cursor(painted, ansi.StringWidth(piece))
		}
		b.WriteString(painted)
		at = to
	}
	paintWords(at, row.to)
	return b.String()
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
	var at int
	found := false
	for i := range a.pastes {
		if a.pastes[i].n == n {
			at = i
			found = true
			break
		}
	}
	if !found {
		return
	}
	held := a.pastes[at]
	old := pasteToken(n, pasteLineCount(before))
	newToken := pasteToken(n, pasteLineCount(after))
	if held.from < 0 || held.to > len(a.input.value) || string(a.input.value[held.from:held.to]) != old {
		return
	}
	newRunes := []rune(newToken)
	cursor := a.input.cursor + len(newRunes) - (held.to - held.from)
	a.input.value = append(append(append([]rune(nil), a.input.value[:held.from]...), newRunes...), a.input.value[held.to:]...)
	a.input.editTags(held.from, held.to, len(newRunes))
	delta := len(newRunes) - (held.to - held.from)
	for i := range a.pastes {
		if a.pastes[i].n == n {
			a.pastes[i].to = a.pastes[i].from + len(newRunes)
		} else if a.pastes[i].from >= held.to {
			a.pastes[i].from += delta
			a.pastes[i].to += delta
		}
	}
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
	value := []rune(text)
	ordered := append([]pasteChip(nil), pastes...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].from > ordered[j].from })
	for _, held := range ordered {
		token := pasteToken(held.n, pasteLineCount(held.text))
		if held.from < 0 || held.to > len(value) || held.from >= held.to || string(value[held.from:held.to]) != token {
			continue
		}
		wrapped := []rune(fmt.Sprintf("paste %d:\n```text\n%s\n```", held.n, held.text))
		value = append(append(append([]rune(nil), value[:held.from]...), wrapped...), value[held.to:]...)
	}
	return string(value)
}

// trimPastePositions applies enter's documented outer trim to the identities
// recorded against the editor. The message passed to [unfoldPastes] then uses
// the same coordinate space even when the draft began with whitespace.
func (a *app) trimPastePositions(raw string) {
	left := strings.TrimLeftFunc(raw, unicode.IsSpace)
	cut := len([]rune(raw)) - len([]rune(left))
	if cut == 0 {
		return
	}
	for i := range a.pastes {
		a.pastes[i].from -= cut
		a.pastes[i].to -= cut
	}
}

// pastesUnfolded is [unfoldPastes] over the box's own chips, WITHOUT spending them: for
// a reader that needs the words as the model would see them while the person is
// still composing (spellout.go).
func (a *app) pastesUnfolded(text string) string {
	return unfoldPastes(text, a.pastesForText(text))
}

// pastesForText returns chip coordinates in the supplied text's space without
// moving the editor's identities. Card and room doors trim before they ask for
// the model form; spellout asks with the raw draft and therefore needs no move.
func (a *app) pastesForText(text string) []pasteChip {
	pastes := append([]pasteChip(nil), a.pastes...)
	raw := a.input.String()
	if text != strings.TrimSpace(raw) || text == raw {
		return pastes
	}
	left := strings.TrimLeftFunc(raw, unicode.IsSpace)
	cut := len([]rune(raw)) - len([]rune(left))
	for i := range pastes {
		pastes[i].from -= cut
		pastes[i].to -= cut
	}
	return pastes
}

// composed is THE DOOR between the box and anything that speaks for the person:
// the text as the model reads it, the text as the screen keeps it, and the box's
// chips spent — they went with the message. Every door that sends the box's
// words to the model, wherever that is (a reply, a steer into a running turn, a
// task's room, a card's answer), asks this and nothing else.
func (a *app) composed(text string) (spoken, shown string) {
	spoken = unfoldPastes(text, a.pastesForText(text))
	a.pastes = nil
	return spoken, text
}

// composedCommandArgument moves paste identities from a leading command line
// into the argument's coordinate space, then returns the two forms of that
// argument. The command word is surface syntax: it belongs in neither the
// model payload nor the compact text retained by a task or standing-order row.
//
// The identities are returned as well as consumed because a standing message
// typed over a running answer is parked before it is sent. Its compact token
// and hidden text must remain one editable message while it waits.
func (a *app) composedCommandArgument(line string) (spoken, shown string, held []pasteChip) {
	_, shown, ok := splitCommandLine(line)
	if !ok || shown == "" {
		a.pastes = nil
		return shown, shown, nil
	}
	runes := []rune(line)
	at := 1 // leading slash
	for at < len(runes) && !unicode.IsSpace(runes[at]) {
		at++
	}
	for at < len(runes) && unicode.IsSpace(runes[at]) {
		at++
	}
	held = append([]pasteChip(nil), a.pastes...)
	for i := range held {
		held[i].from -= at
		held[i].to -= at
	}
	spoken = unfoldPastes(shown, held)
	a.pastes = nil
	return spoken, shown, held
}

// composedWithoutTag performs the two substitutions against one coordinate
// system: paste tokens expand and the live send-door tag disappears from the
// model payload. Paste expansion before the tag moves its range by the exact
// rune delta; paste text and a literal lookalike token remain unrelated.
func (a *app) composedWithoutTag(text string, tag segment) (spoken, shown string) {
	spoken = unfoldPastesWithoutTag(text, a.pastes, tag)
	a.pastes = nil
	return spoken, text
}

func unfoldPastesWithoutTag(text string, pastes []pasteChip, tag segment) string {
	moved := tag
	for _, held := range pastes {
		if held.to > tag.from {
			continue
		}
		wrapped := fmt.Sprintf("paste %d:\n```text\n%s\n```", held.n, held.text)
		movedBy := len([]rune(wrapped)) - (held.to - held.from)
		moved.from += movedBy
		moved.to += movedBy
	}
	spoken := unfoldPastes(text, pastes)
	return removeSlashTag([]rune(spoken), moved)
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
