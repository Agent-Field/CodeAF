package tui3

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

type knowledgePageEdit struct {
	id, revision, title     string
	text                    editor
	deleting, busy          bool
	problem                 string
	top, caretRow, caretCol int
	followCaret             bool
	lineCount, viewport     int
}

func (a *app) knowledgeEditAction(action string) (tea.Cmd, bool) {
	p := &a.kb
	switch action {
	case "@edit", "@delete":
		if p.edit != nil {
			return nil, true
		}
		n, ok := p.page()
		if !ok {
			return nil, true
		}
		p.edit = &knowledgePageEdit{id: n.ID, revision: n.Revision, title: n.Title, deleting: action == "@delete", followCaret: true}
		p.edit.text.setText(knowledge.PageMarkdown(n))
		if p.sources {
			p.toggleSources()
		}
		p.links = false
		a.bar.on = false
	case "@cancel":
		if p.edit != nil && !p.edit.busy {
			p.edit = nil
			a.touch()
			return a.knowledgeRead(false), true
		}
	case "@save", "@confirm-delete":
		if p.edit == nil || p.edit.busy {
			return nil, true
		}
		return a.knowledgeWrite(action == "@confirm-delete"), true
	default:
		return nil, false
	}
	a.touch()
	return nil, true
}

func (a *app) knowledgeEditKey(msg tea.KeyPressMsg) tea.Cmd {
	e := a.kb.edit
	if e.busy {
		return nil
	}
	e.followCaret = true
	k := msg.String()
	if k == "esc" {
		cmd, _ := a.knowledgeEditAction("@cancel")
		return cmd
	}
	if e.deleting {
		if k == "ctrl+d" {
			cmd, _ := a.knowledgeEditAction("@confirm-delete")
			return cmd
		}
		if k == "enter" {
			cmd, _ := a.knowledgeEditAction("@cancel")
			return cmd
		}
		a.touch()
		return nil
	}
	switch k {
	case "ctrl+s":
		cmd, _ := a.knowledgeEditAction("@save")
		return cmd
	case "enter", "shift+enter", "alt+enter", "ctrl+j":
		e.text.insert("\n")
	case "up", "ctrl+p":
		e.text.up()
	case "down", "ctrl+n":
		e.text.down()
	case "tab":
		e.text.insert("    ")
	case "pgup":
		for i := 0; i < 12; i++ {
			e.text.up()
		}
	case "pgdown":
		for i := 0; i < 12; i++ {
			e.text.down()
		}
	default:
		listNavigate(msg, &e.text, func(int) {}, func() {}, memoryPlaceRows)
	}
	a.touch()
	return nil
}

func (a *app) knowledgeWrite(deleting bool) tea.Cmd {
	p := &a.kb
	e := p.edit
	if e == nil || p.vault == nil {
		return nil
	}
	v, id, revision, markdown := p.vault, e.id, e.revision, e.text.String()
	e.busy = true
	e.problem = ""
	p.loading = true
	a.touch()
	return func() tea.Msg {
		var err error
		if deleting {
			err = v.DeletePage(id, revision)
		} else {
			err = v.EditPage(id, revision, markdown)
		}
		msg := knowledgeReadingMsg{vault: v, err: err, editID: id, mutation: "save"}
		msg.mutationApplied = errors.Is(err, knowledge.ErrPageChangeApplied)
		if deleting {
			msg.mutation = "delete"
		}
		if err == nil {
			msg.mutationApplied = true
			msg.reading, msg.err = v.Snapshot()
		}
		return msg
	}
}

func knowledgeActionRow(pal palette, hover string, actions ...string) (string, []knowledgeCellLink) {
	var out strings.Builder
	var hits []knowledgeCellLink
	at := 0
	for i := 0; i+1 < len(actions); i += 2 {
		if i > 0 {
			out.WriteString(pal.dim("  "))
			at += 2
		}
		label, id := actions[i], actions[i+1]
		if hover == id {
			out.WriteString(taskLinkHotInk(pal, label))
		} else {
			out.WriteString(taskLinkInk(pal, label))
		}
		end := at + ansi.StringWidth(label)
		hits = append(hits, knowledgeCellLink{at, end, id})
		at = end
	}
	return out.String(), hits
}

