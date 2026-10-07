package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// sheetChoice keeps navigation separate from the persisted value. Opening or
// cancelling the list never invokes the registry writer.
const sheetChoiceCancelHover = -102

type sheetChoice struct {
	item    sheetItem
	cursor  int
	current string
}

func (a *app) choiceKey(msg tea.KeyPressMsg) tea.Cmd {
	a.sheet.savedKey = ""
	c := a.sheet.choice
	if c == nil {
		return nil
	}
	switch msg.String() {
	case "esc":
		a.sheet.choice = nil
		a.sheet.msg = ""
	case "up", "ctrl+p":
		c.cursor = max(0, c.cursor-1)
	case "down", "ctrl+n":
		c.cursor = min(len(c.item.row.Choices)-1, c.cursor+1)
	case "home":
		c.cursor = 0
	case "end":
		c.cursor = len(c.item.row.Choices) - 1
	case "enter", "space", " ":
		return a.saveSheetChoice()
	}
	a.touch()
	return nil
}

func (a *app) saveSheetChoice() tea.Cmd {
	c := a.sheet.choice
	if c == nil || c.cursor < 0 || c.cursor >= len(c.item.row.Choices) {
		return nil
	}
	if a.applySetting(c.item, c.item.row.Choices[c.cursor]) {
		a.sheet.choice = nil
	}
	a.touch()
	return nil
}

func (s *sheet) choiceRows(width, room int, pal palette, hovered int) []placeRow {
	c := s.choice
	if room < 1 {
		return nil
	}
	rows := []placeRow{{text: "  " + placeHeading(fit(c.item.meta.label, max(1, width-4)), pal)}}
	// Reserve one line per visible option and the Cancel action before adding
	// explanation, so a small terminal cannot hide the selected answer.
	optionRoom := max(1, room-3)
	count := min(len(c.item.row.Choices), optionRoom)
	top := max(0, min(c.cursor-count+1, len(c.item.row.Choices)-count))
	hint := c.item.row.ChatPresentation().Description
	if hint == "" {
		hint = c.item.row.Hint
	}
	hintRoom := max(0, room-count-4)
	for _, line := range wrap(hint, max(1, width-4)) {
		if hintRoom == 0 {
			break
		}
		rows = append(rows, placeRow{text: "  " + pal.dim(line)})
		hintRoom--
	}
	if room-len(rows) > count+1 {
		rows = append(rows, placeRow{})
	}
	for i := top; i < top+count; i++ {
		raw := c.item.row.Choices[i]
		label := settingChoiceWord(c.item.row.Key, raw, raw)
		current := ""
		if raw == c.current {
			current = " · current"
		}
		// Fit the label to keep each option a single pointer target even when narrow.
		label = fit(label, max(1, width-16)) + current
		lines := overlayLines(label, "", i == c.cursor, false, hovered == i, width, pal)
		if len(lines) > 0 {
			rows = append(rows, placeRow{text: lines[0], hit: sheetHit{kind: sheetHitChoice, index: i}})
		}
	}
	if s.msg != "" && len(rows) < room-1 {
		rows = append(rows, placeRow{text: "  " + pal.bad(fit(s.msg, max(1, width-4)))})
	}
	cancel := pal.ink("Cancel")
	if hovered == sheetChoiceCancelHover {
		cancel = pal.underline(cancel)
	}
	rows = append(rows, placeRow{text: "  " + cancel + pal.dim("  esc"), hit: sheetHit{kind: sheetHitChoiceCancel}})
	for _, line := range wrap(c.item.row.ChatPresentation().Activation, max(1, width-4)) {
		if len(rows) >= room {
			break
		}
		rows = append(rows, placeRow{text: "  " + pal.dim(line)})
	}
	return rows
}
