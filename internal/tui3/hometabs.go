package tui3

import (
	"path/filepath"
	"sort"
)

// homeClosedLimit keeps recently closed conversations within easy reach without
// turning Home back into a history list. Typing still searches the whole history.
const homeClosedLimit = 3

// conversationRows joins the window's tab membership with Home's cached facts.
// No disk reading happens here: a newly opened tab appears even before the next
// world refresh, and a closed one disappears from the open list immediately.
func (h *homeView) conversationRows() (open, closed []switcherRow) {
	if h.tabs == nil {
		return nil, nil
	}
	byFile := map[string]switcherRow{}
	var archived []switcherRow
	for _, project := range h.world.Projects {
		for _, row := range project.Sessions {
			read := switcherRow{kind: switcherConversation, session: row, project: project.Name,
				title: homeName(row), age: sinceAt(row.At, h.world.Read), at: switcherSortAt(row), gone: h.gone[switcherWhere(row, project)],
				place: switcherWhere(row, project)}
			byFile[filepath.Clean(row.Transcript)] = read
			if row.Archived {
				archived = append(archived, read)
			}
		}
	}
	seen := map[string]bool{}
	fromTab := func(tab chatTab, isOpen bool) switcherRow {
		row := byFile[filepath.Clean(tab.file)]
		row.kind, row.title, row.here = switcherConversation, tab.word, isOpen && tab.here
		row.chatKey = tab.key
		row.session.Transcript = tab.file
		row.held = row.session.Open && !tab.held && !tab.here
		row.door = row.held && !h.far && row.session.Dir != ""
		if row.session.Workspace == "" {
			row.session.Workspace = tab.where
		}
		row.place = row.session.Workspace
		if isOpen {
			row.session.Archived = false
		}
		return row
	}
	for _, tab := range h.tabs() {
		// A run tab is a view of its conversation, not another conversation.
		if tab.work {
			continue
		}
		seen[filepath.Clean(tab.file)] = true
		open = append(open, fromTab(tab, true))
	}
	if h.closedTabs != nil {
		tabs := h.closedTabs()
		for i := len(tabs) - 1; i >= 0 && len(closed) < homeClosedLimit; i-- {
			tab := tabs[i]
			file := filepath.Clean(tab.file)
			if seen[file] {
				continue
			}
			seen[file] = true
			closed = append(closed, fromTab(tab, false))
		}
	}
	// The close stack gives this window's exact close order. Persisted archived
	// conversations fill any remaining slots after a restart, newest activity first.
	sort.SliceStable(archived, func(i, j int) bool { return archived[i].at.After(archived[j].at) })
	for _, row := range archived {
		file := filepath.Clean(row.session.Transcript)
		if seen[file] || len(closed) >= homeClosedLimit {
			continue
		}
		seen[file] = true
		closed = append(closed, row)
	}
	return open, closed
}
