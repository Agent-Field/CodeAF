package composer

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Paste handles bracketed-paste content. See doc.go for why this cannot be
// part of [Model.Key]: tea.PasteMsg is not a tea.KeyPressMsg, so PaneKeys has
// no seam for it, and this method is what the shell wiring routes tea.PasteMsg
// to instead (the natural PanePaste-shaped door, mirroring PaneMouse/PaneKeys
// in tui2/pane.go).
//
// Pasted content is always inserted as draft text and never submitted —
// however many lines it carries, OnSubmit is not called, which is the whole
// of 7.2's "multi-line paste never auto-sends" rule. Carriage returns are
// normalized to '\n' first: a paste's line endings are a property of
// whatever the terminal/OS on the other end used, not a character this
// package's line-boundary arithmetic (edit.go, wrap.go) knows about, and an
// un-normalized '\r' would be treated as one more printable rune instead of
// the line break it is.
func (m *Model) Paste(msg tea.PasteMsg) tea.Cmd {
	content := strings.ReplaceAll(msg.Content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	m.insert(content)
	return nil
}
