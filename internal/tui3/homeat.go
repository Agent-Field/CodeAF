package tui3

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

// ── THE `@` LIST ON HOME ─────────────────────────────────────────────────────
//
// Home's box is a draft for a conversation that does not exist yet, and until
// 2026-09-22 an `@` typed into it was two letters of a search: the completion
// that every conversation's box has (files.go) was bound to that box alone. The
// owner met it as a bug — the hint on home's own row promised the list — and
// this file is the other binding: the same [completion], synced against home's
// box, drawn as rows of home's column exactly as the slash list is
// (homeslash.go's [homeView.commandLines]), and answered by the same enter.
//
// WHAT IT WALKS IS WHERE THE NEXT CONVERSATION OPENS ([app.targetWhere]),
// because that is the folder the sentence is about; a target moved by `alt+p`
// or `/folder` walks again. It offers files and folders and never tasks: a task
// pointer is minted when a conversation sends (taskmention.go), and home has no
// conversation to mint it in yet.
//
// A PICTURE CHOSEN HERE GOES ON HOME'S TRAY, the way one chosen in a
// conversation goes on that conversation's ([app.completeFile]): the half-typed
// token comes out of the sentence and the chip rides into the conversation that
// opens next ([homeView.carrying]).

// homeCompletion is one row of that list. It is numbered outside the
// [homeRowKind] iota block for [homeCommand]'s reason.
const homeCompletion homeRowKind = 251

// homeCompletionHint is the foot while the list is up: the three keys it takes.
const homeCompletionHint = "↑↓ pick · enter put it in · esc back"

// The list's two empty states, in the conversation list's own words
// ([completion.rows]): the walk still running, and a query nothing matches.
const (
	homeLookingWord = "looking…"
	homeNoFileWord  = "no file matches"
)

// completionLines syncs the list against the box and hands back its file rows,
// or nothing while the box holds no `@` token. It is asked after the command
// list, which wins when both could be open (app.go's [app.syncLists] states the
// same law for the conversation's box).
func (h *homeView) completionLines() []homeLine {
	h.comp.sync(&h.box)
	if !h.comp.open {
		return nil
	}
	lines := make([]homeLine, 0, len(h.comp.lines))
	for i, line := range h.comp.lines {
		if line.header != "" || line.file < 0 {
			continue
		}
		lines = append(lines, homeLine{kind: homeCompletion, comp: i})
	}
	return lines
}

// homeCompletionRow paints one offered path: the path, and in the margin what
// the row is — `folder`, or `img` for a picture that choosing will attach.
func (a *app) homeCompletionRow(line homeLine, at, width int, pal palette) string {
	h := &a.home
	path, note, ok := h.completionWords(line)
	if !ok {
		return ""
	}
	return overlayRow(path, note, at == h.cursor, false, at == h.hover && at == h.cursor, width, pal)
}

// completionWords is the path a completion row offers and its tag, and false
// for a row that no longer points into the list.
func (h *homeView) completionWords(line homeLine) (path, note string, ok bool) {
	c := &h.comp
	if line.comp < 0 || line.comp >= len(c.lines) || c.lines[line.comp].file < 0 {
		return "", "", false
	}
	return c.all[c.lines[line.comp].file], c.lineNote(line.comp), true
}

// loadHomeFiles walks the target folder for the list, once per target: a
// walk already done or already running is left alone, and a target that moved
// since the last walk starts a fresh one. It is asked after every key on home
// (place_home.go), and answers nil on every key that did not open the list.
func (a *app) loadHomeFiles() tea.Cmd {
	h := &a.home
	if !h.comp.open {
		return nil
	}
	root := a.targetWhere()
	if root == "" {
		root = a.pathRoot()
	}
	if root == "" {
		return nil
	}
	if h.walked != root {
		h.comp.all, h.comp.loaded, h.comp.loading = nil, false, false
		h.walked = root
	}
	if h.comp.loaded || h.comp.loading {
		return nil
	}
	// Tasks never load here (the file's own note), so the list is never
	// waiting on them.
	h.comp.tasksLoaded, h.comp.loading = true, true
	return func() tea.Msg { return filesLoadedMsg{paths: walkFiles(root, walkCap), home: true} }
}

// homeFilesLoaded takes the walk back onto home's list and rebuilds the rows
// under the cursor.
func (a *app) homeFilesLoaded(paths []string) {
	h := &a.home
	h.comp.all, h.comp.loaded, h.comp.loading = paths, true, false
	h.comp.rank()
	h.build()
	a.touch()
}

// homeCompleteFile is enter on a row of the list, and it is [app.completeFile]
// said for home's box: the path goes into the sentence after the `@`, or a
// picture comes out of the sentence and onto the tray.
func (a *app) homeCompleteFile(line homeLine) tea.Cmd {
	h := &a.home
	c := &h.comp
	path, _, ok := h.completionWords(line)
	if !ok {
		c.close()
		h.build()
		return nil
	}
	e := &h.box
	if isImagePath(path) {
		head := append([]rune(nil), e.value[:c.at]...)
		tail := append([]rune(nil), e.value[e.cursor:]...)
		e.value = append(head, tail...)
		e.cursor = c.at
		full := path
		if !filepath.IsAbs(full) {
			full = filepath.Join(h.walked, path)
		}
		if a.attach(full) {
			h.say(folderAttachedWord+filepath.Base(path)+homeRidesWord, "")
		}
		h.carrying = len(a.chips) > 0
		c.done = ""
		c.close()
		h.build()
		a.touch()
		return nil
	}
	head := append([]rune(nil), e.value[:c.at+1]...)
	tail := append([]rune(nil), e.value[e.cursor:]...)
	e.value = append(append(head, []rune(path)...), tail...)
	e.cursor = c.at + 1 + len([]rune(path))
	c.done = path
	c.close()
	h.build()
	a.touch()
	return nil
}

// dismissCompletion is esc over the list: it closes, and stays closed over
// exactly this query — the next letter of the token opens it again, which is
// the conversation list's own rule (app.go's [app.dismissLists] seals only the
// command list). [completion.done] is what holds it shut meanwhile.
func (h *homeView) dismissCompletion() {
	h.comp.done = h.comp.query
	h.comp.close()
}