func (a *app) knowledgeEditBody(width, room int) []placeRow {
	e := a.kb.edit
	var rows []placeRow
	add := func(s string) { rows = append(rows, placeRow{text: " " + fit(s, max(1, width-2)), hit: -1}) }
	if e.deleting {
		add(a.pal.muted("Delete " + e.title + "?"))
		add("")
		for _, line := range wrapText("Removes this page from knowledge; original chats remain. Related pages are kept.", max(4, width-4)) {
			add(line)
		}
		add("")
		text, hits := knowledgeActionRow(a.pal, a.kb.hoverLink, "Cancel", "@cancel", "Delete page", "@confirm-delete")
		add(text + a.pal.dim(a.chords.say(" · enter cancel · ctrl+d delete")))
		rows[len(rows)-1].hit = knowledgeHit{tree: -1, links: knowledgeShift(hits, 1)}
	} else {
		heading := "Edit " + e.title
		if e.busy {
			heading += " · Saving…"
		}
		add(a.pal.muted(heading))
		actions, hits := knowledgeActionRow(a.pal, a.kb.hoverLink, "Save", "@save", "Cancel", "@cancel")
		add(actions + a.pal.dim(a.chords.say(" · ctrl+s save · esc cancel")))
		rows[len(rows)-1].hit = knowledgeHit{tree: -1, links: knowledgeShift(hits, 1)}
		add(a.pal.dim("Markdown · [[Topic]] links · your text is preserved as a human edit"))
		if e.problem != "" {
			for _, line := range wrapText(e.problem, max(4, width-4)) {
				add(a.pal.bad(line))
			}
		}
		add("")
		lines, offsets := wrapWithOffsets(strings.ReplaceAll(e.text.String(), "\t", "    "), max(4, width-4))
		displayCursor := len([]rune(strings.ReplaceAll(string(e.text.value[:e.text.cursor]), "\t", "    ")))
		cursorRow, col := 0, 0
		for i, start := range offsets {
			if start <= displayCursor {
				cursorRow = i
				length := max(0, min(displayCursor-start, len([]rune(lines[i]))))
				col = ansi.StringWidth(string([]rune(lines[i])[:length]))
			}
		}
		visible := max(1, room-len(rows))
		e.lineCount, e.viewport = len(lines), visible
		if e.followCaret {
			e.top = placeTop(e.top, cursorRow, len(lines), visible)
		} else {
			e.top = max(0, min(e.top, max(0, len(lines)-visible)))
		}
		e.caretRow, e.caretCol = len(rows)+cursorRow-e.top, 1+col
		if cursorRow < e.top || cursorRow >= e.top+visible {
			e.caretRow = -1
		}
		for i := e.top; i < len(lines) && len(rows) < room; i++ {
			add(lines[i])
			rows[len(rows)-1].hit = knowledgeHit{tree: -1, editor: true, editOffset: offsets[i], editLine: lines[i]}
		}
	}
	if e.busy {
		add(a.pal.dim("Saving…"))
	} else if e.deleting && e.problem != "" {
		add(a.pal.bad(e.problem))
	}
	if len(rows) > room {
		rows = rows[:room]
	}
	return rows
}

func (a *app) knowledgeAdoptEdit(msg knowledgeReadingMsg) {
	p := &a.kb
	if msg.mutation == "" || p.edit == nil || p.edit.id != msg.editID {
		return
	}
	p.edit.busy = false
	if msg.err != nil {
		if msg.mutationApplied {
			result := "Saved"
			if msg.mutation == "delete" {
				result = "Deleted"
			}
			p.edit.problem = fmt.Sprintf("%s, but refresh failed: %v. Cancel to reload.", result, msg.err)
			return
		}
		p.edit.problem = fmt.Sprintf("Could not %s: %v. Your draft is still here.", msg.mutation, msg.err)
		return
	}
	p.edit = nil
	p.article = true
	p.scroll = 0
	p.hoverLink = ""
	if msg.mutation == "delete" {
		p.selected = ""
		p.filter.reset()
		p.cursor, p.top = 0, 0
		kept := p.history[:0]
		for _, id := range p.history {
			if id != msg.editID {
				kept = append(kept, id)
			}
		}
		p.history = kept
	}
}

func (placeMemory) paste(a *app, text string) (tea.Cmd, bool) {
	if !a.kb.open || a.kb.edit == nil {
		return nil, false
	}
	if !a.kb.edit.busy && !a.kb.edit.deleting {
		a.kb.edit.text.insert(text)
		a.kb.edit.followCaret = true
		a.touch()
	}
	return nil, true
}

