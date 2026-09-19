package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// folderCollabVerbs is the optional mark/coordinate strip. NIL IS ABSENT: a
// surface without Collab never offers these keys, so Wave 1 membership verbs
// stay exactly `n f e i` / `n f m w x`. Marking is a convenience; `c` is
// offered only when something is already marked, because the primary path is
// saying "coordinate these" in the ordinary chat.
func (a *app) folderCollabVerbs(line homeLine) []verb {
	if a.collab == nil {
		return nil
	}
	var verbs []verb
	if line.kind == homeSession {
		word := collabMarkWord
		if a.collabMarked(a.placementRefOf(line)) {
			word = collabUnmarkWord
		}
		verbs = append(verbs, verb{key: 'k', word: word, do: func() tea.Cmd { return a.toggleCollabMark(line) }})
	}
	if len(a.collabView.marks) > 0 {
		verbs = append(verbs, verb{key: 'c', word: collabCoordinateWord, do: func() tea.Cmd { return a.coordinateMarked() }})
	}
	return verbs
}

// toggleCollabMark is `k` on a folders-panel chat: mark is a convenience, not
// a required ritual. The current ordinary chat stays the coordinator.
func (a *app) toggleCollabMark(line homeLine) tea.Cmd {
	if a.collab == nil {
		return nil
	}
	ref := a.placementRefOf(line)
	if ref == "" {
		a.folderNote(collabNoStandWord)
		return nil
	}
	if a.collabMarked(ref) {
		return a.unmarkCollab(ref)
	}
	return a.markCollab(ref)
}

func (a *app) markCollab(ref string) tea.Cmd {
	if err := a.collab.Mark(a.folderCtx(), ref); err != nil {
		a.folderNote(collabCouldNotMark)
		return nil
	}
	a.refreshFolderMemo()
	return nil
}

func (a *app) unmarkCollab(ref string) tea.Cmd {
	if err := a.collab.Unmark(a.folderCtx(), ref); err != nil {
		a.folderNote(collabCouldNotUnmark)
		return nil
	}
	a.refreshFolderMemo()
	return nil
}

// coordinateMarked is `c`: this existing chat becomes the management
// conversation for the marked IDs. Natural language is the unmarked path;
// an empty conversation id is a refusal, never a dummy success.
func (a *app) coordinateMarked() tea.Cmd {
	if a.collab == nil {
		return nil
	}
	id := strings.TrimSpace(a.conversationRef())
	if id == "" {
		a.folderNote(collabNeedChatWord)
		return nil
	}
	if err := a.collab.CoordinateMarked(a.folderCtx(), id); err != nil {
		a.folderNote(collabCouldNotCoord)
		return nil
	}
	a.refreshFolderMemo()
	return nil
}
