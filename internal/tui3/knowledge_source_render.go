package tui3

import (
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// Only the currently read page is retained. A new snapshot, terminal width,
// page or measured prose style invalidates its immutable Markdown projection.
type knowledgeSourceRender struct {
	noteID string
	width  int
	styler *tokens.Styler
	lines  []string
}

func (a *app) knowledgeSourceRows(note knowledge.Page, width int) []string {
	cached := &a.kb.sourceRender
	styler := a.styler()
	if cached.noteID == note.ID && cached.width == width && cached.styler == styler {
		return cached.lines
	}
	var rows []string
	for _, e := range knowledgeSources(note.Evidence) {
		rows = append(rows, a.pal.data(knowledgeSourceLabel(e)))
		rows = append(rows, a.renderMarkdown(e.Quote, max(1, width))...)
		rows = append(rows, "")
	}
	*cached = knowledgeSourceRender{noteID: note.ID, width: width, styler: styler, lines: rows}
	return rows
}