// Markdown changes source offsets. Invisible markers preserve each canonical
// reference through rendering; only their terminal cell ranges are retained.
func (a *app) knowledgeMarkdownParagraph(p knowledge.Paragraph, width int) ([]string, [][]knowledgeCellLink) {
	raw := []rune(p.Text)
	stem := "\u2063\u2063"
	for strings.Contains(p.Text, stem) {
		stem += "\u2063"
	}
	begins, ends := make([]string, len(p.InlineLinks)), make([]string, len(p.InlineLinks))
	var replacements []string
	for i := len(p.InlineLinks) - 1; i >= 0; i-- {
		l := p.InlineLinks[i]
		if l.Start < 0 || l.End > len(raw) || l.End <= l.Start {
			continue
		}
		bits := ""
		// Encode the index with zero-width characters. Markers survive Markdown but
		// occupy no terminal cells, and distinguish real references from code literals.
		for _, digit := range strconv.Itoa(i) {
			bits += strings.Repeat("\u2060", int(digit-'0')+1) + "\u200c"
		}
		begins[i], ends[i] = stem+bits+"\u2063", stem+bits+"\u2064"
		replacements = append(replacements, begins[i], "", ends[i], "")
		label := l.Label
		if label == "" {
			label = string(raw[l.Start:l.End])
		}
		replacement := begins[i] + "[[" + label + "]]" + ends[i]
		raw = append(append(append([]rune{}, raw[:l.Start]...), []rune(replacement)...), raw[l.End:]...)
	}
	lines := a.renderMarkdown(string(raw), width)
	stripper := strings.NewReplacer(replacements...)
	var original strings.Builder
	for _, line := range lines {
		original.WriteString(ansi.Strip(line))
		original.WriteByte('\n')
	}
	joined := original.String()
	starts := make([]int, len(lines))
	plain := make([]string, len(lines))
	at := 0
	for i := range lines {
		lines[i] = stripper.Replace(lines[i])
		plain[i] = ansi.Strip(lines[i])
		starts[i] = at
		at += len(plain[i]) + 1
	}
	cells := make([][]knowledgeCellLink, len(lines))
	for i, l := range p.InlineLinks {
		if begins[i] == "" {
			continue
		}
		start := strings.Index(joined, begins[i])
		if start < 0 {
			continue
		}
		start += len(begins[i])
		end := strings.Index(joined[start:], ends[i])
		if end < 0 {
			continue
		}
		end += start
		lo, hi := len(stripper.Replace(joined[:start])), len(stripper.Replace(joined[:end]))
		for row, start := range starts {
			from, to := max(start, lo), min(start+len(plain[row]), hi)
			if from < to {
				cells[row] = append(cells[row], knowledgeCellLink{ansi.StringWidth(plain[row][:from-start]), ansi.StringWidth(plain[row][:to-start]), l.ID})
			}
		}
	}
	for i := range cells {
		sort.Slice(cells[i], func(x, y int) bool { return cells[i][x].start < cells[i][y].start })
	}
	return lines, cells
}

// One active page's immutable Markdown projection is retained. Snapshots,
// revisions, terminal width and prose style invalidate it; hover never reparses.
type knowledgeMarkdownRender struct {
	id, revision string
	width        int
	styler       *tokens.Styler
	paragraphs   map[[2]int]knowledgeMarkdownRows
}
type knowledgeMarkdownRows struct {
	lines []string
	cells [][]knowledgeCellLink
}

func (a *app) knowledgeHumanParagraph(note knowledge.Page, section, paragraph, width int) ([]string, [][]knowledgeCellLink) {
	cache := &a.kb.markdownRender
	styler := a.styler()
	if cache.id != note.ID || cache.revision != note.Revision || cache.width != width || cache.styler != styler {
		*cache = knowledgeMarkdownRender{id: note.ID, revision: note.Revision, width: width, styler: styler, paragraphs: map[[2]int]knowledgeMarkdownRows{}}
	}
	key := [2]int{section, paragraph}
	if rows, ok := cache.paragraphs[key]; ok {
		return rows.lines, rows.cells
	}
	lines, cells := a.knowledgeMarkdownParagraph(note.Sections[section].Paragraphs[paragraph], width)
	cache.paragraphs[key] = knowledgeMarkdownRows{lines, cells}
	return lines, cells
}

func (a *app) knowledgeEditorWheel(delta int) tea.Cmd {
	e := a.kb.edit
	if e == nil || e.busy || e.deleting {
		return nil
	}
	e.followCaret = false
	e.top = max(0, min(e.top+delta, max(0, e.lineCount-e.viewport)))
	a.touch()
	return nil
}

func (a *app) knowledgeEditorCaret(hit knowledgeHit, x int) {
	e := a.kb.edit
	if e == nil || e.busy || e.deleting {
		return
	}
	cell, offset := 0, 0
	for _, r := range hit.editLine {
		step := ansi.StringWidth(string(r))
		if cell+step > max(0, x-1) {
			break
		}
		cell += step
		offset++
	}
	target := hit.editOffset + offset
	display, original := 0, 0
	for _, r := range e.text.value {
		step := 1
		if r == '\t' {
			step = 4
		}
		if display+step > target {
			break
		}
		display += step
		original++
	}
	e.text.cursor = min(original, len(e.text.value))
	e.text.dropPick()
	e.followCaret = true
	a.touch()
}
