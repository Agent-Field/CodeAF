package tui3

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The grid and membership pickers share the switcher's world seam. Reading
// the catalog never resumes a conversation, takes its lock, or creates a tab.
type conversationCandidate struct {
	tab   chatTab
	saved session.SessionRow
}

func (a *app) conversationCatalogRead(take func([]conversationCandidate, bool) tea.Cmd) tea.Cmd {
	door, root, hosted := a.world, a.placesRoot(), a.hosted()
	return a.besideLine(func() func(bool) tea.Cmd {
		world, known := worldSeam(door, root, hosted)
		saved := conversationSavedCandidates(world, hosted)
		return func(bool) tea.Cmd { return take(a.conversationCatalogMerge(saved), known) }
	})
}

// The open strip keeps its order; the remaining saved conversations follow
// by recency. Archived is a visibility preference, not permanent deletion.
func (a *app) conversationCatalog(world session.World) []conversationCandidate {
	return a.conversationCatalogMerge(conversationSavedCandidates(world, a.hosted()))
}

// Canonical keys are resolved on the reading command, never while painting.
func conversationSavedCandidates(world session.World, hosted bool) []conversationCandidate {
	var out []conversationCandidate
	for _, row := range world.Sessions() {
		if row.DeletionPending || strings.TrimSpace(row.Transcript) == "" {
			continue
		}
		key := filepath.Clean(row.Transcript)
		if !hosted {
			key = convKey(row.Transcript)
		}
		where := row.ProjectDir
		if where == "" {
			where = row.Workspace
		}
		out = append(out, conversationCandidate{tab: chatTab{key: key, file: row.Transcript, where: where, word: homeName(row), full: homeName(row)}, saved: row})
	}
	return out
}

func (a *app) conversationCatalogMerge(saved []conversationCandidate) []conversationCandidate {
	byKey := make(map[string]session.SessionRow, len(saved))
	for _, row := range saved {
		byKey[row.tab.key] = row.saved
	}
	seen := map[string]bool{}
	var out []conversationCandidate
	add := func(tab chatTab) {
		if tab.start || tab.work || tab.key == "" || seen[tab.key] || a.conversationDeleted(tab.file) {
			return
		}
		seen[tab.key] = true
		out = append(out, conversationCandidate{tab: tab, saved: byKey[tab.key]})
	}
	for _, tab := range a.tabList() {
		add(tab)
	}
	for _, key := range a.prev {
		if held := a.behind[key]; held != nil {
			word := "Conversation"
			if held.side != nil && held.side.title != "" {
				word = held.side.title
			}
			add(chatTab{key: key, file: held.conv.SessionFile, where: held.conv.Workspace, word: word})
		}
	}
	for _, row := range saved {
		add(row.tab)
	}
	return out
}

func (a *app) conversationDeleted(file string) bool {
	return file != "" && a.deletedSessionRows[tasksChatKey(filepath.Base(filepath.Dir(file)))]
}

// Picker columns deliberately contain names and projects, never internal IDs.
func conversationColumns(tab chatTab, width int) string {
	nameWidth := max(width*2/3, 1)
	projectWidth := max(width-nameWidth-2, 1)
	project := ""
	if tab.where != "" {
		project = filepath.Base(tab.where)
	}
	return teamsPad(tab.word, nameWidth) + "  " + teamsPad(project, projectWidth)
}
