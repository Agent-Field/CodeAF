package tui3

import (
	"path/filepath"
	"strings"

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
// AND IT OFFERS TEAMS AND CONVERSATIONS, with the same prefix words on its first
// row and the same `@team:`, `@chat:` and `@file:` prefixes a conversation's
// box takes (mention.go). Until 2026-10-01 home's list was files alone, and a
// person who had learnt `@chat:` in a conversation found it answered `no file
// matches` on home: one search behaves one way on every box. The catalogs are
// the same memory the conversation's list copies ([app.fillHomeMentions]), the
// recent list is refreshed on openings exactly as there, and choosing a
// team or a conversation types exactly what it types there ([completeTeamIn],
// [completeChatIn]). The one difference is which conversation is left off: a
// conversation's list leaves off the one being typed in, and home's leaves off
// none, because its sentence opens a new one.
//
// A PICTURE CHOSEN HERE GOES ON HOME'S TRAY, the way one chosen in a
// conversation goes on that conversation's ([app.completeFile]): the half-typed
// token comes out of the sentence and the chip rides into the conversation that
// opens next ([homeView.carrying]).

// homeCompletion is one row of that list a person can stand on: a team, a
// conversation or a path. It is numbered outside the [homeRowKind] iota block
// for [homeCommand]'s reason.
const homeCompletion homeRowKind = 251

// homeCompletionRule is a line of that list that NAMES rows rather than being
// one: the prefix words on its first row, a section's heading, or the one dim
// sentence saying nothing matched. It is never a cursor stop and a press on
// it does nothing, exactly as a project's heading on this column.
const homeCompletionRule homeRowKind = 252

// homeCompletionHint is the foot while the list is up: the three keys it takes.
const homeCompletionHint = "↑↓ pick · enter put it in · esc back"

// homeLookingWord is the list's one empty state of its own, in the conversation
// list's own words ([completion.rows]): the walk still running. A query nothing
// matches is said by the list itself, in the section's words — `no conversation
// matches` under `@chat:` ([completion.emptyWord]).
const homeLookingWord = "looking…"

// completionLines syncs the list against the box and hands back its lines, or
// nothing while the box holds no `@` token. It is asked after the command
// list, which wins when both could be open (app.go's [app.syncLists] states the
// same law for the conversation's box).
//
// EVERY LINE OF THE LIST IS A LINE OF THE COLUMN, rules included, and the two
// index each other one to one: [homeLine.comp] is the line's place in the
// list, and the list's own cursor ([completion.selLine]) is where home's
// cursor opens ([homeView.build]).
func (h *homeView) completionLines() []homeLine {
	h.comp.sync(&h.box)
	if !h.comp.open {
		return nil
	}
	lines := make([]homeLine, 0, len(h.comp.lines))
	for i, line := range h.comp.lines {
		kind := homeCompletion
		if line.filters || line.header != "" {
			kind = homeCompletionRule
		}
		lines = append(lines, homeLine{kind: kind, comp: i})
	}
	return lines
}

// completionCursor is the line of the column home's cursor opens on when the
// list is up: the list's own choice, which rank() moves with the query, and
// the first row when the list has none.
func (h *homeView) completionCursor() int {
	if at := h.comp.selLine(); at >= 0 {
		return at
	}
	return 0
}

// homeCompletionRow paints one line of the list: a rule as the list draws its
// own — the prefix words with the one in force accented, a dim heading, the
// dim empty sentence — and a row as its label and its margin: a team's dot and
// name with how many conversations it holds, a conversation's handle with its
// title, a path with `folder` or `img`.
func (a *app) homeCompletionRow(line homeLine, at, width int, pal palette) string {
	h := &a.home
	c := &h.comp
	if line.comp < 0 || line.comp >= len(c.lines) {
		return ""
	}
	if line.kind == homeCompletionRule {
		cl := c.lines[line.comp]
		if cl.filters {
			return mentionHeadLine(pal, c.scope, "", width)
		}
		return pal.dim("  " + fit(cl.header, width-2))
	}
	label, note, ok := h.completionWords(line, pal)
	if !ok {
		return ""
	}
	return overlayRow(label, note, at == h.cursor, false, at == h.hover && at == h.cursor, width, pal)
}

// completionWords is what a row of the list says — its label and its dim
// margin — and false for a line that no longer points at a row.
func (h *homeView) completionWords(line homeLine, pal palette) (label, note string, ok bool) {
	c := &h.comp
	if line.comp < 0 || line.comp >= len(c.lines) {
		return "", "", false
	}
	cl := c.lines[line.comp]
	switch {
	case cl.team >= 0 && cl.team < len(c.teamHits):
		return mentionTeamLabel(c.teamHits[cl.team], pal), c.lineNote(line.comp), true
	case cl.chat >= 0 && cl.chat < len(c.chatHits):
		return mentionChatLabel(c.chatHits[cl.chat]), c.lineNote(line.comp), true
	case cl.file >= 0 && cl.file < len(c.all):
		return c.all[cl.file], c.lineNote(line.comp), true
	}
	return "", "", false
}

// completionPath is the path a row offers, and false on a row that is not one.
func (h *homeView) completionPath(line homeLine) (string, bool) {
	c := &h.comp
	if line.comp < 0 || line.comp >= len(c.lines) || c.lines[line.comp].file < 0 {
		return "", false
	}
	at := c.lines[line.comp].file
	if at >= len(c.all) {
		return "", false
	}
	return c.all[at], true
}

// fillHomeMentions copies the in-memory catalogs onto home's list, the way
// [app.fillMentions] copies them onto the conversation's. It runs before a key
// or paste reaches home's box and never from a frame, and it leaves no
// conversation off (the file's own note).
func (a *app) fillHomeMentions() {
	a.home.comp.teams = a.mentionTeams()
	a.home.comp.chats = a.mentionChatsExcept("")
	a.home.comp.recentsLoaded = a.mentionRecentsReady(true)
}

// homeCompletionRoot uses the explicit pin, otherwise the conversation's file
// root. Hosted lists offer this machine's files while sentences open on the far one.
func (a *app) homeCompletionRoot() string {
	if pinned := strings.TrimSpace(a.target.where); pinned != "" {
		return pinned
	}
	return a.pathRoot()
}

// prepareHomeCompletion supplies catalogs before an edit can rank them. Keys
// outside a search leave the completed file walk and the mention catalogs alone.
func (a *app) prepareHomeCompletion(text string) {
	h := &a.home
	h.comp.recentsLoaded = a.mentionRecentsReady(true)
	if !h.comp.open {
		if text == "" {
			return
		}
		if !strings.Contains(text, "@") {
			if _, _, ok := atToken(h.box.value, h.box.cursor); !ok {
				return
			}
		}
		// Only an edit that can open a search needs catalogs in advance. Arrows,
		// prose and punctuation after a chosen mention preserve the closed cache.
		value := append(append([]rune(nil), h.box.value[:h.box.cursor]...), []rune(text)...)
		at, query, ok := atToken(value, len(value))
		if !ok {
			return
		}
		if (h.comp.done != "" || h.comp.tokenAt == -(at+1)) && at == h.comp.at && strings.HasPrefix(query, h.comp.done) {
			tail := []rune(strings.TrimPrefix(query, h.comp.done))
			if len(tail) == 0 || !mentionContinuation(tail[0]) {
				return
			}
		}
	}
	a.fillHomeMentions()
	a.alignHomeFiles(a.homeCompletionRoot())
}

// syncHomeCompletion starts the same reads for every opening. Catalogs are
// supplied after nonprinting edits too, before the next frame can draw them.
func (a *app) syncHomeCompletion(_ bool) tea.Cmd {
	h := &a.home
	root := a.homeCompletionRoot()
	var recents tea.Cmd
	opening := h.comp.beginToken(&h.box)
	if opening {
		a.noticeEvent(eventAtOpened)
		recents = a.loadMentionRecents()
		a.fillHomeMentions()
		a.alignHomeFiles(root)
	}
	h.comp.sync(&h.box)
	if opening || h.comp.open {
		h.build()
	}
	return tea.Batch(a.loadHomeFiles(root), recents)
}

// loadHomeFiles walks the completion folder once, off the loop. Catalogs and
// their ranked rows change together, so no frame can paint an old file index.
func (a *app) loadHomeFiles(root string) tea.Cmd {
	h := &a.home
	if !h.comp.open || root == "" {
		return nil
	}
	a.alignHomeFiles(root)
	if h.comp.loaded || h.comp.loading {
		return nil
	}
	// Home offers no task pointers, so there is no task read to wait for.
	h.comp.tasksLoaded, h.comp.loading = true, true
	return func() tea.Msg { return filesLoadedMsg{paths: walkFiles(root, walkCap), home: true, root: root} }
}

// alignHomeFiles replaces paths and ranked rows together. An opening must not
// dismiss a spaced file search against a completed walk of another folder.
func (a *app) alignHomeFiles(root string) {
	h := &a.home
	if h.walked == root {
		return
	}
	h.comp.all, h.comp.loaded, h.comp.loading = nil, false, false
	h.walked = root
	h.comp.rank()
	if h.comp.open {
		h.build()
	}
}

// homeFilesLoaded takes the walk back onto home's list and rebuilds the rows
// under the cursor.
func (a *app) homeFilesLoaded(paths []string, root string) {
	h := &a.home
	if root != "" && root != h.walked {
		return
	}
	h.comp.all, h.comp.loaded, h.comp.loading = paths, true, false
	h.comp.rank()
	h.build()
	a.touch()
}

// homeComplete is enter on a row of the list, and it is [app.completeFile],
// [app.completeTeam] and [app.completeChat] said for home's box: a team's mark
// or a conversation's handle goes into the sentence exactly as it does there,
// a path goes in after the `@`, or a picture comes out of the sentence and
// onto the tray.
func (a *app) homeComplete(line homeLine) tea.Cmd {
	h := &a.home
	c := &h.comp
	if line.comp >= 0 && line.comp < len(c.lines) {
		cl := c.lines[line.comp]
		switch {
		case cl.team >= 0 && cl.team < len(c.teamHits):
			completeTeamIn(&h.box, c, c.teamHits[cl.team])
			h.build()
			a.touch()
			return nil
		case cl.chat >= 0 && cl.chat < len(c.chatHits):
			completeChatIn(&h.box, c, c.chatHits[cl.chat])
			h.build()
			a.touch()
			return nil
		}
	}
	path, ok := h.completionPath(line)
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
		h.carrying = len(a.home.chips) > 0
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
// command list). The dismissal ends the opening, so its next letter reads anew.
// [completion.done] also keeps a separator after this query from reopening it.
func (h *homeView) dismissCompletion() {
	h.comp.done = h.comp.query
	h.comp.dismiss()
}
